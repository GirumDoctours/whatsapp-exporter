package main

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	flagDisagree   = "the two AI passes disagreed"
	flagRebuilt    = "rebuilt after failing the template check"
	flagFallback   = "no specific tag fitted"
	flagWholeBody  = "the whole message was replaced because it could not be checked"
	flagRuleOnly   = "found by local checks only"
	flagRenumbered = "renumbered to keep tags consistent"
)

type slot struct {
	Line       int
	Start      int
	End        int
	Tag        string
	Value      string
	Category   string
	Source     string
	Confidence float64
	Flag       string
}

func templateMatch(original, redacted string) bool {
	locs := tagPattern.FindAllStringIndex(redacted, -1)
	if len(locs) == 0 {
		return original == redacted
	}
	var b strings.Builder
	b.WriteString(`(?s)^`)
	prev := 0
	for _, l := range locs {
		b.WriteString(regexp.QuoteMeta(redacted[prev:l[0]]))
		b.WriteString(`(.+?)`)
		prev = l[1]
	}
	b.WriteString(regexp.QuoteMeta(redacted[prev:]))
	b.WriteString(`$`)
	re, err := regexp.Compile(b.String())
	if err != nil {
		return false
	}
	return re.MatchString(original)
}

func tagsValid(redacted string, d *tagDictionary) bool {
	for _, tag := range tagPattern.FindAllString(redacted, -1) {
		if !d.valid(tag) {
			return false
		}
	}
	return true
}

func checkTemplate(original, redacted string, d *tagDictionary) bool {
	return tagsValid(redacted, d) && templateMatch(original, redacted)
}

func sortSlots(slots []slot) {
	sort.SliceStable(slots, func(i, j int) bool {
		if slots[i].Line != slots[j].Line {
			return slots[i].Line < slots[j].Line
		}
		return slots[i].Start < slots[j].Start
	})
}

func expandToTokens(body string, s slot) slot {
	isTokenRune := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
	if s.Start < 0 || s.End > len(body) || s.Start >= s.End || body[s.Start:s.End] != s.Value {
		return s
	}
	first, _ := utf8.DecodeRuneInString(body[s.Start:])
	for s.Start > 0 && isTokenRune(first) {
		r, size := utf8.DecodeLastRuneInString(body[:s.Start])
		if !isTokenRune(r) {
			break
		}
		s.Start -= size
	}
	last, _ := utf8.DecodeLastRuneInString(body[:s.End])
	for s.End < len(body) && isTokenRune(last) {
		r, size := utf8.DecodeRuneInString(body[s.End:])
		if !isTokenRune(r) {
			break
		}
		s.End += size
	}
	s.Value = body[s.Start:s.End]
	return s
}

func cleanSlots(body string, slots []slot, d *tagDictionary) []slot {
	for i := range slots {
		slots[i] = expandToTokens(body, slots[i])
	}
	sort.SliceStable(slots, func(i, j int) bool {
		if slots[i].Confidence != slots[j].Confidence {
			return slots[i].Confidence > slots[j].Confidence
		}
		return slots[i].End-slots[i].Start > slots[j].End-slots[j].Start
	})
	var kept []slot
	for _, s := range slots {
		if s.Start < 0 || s.End > len(body) || s.Start >= s.End || body[s.Start:s.End] != s.Value {
			continue
		}
		if !d.valid(s.Tag) {
			s.Tag = d.fallbackFor(s.Tag)
			s.Flag = firstNonEmpty(s.Flag, flagFallback)
		}
		clash := false
		for _, k := range kept {
			if s.Start < k.End && k.Start < s.End {
				clash = true
				break
			}
		}
		if !clash {
			kept = append(kept, s)
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Start < kept[j].Start })
	return kept
}

func applySlots(body string, slots []slot) string {
	var b strings.Builder
	prev := 0
	for _, s := range slots {
		b.WriteString(body[prev:s.Start])
		b.WriteString(s.Tag)
		prev = s.End
	}
	b.WriteString(body[prev:])
	return b.String()
}

type lineResult struct {
	body    string
	slots   []slot
	rebuilt bool
	whole   bool
}

func redactLine(original chatLine, senderTag string, slots []slot, d *tagDictionary) (chatLine, lineResult) {
	out := original
	if original.HasSender && senderTag != "" {
		out.Sender = senderTag
	}
	cleaned := cleanSlots(original.Body, slots, d)
	out.Body = applySlots(original.Body, cleaned)
	result := lineResult{body: out.Body, slots: cleaned}
	if checkTemplate(original.text(), out.text(), d) {
		return out, result
	}
	result.rebuilt = true
	for i := range cleaned {
		cleaned[i].Flag = firstNonEmpty(cleaned[i].Flag, flagRebuilt)
	}
	if original.Body != "" {
		whole := slot{Line: slots0Line(slots), Start: 0, End: len(original.Body), Tag: "{{OTHER_PII}}", Value: original.Body, Category: "OTHER", Source: "check", Confidence: 0.5, Flag: flagWholeBody}
		out.Body = whole.Tag
		result.slots = []slot{whole}
		result.whole = true
	}
	if !checkTemplate(original.text(), out.text(), d) {
		out.Sender = original.Sender
		if original.HasSender && senderTag != "" && d.valid(senderTag) {
			out.Sender = senderTag
		}
	}
	result.body = out.Body
	return out, result
}

func slots0Line(slots []slot) int {
	if len(slots) > 0 {
		return slots[0].Line
	}
	return 0
}

type consistencyFix struct {
	renumbered int
	retagged   int
	merged     int
	collisions int
}

var (
	honorificPrefix = regexp.MustCompile(`(?i)^(?:mr|mrs|ms|miss|mx|dr|prof|sir|madam|dear)\.?\s+`)
	possessiveTail  = regexp.MustCompile(`(?i)['’]s$`)
	urlQuery        = regexp.MustCompile(`[?#].*$`)
)

func normalizeValue(v string) string {
	v = strings.TrimSpace(v)
	if strings.Contains(v, "://") || strings.HasPrefix(strings.ToLower(v), "www.") {
		return strings.ToLower(urlQuery.ReplaceAllString(v, ""))
	}
	if digits := onlyDigits(v); len(digits) >= 7 && len(digits)*2 >= len(strings.ReplaceAll(v, " ", "")) {
		return strings.TrimLeft(digits, "0")
	}
	v = honorificPrefix.ReplaceAllString(v, "")
	v = possessiveTail.ReplaceAllString(v, "")
	v = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '@' || r == '.' {
			return unicode.ToLower(r)
		}
		return ' '
	}, v)
	return strings.Join(strings.Fields(v), " ")
}

func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

func sameEntityValue(a, b string) bool {
	if a == b {
		return true
	}
	if a == "" || b == "" {
		return false
	}
	if !hasLetter(a) || !hasLetter(b) || strings.ContainsAny(a+b, "@/") {
		da, db := onlyDigits(a), onlyDigits(b)
		return len(da) >= 8 && len(db) >= 8 && (strings.HasSuffix(da, db) || strings.HasSuffix(db, da))
	}
	limit := 1
	if utf8.RuneCountInString(a) >= 8 && utf8.RuneCountInString(b) >= 8 {
		limit = 2
	}
	if utf8.RuneCountInString(a) <= 3 || utf8.RuneCountInString(b) <= 3 {
		limit = 0
	}
	return editDistance(a, b) <= limit
}

var identityAttributes = setOf(strings.Fields("FIRST_NAME LAST_NAME FULL_NAME NICKNAME MIDDLE_NAME MAIDEN_NAME NAME"))

type entityGroup struct {
	base   string
	parsed map[int]parsedTag
}

func groupKey(p parsedTag) string {
	switch p.Kind {
	case tagPerson:
		return "P:" + p.role()
	case tagEntity:
		return "E:" + p.Base
	}
	return "S:" + p.Attribute
}

func enforceConsistency(slots []slot, d *tagDictionary) consistencyFix {
	var fix consistencyFix
	byValue := map[string]map[string]float64{}
	for _, s := range slots {
		key := normalizeValue(s.Value)
		if byValue[key] == nil {
			byValue[key] = map[string]float64{}
		}
		byValue[key][s.Tag] += max(s.Confidence, 0.01)
	}
	for i := range slots {
		tags := byValue[normalizeValue(slots[i].Value)]
		best, score := slots[i].Tag, -1.0
		for tag, w := range tags {
			if w > score || (w == score && tag < best) {
				best, score = tag, w
			}
		}
		if best != slots[i].Tag {
			slots[i].Tag = best
			fix.retagged++
		}
	}

	type member struct {
		idx    int
		parsed parsedTag
		value  string
	}
	groups := map[string][]member{}
	var order []string
	for i, s := range slots {
		p, ok := d.parse(s.Tag)
		if !ok || (p.Kind == tagStandalone && p.Fallback) {
			continue
		}
		key := groupKey(p)
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], member{idx: i, parsed: p, value: normalizeValue(s.Value)})
	}
	for _, key := range order {
		members := groups[key]
		first := members[0].parsed
		owner := first.Kind == tagPerson && first.Base == "OWNER" && first.Relation == ""
		dates := first.Kind == tagStandalone && (strings.HasSuffix(first.Attribute, "DATE") || first.Attribute == "DATE")
		if owner || dates {
			for _, m := range members {
				if m.parsed.Number >= 2 {
					slots[m.idx].Tag = m.parsed.withNumber(0).String()
					fix.merged++
				}
			}
			continue
		}
		used := map[int]bool{}
		for _, m := range members {
			used[max(m.parsed.Number, 1)] = true
		}
		next := 1
		nextFree := func() int {
			for used[next] {
				next++
			}
			used[next] = true
			return next
		}
		byAttr := map[string]map[int][]string{}
		var attrs []string
		for _, m := range members {
			if byAttr[m.parsed.Attribute] == nil {
				byAttr[m.parsed.Attribute] = map[int][]string{}
				attrs = append(attrs, m.parsed.Attribute)
			}
		}
		for _, attr := range attrs {
			numbers := byAttr[attr]
			fuzzy := identityAttributes[attr]
			match := func(a, b string) bool {
				if fuzzy {
					return sameEntityValue(a, b)
				}
				return a == b || (!hasLetter(a) || !hasLetter(b)) && sameEntityValue(a, b)
			}
			for _, m := range members {
				if m.parsed.Attribute != attr {
					continue
				}
				n := max(m.parsed.Number, 1)
				target := 0
				for k, values := range numbers {
					for _, v := range values {
						if match(v, m.value) && (target == 0 || k < target) {
							target = k
						}
					}
				}
				switch {
				case target != 0:
				case len(numbers[n]) == 0:
					target = n
				default:
					target = nextFree()
					fix.renumbered++
					slots[m.idx].Flag = firstNonEmpty(slots[m.idx].Flag, flagRenumbered)
				}
				if target != n {
					if target < n {
						fix.merged++
					}
					slots[m.idx].Tag = m.parsed.withNumber(target).String()
				}
				numbers[target] = append(numbers[target], m.value)
			}
		}
	}
	fix.collisions = tagCollisions(slots, d)
	return fix
}

func tagCollisions(slots []slot, d *tagDictionary) int {
	values := map[string][]string{}
	for _, s := range slots {
		p, ok := d.parse(s.Tag)
		if !ok || p.Fallback || (p.Kind == tagPerson && p.Base == "OWNER" && p.Relation == "") || strings.HasSuffix(p.Attribute, "DATE") {
			continue
		}
		v := normalizeValue(s.Value)
		known := false
		for _, existing := range values[s.Tag] {
			if sameEntityValue(existing, v) {
				known = true
				break
			}
		}
		if !known {
			values[s.Tag] = append(values[s.Tag], v)
		}
	}
	n := 0
	for _, vs := range values {
		if len(vs) > 1 {
			n++
		}
	}
	return n
}

func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
