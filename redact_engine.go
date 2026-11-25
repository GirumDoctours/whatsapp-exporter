package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/anthropics/anthropic-sdk-go/v2"
)

const (
	chunkLines       = 100
	chunkChars       = 22000
	castExcerptHead  = 80
	castExcerptTail  = 40
	highConfidence   = 0.8
	propagateMinConf = 0.8
	pass1MaxTokens   = 16000
	pass2MaxTokens   = 12000
	castMaxTokens    = 12000
	minSplitLines    = 8
)

type chatInput struct {
	Folder  string
	Title   string
	IsGroup bool
	Text    string
}

type chatStats struct {
	Slots          int
	High           int
	Flagged        int
	Disagreements  int
	LeaksCaught    int
	Rebuilt        int
	WholeBody      int
	Fallback       int
	Renumbered     int
	AIChunks       int
	AIFailedChunks int
	Unlocated      int
}

type chatOutput struct {
	Folder     string
	Title      string
	TitleText  string
	Text       string
	Slots      []slot
	Lines      []chatLine
	SenderTags map[string]string
	Roles      map[string]string
	Stats      chatStats
	AIUsed     bool
}

type redactor struct {
	ai         aiClient
	cache      *aiCache
	dict       *tagDictionary
	book       *peopleBook
	detMu      sync.Mutex
	detector   *detector
	detVersion int64
	aiOff      atomic.Bool
	aiReason   atomic.Value
	skipRefine bool
}

func newRedactor(ai aiClient, cache *aiCache, dict *tagDictionary, book *peopleBook) *redactor {
	return &redactor{ai: ai, cache: cache, dict: dict, book: book, detVersion: -1}
}

func (r *redactor) det() *detector {
	version := r.book.version.Load()
	r.detMu.Lock()
	defer r.detMu.Unlock()
	if r.detector == nil || r.detVersion != version {
		r.detector = newDetector(r.book)
		r.detVersion = version
	}
	return r.detector
}

func (r *redactor) aiAvailable() bool {
	return r.ai != nil && !r.aiOff.Load()
}

func (r *redactor) stopAI(err error) {
	if r.aiOff.CompareAndSwap(false, true) {
		r.aiReason.Store(err.Error())
		logRedact.Errorf("Turning the AI check off for the rest of the run: %v", err)
	}
}

func (r *redactor) aiStopReason() string {
	if v, ok := r.aiReason.Load().(string); ok {
		return v
	}
	return ""
}

func fatalForRun(err error) bool {
	status, _ := apiStatus(err)
	return status == 401 || status == 403 || (status == 400 && strings.Contains(strings.ToLower(err.Error()), "credit"))
}

type item struct {
	ID        string
	Line      int
	Det       detection
	Masked    bool
	Tag       string
	TagConf   float64
	Keep      bool
	Decided   bool
	PersonRef string
}

type chatJob struct {
	r          *redactor
	in         chatInput
	doc        chatDoc
	senderRefs map[string]string
	labelPhone map[string]string
	refOrder   []string
	refLabel   map[string]string
	personRefs map[string]string
	refPerson  map[string]string
	roles      map[string]string
	chatPeople map[string]bool
	ownerRef   string
	partnerRef string
	pCount     int
	titleLabel string
	nameWords  map[string]bool
	items      []*item
	byLine     map[int][]*item
	slots      []slot
	stats      chatStats
	aiUsed     bool
	mu         sync.Mutex
}

func (r *redactor) redactChat(ctx context.Context, in chatInput) chatOutput {
	started := time.Now()
	labels := append(r.book.allLabels(), in.Title)
	j := &chatJob{r: r, in: in, doc: parseChat(in.Text, labels), roles: map[string]string{}}
	j.indexPeople()
	j.detect()
	logRedact.Debugf("Chat %q: %d lines, %d senders, %d local findings", in.Folder, len(j.doc.Lines), len(j.senderRefs), len(j.items))
	if r.aiAvailable() {
		if err := j.runAI(ctx); err != nil {
			logRedact.Warnf("Chat %q: AI check failed, using local checks only: %v", in.Folder, err)
		} else {
			j.aiUsed = true
		}
	}
	defer func() {
		logRedact.Infof("Chat %q redacted in %s: %d slots, %d flagged, %d fallback, %d rebuilt, AI %v, roles %v", in.Folder, time.Since(started).Round(time.Millisecond), j.stats.Slots, j.stats.Flagged, j.stats.Fallback, j.stats.Rebuilt, j.aiUsed, j.roles)
	}()
	j.localDefaults()
	j.buildSlots()
	if j.aiUsed {
		j.propagate(ctx)
		j.check(ctx)
		j.propagate(ctx)
		j.resolveRoles(ctx)
	}
	j.mergeAddresses()
	j.dropWrongNames()
	base := j.stats
	out := j.finish()
	if r.skipRefine {
		return out
	}
	refined, ok := j.refine(out)
	if !ok {
		return out
	}
	j.slots = refined
	j.dropWrongNames()
	j.stats = base
	final := j.finish()
	if gap, missing := uncovered(out.Slots, final.Slots); missing {
		logRedact.Warnf("Chat %q: final local step lost a replacement on line %d, keeping the earlier result", in.Folder, gap.Line+1)
		return out
	}
	return final
}

func (j *chatJob) refine(out chatOutput) (refined []slot, ok bool) {
	started := time.Now()
	defer func() {
		if p := recover(); p != nil {
			logRedact.Warnf("Chat %q: final local step failed, keeping the earlier result: %v", j.in.Folder, p)
			refined, ok = nil, false
		}
	}()
	refined = refineSlots(refineInput{
		Folder:     j.in.Folder,
		Title:      j.in.Title,
		IsGroup:    j.in.IsGroup,
		Text:       j.in.Text,
		Slots:      append([]slot(nil), out.Slots...),
		SenderTags: out.SenderTags,
		TitleText:  out.TitleText,
		Roles:      out.Roles,
		doc:        &j.doc,
	})
	logRedact.Debugf("Chat %q: final local step took %s, slots %d -> %d", j.in.Folder, time.Since(started).Round(time.Millisecond), len(out.Slots), len(refined))
	return refined, true
}

func (j *chatJob) indexPeople() {
	j.senderRefs = map[string]string{}
	j.labelPhone = map[string]string{}
	j.refLabel = map[string]string{}
	j.personRefs = map[string]string{}
	j.refPerson = map[string]string{}
	j.chatPeople = map[string]bool{}
	if len(j.doc.Lines) > 0 && j.doc.Lines[0].HasSender {
		j.titleLabel = j.doc.Lines[0].Sender
	}
	if j.in.Title != "" {
		j.titleLabel = j.in.Title
	}
	for _, label := range j.doc.senders() {
		if j.in.IsGroup && label == j.titleLabel {
			continue
		}
		ref := "S" + strconv.Itoa(len(j.refOrder)+1)
		j.senderRefs[label] = ref
		j.refLabel[ref] = label
		j.refOrder = append(j.refOrder, ref)
		if strings.HasPrefix(label, "+") {
			if digits := onlyDigits(label); len(digits) >= 8 {
				j.labelPhone[digits] = ref
			}
		}
		key := j.r.book.keyForLabel(label)
		if key == "" && !strings.HasPrefix(label, "+") && hasLetter(label) {
			key = "label:" + label
			j.r.book.addLabel(key, label)
		}
		if key != "" {
			j.chatPeople[key] = true
			j.refPerson[ref] = key
			if _, ok := j.personRefs[key]; !ok {
				j.personRefs[key] = ref
			}
			if key == j.r.book.OwnerKey {
				j.ownerRef = ref
			}
		}
		if !j.in.IsGroup && label == j.titleLabel {
			j.partnerRef = ref
		}
	}
	if j.ownerRef == "" {
		owner := j.r.book.labelsOf(j.r.book.OwnerKey)
		for _, ref := range j.refOrder {
			if containsString(owner, j.refLabel[ref]) {
				j.ownerRef = ref
				break
			}
		}
	}
}

func (j *chatJob) personRef(key string) string {
	if key == "" {
		return ""
	}
	if ref, ok := j.personRefs[key]; ok {
		return ref
	}
	j.pCount++
	ref := "P" + strconv.Itoa(j.pCount)
	j.personRefs[key] = ref
	j.refPerson[ref] = key
	j.refOrder = append(j.refOrder, ref)
	return ref
}

func (j *chatJob) detect() {
	j.byLine = map[int][]*item{}
	masked, cands := 0, 0
	det := j.r.det()
	addItems := func(line int, text string) {
		for _, d := range det.detect(text, j.chatPeople) {
			it := &item{Line: line, Det: d, Masked: d.Confidence >= maskThreshold}
			if it.Masked {
				masked++
				it.ID = "m" + strconv.Itoa(masked)
			} else {
				cands++
				it.ID = "c" + strconv.Itoa(cands)
			}
			if d.Person != "" {
				it.PersonRef = j.personRef(d.Person)
			} else if d.Category == catPhone {
				it.PersonRef = j.senderForPhone(onlyDigits(d.Value))
			}
			j.items = append(j.items, it)
			j.byLine[line] = append(j.byLine[line], it)
		}
	}
	if j.in.IsGroup && j.titleLabel != "" {
		addItems(-1, j.titleLabel)
	}
	for i, l := range j.doc.Lines {
		if l.Body != "" {
			addItems(i, l.Body)
		}
	}
}

func (j *chatJob) senderForPhone(digits string) string {
	local := strings.TrimLeft(digits, "0")
	for phone, ref := range j.labelPhone {
		if phone == digits || (len(local) >= 8 && (strings.HasSuffix(phone, local) || strings.HasSuffix(local, phone))) {
			return ref
		}
	}
	return ""
}

func (j *chatJob) maskedBody(line int, body string) string {
	items := j.byLine[line]
	var b strings.Builder
	prev := 0
	for _, it := range items {
		if !it.Masked {
			continue
		}
		b.WriteString(body[prev:it.Det.Start])
		marker := it.ID + ":" + it.Det.Category
		if it.PersonRef != "" && it.Det.Category == catName {
			marker += ":" + it.PersonRef + ":" + it.Det.Hint
		} else if it.PersonRef != "" {
			marker += ":" + it.PersonRef
		}
		b.WriteString("⟦" + marker + "⟧")
		prev = it.Det.End
	}
	b.WriteString(body[prev:])
	return b.String()
}

var noticePattern = regexp.MustCompile(`^\x{200e}?(?:.+ )?(?:joined using this group's invite link|joined using this community's invite link|added |removed |left$|changed the subject|changed this group's|changed the group|created group|created this group|changed their phone number|is now an admin|are now an admin|no longer an admin|Messages and calls are end-to-end encrypted|deleted this message|This message was deleted|Waiting for this message|security code|turned on disappearing|turned off disappearing|changed the settings|pinned a message|started a call|missed (?:voice|video) call|joined from the community|was added|requested to join|reset this group's invite link)`)

func isNotice(body string) bool {
	return noticePattern.MatchString(body)
}

func (j *chatJob) senderDisplay(l chatLine) string {
	if !l.HasSender {
		return "(system)"
	}
	if ref, ok := j.senderRefs[l.Sender]; ok {
		return ref
	}
	if l.Sender == j.titleLabel {
		if j.in.IsGroup {
			return "(group)"
		}
		return j.partnerRef
	}
	return "(unknown)"
}

func (j *chatJob) renderLines(start, end int) string {
	var b strings.Builder
	if start == 0 && j.in.IsGroup && j.titleLabel != "" {
		fmt.Fprintf(&b, "#0 (group name): %s\n", j.maskedBody(-1, j.titleLabel))
	}
	for i := start; i < end; i++ {
		l := j.doc.Lines[i]
		sender := j.senderDisplay(l)
		if isNotice(l.Body) {
			sender = "(WhatsApp notice)"
		}
		fmt.Fprintf(&b, "#%d %s: %s\n", i+1, sender, j.maskedBody(i, l.Body))
	}
	return b.String()
}

func (j *chatJob) castTable() string {
	var b strings.Builder
	b.WriteString("CAST\n")
	for _, ref := range j.refOrder {
		role := j.roles[ref]
		note := ""
		if ref == j.ownerRef {
			note = " (the owner, who exported the chats)"
		}
		if ref == j.partnerRef {
			note = " (the other person in this one-to-one chat)"
		}
		if role == "" {
			role = "unknown"
		}
		fmt.Fprintf(&b, "%s = %s%s\n", ref, role, note)
	}
	return b.String()
}

func (j *chatJob) chatKind() string {
	if j.in.IsGroup {
		return "Group chat."
	}
	return "One-to-one chat."
}

func (j *chatJob) runAI(ctx context.Context) error {
	if err := j.cast(ctx); err != nil {
		return err
	}
	ranges := chunkRanges(j.doc.Lines, chunkLines, chunkChars)
	if len(ranges) == 0 && j.in.IsGroup {
		ranges = []lineRange{{0, 0}}
	}
	var wg sync.WaitGroup
	errs := make([]error, len(ranges))
	for i, rg := range ranges {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = j.pass1Range(ctx, rg)
		}()
	}
	wg.Wait()
	failed := 0
	for _, err := range errs {
		if err != nil {
			failed++
		}
	}
	j.stats.AIChunks += len(ranges)
	j.stats.AIFailedChunks += failed
	if failed == len(ranges) && failed > 0 {
		return errs[0]
	}
	j.harmonizeRoles()
	return nil
}

var selfAttributes = setOf(strings.Fields("AGE BIRTH_DATE BIRTH_YEAR BIRTHDAY HOME_ADDRESS STREET_ADDRESS POSTCODE HOME_CITY HOME_COUNTRY NATIONALITY HOME_AIRPORT PASSPORT_NUMBER NATIONAL_ID_NUMBER DRIVER_LICENSE_NUMBER VISA_NUMBER EMAIL INSTAGRAM HANDLE FACEBOOK TIKTOK X_HANDLE LINKEDIN SNAPCHAT TELEGRAM EMPLOYER JOB_TITLE"))

func genericRole(role string) bool {
	name, _ := splitNumber(role)
	return name == "OWNER" || name == "CONTACT" || name == "GROUP_MEMBER" || name == "PERSON" || name == ""
}

func (j *chatJob) harmonizeRoles() {
	votes := map[string]map[string]float64{}
	vote := func(ref, role string, weight float64) {
		if ref == "" || genericRole(role) {
			return
		}
		if votes[ref] == nil {
			votes[ref] = map[string]float64{}
		}
		votes[ref][role] += weight
	}
	senderRef := func(line int) string {
		if line < 0 || line >= len(j.doc.Lines) {
			return ""
		}
		l := j.doc.Lines[line]
		if ref, ok := j.senderRefs[l.Sender]; ok {
			return ref
		}
		if !j.in.IsGroup && l.Sender == j.titleLabel {
			return j.partnerRef
		}
		return ""
	}
	consider := func(line int, tag string, personRef string) {
		p, ok := j.r.dict.parse(tag)
		if !ok || p.Kind != tagPerson || p.Fallback {
			return
		}
		switch {
		case personRef != "" && p.Relation == "":
			vote(personRef, p.role(), 2)
		case p.Relation == "" && selfAttributes[p.Attribute]:
			vote(senderRef(line), p.role(), 1)
		case p.Relation != "" && !j.in.IsGroup:
			vote(j.partnerRef, p.Base, 0.5)
		}
	}
	j.mu.Lock()
	for _, s := range j.slots {
		consider(s.Line, s.Tag, "")
	}
	for _, it := range j.items {
		if it.Decided && it.Tag != "" {
			consider(it.Line, it.Tag, it.PersonRef)
		}
	}
	j.mu.Unlock()
	used := map[string]bool{}
	for _, role := range j.roles {
		used[role] = true
	}
	for _, ref := range j.refOrder {
		if !genericRole(j.roles[ref]) || ref == j.ownerRef || ref == j.partnerRef {
			continue
		}
		best, score := "", 0.0
		for role, w := range votes[ref] {
			if w > score || (w == score && role < best) {
				best, score = role, w
			}
		}
		if best != "" && score >= 1.5 && !used[best] {
			logRedact.Debugf("Chat %q: %s role %s -> %s from pass 1 tags", j.in.Folder, ref, firstNonEmpty(j.roles[ref], "none"), best)
			j.roles[ref] = best
			used[best] = true
		}
	}
}

func (j *chatJob) generate(ctx context.Context, stage, system, prompt string, schema map[string]any, maxTokens int64, out any) error {
	if !j.r.aiAvailable() {
		return errors.New("AI is off")
	}
	effort := anthropic.OutputConfigEffortMedium
	key := cacheKey(stage, promptVersion, string(effort), system, prompt)
	err := cachedGenerate(ctx, j.r.ai, j.r.cache, key, aiRequest{System: system, Prompt: prompt, Schema: schema, MaxTokens: maxTokens, Effort: effort}, out)
	if err != nil && fatalForRun(err) {
		j.r.stopAI(err)
	}
	return err
}

func (j *chatJob) cast(ctx context.Context) error {
	var b strings.Builder
	b.WriteString(j.chatKind() + "\n")
	b.WriteString("References:\n")
	counts := map[string]int{}
	for _, l := range j.doc.Lines {
		if ref, ok := j.senderRefs[l.Sender]; ok {
			counts[ref]++
		}
	}
	for _, ref := range j.refOrder {
		kind := "mentioned contact"
		if strings.HasPrefix(ref, "S") {
			kind = fmt.Sprintf("sender of %d messages", counts[ref])
			if strings.HasPrefix(j.refLabel[ref], "+") {
				kind += ", saved only as a phone number"
			}
		}
		if ref == j.ownerRef {
			kind += ", the owner who exported the chats"
		}
		if ref == j.partnerRef {
			kind += ", the other person in this one-to-one chat"
		}
		fmt.Fprintf(&b, "%s: %s\n", ref, kind)
	}
	if j.ownerRef == "" {
		b.WriteString("The owner does not write in this chat: never use OWNER for anyone here (OWNER_<RELATION> is still allowed for the owner's family).\n")
	}
	if j.in.IsGroup {
		b.WriteString("This is a group: never use CONTACT here.\n")
	}
	n := len(j.doc.Lines)
	b.WriteString("\nExcerpt:\n")
	if n <= castExcerptHead+castExcerptTail {
		b.WriteString(j.renderLines(0, n))
	} else {
		b.WriteString(j.renderLines(0, castExcerptHead))
		b.WriteString("…\n")
		b.WriteString(j.renderLines(n-castExcerptTail, n))
	}
	var mentions strings.Builder
	for _, it := range j.items {
		if it.PersonRef != "" && strings.HasPrefix(it.PersonRef, "P") && it.Line >= 0 && !strings.Contains(mentions.String(), "#"+strconv.Itoa(it.Line+1)+" ") {
			if strings.Count(mentions.String(), "\n") > 60 {
				break
			}
			l := j.doc.Lines[it.Line]
			fmt.Fprintf(&mentions, "#%d %s: %s\n", it.Line+1, j.senderDisplay(l), j.maskedBody(it.Line, l.Body))
		}
	}
	if mentions.Len() > 0 {
		b.WriteString("\nLines that mention P references:\n" + mentions.String())
	}
	var reply castReply
	if err := j.generate(ctx, "cast", castSystemPrompt+j.r.dict.promptText(), b.String(), castSchema(), castMaxTokens, &reply); err != nil {
		return err
	}
	used := map[string]bool{}
	for _, cr := range reply.Roles {
		ref := strings.TrimSpace(cr.Ref)
		if _, known := j.refPerson[ref]; !known && j.refLabel[ref] == "" {
			continue
		}
		role := j.validRole(strings.ToUpper(strings.TrimSpace(cr.Role)))
		if role == "" || !j.roleAllowed(ref, role) {
			continue
		}
		role = uniqueRole(role, used, j.r.dict)
		j.roles[ref] = role
	}
	logRedact.Debugf("Chat %q cast: %v", j.in.Folder, j.roles)
	return nil
}

func (j *chatJob) validRole(role string) string {
	name, n := splitNumber(role)
	if _, _, _, ok := j.r.dict.role(name); !ok {
		return ""
	}
	if n >= 2 {
		return name + "_" + strconv.Itoa(n)
	}
	return name
}

func (j *chatJob) roleAllowed(ref, role string) bool {
	name, _ := splitNumber(role)
	switch name {
	case "OWNER":
		return ref == j.ownerRef
	case "CONTACT":
		return !j.in.IsGroup && ref == j.partnerRef
	}
	if strings.HasPrefix(name, "CONTACT_") && j.in.IsGroup {
		return false
	}
	return true
}

func (j *chatJob) guardTag(tag, value string, line int) string {
	p, ok := j.r.dict.parse(tag)
	if !ok || p.Kind != tagPerson {
		return tag
	}
	switch {
	case p.Base == "OWNER" && p.Relation == "":
		if j.r.book.ownerValues()[normalizeValue(value)] {
			return p.withNumber(0).String()
		}
		if identityAttributes[p.Attribute] && j.ownerNameVariant(value) {
			return p.withNumber(0).String()
		}
		return roleTag("PERSON", p.Attribute)
	case p.Base == "CONTACT" && (j.in.IsGroup || j.partnerRef == ""):
		return roleTag("PERSON", p.Attribute)
	}
	return tag
}

func (j *chatJob) ownerNameVariant(value string) bool {
	v := normalizeValue(value)
	if utf8.RuneCountInString(v) < 3 {
		return false
	}
	for known := range j.r.book.ownerValues() {
		if !hasLetter(known) || utf8.RuneCountInString(known) < 3 {
			continue
		}
		if editDistance(v, known) <= 2 || strings.HasPrefix(v, known) || strings.HasPrefix(known, v) {
			return true
		}
	}
	return false
}

func implausibleName(value string) bool {
	v := strings.TrimSpace(value)
	if utf8.RuneCountInString(v) < 2 || romanNumeral.MatchString(v) || !hasLetter(v) {
		return true
	}
	return !hasUpper(v) && isCommonWord(strings.ToLower(v))
}

var romanNumeral = regexp.MustCompile(`^(?i)[ivxlcdm]+\.?$`)

func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

var pronounAfter = regexp.MustCompile(`^\s+(?i:you|u|i|we|they|he|she|it|ya|someone|anyone|this|that|the|a|do|be|have|get|go|send|see|help|come|call|make|check|please)\b`)

func (j *chatJob) ordinaryWordUse(it *item) bool {
	if !isCommonWord(it.Det.Value) || it.Line < 0 || it.Line >= len(j.doc.Lines) {
		return false
	}
	return pronounAfter.MatchString(j.doc.Lines[it.Line].Body[it.Det.End:])
}

func isNameTag(tag string, d *tagDictionary) bool {
	p, ok := d.parse(tag)
	return ok && identityAttributes[p.Attribute] && p.Kind == tagPerson
}

func uniqueRole(role string, used map[string]bool, d *tagDictionary) string {
	if !used[role] {
		used[role] = true
		return role
	}
	name, n := splitNumber(role)
	for k := max(n, 1) + 1; ; k++ {
		candidate := name + "_" + strconv.Itoa(k)
		if !used[candidate] {
			used[candidate] = true
			return candidate
		}
	}
}

func (j *chatJob) pass1Range(ctx context.Context, rg lineRange) error {
	err := j.pass1(ctx, rg)
	if err != nil {
		logRedact.Warnf("Chat %q lines %d-%d: first pass failed: %v", j.in.Folder, rg.start+1, rg.end, err)
	}
	if errors.Is(err, errAITruncated) && rg.end-rg.start >= 2*minSplitLines {
		mid := (rg.start + rg.end) / 2
		errA := j.pass1Range(ctx, lineRange{rg.start, mid})
		errB := j.pass1Range(ctx, lineRange{mid, rg.end})
		return errors.Join(errA, errB)
	}
	return err
}

func (j *chatJob) itemsIn(rg lineRange) []*item {
	var out []*item
	for _, it := range j.items {
		if (it.Line >= rg.start && it.Line < rg.end) || (it.Line == -1 && rg.start == 0) {
			out = append(out, it)
		}
	}
	return out
}

func (j *chatJob) pass1(ctx context.Context, rg lineRange) error {
	var b strings.Builder
	b.WriteString(j.chatKind() + "\n" + j.castTable() + "\n")
	if rg.start > 0 {
		from := max(0, rg.start-5)
		b.WriteString("Earlier lines, for context only:\n" + j.renderLines(from, rg.start) + "\n")
	}
	b.WriteString("CHUNK\n" + j.renderLines(rg.start, rg.end))
	items := j.itemsIn(rg)
	var cands strings.Builder
	for _, it := range items {
		if !it.Masked {
			fmt.Fprintf(&cands, "%s (line %d): %q, possibly %s\n", it.ID, it.Line+1, it.Det.Value, it.Det.Category)
		}
	}
	if cands.Len() > 0 {
		b.WriteString("\nCANDIDATES\n" + cands.String())
	}
	var reply pass1Reply
	if err := j.generate(ctx, "pass1", pass1SystemPrompt+j.r.dict.promptText(), b.String(), pass1Schema(), pass1MaxTokens, &reply); err != nil {
		return err
	}
	byID := map[string]*item{}
	for _, it := range items {
		byID[it.ID] = it
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, m := range reply.Masked {
		if it, ok := byID[strings.TrimSpace(m.ID)]; ok && it.Masked {
			it.Tag, it.TagConf, it.Decided = j.guardTag(normalizeTag(m.Tag), it.Det.Value, it.Line), m.Confidence, true
		}
	}
	for _, c := range reply.Candidates {
		if it, ok := byID[strings.TrimSpace(c.ID)]; ok && !it.Masked {
			it.Keep, it.Tag, it.TagConf, it.Decided = c.Keep, j.guardTag(normalizeTag(c.Tag), it.Det.Value, it.Line), c.Confidence, true
			if it.Keep && it.Det.Category == catName && (implausibleName(it.Det.Value) || j.ordinaryWordUse(it)) {
				it.Keep = false
			}
		}
	}
	for _, f := range reply.Found {
		j.addFound(f, rg, "ai", 0)
	}
	return nil
}

func normalizeTag(tag string) string {
	tag = strings.ToUpper(strings.TrimSpace(tag))
	tag = strings.Trim(tag, "{} ")
	if tag == "" {
		return ""
	}
	return "{{" + tag + "}}"
}

var markerPattern = regexp.MustCompile(`⟦[^⟧]*⟧|\{\{[A-Z0-9_]+\}\}`)

func (j *chatJob) addFound(f foundTag, rg lineRange, source string, minConf float64) bool {
	text := f.Text
	if text == "" || markerPattern.MatchString(text) || f.Confidence < minConf {
		j.stats.Unlocated++
		return false
	}
	tag := normalizeTag(f.Tag)
	for _, line := range []int{f.Line - 1, f.Line - 2, f.Line} {
		if f.Line == 0 && j.in.IsGroup {
			line = -1
		}
		if line != -1 && (line < rg.start || line >= rg.end || line >= len(j.doc.Lines)) {
			continue
		}
		body := j.titleLabel
		if line >= 0 {
			body = j.doc.Lines[line].Body
		}
		lineTag := j.guardTag(tag, text, line)
		if isNameTag(lineTag, j.r.dict) && implausibleName(text) {
			j.stats.Unlocated++
			return false
		}
		if line >= 0 && isNameTag(lineTag, j.r.dict) && isCommonWord(text) {
			if i := strings.Index(body, text); i >= 0 && pronounAfter.MatchString(body[i+len(text):]) {
				j.stats.Unlocated++
				return false
			}
		}
		if line >= 0 && isNotice(body) && !hasUpper(text) && !strings.ContainsAny(text, "0123456789@") {
			j.stats.Unlocated++
			return false
		}
		spans := findAll(body, text)
		added := false
		for _, sp := range spans {
			if j.overlapsLocked(line, sp[0], sp[1]) || !boundaryOK(body, sp[0], sp[1]) {
				continue
			}
			j.slots = append(j.slots, slot{Line: line, Start: sp[0], End: sp[1], Tag: lineTag, Value: body[sp[0]:sp[1]], Category: "AI", Source: source, Confidence: clamp(f.Confidence)})
			added = true
		}
		if added {
			return true
		}
		if line == -1 {
			break
		}
	}
	j.stats.Unlocated++
	return false
}

func clamp(v float64) float64 {
	return min(1, max(0, v))
}

func findAll(body, text string) [][2]int {
	var out [][2]int
	for offset := 0; offset < len(body); {
		i := strings.Index(body[offset:], text)
		if i < 0 {
			break
		}
		start := offset + i
		out = append(out, [2]int{start, start + len(text)})
		offset = start + len(text)
	}
	if len(out) > 0 {
		return out
	}
	re, err := regexp.Compile(`(?i)` + regexp.QuoteMeta(text))
	if err != nil {
		return nil
	}
	for _, m := range re.FindAllStringIndex(body, -1) {
		out = append(out, [2]int{m[0], m[1]})
	}
	return out
}

func (j *chatJob) overlapsLocked(line, start, end int) bool {
	for _, s := range j.slots {
		if s.Line == line && start < s.End && s.Start < end {
			return true
		}
	}
	for _, it := range j.byLine[line] {
		if (it.Masked || it.Keep) && start < it.Det.End && it.Det.Start < end {
			return true
		}
	}
	return false
}

func (j *chatJob) roleFor(ref string) string {
	if role := j.roles[ref]; role != "" {
		return role
	}
	switch {
	case ref == "":
		return "PERSON"
	case ref == j.ownerRef:
		return "OWNER"
	case ref == j.partnerRef:
		return "CONTACT"
	case j.in.IsGroup:
		return "GROUP_MEMBER"
	}
	return "PERSON"
}

func (j *chatJob) fillMissingRoles() {
	used := map[string]bool{}
	for _, ref := range j.refOrder {
		if role := j.roles[ref]; role != "" {
			used[role] = true
		}
	}
	for _, ref := range j.refOrder {
		if j.roles[ref] == "" {
			j.roles[ref] = uniqueRole(j.roleFor(ref), used, j.r.dict)
		}
	}
}

func (j *chatJob) senderRole(line int) string {
	if line < 0 || line >= len(j.doc.Lines) {
		return "PERSON"
	}
	l := j.doc.Lines[line]
	if ref, ok := j.senderRefs[l.Sender]; ok {
		return j.roles[ref]
	}
	if !j.in.IsGroup && l.Sender == j.titleLabel && j.partnerRef != "" {
		return j.roles[j.partnerRef]
	}
	return "PERSON"
}

func idAttribute(hint string) string {
	switch {
	case strings.Contains(hint, "pass") || strings.Contains(hint, "pasap") || strings.Contains(hint, "reisepass"):
		return "PASSPORT_NUMBER"
	case strings.Contains(hint, "kimlik") || strings.Contains(hint, "national") || strings.Contains(hint, "tc") || strings.Contains(hint, "identity") || strings.Contains(hint, "id card"):
		return "NATIONAL_ID_NUMBER"
	case strings.Contains(hint, "licen") || strings.Contains(hint, "ehliyet"):
		return "DRIVER_LICENSE_NUMBER"
	case strings.Contains(hint, "ssn") || strings.Contains(hint, "social"):
		return "SOCIAL_SECURITY_NUMBER"
	case strings.Contains(hint, "nhs") || strings.Contains(hint, "insurance"):
		return "HEALTH_INSURANCE_NUMBER"
	case strings.Contains(hint, "policy"):
		return "INSURANCE_POLICY_NUMBER"
	case strings.Contains(hint, "visa"):
		return "VISA_NUMBER"
	case strings.Contains(hint, "residence") || strings.Contains(hint, "ikamet"):
		return "RESIDENCE_PERMIT_NUMBER"
	case strings.Contains(hint, "medical"):
		return "MEDICAL_RECORD_NUMBER"
	case strings.Contains(hint, "patient"):
		return "CLINIC_PATIENT_ID"
	case strings.Contains(hint, "booking") || strings.Contains(hint, "reservation") || strings.Contains(hint, "pnr"):
		return ""
	}
	return ""
}

func (j *chatJob) defaultTag(it *item) string {
	d := it.Det
	owner := j.senderRole(it.Line)
	person := "PERSON"
	if it.PersonRef != "" {
		person = j.roles[it.PersonRef]
	}
	switch d.Category {
	case catName:
		if it.PersonRef == "" {
			return roleTag("PERSON", firstNonEmpty(d.Hint, "FIRST_NAME"))
		}
		return roleTag(person, firstNonEmpty(d.Hint, "FIRST_NAME"))
	case catPhone:
		if it.PersonRef != "" {
			return roleTag(person, "PHONE")
		}
		return "{{PHONE_NUMBER}}"
	case catEmail:
		return "{{EMAIL_ADDRESS}}"
	case catHandle:
		return roleTag("PERSON", "HANDLE")
	case catProfile:
		return "{{PERSONAL_PROFILE_LINK}}"
	case catInvite:
		return "{{GROUP_INVITE_LINK}}"
	case catMaps:
		return "{{MAPS_LINK}}"
	case catLocation:
		return roleTag(owner, "SHARED_LOCATION")
	case catPayment, catMeeting, catCalendar, catForm, catDocument, catPhotos:
		return "{{" + d.Category + "}}"
	case catLink:
		return "{{LINK}}"
	case catEventLink, catVideoLink, catPostLink:
		return "{{" + d.Category + "}}"
	case catDomain:
		return "{{COMPANY_WEBSITE}}"
	case catWhatsAppID:
		return roleTag(person, "WHATSAPP_ID")
	case catCard, catCVV, catIBAN, catAge, catBirthDate, catBirthYear:
		return roleTag(owner, d.Category)
	case catID:
		if strings.Contains(d.Hint, "booking") || strings.Contains(d.Hint, "reservation") || strings.Contains(d.Hint, "pnr") {
			return "{{BOOKING_REF}}"
		}
		if attr := idAttribute(d.Hint); attr != "" {
			return roleTag(owner, attr)
		}
		return "{{ID_NUMBER}}"
	case catPostcode:
		return roleTag(owner, "POSTCODE")
	case catAddress:
		return roleTag(owner, "HOME_ADDRESS")
	}
	return "{{OTHER_PII}}"
}

func (j *chatJob) localDefaults() {
	j.fillMissingRoles()
	for _, it := range j.items {
		knownPerson := it.PersonRef != "" && (it.Det.Category == catName || it.Det.Category == catPhone)
		if !it.Masked {
			if !it.Decided {
				it.Keep = it.Det.Confidence >= 0.5 && it.Det.Category != catLink
			}
			if !it.Keep {
				continue
			}
		}
		switch {
		case knownPerson:
			it.Tag = j.defaultTag(it)
		case it.Tag == "":
			it.Tag = j.defaultTag(it)
		case !j.r.dict.valid(it.Tag) && it.Masked:
			it.Tag = j.defaultTag(it)
		case !j.r.dict.valid(it.Tag):
			it.Tag = j.r.dict.fallbackFor(it.Tag)
		}
		if it.TagConf == 0 {
			it.TagConf = it.Det.Confidence
		}
	}
}

func (j *chatJob) buildSlots() {
	if j.in.IsGroup && strings.TrimSpace(j.titleLabel) != "" {
		j.slots = append(j.slots, slot{Line: -1, Start: 0, End: len(j.titleLabel), Tag: "{{GROUP_NAME}}", Value: j.titleLabel, Category: "GROUP", Source: "rule", Confidence: 0.99})
	}
	for _, it := range j.items {
		if !it.Masked && !it.Keep {
			continue
		}
		body := j.titleLabel
		if it.Line >= 0 {
			body = j.doc.Lines[it.Line].Body
		}
		if it.Det.End > len(body) {
			continue
		}
		conf := it.Det.Confidence
		source := "rule"
		if it.Decided {
			conf = clamp((it.Det.Confidence+max(it.TagConf, 0.5))/2 + 0.1)
			source = "rule+ai"
		}
		s := slot{Line: it.Line, Start: it.Det.Start, End: it.Det.End, Tag: it.Tag, Value: it.Det.Value, Category: it.Det.Category, Source: source, Confidence: conf}
		if !it.Decided {
			s.Flag = flagRuleOnly
		}
		j.slots = append(j.slots, s)
	}
}

const (
	propagateRounds = 4
	confirmBatch    = 150
	confirmTokens   = 8000
)

type spreadValue struct {
	tag   string
	conf  float64
	re    *regexp.Regexp
	value string
}

type spreadCandidate struct {
	slot  slot
	from  string
	sure  bool
	rough bool
}

func (j *chatJob) spreadValues() []spreadValue {
	best := map[string]slot{}
	consider := func(s slot) {
		value := strings.TrimSpace(s.Value)
		key := strings.ToLower(value)
		prev, seen := best[key]
		prevFallback := seen && isFallbackTag(prev.Tag, j.r.dict)
		fallback := isFallbackTag(s.Tag, j.r.dict)
		if !seen || (prevFallback && !fallback) || (prevFallback == fallback && s.Confidence > prev.Confidence) {
			best[key] = s
		}
	}
	for _, s := range j.slots {
		if s.Confidence < propagateMinConf || j.wrongNameTag(s) {
			continue
		}
		parsed, ok := j.r.dict.parse(s.Tag)
		if !ok || (parsed.Fallback && !identityAttributes[parsed.Attribute]) {
			continue
		}
		attr := parsed.Attribute
		if strings.HasSuffix(attr, "DATE") || strings.HasSuffix(attr, "AGE") || strings.HasSuffix(attr, "YEAR") || attr == "BIRTHDAY" {
			continue
		}
		value := strings.TrimSpace(s.Value)
		if utf8.RuneCountInString(value) < 3 || (identityAttributes[attr] && implausibleName(value)) {
			continue
		}
		if allGeneric(value) && !(identityAttributes[attr] && hasUpper(value) && !strings.Contains(value, " ")) {
			continue
		}
		if d := onlyDigits(value); d == value && len(d) < 6 {
			continue
		}
		consider(s)
		parts := strings.Fields(cleanName(value))
		if len(parts) < 2 {
			continue
		}
		switch {
		case parsed.Kind == tagPerson && attr == "FULL_NAME":
			role := parsed.role() + numberSuffix(parsed.Number)
			for i, part := range map[int]string{0: "FIRST_NAME", len(parts) - 1: "LAST_NAME"} {
				name := parts[i]
				if utf8.RuneCountInString(name) >= 3 && hasUpper(name) {
					if _, seen := best[strings.ToLower(name)]; !seen {
						best[strings.ToLower(name)] = slot{Tag: roleTag(role, part), Value: name, Confidence: s.Confidence * 0.95}
					}
				}
			}
		case parsed.Kind == tagEntity && attr == "NAME":
			stem := parts[0]
			if utf8.RuneCountInString(stem) >= 3 && hasUpper(stem) && !isCommonWord(stem) && !notANameAlone(stem) {
				if _, seen := best[strings.ToLower(stem)]; !seen {
					best[strings.ToLower(stem)] = slot{Tag: s.Tag, Value: stem, Confidence: s.Confidence * 0.9}
				}
			}
		}
	}
	if j.in.IsGroup {
		if stem := cleanName(j.titleLabel); utf8.RuneCountInString(stem) >= 4 && !isCommonWord(stem) && !onlyNonNames(stem) {
			if _, seen := best[strings.ToLower(stem)]; !seen {
				best[strings.ToLower(stem)] = slot{Tag: "{{GROUP_NAME}}", Value: stem, Confidence: 0.99}
			}
		}
	}
	var values []spreadValue
	for _, s := range best {
		value := strings.TrimSpace(s.Value)
		re, err := regexp.Compile(`(?i)` + regexp.QuoteMeta(value))
		if err != nil {
			continue
		}
		values = append(values, spreadValue{tag: s.Tag, conf: s.Confidence * 0.95, re: re, value: value})
	}
	sort.Slice(values, func(a, b int) bool { return len(values[a].value) > len(values[b].value) })
	return values
}

func unambiguousRepeat(match, value string) bool {
	if match != value || isCommonWord(value) {
		return false
	}
	return hasUpper(value) || strings.ContainsAny(value, "0123456789@.")
}

func (j *chatJob) findCandidates(values []spreadValue) []spreadCandidate {
	bodies := map[int]string{-1: j.titleLabel}
	for i, l := range j.doc.Lines {
		bodies[i] = l.Body
	}
	var fuzzy []spreadValue
	for _, v := range values {
		if p, ok := j.r.dict.parse(v.tag); ok && identityAttributes[p.Attribute] && !strings.Contains(v.value, " ") && utf8.RuneCountInString(v.value) >= 6 {
			fuzzy = append(fuzzy, v)
		}
	}
	var out []spreadCandidate
	taken := func(line, start, end int) bool {
		if j.overlapsLocked(line, start, end) {
			return true
		}
		for _, c := range out {
			if c.slot.Line == line && start < c.slot.End && c.slot.Start < end {
				return true
			}
		}
		return false
	}
	for line, body := range bodies {
		if body == "" || (line == -1 && !j.in.IsGroup) {
			continue
		}
		for _, v := range values {
			for _, m := range v.re.FindAllStringIndex(body, -1) {
				if !boundaryOK(body, m[0], m[1]) || taken(line, m[0], m[1]) {
					continue
				}
				match := body[m[0]:m[1]]
				out = append(out, spreadCandidate{
					slot: slot{Line: line, Start: m[0], End: m[1], Tag: v.tag, Value: match, Category: "PROPAGATED", Source: "propagated", Confidence: v.conf},
					from: v.value,
					sure: unambiguousRepeat(match, v.value),
				})
			}
		}
		for _, m := range wordPattern.FindAllStringIndex(body, -1) {
			word := body[m[0]:m[1]]
			if utf8.RuneCountInString(word) < 6 || isCommonWord(word) || taken(line, m[0], m[1]) {
				continue
			}
			for _, v := range fuzzy {
				if !strings.EqualFold(word, v.value) && editDistance(strings.ToLower(word), strings.ToLower(v.value)) == 1 {
					out = append(out, spreadCandidate{
						slot:  slot{Line: line, Start: m[0], End: m[1], Tag: v.tag, Value: word, Category: "PROPAGATED", Source: "propagated", Confidence: v.conf * 0.9},
						from:  v.value,
						rough: true,
					})
					break
				}
			}
		}
	}
	return out
}

func (j *chatJob) ruleConfirms(c spreadCandidate) bool {
	if c.sure {
		return true
	}
	body := j.titleLabel
	if c.slot.Line >= 0 {
		body = j.doc.Lines[c.slot.Line].Body
	}
	switch {
	case isCommonWord(c.slot.Value):
		return hasUpper(c.slot.Value) && vocativeUse(body, c.slot.Start, c.slot.End)
	case c.rough:
		return hasUpper(c.slot.Value)
	}
	return hasUpper(c.slot.Value) || utf8.RuneCountInString(c.from) >= 5
}

func (j *chatJob) confirmCandidates(ctx context.Context, cands []spreadCandidate) []bool {
	keep := make([]bool, len(cands))
	var ask []int
	for i, c := range cands {
		if c.sure {
			keep[i] = true
			continue
		}
		if !j.aiUsed || !j.r.aiAvailable() || ctx == nil {
			keep[i] = j.ruleConfirms(c)
			continue
		}
		ask = append(ask, i)
	}
	for start := 0; start < len(ask); start += confirmBatch {
		batch := ask[start:min(start+confirmBatch, len(ask))]
		var b strings.Builder
		b.WriteString(j.chatKind() + "\nCANDIDATES\n")
		for _, idx := range batch {
			c := cands[idx]
			body := j.titleLabel
			sender := "(group name)"
			if c.slot.Line >= 0 {
				l := j.doc.Lines[c.slot.Line]
				body = l.Body
				sender = j.senderDisplay(l)
			}
			line := j.previewLine(c.slot.Line, body[:c.slot.Start]) + "«" + c.slot.Value + "»" + j.previewLineFrom(c.slot.Line, body, c.slot.End)
			fmt.Fprintf(&b, "C%d: found %q tagged %s. Line #%d %s: %s\n", idx, c.from, c.slot.Tag, c.slot.Line+1, sender, around200(line))
		}
		var reply confirmReply
		err := j.generate(ctx, "confirm", confirmSystemPrompt, b.String(), confirmSchema(), confirmTokens, &reply)
		if err != nil {
			logRedact.Warnf("Chat %q: confirming %d possible repeats failed, using rules: %v", j.in.Folder, len(batch), err)
			for _, idx := range batch {
				keep[idx] = j.ruleConfirms(cands[idx])
			}
			continue
		}
		answered := map[int]bool{}
		for _, d := range reply.Decisions {
			idx, convErr := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(d.ID), "C"))
			if convErr != nil || idx < 0 || idx >= len(cands) {
				continue
			}
			keep[idx] = d.Same
			answered[idx] = true
		}
		for _, idx := range batch {
			if !answered[idx] {
				keep[idx] = j.ruleConfirms(cands[idx])
			}
		}
	}
	return keep
}

func around200(line string) string {
	i := strings.Index(line, "«")
	if i < 0 || len(line) <= 400 {
		return strings.ReplaceAll(line, "\n", " ⏎ ")
	}
	start, end := max(0, i-200), min(len(line), i+200)
	for start > 0 && !utf8.RuneStart(line[start]) {
		start--
	}
	for end < len(line) && !utf8.RuneStart(line[end]) {
		end++
	}
	return strings.ReplaceAll("…"+line[start:end]+"…", "\n", " ⏎ ")
}

func (j *chatJob) propagate(ctx context.Context) {
	rejected := map[string]bool{}
	for round := 0; round < propagateRounds; round++ {
		var cands []spreadCandidate
		for _, c := range j.findCandidates(j.spreadValues()) {
			if !rejected[fmt.Sprintf("%d:%d", c.slot.Line, c.slot.Start)] {
				cands = append(cands, c)
			}
		}
		if len(cands) == 0 {
			return
		}
		keep := j.confirmCandidates(ctx, cands)
		added := 0
		for i, c := range cands {
			if keep[i] {
				j.slots = append(j.slots, c.slot)
				added++
			} else {
				rejected[fmt.Sprintf("%d:%d", c.slot.Line, c.slot.Start)] = true
			}
		}
		logRedact.Debugf("Chat %q: spread round %d confirmed %d of %d possible repeats", j.in.Folder, round+1, added, len(cands))
		if added == 0 {
			return
		}
	}
}

var (
	greetingBefore = regexp.MustCompile(`(?i)(?:^|\b(?:hi|hey|hello|thanks|thank you|thx|dear|mr|mrs|ms|dr|bye|morning|evening|cheers|ok|okay|sure|yes|no)[\s,!.]*)$`)
	endAfter       = regexp.MustCompile(`^\s*(?:[,.!?;:)]|$)`)
)

func vocativeUse(body string, start, end int) bool {
	before := strings.TrimRight(body[:start], " ")
	if !greetingBefore.MatchString(before) && strings.TrimSpace(before) != "" && !strings.HasSuffix(before, ",") {
		return false
	}
	after := body[end:]
	if pronounAfter.MatchString(after) {
		return false
	}
	return endAfter.MatchString(after) || strings.TrimSpace(before) != ""
}

func isFallbackTag(tag string, d *tagDictionary) bool {
	p, ok := d.parse(tag)
	return !ok || p.Fallback
}

func allGeneric(value string) bool {
	for _, part := range strings.Fields(strings.ToLower(value)) {
		part = strings.Trim(part, ".,:;!?()[]\"'")
		if part == "" {
			continue
		}
		if !isCommonWord(part) {
			return false
		}
	}
	return true
}

const (
	roleBatch     = 120
	roleMaxTokens = 12000
)

var personDetailAttributes = setOf(strings.Fields("FIRST_NAME LAST_NAME FULL_NAME NICKNAME MIDDLE_NAME MAIDEN_NAME PHONE EMAIL HANDLE INSTAGRAM FACEBOOK TIKTOK X_HANDLE LINKEDIN SNAPCHAT TELEGRAM WHATSAPP_ID"))

type roleEntry struct {
	id    string
	value string
	slots []int
}

func (j *chatJob) resolveRoles(ctx context.Context) {
	byValue := map[string]*roleEntry{}
	var entries []*roleEntry
	for i, s := range j.slots {
		if strings.HasPrefix(s.Source, "rule") || s.Line < 0 {
			continue
		}
		p, ok := j.r.dict.parse(s.Tag)
		if !ok || p.Kind != tagPerson || !personDetailAttributes[p.Attribute] {
			continue
		}
		key := normalizeValue(s.Value)
		e, seen := byValue[key]
		if !seen {
			e = &roleEntry{id: "E" + strconv.Itoa(len(entries)+1), value: s.Value}
			byValue[key] = e
			entries = append(entries, e)
		}
		e.slots = append(e.slots, i)
	}
	for start := 0; start < len(entries); start += roleBatch {
		batch := entries[start:min(start+roleBatch, len(entries))]
		var b strings.Builder
		b.WriteString(j.chatKind() + "\n" + j.castTable() + "\nDETAILS\n")
		for _, e := range batch {
			first := j.slots[e.slots[0]]
			fmt.Fprintf(&b, "%s (currently %s):\n", e.id, first.Tag)
			shown := 0
			for _, idx := range e.slots {
				s := j.slots[idx]
				if shown == 2 || s.Line < 0 || s.Line >= len(j.doc.Lines) {
					continue
				}
				l := j.doc.Lines[s.Line]
				body := l.Body
				context := j.previewLine(s.Line, body[:s.Start]) + "«" + s.Value + "»" + j.previewLineFrom(s.Line, body, s.End)
				fmt.Fprintf(&b, "  #%d %s: %s\n", s.Line+1, j.senderDisplay(l), context)
				shown++
			}
		}
		var reply roleReply
		if err := j.generate(ctx, "roles", roleSystemPrompt+j.r.dict.promptText(), b.String(), roleSchema(), roleMaxTokens, &reply); err != nil {
			logRedact.Warnf("Chat %q: role step failed: %v", j.in.Folder, err)
			continue
		}
		roles := map[string]string{}
		same := map[string]string{}
		for _, a := range reply.People {
			id := strings.TrimSpace(a.ID)
			role := strings.ToUpper(strings.TrimSpace(a.Role))
			if role == "ORGANIZATION" {
				roles[id] = "ORGANIZATION"
				continue
			}
			if r := j.validRole(role); r != "" {
				roles[id] = r
			}
			if a.SameAs != "" {
				same[id] = strings.TrimSpace(a.SameAs)
			}
		}
		root := func(id string) string {
			for i := 0; i < 10; i++ {
				next, ok := same[id]
				if !ok || next == id {
					break
				}
				id = next
			}
			return id
		}
		changed := 0
		for _, e := range batch {
			role := firstNonEmpty(roles[root(e.id)], roles[e.id])
			if role == "" {
				continue
			}
			for _, idx := range e.slots {
				s := &j.slots[idx]
				p, ok := j.r.dict.parse(s.Tag)
				if !ok {
					continue
				}
				newTag := roleTag(role, p.Attribute)
				if role == "ORGANIZATION" {
					if !identityAttributes[p.Attribute] {
						continue
					}
					newTag = "{{ORGANIZATION_NAME}}"
				}
				if !j.r.dict.valid(newTag) {
					continue
				}
				newTag = j.guardTag(newTag, s.Value, s.Line)
				if newTag != s.Tag {
					s.Tag = newTag
					changed++
				}
			}
		}
		logRedact.Debugf("Chat %q: role step changed %d tags across %d details", j.in.Folder, changed, len(batch))
	}
}

func (j *chatJob) previewLineFrom(line int, body string, from int) string {
	var slots []slot
	for _, s := range j.slots {
		if s.Line == line && s.Start >= from {
			s.Start -= from
			s.End -= from
			slots = append(slots, s)
		}
	}
	rest := body[from:]
	return applySlots(rest, cleanSlots(rest, slots, j.r.dict))
}

var wordPattern = regexp.MustCompile(`[\p{L}][\p{L}'’-]*[\p{L}]`)

var addressFamily = setOf(strings.Fields("HOME_ADDRESS STREET_ADDRESS POSTCODE HOME_CITY HOME_STATE WORK_ADDRESS"))

func (j *chatJob) mergeAddresses() {
	sortSlots(j.slots)
	var merged []slot
	for _, s := range j.slots {
		if n := len(merged); n > 0 {
			prev := &merged[n-1]
			pp, ok1 := j.r.dict.parse(prev.Tag)
			ps, ok2 := j.r.dict.parse(s.Tag)
			if ok1 && ok2 && prev.Line == s.Line && pp.Kind == tagPerson && ps.Kind == tagPerson && pp.role() == ps.role() &&
				addressFamily[pp.Attribute] && addressFamily[ps.Attribute] && (pp.Attribute != "HOME_CITY" || ps.Attribute != "HOME_CITY") && s.Start >= prev.End {
				body := j.titleLabel
				if s.Line >= 0 {
					body = j.doc.Lines[s.Line].Body
				}
				if strings.Trim(body[prev.End:s.Start], " ,") == "" && (pp.Attribute == "HOME_ADDRESS" || pp.Attribute == "STREET_ADDRESS" || ps.Attribute == "POSTCODE") {
					prev.End = s.End
					prev.Value = body[prev.Start:prev.End]
					attr := "HOME_ADDRESS"
					if pp.Attribute == "WORK_ADDRESS" {
						attr = "WORK_ADDRESS"
					}
					prev.Tag = roleTag(pp.role()+numberSuffix(pp.Number), attr)
					prev.Confidence = min(prev.Confidence, s.Confidence)
					continue
				}
			}
		}
		merged = append(merged, s)
	}
	j.slots = merged
}

func numberSuffix(n int) string {
	if n >= 2 {
		return "_" + strconv.Itoa(n)
	}
	return ""
}

func (j *chatJob) senderTagFor(label string) string {
	if label == "" {
		return ""
	}
	role := ""
	if ref, ok := j.senderRefs[label]; ok {
		role = j.roles[ref]
	} else if !j.in.IsGroup && label == j.titleLabel && j.partnerRef != "" {
		role = j.roles[j.partnerRef]
	}
	if role == "" {
		return ""
	}
	attr := "FULL_NAME"
	switch {
	case strings.HasPrefix(label, "+"):
		attr = "PHONE"
	case len(strings.Fields(label)) == 1:
		attr = "FIRST_NAME"
	}
	return roleTag(role, attr)
}

func (j *chatJob) titleText() string {
	if j.in.IsGroup {
		var title []slot
		for _, s := range j.slots {
			if s.Line == -1 {
				title = append(title, s)
			}
		}
		cleaned := cleanSlots(j.titleLabel, title, j.r.dict)
		return applySlots(j.titleLabel, cleaned)
	}
	if tag := j.senderTagFor(j.titleLabel); tag != "" {
		return tag
	}
	if j.partnerRef != "" {
		return roleTag(j.roles[j.partnerRef], "FULL_NAME")
	}
	return "{{CONTACT_FULL_NAME}}"
}

func (j *chatJob) pass2Range(ctx context.Context, rg lineRange) {
	if errors.Is(j.pass2(ctx, rg, rg.end-rg.start >= 2*minSplitLines), errAITruncated) {
		mid := (rg.start + rg.end) / 2
		logRedact.Debugf("Chat %q: splitting second-pass lines %d-%d after a cut-off reply", j.in.Folder, rg.start+1, rg.end)
		j.pass2Range(ctx, lineRange{rg.start, mid})
		j.pass2Range(ctx, lineRange{mid, rg.end})
	}
}

func (j *chatJob) check(ctx context.Context) {
	ranges := chunkRanges(j.doc.Lines, chunkLines, chunkChars)
	if len(ranges) == 0 && j.in.IsGroup {
		ranges = []lineRange{{0, 0}}
	}
	sortSlots(j.slots)
	var wg sync.WaitGroup
	for _, rg := range ranges {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j.pass2Range(ctx, rg)
		}()
	}
	wg.Wait()
}

func (j *chatJob) pass2(ctx context.Context, rg lineRange, canSplit bool) error {
	j.mu.Lock()
	var table strings.Builder
	ids := map[string]int{}
	var b strings.Builder
	b.WriteString(j.chatKind() + "\n" + j.castTable() + "\nREDACTED CHUNK\n")
	if rg.start == 0 && j.in.IsGroup {
		fmt.Fprintf(&b, "#0 (group name): %s\n", j.previewLine(-1, j.titleLabel))
	}
	for i := rg.start; i < rg.end; i++ {
		l := j.doc.Lines[i]
		sender := "(system)"
		if l.HasSender {
			sender = firstNonEmpty(j.senderTagFor(l.Sender), j.senderDisplay(l))
			if j.in.IsGroup && l.Sender == j.titleLabel {
				sender = "(group)"
			}
		}
		fmt.Fprintf(&b, "#%d %s: %s\n", i+1, sender, j.previewLine(i, l.Body))
	}
	for idx, s := range j.slots {
		if (s.Line < rg.start || s.Line >= rg.end) && !(s.Line == -1 && rg.start == 0) {
			continue
		}
		id := "s" + strconv.Itoa(idx+1)
		ids[id] = idx
		original := fmt.Sprintf("%q", s.Value)
		if s.Source == "rule" || s.Source == "rule+ai" {
			original = "(hidden " + strings.ToLower(s.Category) + ")"
		}
		fmt.Fprintf(&table, "%s line %d: %s was %s\n", id, s.Line+1, s.Tag, original)
	}
	j.mu.Unlock()
	if table.Len() > 0 {
		b.WriteString("\nSLOT TABLE\n" + table.String())
	}
	var reply pass2Reply
	err := j.generate(ctx, "pass2", pass2SystemPrompt+j.r.dict.promptText(), b.String(), pass2Schema(), pass2MaxTokens, &reply)
	if errors.Is(err, errAITruncated) && canSplit {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.stats.AIChunks++
	if err != nil {
		j.stats.AIFailedChunks++
		logRedact.Warnf("Chat %q lines %d-%d: second pass failed: %v", j.in.Folder, rg.start+1, rg.end, err)
		return err
	}
	for _, v := range reply.Verdicts {
		idx, ok := ids[strings.TrimSpace(v.Slot)]
		if !ok {
			continue
		}
		s := &j.slots[idx]
		switch {
		case !v.IsPII:
			s.Flag = firstNonEmpty(s.Flag, flagDisagree)
			s.Confidence = min(s.Confidence, 0.5)
			j.stats.Disagreements++
		case v.Correct:
			s.Confidence = max(s.Confidence, clamp((s.Confidence+v.Confidence)/2+0.1))
		default:
			better := j.guardTag(normalizeTag(v.BetterTag), s.Value, s.Line)
			if better != "" && j.r.dict.valid(better) {
				s.Tag = better
				s.Confidence = clamp(v.Confidence * 0.9)
				s.Source += "+check"
			} else {
				s.Flag = firstNonEmpty(s.Flag, flagDisagree)
				j.stats.Disagreements++
			}
			if v.Confidence < 0.7 {
				s.Flag = firstNonEmpty(s.Flag, flagDisagree)
			}
		}
	}
	for _, leak := range reply.Leaks {
		leak.Confidence = clamp(leak.Confidence * 0.9)
		if j.addFound(leak, rg, "ai-check", 0.3) {
			j.stats.LeaksCaught++
		}
	}
	return nil
}

func (j *chatJob) previewLine(line int, body string) string {
	var slots []slot
	for _, s := range j.slots {
		if s.Line == line {
			slots = append(slots, s)
		}
	}
	return applySlots(body, cleanSlots(body, slots, j.r.dict))
}

func (j *chatJob) finish() chatOutput {
	pinned := 0
	var anchored []slot
	for _, label := range j.doc.senders() {
		if j.in.IsGroup && label == j.titleLabel {
			continue
		}
		if tag := j.senderTagFor(label); tag != "" {
			anchored = append(anchored, slot{Line: -2, Tag: tag, Value: label, Confidence: 100})
			pinned++
		}
	}
	all := append(anchored, j.slots...)
	fix := enforceConsistency(all, j.r.dict)
	j.slots = all[pinned:]
	j.stats.Renumbered = fix.renumbered
	out := chatOutput{Folder: j.in.Folder, Title: j.titleLabel, SenderTags: map[string]string{}, Roles: map[string]string{}, AIUsed: j.aiUsed}
	for ref, role := range j.roles {
		out.Roles[ref] = role
	}
	byLine := map[int][]slot{}
	for _, s := range j.slots {
		byLine[s.Line] = append(byLine[s.Line], s)
	}
	out.TitleText = j.titleText()
	redacted := chatDoc{Newline: j.doc.Newline, Trailing: j.doc.Trailing, Preamble: j.doc.Preamble}
	for i, l := range j.doc.Lines {
		senderTag := ""
		if l.HasSender {
			if j.in.IsGroup && l.Sender == j.titleLabel {
				senderTag = out.TitleText
			} else {
				senderTag = j.senderTagFor(l.Sender)
			}
			if senderTag == "" {
				senderTag = "{{PERSON_FULL_NAME}}"
			}
			out.SenderTags[l.Sender] = senderTag
		}
		line, result := redactLine(l, senderTag, byLine[i], j.r.dict)
		redacted.Lines = append(redacted.Lines, line)
		if result.rebuilt {
			j.stats.Rebuilt++
			logRedact.Warnf("Chat %q line %d failed the template check and was rebuilt (whole body %v)", j.in.Folder, i+1, result.whole)
		}
		if result.whole {
			j.stats.WholeBody++
		}
		out.Slots = append(out.Slots, result.slots...)
	}
	out.Slots = append(out.Slots, cleanSlots(j.titleLabel, byLine[-1], j.r.dict)...)
	out.Lines = redacted.Lines
	out.Text = redacted.render()
	for _, s := range out.Slots {
		j.stats.Slots++
		if s.Confidence >= highConfidence && s.Flag == "" {
			j.stats.High++
		}
		if s.Flag != "" {
			j.stats.Flagged++
		}
		if p, ok := j.r.dict.parse(s.Tag); ok && p.Fallback {
			j.stats.Fallback++
		}
	}
	out.Stats = j.stats
	return out
}
