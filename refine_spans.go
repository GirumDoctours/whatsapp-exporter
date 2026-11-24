package main

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type spanKind int

const (
	spanOther spanKind = iota
	spanPerson
	spanEntity
)

var spanOrgSuffixes = setOf(strings.Fields(`
health healthcare clinic clinics klinik kliniği klinigi clinica clínica clinique klinikum poliklinik poliklinigi polikliniği
hastanesi hastane hospital hospitals hôpital ospedale krankenhaus
lab labs laboratory laboratories laboratuvarı laboratuvari
ltd limited inc incorporated llc llp lp plc gmbh ag sa sas sarl srl spa bv nv ab oy aş şti sti pty pvt corp corporation co company
group grup groupe gruppe gruppo holding holdings
hotel hotels otel oteli hotelі resort resorts suites inn hostel apartments residence residences palace
center centre centro centers merkezi merkez zentrum
foundation vakfı vakfi fund capital ventures partners associates
medical medikal med dental dentistry aesthetics aesthetic estetik cosmetic cosmetics tıp tip sağlık saglik
university üniversitesi universitesi universität universidad université institute institut enstitüsü academy akademi akademisi
studio studios technologies technology systems solutions agency ajans media network networks consulting
bank insurance pharma pharmaceuticals biotech ai app apps
restaurant bar cafe café gym fitness salon airways airlines motors logistics travel tours turizm tourism
international global
`))

var spanPostHonorific = regexp.MustCompile(`^(?i:bey|hanım|hanim|hoca|hocam|abi|abla|efendi|beyefendi|hanımefendi|san|sama)$`)

var spanWordAt = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}]*(?:[-&][\p{L}\p{N}]+)*\.?`)

var spanPossessiveTail = regexp.MustCompile(`(?:['’][\p{L}]{1,4})$`)

var spanWordFinder = regexp.MustCompile(`[\p{L}\p{N}]+`)

func spanKindOf(tag string) (spanKind, parsedTag) {
	p, ok := dictionary.parse(tag)
	if !ok {
		return spanOther, p
	}
	switch p.Kind {
	case tagPerson:
		switch p.Attribute {
		case "FIRST_NAME", "LAST_NAME", "FULL_NAME", "MIDDLE_NAME", "MAIDEN_NAME":
			return spanPerson, p
		}
	case tagEntity:
		if p.Attribute == "NAME" {
			return spanEntity, p
		}
	}
	return spanOther, p
}

func spanRetag(tag, attr string) string {
	p, ok := dictionary.parse(tag)
	if !ok || p.Kind != tagPerson {
		return tag
	}
	p.Attribute = attr
	p = p.withNumber(p.Number)
	if out := p.String(); dictionary.valid(out) {
		return out
	}
	return tag
}

func spanIsCap(w string) bool {
	r, _ := utf8.DecodeRuneInString(w)
	return unicode.IsUpper(r)
}

func spanIsCommon(w string) bool {
	return isCommonWord(strings.TrimRight(w, "."))
}

func spanNameLike(w string) bool {
	w = strings.TrimRight(w, ".")
	if utf8.RuneCountInString(w) < 2 || !spanIsCap(w) || spanIsCommon(w) {
		return false
	}
	for _, r := range w {
		if unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func spanNextWord(body string, pos int, allowComma bool) (int, int, bool) {
	i := pos
	if allowComma && i < len(body) && body[i] == ',' {
		i++
	}
	if i >= len(body) || body[i] != ' ' {
		return 0, 0, false
	}
	i++
	m := spanWordAt.FindString(body[i:])
	if m == "" {
		return 0, 0, false
	}
	end := i + len(m)
	if strings.HasSuffix(m, ".") && !spanOrgSuffixes[strings.ToLower(strings.TrimSuffix(m, "."))] && utf8.RuneCountInString(m) > 2 {
		end--
	}
	return i, end, true
}

func spanPrevWord(body string, pos int) (int, int, bool) {
	if pos < 2 || body[pos-1] != ' ' {
		return 0, 0, false
	}
	end := pos - 1
	start := end
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(body[:start])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			break
		}
		start -= size
	}
	if start == end {
		return 0, 0, false
	}
	return start, end, true
}

func spanSentenceStart(body string, start int) bool {
	t := strings.TrimRight(body[:start], " ")
	if t == "" {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(t)
	return strings.ContainsRune(".!?\n:;-—•*(\"“", r) || !unicode.IsPrint(r)
}

func spanFree(slots []slot, start, end int, self int) bool {
	for i, s := range slots {
		if i != self && start < s.End && s.Start < end {
			return false
		}
	}
	return true
}

func spanExtend(body string, slots []slot) []slot {
	for i := range slots {
		s := &slots[i]
		kind, p := spanKindOf(s.Tag)
		if kind == spanOther {
			continue
		}
		for step := 0; step < 4; step++ {
			a, b, ok := spanNextWord(body, s.End, kind == spanEntity)
			if !ok || !spanFree(slots, s.End, b, i) {
				break
			}
			w := body[a:b]
			lw := strings.ToLower(strings.TrimRight(w, "."))
			grow := false
			switch kind {
			case spanEntity:
				grow = spanOrgSuffixes[lw] || (spanNameLike(w) && body[s.End] == ' ')
			case spanPerson:
				if spanPostHonorific.MatchString(lw) {
					grow = true
				} else if spanNameLike(w) && strings.ToUpper(w) != w && body[s.End] == ' ' && (p.Attribute == "FIRST_NAME" || p.Attribute == "MIDDLE_NAME") {
					grow = true
					s.Tag = spanRetag(s.Tag, "FULL_NAME")
					p.Attribute = "FULL_NAME"
				}
			}
			if !grow {
				break
			}
			s.End = b
		}
		for step := 0; step < 2; step++ {
			a, b, ok := spanPrevWord(body, s.Start)
			if !ok || !spanFree(slots, a, s.Start, i) || spanSentenceStart(body, a) {
				break
			}
			w := body[a:b]
			grow := false
			switch kind {
			case spanEntity:
				grow = spanNameLike(w)
			case spanPerson:
				grow = spanNameLike(w) && (p.Attribute == "LAST_NAME" || p.Attribute == "MIDDLE_NAME")
				if grow {
					s.Tag = spanRetag(s.Tag, "FULL_NAME")
					p.Attribute = "FULL_NAME"
				}
			}
			if !grow {
				break
			}
			s.Start = a
		}
		s.Value = body[s.Start:s.End]
	}
	return slots
}

func spanMerge(body string, slots []slot) []slot {
	sort.Slice(slots, func(i, j int) bool { return slots[i].Start < slots[j].Start })
	var out []slot
	for _, s := range slots {
		if n := len(out); n > 0 {
			prev := &out[n-1]
			gap := body[min(prev.End, s.Start):s.Start]
			kp, pp := spanKindOf(prev.Tag)
			ks, _ := spanKindOf(s.Tag)
			if prev.End <= s.Start && (gap == " " || gap == "") && kp == ks && kp != spanOther {
				if kp == spanPerson && pp.Attribute != "FULL_NAME" {
					prev.Tag = spanRetag(prev.Tag, "FULL_NAME")
				}
				prev.End = s.End
				prev.Value = body[prev.Start:prev.End]
				continue
			}
		}
		out = append(out, s)
	}
	return out
}

type spanPhrase struct {
	text string
	tag  string
}

func spanStripTail(v string) string {
	v = strings.TrimSpace(v)
	v = spanPossessiveTail.ReplaceAllString(v, "")
	return strings.TrimRight(v, ".,;: ")
}

func spanDistinctive(v string) bool {
	words := spanWordFinder.FindAllString(v, -1)
	if len(words) == 0 {
		return false
	}
	ok := false
	for _, w := range words {
		if utf8.RuneCountInString(w) >= 3 && !isCommonWord(w) && !spanOrgSuffixes[strings.ToLower(w)] {
			ok = true
		}
	}
	return ok && utf8.RuneCountInString(v) >= 3
}

func spanPhrases(fc refineInput, slots []slot) []spanPhrase {
	seen := map[string]bool{}
	var out []spanPhrase
	add := func(text, tag string) {
		text = spanStripTail(text)
		key := strings.ToLower(text)
		if seen[key] || !spanDistinctive(text) || !dictionary.valid(tag) || strings.ToLower(text) == text {
			return
		}
		seen[key] = true
		out = append(out, spanPhrase{text, tag})
	}
	addParts := func(v, tag string, kind spanKind) {
		words := spanWordFinder.FindAllStringIndex(v, -1)
		switch kind {
		case spanPerson:
			if len(words) < 2 {
				return
			}
			for i, w := range words {
				t := v[w[0]:w[1]]
				if !spanNameLike(t) || utf8.RuneCountInString(t) < 3 {
					continue
				}
				attr := "MIDDLE_NAME"
				if i == 0 {
					attr = "FIRST_NAME"
				} else if i == len(words)-1 {
					attr = "LAST_NAME"
				}
				add(t, spanRetag(tag, attr))
			}
		case spanEntity:
			end := len(words)
			for end > 1 && spanOrgSuffixes[strings.ToLower(v[words[end-1][0]:words[end-1][1]])] {
				end--
			}
			if end < len(words) {
				add(v[words[0][0]:words[end-1][1]], tag)
			}
		}
	}
	for _, s := range slots {
		kind, _ := spanKindOf(s.Tag)
		if kind == spanOther {
			continue
		}
		add(s.Value, s.Tag)
		addParts(s.Value, s.Tag, kind)
	}
	for _, sender := range sortedKeys(fc.SenderTags) {
		tag := fc.SenderTags[sender]
		kind, p := spanKindOf(tag)
		if kind != spanPerson || p.Attribute != "FULL_NAME" || !hasLetter(sender) {
			continue
		}
		addParts(sender, tag, kind)
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i].text) > len(out[j].text) })
	return out
}

func spanBoundary(body string, start, end int) bool {
	if start > 0 {
		r, _ := utf8.DecodeLastRuneInString(body[:start])
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	if end < len(body) {
		r, _ := utf8.DecodeRuneInString(body[end:])
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func spanCaseOK(src, occ string) bool {
	if occ == src {
		return true
	}
	if spanIsCap(src) && !spanIsCap(occ) {
		return false
	}
	return true
}

func spanPropagate(body string, line int, slots []slot, phrases []spanPhrase) []slot {
	lower := strings.ToLower(body)
	for _, ph := range phrases {
		needle := strings.ToLower(ph.text)
		if len(lower) != len(body) {
			continue
		}
		from := 0
		for {
			k := strings.Index(lower[from:], needle)
			if k < 0 {
				break
			}
			start := from + k
			end := start + len(needle)
			from = start + 1
			if !spanBoundary(body, start, end) || !spanCaseOK(ph.text, body[start:end]) || !spanFree(slots, start, end, -1) {
				continue
			}
			slots = append(slots, slot{Line: line, Start: start, End: end, Tag: ph.tag, Value: body[start:end], Category: "OTHER", Source: "s01", Confidence: 0.6})
		}
	}
	for _, m := range spanWordFinder.FindAllStringIndex(body, -1) {
		w := body[m[0]:m[1]]
		if utf8.RuneCountInString(w) < 5 || isCommonWord(w) || !spanFree(slots, m[0], m[1], -1) {
			continue
		}
		if tag := spanFuzzyMatch(w, phrases); tag != "" {
			slots = append(slots, slot{Line: line, Start: m[0], End: m[1], Tag: tag, Value: w, Category: "OTHER", Source: "s01", Confidence: 0.55})
		}
	}
	return slots
}

var spanTurkishSuffixes = setOf(strings.Fields("ın in un ün nın nin nun nün a e ya ye na ne da de ta te dan den tan ten ndan nden la le yla yle ı i u ü yı yi yu yü nı ni nu nü lar ler ları leri"))

var spanFold = strings.NewReplacer("ş", "s", "ı", "i", "ç", "c", "ğ", "g", "ö", "o", "ü", "u", "â", "a", "î", "i", "û", "u", "é", "e", "è", "e", "á", "a", "í", "i", "ó", "o", "ú", "u", "ñ", "n")

func spanNorm(w string) string {
	return spanFold.Replace(strings.ToLower(w))
}

func spanFuzzyMatch(w string, phrases []spanPhrase) string {
	nw := spanNorm(w)
	for _, ph := range phrases {
		if strings.ContainsAny(ph.text, " -") || utf8.RuneCountInString(ph.text) < 5 {
			continue
		}
		if spanIsCap(ph.text) && !spanIsCap(w) {
			continue
		}
		np := spanNorm(ph.text)
		if nw == np {
			return ph.tag
		}
		if rest, ok := strings.CutPrefix(nw, np); ok && spanTurkishSuffixes[rest] {
			return ph.tag
		}
		limit := 1
		if utf8.RuneCountInString(np) >= 8 && utf8.RuneCountInString(nw) >= 8 {
			limit = 2
		}
		if utf8.RuneCountInString(nw) >= 6 && utf8.RuneCountInString(np) >= 6 && editDistance(nw, np) <= limit {
			return ph.tag
		}
	}
	return ""
}

func spanSpanRepair(fc refineInput) []slot {
	doc := fc.parsed()
	byLine := map[int][]slot{}
	var out []slot
	for _, s := range fc.Slots {
		if s.Line < 0 || s.Line >= len(doc.Lines) || s.Start < 0 || s.End > len(doc.Lines[s.Line].Body) || doc.Lines[s.Line].Body[s.Start:s.End] != s.Value {
			out = append(out, s)
			continue
		}
		byLine[s.Line] = append(byLine[s.Line], s)
	}
	for i := range doc.Lines {
		if len(byLine[i]) > 0 {
			byLine[i] = spanMerge(doc.Lines[i].Body, spanExtend(doc.Lines[i].Body, spanMerge(doc.Lines[i].Body, byLine[i])))
		}
	}
	spanDetect(fc, doc, byLine)
	var all []slot
	for _, ss := range byLine {
		all = append(all, ss...)
	}
	sortSlots(all)
	phrases := spanPhrases(fc, all)
	for i, l := range doc.Lines {
		ss := spanPropagate(l.Body, i, byLine[i], phrases)
		if len(ss) > len(byLine[i]) {
			ss = spanMerge(l.Body, spanExtend(l.Body, spanMerge(l.Body, ss)))
		}
		out = append(out, ss...)
	}
	sortSlots(out)
	return out
}

var spanFunctionWords = setOf(strings.Fields(`
many most every each several various another other others such whose also even only just soon sometimes often usually always never
general local official some any all both either neither no not without within under over upon onto via per
did does done do let lets get got make made like unlike near after before since until while though although
which what whoever whatever where when why how there here this that these those its it's his her their our my your
a an the and or but nor yet so if then else than as at by for from in into of on to with about
dear hi hey hello thanks welcome congrats congratulations happy merry
our their whole full entire real true same different big small large top best main major minor
private public international national regional global european american african asian turkish english
`))

var spanDescriptor = setOf(strings.Fields(`
usd eur euro euros gbp try tl aed sar chf jpy cny inr rub cad aud dollar dollars lira
dental medical hair aesthetic aesthetics plastic surgery surgical eye eyes beauty cosmetic skin health travel agency private
state city general family children women kids pet vet veterinary university teaching training research test demo sample
`))

var spanDemonym = regexp.MustCompile(`(?i)(ish|ese|ian|can|yan|ean)$`)

var spanStrongSuffix = map[string]string{
	"clinic": "CLINIC", "klinik": "CLINIC", "kliniği": "CLINIC", "klinigi": "CLINIC", "clinica": "CLINIC", "clínica": "CLINIC", "clinique": "CLINIC", "klinikum": "CLINIC", "polikliniği": "CLINIC", "poliklinik": "CLINIC",
	"hastanesi": "HOSPITAL", "hospital": "HOSPITAL",
	"hotel": "HOTEL", "otel": "HOTEL", "oteli": "HOTEL", "resort": "HOTEL", "suites": "HOTEL",
	"labs": "COMPANY", "inc": "COMPANY", "ltd": "COMPANY", "llc": "COMPANY", "llp": "COMPANY", "gmbh": "COMPANY", "plc": "COMPANY",
	"group": "COMPANY", "grup": "COMPANY", "holding": "COMPANY",
	"capital": "FUND", "ventures": "FUND", "partners": "FUND",
}

var spanPreHonorific = map[string]string{
	"dr": "DOCTOR", "doç": "DOCTOR", "doc": "DOCTOR", "prof": "DOCTOR", "op": "DOCTOR", "uzm": "DOCTOR", "dt": "DOCTOR", "dra": "DOCTOR",
	"mr": "PERSON", "mrs": "PERSON", "ms": "PERSON", "miss": "PERSON", "mx": "PERSON", "sir": "PERSON", "hon": "PERSON", "amb": "PERSON",
	"herr": "PERSON", "frau": "PERSON", "sr": "PERSON", "sra": "PERSON", "mme": "PERSON", "mlle": "PERSON",
}

var spanToken = regexp.MustCompile(`[\p{L}\p{N}][\p{L}\p{N}&'’-]*\.?`)

func spanLead(w string) bool {
	w = strings.TrimRight(w, ".")
	lw := strings.ToLower(w)
	if utf8.RuneCountInString(w) < 2 || !spanIsCap(w) || isCommonWord(lw) || spanFunctionWords[lw] || spanPreHonorific[lw] != "" {
		return false
	}
	if strings.HasSuffix(lw, "ing") && strings.ToUpper(w) != w {
		return false
	}
	return !strings.ContainsAny(w, "'’")
}

type spanAlloc struct {
	max map[string]int
	got map[string]string
}

func spanNewAlloc(fc refineInput) *spanAlloc {
	a := &spanAlloc{max: map[string]int{}, got: map[string]string{}}
	note := func(tag string) {
		for _, t := range tagPattern.FindAllString(tag, -1) {
			if p, ok := dictionary.parse(t); ok && p.Kind != tagStandalone {
				a.max[p.role()] = max(a.max[p.role()], max(p.Number, 1))
			}
		}
	}
	for _, s := range fc.Slots {
		note(s.Tag)
	}
	for _, t := range fc.SenderTags {
		note(t)
	}
	note(fc.TitleText)
	return a
}

func (a *spanAlloc) tag(base, attr, key string, person bool) string {
	key = base + "|" + strings.ToLower(key)
	if t, ok := a.got[key]; ok {
		p, _ := dictionary.parse(t)
		p.Attribute = attr
		return p.withNumber(p.Number).String()
	}
	n := a.max[base] + 1
	a.max[base] = n
	kind := tagEntity
	if person {
		kind = tagPerson
	}
	t := parsedTag{Kind: kind, Base: base, Attribute: attr}.withNumber(n).String()
	a.got[key] = t
	return t
}

func spanDetect(fc refineInput, doc chatDoc, byLine map[int][]slot) {
	alloc := spanNewAlloc(fc)
	for i, l := range doc.Lines {
		body := l.Body
		toks := spanToken.FindAllStringIndex(body, -1)
		joined := func(a, b int) bool {
			return body[toks[a][1]:toks[b][0]] == " "
		}
		for j := 0; j < len(toks); j++ {
			w := strings.TrimRight(body[toks[j][0]:toks[j][1]], ".")
			lw := strings.ToLower(w)
			if base, ok := spanStrongSuffix[lw]; ok {
				k := j
				for k > 0 && j-k < 3 && joined(k-1, k) && spanLead(body[toks[k-1][0]:toks[k-1][1]]) && !strings.HasSuffix(body[toks[k-1][0]:toks[k-1][1]], ".") {
					k--
				}
				if k == j || (base != "CLINIC" && base != "HOSPITAL" && base != "HOTEL" && !spanIsCap(w)) {
					continue
				}
				distinct := false
				for q := k; q < j; q++ {
					lq := strings.ToLower(strings.TrimRight(body[toks[q][0]:toks[q][1]], "."))
					if !spanDescriptor[lq] && !spanOrgSuffixes[lq] {
						distinct = true
					}
				}
				if !distinct {
					continue
				}
				if j-k == 1 && spanDemonym.MatchString(body[toks[k][0]:toks[k][1]]) {
					continue
				}
				start, end := toks[k][0], toks[j][0]+len(w)
				if !spanFree(byLine[i], start, end, -1) {
					continue
				}
				core := body[toks[k][0]:toks[j-1][1]]
				t := alloc.tag(base, "NAME", core, false)
				if dictionary.valid(t) {
					byLine[i] = append(byLine[i], slot{Line: i, Start: start, End: end, Tag: t, Value: body[start:end], Category: "OTHER", Source: "s01", Confidence: 0.6})
				}
				continue
			}
			if base, ok := spanPreHonorific[lw]; ok {
				k := j + 1
				for k < len(toks) && spanPreHonorific[strings.ToLower(strings.TrimRight(body[toks[k][0]:toks[k][1]], "."))] != "" {
					k++
				}
				m := k
				for m < len(toks) && m-k < 2 && (m == k || joined(m-1, m)) && spanLead(body[toks[m][0]:toks[m][1]]) {
					m++
				}
				if m == k || (k > j+1 && !joined(k-1, k) && body[toks[k-1][1]:toks[k][0]] != " ") {
					continue
				}
				if gap := body[toks[k-1][1]:toks[k][0]]; gap != " " && gap != ". " && !(strings.HasSuffix(body[toks[k-1][0]:toks[k-1][1]], ".") && gap == " ") {
					continue
				}
				start := toks[k][0]
				end := toks[m-1][1]
				end = start + len(strings.TrimRight(body[start:end], "."))
				if !spanFree(byLine[i], start, end, -1) {
					continue
				}
				attr := "FULL_NAME"
				if m-k == 1 {
					attr = "FIRST_NAME"
					if base == "DOCTOR" {
						attr = "LAST_NAME"
					}
				}
				t := alloc.tag(base, attr, body[start:end], true)
				if dictionary.valid(t) {
					byLine[i] = append(byLine[i], slot{Line: i, Start: start, End: end, Tag: t, Value: body[start:end], Category: "OTHER", Source: "s01", Confidence: 0.6})
				}
				j = m - 1
				continue
			}
			if spanPostHonorific.MatchString(lw) && j > 0 && joined(j-1, j) {
				pw := body[toks[j-1][0]:toks[j-1][1]]
				lpw := strings.ToLower(pw)
				if utf8.RuneCountInString(pw) < 3 || isCommonWord(lpw) || spanFunctionWords[lpw] || strings.ContainsAny(pw, ".'’") {
					continue
				}
				start, end := toks[j-1][0], toks[j-1][1]
				if !spanFree(byLine[i], start, end+1+len(w), -1) {
					continue
				}
				t := alloc.tag("PERSON", "FIRST_NAME", pw, true)
				if dictionary.valid(t) {
					byLine[i] = append(byLine[i], slot{Line: i, Start: start, End: end, Tag: t, Value: pw, Category: "OTHER", Source: "s01", Confidence: 0.6})
				}
			}
		}
		for _, m := range spanGreeting.FindAllStringSubmatchIndex(body, -1) {
			start, end := m[2], m[3]
			w := body[start:end]
			lw := strings.ToLower(w)
			if isCommonWord(lw) || spanFunctionWords[lw] || spanDescriptor[lw] || spanOrgSuffixes[lw] || utf8.RuneCountInString(w) < 3 || !spanFree(byLine[i], start, end, -1) {
				continue
			}
			t := alloc.tag("PERSON", "FIRST_NAME", w, true)
			if dictionary.valid(t) {
				byLine[i] = append(byLine[i], slot{Line: i, Start: start, End: end, Tag: t, Value: w, Category: "OTHER", Source: "s01", Confidence: 0.55})
			}
		}
		sort.Slice(byLine[i], func(a, b int) bool { return byLine[i][a].Start < byLine[i][b].Start })
	}
}

var spanGreeting = regexp.MustCompile(`(?:^|[\s,.!])(?i:hi|hello|hey|dear|thanks|thank you|thx|merhaba|selam|selamlar|teşekkürler|tesekkurler|teşekkür ederim|günaydın|gunaydin|iyi akşamlar|hola|gracias|ciao|bonjour|salut|hallo|danke)[,!]?\s+(\p{Lu}[\p{Ll}\p{M}]+)(?:$|[\s,.!?:;'’])`)
