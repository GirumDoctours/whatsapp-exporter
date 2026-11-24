package main

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type layoutKind int

const (
	layoutPerson layoutKind = iota
	layoutDoctor
	layoutCompany
	layoutClinic
	layoutHospital
	layoutHotel
	layoutOrg
)

type layoutState struct {
	bodies []string
	lines  [][]slot
	known  map[string]string
	maxNum map[string]int
	rule   string
	origin map[string]string
}

const layoutCap = `\p{Lu}[\p{L}'’\-]*\p{L}`

var (
	layoutBullet          = regexp.MustCompile(`^[\t ]*(?:[-•*–·▪◦➢➤]|\d{1,2}[.)\-]|\p{So})[\t ]*[\x{2060}\x{200e}\x{200f}]*[\t ]*`)
	layoutHonorific       = regexp.MustCompile(`(?i:\b(?:op\.\s?dr|doç\.\s?dr|uzm\.\s?dr|prof\.\s?dr|mr|mrs|ms|miss|dr|prof|sayın)\b)\.?[ \t]+(` + layoutCap + `(?:[ \t]+` + layoutCap + `){0,3})`)
	layoutGreeting        = regexp.MustCompile(`(?m)(?:^|[.!?]\s+)(?:Hi|Hey|Hello|Hiya|Dear|Merhaba|Selam|Selamlar|Thanks|Thank you|Thx|Good morning|Good night|Good evening|Morning|Günaydın|Hola|Bonjour|Ciao|Welcome|Congrats|Congratulations|Happy birthday|Bye|Sorry|Cheers|Well done|Great job)[ \t]*,?[ \t]+(` + layoutCap + `)(?:[ \t]+(` + layoutCap + `))?[ \t]*(?:[,!.?:]|$)`)
	layoutClosing         = regexp.MustCompile(`(?i)^[\t ]*(?:best regards|kind regards|warm regards|warmest regards|regards|best|cheers|sincerely|many thanks|thanks|thank you|saygılarımla|saygılarımızla|teşekkürler|iyi çalışmalar)[,!.]?[\t ]*$`)
	layoutNameLine        = regexp.MustCompile(`^(` + layoutCap + `(?:[ \t]+` + layoutCap + `){0,3})[ \t]*$`)
	layoutOrgSuffix       = regexp.MustCompile(`(?:` + `\p{Lu}[\p{L}\p{N}'’&.\-]*` + `[ \t]+){1,4}(?:(?:of|de|&)[ \t]+\p{Lu}[\p{L}'’\-]*[ \t]+)?(?:Clinic|Clinics|Klinik|Kliniği|Hospital|Hastanesi|Hastane|Group|Digital|Inc\.?|LLC|Ltd\.?|GmbH|Agency|Ajans|Foundation|Medical|Center|Centre|Labs?|Studios?|Capital|Ventures|Partners|Holdings?|Technologies|Media|Consulting|Academy|Institute|Hotel|Hotels|Otel|Oteli|Resort|Suites|Hostel)\b`)
	layoutListHead        = regexp.MustCompile(`^(` + layoutCap + `(?:[ \t]+(?:` + layoutCap + `|of|de|&))*)`)
	layoutPersonLine      = regexp.MustCompile(`^(` + layoutCap + `(?:[ \t]+` + layoutCap + `){1,2})[ \t]*(?::|,|[ \t]-|[ \t]–|[ \t]—)[ \t]+(.+)$`)
	layoutAtCompany       = regexp.MustCompile(`\b(?:at|@)[ \t]+(\p{Lu}[\p{L}\p{N}'’&.\-]*(?:[ \t]+(?:\p{Lu}[\p{L}\p{N}'’&.\-]*|of|de|&))*)`)
	layoutFilename        = regexp.MustCompile(`^[\t ]*([^\n]{2,120}\.[A-Za-z0-9]{2,5})[\t ]*$`)
	layoutFileToken       = regexp.MustCompile(`[\p{L}]+`)
	layoutPhone           = regexp.MustCompile(`\+?\d[\d \-().]{6,18}\d`)
	layoutEmail           = regexp.MustCompile(`[\p{L}\p{N}._%+\-]+@[\p{L}\p{N}.\-]+\.\p{L}{2,}`)
	layoutKeyLine         = regexp.MustCompile(`^[\t ]*(?:[-•*–·]|\p{So})?[\t ]*\*?([\p{L}][\p{L} ./\-]{0,28}?)\*?[\t ]*[:：][\t ]*(.*?)[\t ]*$`)
	layoutWordRun         = regexp.MustCompile(`[\p{L}\p{N}][\p{L}\p{N}'’\-]*`)
	layoutNameSep         = regexp.MustCompile(`\s+(?:and|ve|&)\s+|[,/;]`)
	layoutHonorificPrefix = regexp.MustCompile(`^(?i:(?:op\.\s?dr|doç\.\s?dr|mr|mrs|ms|miss|dr|prof|sayın)\.?\s+)`)
	layoutNameRun         = regexp.MustCompile(`^` + layoutCap + `(?:[ \t]+` + layoutCap + `){0,3}`)
	layoutTemplate        = regexp.MustCompile(`\{\{[A-Z0-9_]+\}\}`)
)

var layoutPersonKeys = map[string]bool{
	"name": true, "full name": true, "fullname": true, "contact": true, "contact person": true, "contact name": true,
	"ad soyad": true, "adı soyadı": true, "adi soyadi": true, "ad": true, "soyad": true, "soyadı": true, "isim": true, "isim soyisim": true,
	"patient": true, "patient name": true, "hasta": true, "hasta adı": true, "doctor": true, "doktor": true, "surgeon": true,
	"guest": true, "guest name": true, "passenger": true, "yolcu": true, "attendee": true, "consultant": true, "client": true,
	"first name": true, "last name": true, "surname": true, "nombre": true, "nom": true, "prénom": true, "admin": true,
	"from": true, "cc": true, "sender": true, "sent by": true, "forwarded from": true, "written by": true, "hosted by": true,
	"founder": true, "ceo": true, "owner": true, "host": true, "speaker": true, "organizer": true, "coordinator": true, "driver": true,
}

var layoutOrgKeys = map[string]bool{
	"company": true, "company name": true, "clinic": true, "clinic name": true, "klinik": true, "hospital": true, "hastane": true,
	"organization": true, "organisation": true, "firma": true, "şirket": true, "employer": true, "business": true, "agency": true,
	"hotel": true, "otel": true, "startup": true, "brand": true, "venue": true, "company/clinic": true,
}

var layoutAddressKeys = map[string]bool{
	"address": true, "adres": true, "adresi": true, "official address": true, "home address": true, "work address": true,
	"pickup address": true, "pick up address": true, "location": true, "konum": true, "dirección": true, "adresse": true,
}

var layoutPhoneKeys = map[string]bool{
	"phone": true, "tel": true, "telefon": true, "mobile": true, "cell": true, "cep": true, "gsm": true, "whatsapp": true,
	"phone number": true, "telefon numarası": true, "contact number": true, "mobile number": true, "tel no": true,
}

var layoutEmailKeys = map[string]bool{
	"email": true, "e-mail": true, "mail": true, "e-posta": true, "eposta": true, "email address": true,
}

var layoutGreetingStop = map[string]bool{
	"everyone": true, "all": true, "team": true, "guys": true, "folks": true, "there": true, "friends": true, "both": true,
	"again": true, "brother": true, "sister": true, "sir": true, "madam": true, "dear": true, "love": true, "baby": true,
	"bro": true, "sis": true, "man": true, "mate": true, "buddy": true, "fam": true, "you": true, "everybody": true,
	"hocam": true, "abi": true, "abla": true, "ladies": true, "gentlemen": true, "so": true, "much": true, "for": true,
	"partner": true, "god": true, "dr": true, "mr": true, "ms": true, "mrs": true, "prof": true, "doctor": true, "hope": true, "please": true, "yes": true, "no": true, "ok": true,
	"guys!": true, "family": true, "people": true, "community": true, "members": true, "world": true, "again!": true,
}

var layoutFileGeneric = map[string]bool{
	"cv": true, "resume": true, "résumé": true, "özgeçmiş": true, "ozgecmis": true, "pdf": true, "docx": true, "doc": true,
	"design": true, "professional": true, "profesyonel": true, "page": true, "türkçe": true, "turkish": true,
	"english": true, "tr": true, "en": true, "final": true, "new": true, "version": true, "copy": true, "draft": true,
	"img": true, "image": true, "scan": true, "document": true, "file": true, "jpg": true, "jpeg": true, "png": true,
	"xlsx": true, "pptx": true, "deck": true, "invoice": true, "contract": true, "agreement": true, "brochure": true,
	"presentation": true, "report": true, "screenshot": true, "photo": true, "video": true,
}

func layoutLayout(fc refineInput) []slot {
	doc := fc.parsed()
	st := &layoutState{known: map[string]string{}, maxNum: map[string]int{}, origin: map[string]string{}}
	st.lines = make([][]slot, len(doc.Lines))
	for _, l := range doc.Lines {
		st.bodies = append(st.bodies, l.Body)
	}
	for _, s := range fc.Slots {
		if s.Line < 0 || s.Line >= len(st.bodies) {
			continue
		}
		st.lines[s.Line] = append(st.lines[s.Line], s)
		st.noteTag(s.Tag)
		st.remember(s.Value, s.Tag)
	}
	for _, tag := range fc.SenderTags {
		st.noteTag(tag)
	}
	st.noteTag(fc.TitleText)
	st.rule = "senders"
	for _, name := range sortedKeys(fc.SenderTags) {
		st.learnSender(name, fc.SenderTags[name])
	}
	if !fc.IsGroup {
		st.learnSender(fc.Title, fc.TitleText)
	}
	for i, body := range st.bodies {
		st.rule = "keyValues"
		st.keyValues(i, body)
		st.rule = "personLists"
		st.personLists(i, body)
		st.rule = "contactHeaders"
		st.contactHeaders(i, body)
		st.rule = "honorifics"
		st.honorifics(i, body)
		st.rule = "greetings"
		st.greetings(i, body)
		st.rule = "signatures"
		st.signatures(i, body)
		st.rule = "attachments"
		st.attachments(i, body)
		st.rule = "tables"
		st.tables(i, body)
		st.rule = "attribution"
		st.attribution(i, body)
		st.rule = "itinerary"
		st.itinerary(i, body)
		st.rule = "orgSuffixes"
		st.orgSuffixes(i, body)
	}
	for i, body := range st.bodies {
		st.rule = "listPropagation"
		st.listPropagation(i, body)
	}
	st.rule = "vocatives"
	st.vocatives(fc)
	st.rule = "propagate"
	st.propagate()
	var out []slot
	for _, ls := range st.lines {
		out = append(out, ls...)
	}
	sortSlots(out)
	return out
}

func (st *layoutState) noteTag(tag string) {
	for _, t := range layoutTemplate.FindAllString(tag, -1) {
		p, ok := dictionary.parse(t)
		if !ok || p.Kind == tagStandalone {
			continue
		}
		key := p.Base
		if p.Kind == tagPerson {
			key = p.role()
		}
		n := max(p.Number, 1)
		if n > st.maxNum[key] {
			st.maxNum[key] = n
		}
	}
}

func layoutIsNameTag(tag string) bool {
	p, ok := dictionary.parse(tag)
	if !ok {
		return false
	}
	switch p.Kind {
	case tagPerson:
		switch p.Attribute {
		case "FIRST_NAME", "LAST_NAME", "FULL_NAME", "NICKNAME", "MIDDLE_NAME", "MAIDEN_NAME", "EMPLOYER":
			return true
		}
	case tagEntity:
		return p.Attribute == "NAME" || p.Attribute == "COMPANY"
	}
	return false
}

func (st *layoutState) remember(value, tag string) {
	if !layoutIsNameTag(tag) {
		return
	}
	v := strings.TrimSpace(value)
	if _, ok := st.known[v]; !ok {
		st.known[v] = tag
		st.origin[v] = st.rule
	}
}

func (st *layoutState) alloc(key, attr string) string {
	n := st.maxNum[key] + 1
	st.maxNum[key] = n
	suffix := ""
	if n >= 2 {
		suffix = "_" + strconv.Itoa(n)
	}
	return "{{" + key + suffix + "_" + attr + "}}"
}

func (st *layoutState) tagFor(value string, kind layoutKind) string {
	if t, ok := st.known[value]; ok {
		return t
	}
	var tag string
	switch kind {
	case layoutPerson, layoutDoctor:
		role := "PERSON"
		if kind == layoutDoctor {
			role = "DOCTOR"
		}
		attr := "FULL_NAME"
		if len(strings.Fields(value)) == 1 {
			attr = "FIRST_NAME"
		}
		tag = st.alloc(role, attr)
	case layoutClinic:
		tag = st.alloc("CLINIC", "NAME")
	case layoutHospital:
		tag = st.alloc("HOSPITAL", "NAME")
	case layoutCompany:
		tag = st.alloc("COMPANY", "NAME")
	case layoutHotel:
		tag = st.alloc("HOTEL", "NAME")
	default:
		tag = st.alloc("ORGANIZATION", "NAME")
	}
	return tag
}

func layoutIsWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func layoutAligned(body string, start, end int) bool {
	if start < 0 || end > len(body) || start >= end {
		return false
	}
	if start > 0 {
		r, _ := utf8.DecodeLastRuneInString(body[:start])
		f, _ := utf8.DecodeRuneInString(body[start:])
		if layoutIsWord(r) && layoutIsWord(f) {
			return false
		}
	}
	if end < len(body) {
		r, _ := utf8.DecodeRuneInString(body[end:])
		l, _ := utf8.DecodeLastRuneInString(body[:end])
		if layoutIsWord(r) && layoutIsWord(l) {
			return false
		}
	}
	return true
}

func (st *layoutState) free(line, start, end int) bool {
	for _, s := range st.lines[line] {
		if start < s.End && s.Start < end {
			return false
		}
	}
	return true
}

func (st *layoutState) add(line, start, end int, tag string) bool {
	body := st.bodies[line]
	for start < end && body[start] == ' ' {
		start++
	}
	for end > start && (body[end-1] == ' ' || body[end-1] == '\t') {
		end--
	}
	if !layoutAligned(body, start, end) || !st.free(line, start, end) || !dictionary.valid(tag) {
		return false
	}
	value := body[start:end]
	if strings.Contains(value, "{{") || strings.Contains(value, "}}") {
		return false
	}
	st.lines[line] = append(st.lines[line], slot{Line: line, Start: start, End: end, Tag: tag, Value: value, Source: st.rule, Confidence: 0.7})
	st.remember(value, tag)
	return true
}

func (st *layoutState) addName(line, start, end int, kind layoutKind) bool {
	body := st.bodies[line]
	if start < 0 || end > len(body) || start >= end {
		return false
	}
	for _, poss := range []string{"’s", "'s", "’", "'"} {
		if strings.HasSuffix(body[start:end], poss) {
			end -= len(poss)
			break
		}
	}
	value := strings.TrimSpace(body[start:end])
	if value == "" {
		return false
	}
	return st.add(line, start, end, st.tagFor(value, kind))
}

func layoutPhysical(body string) [][2]int {
	var out [][2]int
	start := 0
	for i := 0; i <= len(body); i++ {
		if i == len(body) || body[i] == '\n' {
			out = append(out, [2]int{start, i})
			start = i + 1
		}
	}
	return out
}

func layoutOrgKind(value string) layoutKind {
	lower := strings.ToLower(value)
	switch {
	case strings.Contains(lower, "clinic") || strings.Contains(lower, "klinik"):
		return layoutClinic
	case strings.Contains(lower, "hospital") || strings.Contains(lower, "hastane"):
		return layoutHospital
	case strings.Contains(lower, "hotel") || strings.Contains(lower, "otel") || strings.Contains(lower, "resort") || strings.Contains(lower, "suites") || strings.Contains(lower, "hostel"):
		return layoutHotel
	case strings.Contains(lower, "foundation") || strings.Contains(lower, "institute") || strings.Contains(lower, "academy"):
		return layoutOrg
	}
	return layoutCompany
}

func layoutAllCommon(value string) bool {
	for _, w := range layoutWordRun.FindAllString(value, -1) {
		if !isCommonWord(w) {
			return false
		}
	}
	return true
}

func (st *layoutState) namesIn(line, start, end int, kind layoutKind) {
	body := st.bodies[line]
	seg := body[start:end]
	if strings.HasPrefix(strings.TrimSpace(seg), "[") || strings.HasPrefix(strings.TrimSpace(seg), "<") {
		return
	}
	for _, m := range layoutHonorific.FindAllStringSubmatchIndex(seg, -1) {
		st.addName(line, start+m[2], start+m[3], kind)
	}
	bounds := []int{0}
	for _, sep := range layoutNameSep.FindAllStringIndex(seg, -1) {
		bounds = append(bounds, sep[0], sep[1])
	}
	bounds = append(bounds, len(seg))
	for k := 0; k+1 < len(bounds); k += 2 {
		part := seg[bounds[k]:bounds[k+1]]
		lead := len(part) - len(strings.TrimLeft(part, " \t"))
		if h := layoutHonorificPrefix.FindStringIndex(part[lead:]); h != nil {
			lead += h[1]
		}
		m := layoutNameRun.FindString(part[lead:])
		if m == "" || len(strings.Fields(m)) == 1 && layoutAllCommon(m) {
			continue
		}
		ps := start + bounds[k] + lead
		st.addName(line, ps, ps+len(m), kind)
	}
}

func layoutNormKey(k string) string {
	k = strings.ToLower(strings.TrimSpace(k))
	k = strings.Trim(k, "*_ ")
	return strings.Join(strings.Fields(k), " ")
}

func (st *layoutState) keyValues(line int, body string) {
	phys := layoutPhysical(body)
	for pi, pr := range phys {
		text := body[pr[0]:pr[1]]
		m := layoutKeyLine.FindStringSubmatchIndex(text)
		if m == nil {
			continue
		}
		key := layoutNormKey(text[m[2]:m[3]])
		vs, ve := pr[0]+m[4], pr[0]+m[5]
		if vs == ve {
			if pi+1 >= len(phys) || !layoutAddressKeys[key] {
				continue
			}
			next := phys[pi+1]
			vs, ve = next[0], next[1]
			for vs < ve && (body[vs] == ' ' || body[vs] == '\t') {
				vs++
			}
		}
		if vs >= ve {
			continue
		}
		value := body[vs:ve]
		switch {
		case layoutPersonKeys[key]:
			kind := layoutPerson
			if key == "doctor" || key == "doktor" || key == "surgeon" {
				kind = layoutDoctor
			}
			st.namesIn(line, vs, ve, kind)
		case layoutOrgKeys[key]:
			cut := strings.IndexAny(value, ",;(|")
			if cut < 0 {
				cut = len(value)
			}
			v := strings.TrimSpace(value[:cut])
			if v == "" || !unicode.IsUpper(firstRune(v)) && !unicode.IsDigit(firstRune(v)) || len(strings.Fields(v)) > 6 {
				continue
			}
			st.addName(line, vs, vs+strings.Index(value, v)+len(v), layoutOrgKind(v+" "+key))
		case layoutAddressKeys[key]:
			if strings.HasPrefix(value, "http") || !strings.ContainsAny(value, "0123456789,") {
				continue
			}
			st.add(line, vs, ve, "{{ADDRESS}}")
		case layoutPhoneKeys[key]:
			if p := layoutPhone.FindStringIndex(value); p != nil {
				st.add(line, vs+p[0], vs+p[1], "{{PHONE_NUMBER}}")
			}
		case layoutDateKeys[key]:
			if d := layoutDateValue.FindStringIndex(value); d != nil {
				st.add(line, vs+d[0], vs+d[1], layoutDateTag(key))
			}
		case layoutEmailKeys[key]:
			if e := layoutEmail.FindStringIndex(value); e != nil {
				st.add(line, vs+e[0], vs+e[1], "{{EMAIL_ADDRESS}}")
			}
		}
	}
}

func (st *layoutState) personLists(line int, body string) {
	phys := layoutPhysical(body)
	type hit struct {
		pr [2]int
		m  []int
	}
	var run []hit
	flush := func() {
		if len(run) >= 2 {
			for _, h := range run {
				base := h.pr[0]
				head := body[base+h.m[2] : base+h.m[3]]
				if layoutFieldWordsIn(head) || !layoutPersonHead(head) {
					continue
				}
				st.addName(line, base+h.m[2], base+h.m[3], layoutPerson)
				rest := body[base+h.m[4] : base+h.m[5]]
				if a := layoutAtCompany.FindStringSubmatchIndex(rest); a != nil {
					v := strings.TrimRight(rest[a[2]:a[3]], ".& ")
					st.addName(line, base+h.m[4]+a[2], base+h.m[4]+a[2]+len(v), layoutOrgKind(v))
				}
			}
		}
		run = nil
	}
	for _, pr := range phys {
		text := body[pr[0]:pr[1]]
		bl := layoutBullet.FindStringIndex(text)
		off := 0
		if bl != nil {
			off = bl[1]
		}
		m := layoutPersonLine.FindStringSubmatchIndex(text[off:])
		if m != nil && !layoutShortDescription(text[off+m[4]:off+m[5]]) {
			m = nil
		}
		if m == nil {
			flush()
			continue
		}
		for k := range m {
			if m[k] >= 0 {
				m[k] += off
			}
		}
		run = append(run, hit{pr, m})
	}
	flush()
}

var layoutFieldWords = map[string]bool{
	"number": true, "date": true, "name": true, "price": true, "link": true, "code": true, "address": true, "account": true,
	"routing": true, "time": true, "total": true, "status": true, "level": true, "part": true, "bonus": true, "option": true,
	"update": true, "note": true, "question": true, "answer": true, "location": true, "duration": true, "video": true,
	"register": true, "rsvp": true, "wod": true, "title": true, "email": true, "phone": true, "mail": true,
	"contact": true, "company": true, "clinic": true, "position": true, "role": true, "fee": true, "cost": true, "payment": true,
	"step": true, "day": true, "week": true, "month": true, "round": true, "monday": true, "tuesday": true, "wednesday": true,
	"thursday": true, "friday": true, "saturday": true, "sunday": true, "pros": true, "cons": true, "summary": true, "agenda": true,
	"goal": true, "goals": true, "next": true, "steps": true, "action": true, "items": true, "deadline": true, "budget": true,
	"term": true, "termination": true, "notice": true, "assignment": true, "services": true, "warranties": true, "miscellaneous": true,
	"exhibit": true, "section": true, "schedule": true, "venue": true, "event": true, "ticket": true, "tickets": true, "theme": true,
}

func layoutFieldWordsIn(s string) bool {
	for _, w := range layoutWordRun.FindAllString(s, -1) {
		if layoutFieldWords[strings.ToLower(w)] || isCommonWord(w) {
			return true
		}
	}
	return false
}

func layoutIsField(text string) bool {
	if layoutEmail.MatchString(text) || strings.Contains(text, "{{") {
		return true
	}
	m := layoutKeyLine.FindStringSubmatch(text)
	if m == nil {
		return false
	}
	k := layoutNormKey(m[1])
	return layoutPersonKeys[k] || layoutEmailKeys[k] || layoutPhoneKeys[k] || layoutAddressKeys[k] || layoutOrgKeys[k]
}

func (st *layoutState) contactHeaders(line int, body string) {
	phys := layoutPhysical(body)
	if len(phys) < 3 {
		return
	}
	fields := 0
	for _, pr := range phys {
		if layoutIsField(body[pr[0]:pr[1]]) {
			fields++
		}
	}
	if fields < 2 {
		return
	}
	for pi := 0; pi+1 < len(phys); pi++ {
		pr := phys[pi]
		text := body[pr[0]:pr[1]]
		if layoutIsField(text) || !layoutIsField(body[phys[pi+1][0]:phys[pi+1][1]]) {
			continue
		}
		t := strings.TrimRight(strings.TrimSpace(text), ":")
		if utf8.RuneCountInString(t) < 3 || len(strings.Fields(t)) > 5 || !unicode.IsUpper(firstRune(t)) || strings.ContainsAny(t, ".?!") {
			continue
		}
		if layoutAllCommon(t) || layoutFieldWordsIn(strings.Fields(t)[0]) || !layoutAllCapitalized(t) {
			continue
		}
		start := pr[0] + strings.Index(text, t)
		kind := layoutOrgKind(t)
		if kind == layoutCompany && layoutNameLine.MatchString(t) && len(strings.Fields(t)) >= 2 {
			next := strings.ToLower(body[phys[pi+1][0]:phys[pi+1][1]])
			if !strings.Contains(next, "contact") {
				kind = layoutPerson
			}
		}
		st.addName(line, start, start+len(t), kind)
	}
}

func (st *layoutState) honorifics(line int, body string) {
	for _, m := range layoutHonorific.FindAllStringSubmatchIndex(body, -1) {
		kind := layoutPerson
		h := strings.ToLower(body[m[0]:m[2]])
		if strings.Contains(h, "dr") || strings.Contains(h, "prof") {
			kind = layoutDoctor
		}
		value := body[m[2]:m[3]]
		words := strings.Fields(value)
		keep := 0
		for _, w := range words {
			if layoutFieldWords[strings.ToLower(w)] || keep > 0 && utf8.RuneCountInString(w) <= 3 && strings.ToUpper(w) == w {
				break
			}
			keep++
		}
		if keep == 0 {
			continue
		}
		end := m[2] + len(strings.Join(words[:keep], " "))
		if !strings.HasPrefix(body[m[2]:], strings.Join(words[:keep], " ")) {
			end = m[2] + len(words[0])
		}
		st.addName(line, m[2], end, kind)
	}
}

func (st *layoutState) greetings(line int, body string) {
	for _, m := range layoutGreeting.FindAllStringSubmatchIndex(body, -1) {
		first := body[m[2]:m[3]]
		lf := strings.ToLower(first)
		if layoutGreetingStop[lf] || strings.HasSuffix(lf, "ers") || strings.HasSuffix(lf, "ists") || strings.HasSuffix(lf, "ians") {
			continue
		}
		end := m[3]
		if m[4] >= 0 {
			second := body[m[4]:m[5]]
			if !isCommonWord(second) && !layoutGreetingStop[strings.ToLower(second)] {
				end = m[5]
			}
		}
		st.addName(line, m[2], end, layoutPerson)
	}
}

func (st *layoutState) signatures(line int, body string) {
	phys := layoutPhysical(body)
	for pi, pr := range phys {
		if !layoutClosing.MatchString(body[pr[0]:pr[1]]) {
			continue
		}
		for pj := pi + 1; pj < len(phys); pj++ {
			text := body[phys[pj][0]:phys[pj][1]]
			t := strings.TrimSpace(text)
			if t == "" {
				continue
			}
			if !layoutNameLine.MatchString(t) || len(strings.Fields(t)) > 4 {
				break
			}
			start := phys[pj][0] + strings.Index(text, t)
			if layoutOrgSuffix.MatchString(t) {
				st.addName(line, start, start+len(t), layoutOrgKind(t))
			} else if !(len(strings.Fields(t)) == 1 && layoutFieldWords[strings.ToLower(t)]) {
				st.addName(line, start, start+len(t), layoutPerson)
			}
			break
		}
	}
}

func (st *layoutState) attachments(line int, body string) {
	phys := layoutPhysical(body)
	for pi, pr := range phys {
		if !strings.Contains(body[pr[0]:pr[1]], "document omitted") || pi+1 >= len(phys) {
			continue
		}
		next := phys[pi+1]
		text := body[next[0]:next[1]]
		m := layoutFilename.FindStringSubmatchIndex(text)
		if m == nil {
			continue
		}
		name := text[m[2]:m[3]]
		informative := false
		for _, tok := range layoutFileToken.FindAllString(strings.TrimSuffix(name, name[strings.LastIndex(name, "."):]), -1) {
			lower := strings.ToLower(tok)
			if utf8.RuneCountInString(tok) >= 3 && !layoutFileGeneric[lower] && !isCommonWord(lower) {
				informative = true
			}
		}
		if !informative {
			continue
		}
		st.add(line, next[0]+m[2], next[0]+m[3], "{{ATTACHMENT_FILENAME}}")
	}
}

var layoutDeterminers = map[string]bool{
	"the": true, "our": true, "my": true, "your": true, "their": true, "this": true, "that": true, "a": true, "an": true,
	"his": true, "her": true, "its": true, "these": true, "those": true, "from": true, "with": true, "at": true, "in": true,
	"for": true, "and": true, "of": true, "to": true, "by": true, "on": true, "new": true, "dear": true, "hi": true, "hello": true,
	"also": true, "but": true, "so": true, "then": true, "visit": true, "call": true, "contact": true, "meet": true, "ask": true,
	"via": true, "like": true, "including": true, "both": true, "every": true, "each": true, "which": true, "whether": true,
}

var layoutGenericOrg = map[string]bool{
	"chief": true, "vp": true, "svp": true, "evp": true, "head": true, "international": true, "social": true, "dental": true,
	"hair": true, "medical": true, "health": true, "global": true, "national": true, "private": true, "public": true,
	"digital": true, "mental": true, "primary": true, "general": true, "working": true, "policy": true, "forum": true,
	"healthcare": true, "aesthetic": true, "aesthetics": true, "beauty": true, "plastic": true, "surgery": true, "eye": true,
	"cancer": true, "research": true, "university": true, "city": true, "community": true, "regional": true, "local": true,
	"marketing": true, "sales": true, "creative": true, "media": true, "tech": true, "technology": true, "ai": true,
	"business": true, "innovation": true, "startup": true, "venture": true, "test": true, "partner": true, "partners": true,
	"doctors": true, "doctor": true, "dentist": true, "dentists": true, "surgeons": true, "patients": true, "transplant": true, "obesity": true, "wellness": true, "center": true, "centre": true, "group": true, "clinic": true,
}

func (st *layoutState) orgSuffixes(line int, body string) {
	for _, m := range layoutOrgSuffix.FindAllStringIndex(body, -1) {
		start, end := m[0], m[1]
		for {
			seg := body[start:end]
			sp := strings.IndexAny(seg, " \t")
			if sp < 0 {
				break
			}
			w := strings.Trim(seg[:sp], ".,")
			if layoutDeterminers[strings.ToLower(w)] || isCommonWord(w) {
				start += sp + 1
				for start < end && (body[start] == ' ' || body[start] == '\t') {
					start++
				}
				continue
			}
			break
		}
		seg := body[start:end]
		if !strings.ContainsAny(seg, " \t") {
			continue
		}
		fields := strings.Fields(seg)
		if layoutFieldWords[strings.ToLower(fields[0])] {
			continue
		}
		distinct := false
		for _, w := range fields[:len(fields)-1] {
			lw := strings.ToLower(strings.Trim(w, ".,&"))
			if lw != "" && !layoutGenericOrg[lw] && !isCommonWord(lw) && !layoutDeterminers[lw] {
				distinct = true
			}
		}
		if !distinct {
			continue
		}
		st.addName(line, start, end, layoutOrgKind(seg))
	}
}

func (st *layoutState) covering(line, start, end int) (slot, bool) {
	for _, s := range st.lines[line] {
		if s.Start <= start && s.End >= end && layoutIsNameTag(s.Tag) {
			return s, true
		}
		if s.Start >= start && s.End <= end && s.End-s.Start >= (end-start)/2 && layoutIsNameTag(s.Tag) {
			return s, true
		}
	}
	return slot{}, false
}

func layoutKindOf(tag string) layoutKind {
	p, ok := dictionary.parse(tag)
	if !ok {
		return layoutOrg
	}
	if p.Kind == tagPerson {
		return layoutPerson
	}
	switch p.Base {
	case "CLINIC":
		return layoutClinic
	case "HOSPITAL":
		return layoutHospital
	case "COMPANY":
		return layoutCompany
	case "HOTEL":
		return layoutHotel
	}
	return layoutOrg
}

func (st *layoutState) listPropagation(line int, body string) {
	phys := layoutPhysical(body)
	type item struct{ start, end int }
	var run []item
	flush := func() {
		defer func() { run = nil }()
		if len(run) < 2 {
			return
		}
		votes := map[layoutKind]int{}
		covered := 0
		for _, it := range run {
			if s, ok := st.covering(line, it.start, it.end); ok {
				votes[layoutKindOf(s.Tag)]++
				covered++
			}
		}
		if covered == 0 || covered*3 < len(run) {
			return
		}
		best, bestN := layoutOrg, -1
		for k, n := range votes {
			if n > bestN || n == bestN && k < best {
				best, bestN = k, n
			}
		}
		for _, it := range run {
			if _, ok := st.covering(line, it.start, it.end); ok {
				continue
			}
			v := body[it.start:it.end]
			if layoutAllCommon(v) || layoutFieldWordsIn(strings.Fields(v)[0]) {
				continue
			}
			kind := best
			if kind != layoutPerson {
				kind = layoutOrgKind(v)
				if kind == layoutCompany && best != layoutCompany {
					kind = best
				}
			}
			st.addName(line, it.start, it.end, kind)
		}
	}
	for _, pr := range phys {
		text := body[pr[0]:pr[1]]
		bl := layoutBullet.FindStringIndex(text)
		off := 0
		if bl != nil {
			off = bl[1]
		}
		rest := text[off:]
		m := layoutListHead.FindStringIndex(rest)
		if m == nil || strings.TrimSpace(rest) == "" {
			flush()
			continue
		}
		head := strings.TrimRight(rest[:m[1]], " &")
		tail := strings.TrimSpace(rest[len(head):])
		if bl == nil && (len(strings.Fields(rest)) > 5 || strings.ContainsAny(tail, ".?!")) {
			flush()
			continue
		}
		if len(strings.Fields(head)) > 5 {
			flush()
			continue
		}
		run = append(run, item{pr[0] + off, pr[0] + off + len(head)})
	}
	flush()
}

var layoutOrgTail = map[string]bool{
	"clinic": true, "clinics": true, "klinik": true, "kliniği": true, "hospital": true, "hastanesi": true, "group": true,
	"digital": true, "inc": true, "inc.": true, "llc": true, "ltd": true, "ltd.": true, "gmbh": true, "agency": true,
	"ajans": true, "health": true, "healthcare": true, "medical": true, "center": true, "centre": true, "labs": true,
	"studio": true, "co": true, "co.": true, "corp": true, "corp.": true,
}

func (st *layoutState) orgCores() {
	for _, v := range sortedKeys(st.known) {
		t := st.known[v]
		p, ok := dictionary.parse(t)
		if !ok || p.Kind != tagEntity {
			continue
		}
		words := strings.Fields(strings.NewReplacer(",", " ").Replace(v))
		for len(words) >= 2 && layoutOrgTail[strings.ToLower(words[len(words)-1])] {
			words = words[:len(words)-1]
			core := strings.Join(words, " ")
			if !strings.HasPrefix(v, core) {
				break
			}
			if len(words) == 1 && (layoutGenericOrg[strings.ToLower(core)] || layoutPlaces[strings.ToLower(core)] || isCommonWord(core) || utf8.RuneCountInString(core) < 3) {
				continue
			}
			if _, exists := st.known[core]; !exists {
				st.known[core] = t
				st.origin[core] = "core"
			}
		}
	}
}

func (st *layoutState) propagate() {
	st.orgCores()
	type kv struct{ value, tag string }
	var list []kv
	for _, v := range sortedKeys(st.known) {
		t := st.known[v]
		if utf8.RuneCountInString(v) < 3 || !unicode.IsUpper(firstRune(v)) && !unicode.IsDigit(firstRune(v)) || strings.ContainsAny(v, "{}\n") {
			continue
		}
		if layoutAllCommon(v) || !st.propagatable(v) {
			continue
		}
		list = append(list, kv{v, t})
	}
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && (len(list[j].value) > len(list[j-1].value) || len(list[j].value) == len(list[j-1].value) && list[j].value < list[j-1].value); j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
	for line, body := range st.bodies {
		lower := strings.ToLower(body)
		for _, e := range list {
			needle, hay := e.value, body
			fold := utf8.RuneCountInString(e.value) >= 5 && !strings.ContainsAny(e.value, " \t") && len(strings.ToLower(e.value)) == len(e.value) && len(lower) == len(body)
			if fold {
				needle, hay = strings.ToLower(e.value), lower
			}
			from := 0
			for {
				idx := strings.Index(hay[from:], needle)
				if idx < 0 {
					break
				}
				s := from + idx
				end := s + len(needle)
				from = end
				if !st.add(line, s, end, e.tag) && utf8.RuneCountInString(e.value) >= 5 {
					ext := end
					for ext < len(body) && ext-end < 8 {
						r, size := utf8.DecodeRuneInString(body[ext:])
						if !unicode.IsLower(r) {
							break
						}
						ext += size
					}
					if ext > end && utf8.RuneCountInString(body[end:ext]) <= 4 {
						st.add(line, s, ext, e.tag)
					}
				}
			}
		}
	}
}

func (st *layoutState) propagatable(v string) bool {
	switch st.origin[v] {
	case "", "greetings", "honorifics", "keyValues", "signatures", "personLists", "core", "senders":
		return true
	}
	return len(strings.Fields(v)) >= 2
}

func layoutPersonHead(head string) bool {
	for _, w := range strings.Fields(head) {
		if utf8.RuneCountInString(w) > 1 && strings.ToUpper(w) == w {
			return false
		}
		if layoutGenericOrg[strings.ToLower(w)] {
			return false
		}
	}
	return true
}

func layoutShortDescription(rest string) bool {
	words := strings.Fields(rest)
	if len(words) == 0 || len(words) > 14 {
		return false
	}
	trimmed := strings.TrimRight(strings.TrimSpace(rest), ".")
	return !strings.ContainsAny(trimmed, ".?!")
}

func layoutAllCapitalized(t string) bool {
	for _, w := range strings.Fields(t) {
		lw := strings.ToLower(w)
		if lw == "of" || lw == "de" || lw == "&" || lw == "-" || lw == "–" {
			continue
		}
		r := firstRune(w)
		if !unicode.IsUpper(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

var layoutPlaces = map[string]bool{
	"istanbul": true, "i̇stanbul": true, "ankara": true, "izmir": true, "antalya": true, "turkey": true, "türkiye": true,
	"turkiye": true, "london": true, "paris": true, "berlin": true, "dubai": true, "nyc": true, "sf": true, "la": true,
	"america": true, "american": true, "usa": true, "us": true, "uk": true, "africa": true, "african": true, "europe": true,
	"european": true, "asia": true, "india": true, "nigeria": true, "kenya": true, "ghana": true, "mexico": true,
	"california": true, "texas": true, "florida": true, "brooklyn": true, "manhattan": true, "boston": true, "chicago": true,
}

const layoutMonths = `(?:jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|june?|july?|aug(?:ust)?|sep(?:t|tember)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?|ocak|şubat|mart|nisan|mayıs|haziran|temmuz|ağustos|eylül|ekim|kasım|aralık)`

var (
	layoutDateValue = regexp.MustCompile(`(?i)\b(?:` + layoutMonths + `\.? \d{1,2}(?:st|nd|rd|th)?(?:,? \d{4})?|\d{1,2}(?:st|nd|rd|th)?(?: of)? ` + layoutMonths + `(?:,? \d{4})?|\d{1,2}[./]\d{1,2}[./]\d{2,4}|\d{4}-\d{2}-\d{2})\b`)
	layoutItinLine  = regexp.MustCompile(`^[\t ]*(?:[-•*–·]|\p{So})?[\t ]*\**[\t ]*((?:(?:mon|tue|wed|thu|fri|sat|sun)[a-z]*,? )?(?:\p{L}+\.? \d{1,2}(?:st|nd|rd|th)?(?:,? \d{4})?|\d{1,2}(?:st|nd|rd|th)? \p{L}+|\d{1,2}[./]\d{1,2}(?:[./]\d{2,4})?))(?:[\t ]*\([^)]*\))?\**[\t ]*(?::|[\t ]-|[\t ]–)\**[\t ]*(.+)$`)
	layoutMonthWord = regexp.MustCompile(`(?i)^` + layoutMonths + `$`)
)

var layoutDateKeys = map[string]bool{
	"date": true, "tarih": true, "surgery date": true, "operation date": true, "ameliyat tarihi": true, "arrival": true,
	"arrival date": true, "departure": true, "departure date": true, "check-in": true, "check in": true, "check-out": true,
	"check out": true, "dob": true, "date of birth": true, "doğum tarihi": true, "birthday": true, "when": true,
	"appointment": true, "appointment date": true, "randevu": true, "geliş tarihi": true, "dönüş tarihi": true,
}

func layoutDateTag(key string) string {
	k := strings.ToLower(key)
	switch {
	case strings.Contains(k, "birth") || k == "dob" || strings.Contains(k, "doğum"):
		return "{{PERSON_BIRTH_DATE}}"
	case strings.Contains(k, "surgery") || strings.Contains(k, "operation") || strings.Contains(k, "ameliyat") || strings.Contains(k, "procedure"):
		return "{{SURGERY_DATE}}"
	case strings.Contains(k, "depart") || strings.Contains(k, "dönüş") || strings.Contains(k, "return"):
		return "{{DEPARTURE_DATE}}"
	case strings.Contains(k, "arriv") || strings.Contains(k, "geliş") || strings.Contains(k, "pick-up") && strings.Contains(k, "airport"):
		return "{{ARRIVAL_DATE}}"
	case strings.Contains(k, "check-in") || strings.Contains(k, "check in"):
		return "{{CHECK_IN_DATE}}"
	case strings.Contains(k, "check-out") || strings.Contains(k, "check out"):
		return "{{CHECK_OUT_DATE}}"
	case strings.Contains(k, "appointment") || strings.Contains(k, "randevu"):
		return "{{APPOINTMENT_DATE}}"
	}
	return "{{DATE}}"
}

func layoutLooksLikeMonth(w string) bool {
	w = strings.ToLower(strings.TrimRight(w, "."))
	if layoutMonthWord.MatchString(w) {
		return true
	}
	if utf8.RuneCountInString(w) < 5 {
		return false
	}
	for _, m := range []string{"january", "february", "march", "april", "june", "july", "august", "september", "october", "november", "december", "haziran", "temmuz", "ağustos"} {
		if editDistance(w, m) <= 1 {
			return true
		}
	}
	return false
}

func (st *layoutState) itinerary(line int, body string) {
	phys := layoutPhysical(body)
	type hit struct{ start, end, restStart, restEnd int }
	var hits []hit
	for _, pr := range phys {
		text := body[pr[0]:pr[1]]
		m := layoutItinLine.FindStringSubmatchIndex(text)
		if m == nil {
			continue
		}
		date := text[m[2]:m[3]]
		ok := false
		for _, w := range strings.Fields(strings.NewReplacer(",", " ").Replace(date)) {
			if layoutLooksLikeMonth(w) {
				ok = true
			}
		}
		if !ok && !layoutPattern1.MatchString(date) {
			continue
		}
		hits = append(hits, hit{pr[0] + m[2], pr[0] + m[3], pr[0] + m[4], pr[0] + m[5]})
	}
	if len(hits) < 2 {
		return
	}
	for _, h := range hits {
		st.add(line, h.start, h.end, layoutDateTag(body[h.restStart:h.restEnd]))
	}
}

func (st *layoutState) learnSender(name, tag string) {
	p, ok := dictionary.parse(tag)
	if !ok || p.Kind != tagPerson || strings.ContainsAny(name, "0123456789+@") {
		return
	}
	words := strings.Fields(name)
	learn := func(v, attr string) {
		if utf8.RuneCountInString(v) < 3 || !unicode.IsUpper(firstRune(v)) || isCommonWord(v) {
			return
		}
		t := "{{" + p.role()
		if p.Number >= 2 {
			t += "_" + strconv.Itoa(p.Number)
		}
		t += "_" + attr + "}}"
		if !dictionary.valid(t) {
			return
		}
		if _, exists := st.known[v]; !exists {
			st.known[v] = t
			st.origin[v] = "senders"
		}
	}
	switch len(words) {
	case 0:
		return
	case 1:
		learn(words[0], "FIRST_NAME")
	default:
		learn(name, "FULL_NAME")
		learn(words[0], "FIRST_NAME")
		learn(words[len(words)-1], "LAST_NAME")
	}
}

func (st *layoutState) vocativeNames(fc refineInput) map[string]string {
	names := map[string]string{}
	consider := func(full, tag string) {
		p, ok := dictionary.parse(tag)
		if !ok || p.Kind != tagPerson || strings.ContainsAny(full, "0123456789+@{") {
			return
		}
		words := strings.Fields(full)
		if len(words) == 0 || !unicode.IsUpper(firstRune(words[0])) || utf8.RuneCountInString(words[0]) < 2 {
			return
		}
		t := "{{" + p.role()
		if p.Number >= 2 {
			t += "_" + strconv.Itoa(p.Number)
		}
		t += "_FIRST_NAME}}"
		if dictionary.valid(t) {
			if _, ok := names[words[0]]; !ok {
				names[words[0]] = t
			}
		}
	}
	for _, name := range sortedKeys(fc.SenderTags) {
		consider(name, fc.SenderTags[name])
	}
	if !fc.IsGroup {
		consider(fc.Title, fc.TitleText)
	}
	for _, v := range sortedKeys(st.known) {
		consider(v, st.known[v])
	}
	return names
}

func (st *layoutState) vocatives(fc refineInput) {
	names := st.vocativeNames(fc)
	for _, name := range sortedKeys(names) {
		tag := names[name]
		q := regexp.QuoteMeta(name)
		patterns := []*regexp.Regexp{
			regexp.MustCompile(`(?m)^[\t ]*(` + q + `)[\t ]*,`),
			regexp.MustCompile(`(?m),[\t ]+(` + q + `)[\t ]*[.!?]*[\t ]*(?:$|\p{So})`),
			regexp.MustCompile(`(?m)[a-z][\t ]+(` + q + `)[\t ]*[.!?]+[\t ]*$`),
		}
		for line, body := range st.bodies {
			if !strings.Contains(body, name) {
				continue
			}
			for _, re := range patterns {
				for _, m := range re.FindAllStringSubmatchIndex(body, -1) {
					st.add(line, m[2], m[3], tag)
				}
			}
		}
	}
}

var layoutAttribution = regexp.MustCompile(`^[\t ]*(?:[—–]|--|~)[\t ]*(` + layoutCap + `(?:[ \t]+` + layoutCap + `){0,3})[\t ]*$`)

func (st *layoutState) attribution(line int, body string) {
	for _, pr := range layoutPhysical(body) {
		text := body[pr[0]:pr[1]]
		m := layoutAttribution.FindStringSubmatchIndex(text)
		if m == nil {
			continue
		}
		v := text[m[2]:m[3]]
		if layoutAllCommon(v) || layoutFieldWordsIn(v) {
			continue
		}
		st.addName(line, pr[0]+m[2], pr[0]+m[3], layoutPerson)
	}
}

type layoutColumn int

const (
	layoutColNone layoutColumn = iota
	layoutColPerson
	layoutColOrg
	layoutColPhone
	layoutColEmail
	layoutColDate
	layoutColAddress
)

func layoutColumnKind(header string) layoutColumn {
	h := strings.ToLower(strings.TrimSpace(header))
	switch {
	case strings.Contains(h, "email") || strings.Contains(h, "e-mail") || h == "mail":
		return layoutColEmail
	case strings.Contains(h, "phone") || strings.Contains(h, "telefon") || strings.Contains(h, "mobile") || h == "tel":
		return layoutColPhone
	case strings.Contains(h, "date") || strings.Contains(h, "tarih"):
		return layoutColDate
	case strings.Contains(h, "address") || strings.Contains(h, "adres"):
		return layoutColAddress
	case strings.Contains(h, "clinic") || strings.Contains(h, "company") || strings.Contains(h, "hospital") || strings.Contains(h, "klinik") || strings.Contains(h, "firma") || strings.Contains(h, "organi"):
		return layoutColOrg
	case strings.Contains(h, "name") || strings.Contains(h, "isim") || strings.Contains(h, "ad soyad") || strings.Contains(h, "contact") || strings.Contains(h, "patient") || strings.Contains(h, "person"):
		return layoutColPerson
	}
	return layoutColNone
}

func layoutSplitCells(text string, sep string) [][2]int {
	var cells [][2]int
	start := 0
	for {
		idx := strings.Index(text[start:], sep)
		if idx < 0 {
			cells = append(cells, [2]int{start, len(text)})
			return cells
		}
		cells = append(cells, [2]int{start, start + idx})
		start += idx + len(sep)
	}
}

func (st *layoutState) tables(line int, body string) {
	phys := layoutPhysical(body)
	for _, sep := range []string{"|", "\t", ";", ","} {
		var columns []layoutColumn
		for _, pr := range phys {
			text := body[pr[0]:pr[1]]
			cells := layoutSplitCells(text, sep)
			if len(cells) < 3 {
				if strings.TrimSpace(text) != "" && !strings.HasSuffix(strings.TrimSpace(text), ":") {
					columns = nil
				}
				continue
			}
			longest := 0
			for _, c := range cells {
				longest = max(longest, len(strings.Fields(text[c[0]:c[1]])))
			}
			if longest > 6 {
				columns = nil
				continue
			}
			kinds := make([]layoutColumn, len(cells))
			typed := 0
			for k, c := range cells {
				kinds[k] = layoutColumnKind(text[c[0]:c[1]])
				if kinds[k] != layoutColNone {
					typed++
				}
			}
			if longest > 4 {
				typed = 0
			}
			if typed >= 2 && typed*2 >= len(cells) {
				columns = kinds
				continue
			}
			if columns == nil || len(cells) > len(columns)+1 {
				continue
			}
			for k, c := range cells {
				if k >= len(columns) {
					break
				}
				cs, ce := pr[0]+c[0], pr[0]+c[1]
				cell := body[cs:ce]
				trim := strings.TrimSpace(cell)
				if trim == "" || strings.HasPrefix(trim, "<") || strings.HasPrefix(trim, "[") {
					continue
				}
				cs += strings.Index(cell, trim)
				ce = cs + len(trim)
				switch columns[k] {
				case layoutColPerson:
					st.namesIn(line, cs, ce, layoutPerson)
				case layoutColOrg:
					if unicode.IsUpper(firstRune(trim)) && len(strings.Fields(trim)) <= 6 {
						st.addName(line, cs, ce, layoutOrgKind(trim))
					}
				case layoutColPhone:
					if p := layoutPhone.FindStringIndex(trim); p != nil {
						st.add(line, cs+p[0], cs+p[1], "{{PHONE_NUMBER}}")
					}
				case layoutColEmail:
					if e := layoutEmail.FindStringIndex(trim); e != nil {
						st.add(line, cs+e[0], cs+e[1], "{{EMAIL_ADDRESS}}")
					}
				case layoutColDate:
					if d := layoutDateValue.FindStringIndex(trim); d != nil {
						st.add(line, cs+d[0], cs+d[1], "{{DATE}}")
					}
				case layoutColAddress:
					if strings.ContainsAny(trim, "0123456789") {
						st.add(line, cs, ce, "{{ADDRESS}}")
					}
				}
			}
		}
	}
}

var (
	layoutPattern1 = regexp.MustCompile(`^\d{1,2}[./]\d{1,2}`)
)
