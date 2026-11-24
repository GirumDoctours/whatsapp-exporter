package main

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type mentionToken struct {
	start, end int
	text       string
	transcript bool
}

type mentionEntity struct {
	tag   string
	rank  int
	risky bool
}

type mentionState struct {
	fc       refineInput
	bodies   []string
	senders  []string
	hasSend  []bool
	tokens   [][]mentionToken
	covered  []map[int]bool
	slots    []slot
	known    map[string]mentionEntity
	used     map[string]bool
	roleOf   map[string]parsedTag
	nextNums map[string]int
	rule     string
	third    bool
	lowerCnt map[string]int
	midCap   map[string]int
}

var mentionStop = mentionSet(`a an the and or but if so to of in on at by for with from as is are was were be been am do does did have has had i me my we us our you your he him his she her they them their it its this that these those there here what who whom which when where why how all any some no not yes ok okay okey thanks thank thx ty hi hey hello hii dear guys everyone everybody all folks team friends friend family people sir madam mam maam bro brother sis sister man dude mate buddy love babe baby honey sweetie doc doctor dr mr mrs ms miss boss chief captain coach prof professor morning evening night afternoon day good great nice cool awesome perfect sure yeah yep nope please pls plz sorry welcome congrats congratulations happy merry new best regards cheers bye see soon later again also just still very really much many more most well back here now today tomorrow tonight yesterday week weekend month year monday tuesday wednesday thursday friday saturday sunday january february march april may june july august september october november december can will would could should shall might must let lets get got go going come coming came done make made take took give gave say said tell told know knew think thought want wanted need needed like liked looking look feel try trying sending send sent call called calling meet meeting talk talking update updates info question questions one two three first last next other another every each both either neither same such own only even ever never always already yet still lol haha hahaha omg wow oh ah uh um hmm btw fyi asap ps admin group chat whatsapp message messages photo video image audio voice note document link file deleted edited attached omitted transcript media missed sticker gif location contact poll you're i'm im it's thats that's we're they're don't dont can't cant won't wont didn't isn't ill i'll i've ive id i'd hope wish glad excited sorry ready fine free busy online available interested on off up down out over under about after before between into through during without within along across behind beyond plus except via per vs etc everyone's anyone someone something anything nothing everything somebody anybody nobody whoever whatever whichever ladies gentlemen gents all y'all yall kids son daughter mom mum dad mother father wife husband partner uncle aunt cousin grandma grandpa sire hoca hocam abi abla bey hanım hanim efendi kardeşim kardesim arkadaşlar arkadaslar herkes hepiniz hepinize merhaba selam selamlar günaydın gunaydin iyi akşamlar aksamlar geceler teşekkürler tesekkurler teşekkür tesekkur sağol sagol sağolun sagolun rica ederim ben sen o biz siz onlar bu şu su ve ile de da ki mi mu mı mü için icin evet hayır hayir tamam peki olur var yok çok cok güzel guzel nasılsın nasilsin nasılsınız nasilsiniz hoşgeldin hosgeldin hoşgeldiniz hosgeldiniz canım canim dostum hocalar abim ablam yenge amca teyze dayı dayi hala anne baba eşim esim kardeş kardes unknown whomever whoever anyone someone`)

var mentionCommonLower = mentionSet(`able about above across act add age ago agree air all almost alone along already also always among amount and angel april art autumn baker bank bell berry bill bishop black blake bloom blue bond brook brown bush butler can carter case chase cherry child christian clay cliff cole cook crystal dale dawn day dean destiny diamond dot drew duke earl ebony eden ella faith fisher fortune fox frank gay gene gift glen gold grace grant gray green guy hall hardy harmony hart haven hazel heath heaven hill holly honor hope hunter iris ivory ivy jack jade jay jewel joy june justice king lane lark lee lily lucky major mark marsh mason may melody mercy miles miller moon morgan nick noble north oak ocean olive page park parker pat patience pearl penny pepper porter price prince queen rain ray read reed rich river robin rock rose ruby rush sage sandy savannah scout shepherd sky smith snow sonny star sterling stone storm summer sunny swift taylor terry tiger trinity tucker victory violet wade walker ward warren water wells west white will win winter wood worth wren young`)

var mentionGroup = mentionSet(`founders founder members member builders fam family friends team crew gang squad besties people humans ladies gentlemen guys girls boys peeps everyone fellows colleagues partners investors folks community world africa diaspora english turkish american african american british german french spanish italian arabic russian chinese japanese korean indian ethiopian amharic nigerian kenyan ghanaian european asian canadian mexican brazilian dutch greek persian kurdish hebrew jewish muslim christian islamic catholic god allah jesus lord`)

func mentionSet(words string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(words) {
		m[w] = true
	}
	return m
}

func mentionFold(s string) string {
	s = strings.ToLower(s)
	r := strings.NewReplacer("ı", "i", "ğ", "g", "ş", "s", "ç", "c", "ö", "o", "ü", "u", "â", "a", "î", "i", "û", "u", "é", "e", "è", "e", "á", "a", "í", "i", "ó", "o", "ú", "u", "ñ", "n", "̇", "")
	return r.Replace(s)
}

func mentionTokenize(body string) []mentionToken {
	var out []mentionToken
	transcriptFrom := -1
	if i := strings.Index(body, transcriptPrefix); i >= 0 {
		transcriptFrom = i
	}
	start := -1
	for i, r := range body {
		isL := unicode.IsLetter(r) || unicode.Is(unicode.Mn, r)
		if isL && start < 0 {
			start = i
		}
		if !isL && start >= 0 {
			out = append(out, mentionToken{start: start, end: i, text: body[start:i], transcript: transcriptFrom >= 0 && start > transcriptFrom})
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, mentionToken{start: start, end: len(body), text: body[start:], transcript: transcriptFrom >= 0 && start > transcriptFrom})
	}
	return out
}

func mentionIsCap(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsUpper(r) || (unicode.IsLetter(r) && !unicode.IsLower(r) && !unicode.IsUpper(r))
}

func mentionAllCaps(s string) bool {
	upper := false
	for _, r := range s {
		if unicode.IsLower(r) {
			return false
		}
		if unicode.IsUpper(r) {
			upper = true
		}
	}
	return upper
}

func mentionIsPhoneLabel(s string) bool {
	digits := 0
	for _, r := range s {
		if unicode.IsDigit(r) {
			digits++
		} else if !strings.ContainsRune("+ -()", r) {
			return false
		}
	}
	return digits >= 5
}

func mentionNameLike(tok string) bool {
	if utf8.RuneCountInString(tok) < 3 {
		return false
	}
	f := mentionFold(tok)
	if mentionStop[f] || mentionStop[strings.ToLower(tok)] || mentionGreet[f] || mentionGreet[tok] {
		return false
	}
	for _, r := range tok {
		if !unicode.IsLetter(r) && !unicode.Is(unicode.Mn, r) {
			return false
		}
	}
	return true
}

func mentionConvo(fc refineInput) []slot {
	doc := fc.parsed()
	st := &mentionState{fc: fc, known: map[string]mentionEntity{}, used: map[string]bool{}, roleOf: map[string]parsedTag{}, nextNums: map[string]int{}}
	for _, l := range doc.Lines {
		st.bodies = append(st.bodies, l.Body)
		st.senders = append(st.senders, l.Sender)
		st.hasSend = append(st.hasSend, l.HasSender)
		st.tokens = append(st.tokens, mentionTokenize(l.Body))
		st.covered = append(st.covered, map[int]bool{})
	}
	for _, s := range fc.Slots {
		st.used[s.Tag] = true
		if s.Line >= 0 && s.Line < len(st.bodies) {
			st.slots = append(st.slots, s)
			for i := s.Start; i < s.End; i++ {
				st.covered[s.Line][i] = true
			}
		} else {
			st.slots = append(st.slots, s)
		}
	}
	for label, tag := range fc.SenderTags {
		st.used[tag] = true
		if p, ok := dictionary.parse(tag); ok && p.Kind == tagPerson {
			st.roleOf[label] = p
		}
	}
	st.countCase()
	st.collectKnown()
	st.findPatterns()
	st.rule = "prop"
	st.propagate()
	st.rule = "entity"
	st.propagateEntities()
	st.rule = "surname"
	st.extendSurnames()
	st.rule = "prop2"
	st.propagate()
	return st.slots
}

func (st *mentionState) extendSurnames() {
	snapshot := append([]slot(nil), st.slots...)
	for _, s := range snapshot {
		if s.Line < 0 || s.Line >= len(st.bodies) {
			continue
		}
		p, ok := dictionary.parse(s.Tag)
		if !ok || p.Kind != tagPerson || p.Attribute != "FIRST_NAME" {
			continue
		}
		body := st.bodies[s.Line]
		for _, t := range st.tokens[s.Line] {
			if t.start < s.End {
				continue
			}
			if body[s.End:t.start] != " " {
				break
			}
			f := mentionFold(t.text)
			if e, ok := st.known[f]; ok {
				if q, ok2 := dictionary.parse(e.tag); ok2 && q.Attribute == "LAST_NAME" {
					st.add(s.Line, t, e.tag)
				}
				break
			}
			if !mentionIsCap(t.text) || mentionAllCaps(t.text) || !mentionNameLike(t.text) || mentionPostVoc[f] || mentionGroup[f] || st.commonWord(t.text) {
				break
			}
			lw := strings.ToLower(t.text)
			bad := false
			for _, suf := range []string{"ly", "tion", "ment", "ness", "ing", "ity", "ive", "ful", "ous", "able", "ance", "ence", "ship", "ism", "ers", "ics", "ies"} {
				if strings.HasSuffix(lw, suf) {
					bad = true
				}
			}
			for pv := range mentionPostVoc {
				fp := mentionFold(pv)
				if strings.HasPrefix(f, fp) && len(f)-len(fp) <= 4 {
					bad = true
				}
			}
			if bad {
				break
			}
			tag := st.personTag(p, "LAST_NAME")
			if tag == "" {
				break
			}
			st.known[f] = mentionEntity{tag: tag, rank: 1}
			st.add(s.Line, t, tag)
			break
		}
	}
}

func (st *mentionState) propagateEntities() {
	type phrase struct {
		words []string
		tag   string
	}
	seen := map[string]bool{}
	var phrases []phrase
	for _, s := range st.fc.Slots {
		p, ok := dictionary.parse(s.Tag)
		if !ok || p.Kind == tagPerson || !strings.HasSuffix(p.Attribute, "NAME") {
			continue
		}
		var ws []string
		for _, t := range mentionTokenize(s.Value) {
			ws = append(ws, mentionFold(t.text))
		}
		if len(ws) == 0 || len(ws) > 5 {
			continue
		}
		if len(ws) == 1 && (utf8.RuneCountInString(ws[0]) < 4 || st.commonLoose(ws[0]) || mentionStop[ws[0]]) {
			continue
		}
		if mentionStop[ws[0]] || !mentionIsCap(s.Value) {
			continue
		}
		distinct := false
		for _, w := range ws {
			if utf8.RuneCountInString(w) >= 3 && !st.commonWord(w) {
				distinct = true
			}
		}
		if !distinct {
			continue
		}
		key := strings.Join(ws, " ")
		if seen[key] {
			continue
		}
		seen[key] = true
		phrases = append(phrases, phrase{ws, s.Tag})
	}
	sort.SliceStable(phrases, func(i, j int) bool { return len(phrases[i].words) > len(phrases[j].words) })
	for line, toks := range st.tokens {
		body := st.bodies[line]
		for i := range toks {
			for _, ph := range phrases {
				n := len(ph.words)
				if i+n > len(toks) {
					continue
				}
				match := true
				for k := 0; k < n; k++ {
					if mentionFold(toks[i+k].text) != ph.words[k] {
						match = false
						break
					}
					if k > 0 && strings.TrimSpace(body[toks[i+k-1].end:toks[i+k].start]) != "" && body[toks[i+k-1].end:toks[i+k].start] != "-" && body[toks[i+k-1].end:toks[i+k].start] != "&" && body[toks[i+k-1].end:toks[i+k].start] != " & " {
						match = false
						break
					}
				}
				if !match {
					continue
				}
				if !mentionIsCap(toks[i].text) {
					continue
				}
				t := mentionToken{start: toks[i].start, end: toks[i+n-1].end}
				t.text = body[t.start:t.end]
				if st.free(line, t) {
					for x := t.start; x < t.end; x++ {
						st.covered[line][x] = true
					}
					st.slots = append(st.slots, slot{Line: line, Start: t.start, End: t.end, Tag: ph.tag, Value: t.text, Category: "entity", Source: "s02:" + st.rule, Confidence: 0.3})
				}
				break
			}
		}
	}
}

func (st *mentionState) countCase() {
	st.lowerCnt, st.midCap = map[string]int{}, map[string]int{}
	for line, toks := range st.tokens {
		for _, t := range toks {
			if t.transcript {
				continue
			}
			f := mentionFold(t.text)
			if !mentionIsCap(t.text) {
				st.lowerCnt[f]++
			} else if !st.messageStart(st.bodies[line], t.start) {
				st.midCap[f]++
			}
		}
	}
}

func (st *mentionState) commonWord(tok string) bool {
	f := mentionFold(tok)
	if mentionStop[f] || mentionCommonLower[strings.ToLower(tok)] || mentionGroup[f] {
		return true
	}
	return st.lowerCnt[f] > st.midCap[f]
}

func (st *mentionState) commonLoose(tok string) bool {
	f := mentionFold(tok)
	if mentionStop[f] || mentionCommonLower[strings.ToLower(tok)] || mentionGroup[f] {
		return true
	}
	return st.lowerCnt[f] >= 3*st.midCap[f]+4
}

func (st *mentionState) commonPhrase(v string) bool {
	for _, t := range mentionTokenize(v) {
		if st.commonLoose(t.text) {
			return true
		}
	}
	return false
}

func (st *mentionState) personTag(p parsedTag, attr string) string {
	q := p
	q.Attribute = attr
	q = q.withNumber(q.Number)
	t := q.String()
	if !dictionary.valid(t) {
		return ""
	}
	return t
}

func (st *mentionState) addKnown(tok, tag string, rank int) {
	risky := false
	if !mentionNameLike(tok) {
		if rank < 3 || utf8.RuneCountInString(tok) < 3 || mentionGroup[mentionFold(tok)] {
			return
		}
		risky = true
	}
	f := mentionFold(tok)
	if e, ok := st.known[f]; ok && e.rank >= rank {
		return
	}
	st.known[f] = mentionEntity{tag: tag, rank: rank, risky: risky}
}

func (st *mentionState) addNameParts(value string, p parsedTag, rank int) {
	parts := []string{}
	for _, t := range mentionTokenize(value) {
		parts = append(parts, t.text)
	}
	if len(parts) == 0 {
		return
	}
	attrFor := func(i int) string {
		switch p.Attribute {
		case "FIRST_NAME", "LAST_NAME", "MIDDLE_NAME", "NICKNAME", "MAIDEN_NAME":
			return p.Attribute
		}
		if len(parts) == 1 {
			return "FIRST_NAME"
		}
		if i == 0 {
			return "FIRST_NAME"
		}
		if i == len(parts)-1 {
			return "LAST_NAME"
		}
		return "MIDDLE_NAME"
	}
	for i, part := range parts {
		if t := st.personTag(p, attrFor(i)); t != "" {
			st.addKnown(part, t, rank)
		}
	}
}

func (st *mentionState) collectKnown() {
	for _, label := range sortedKeys(st.roleOf) {
		p := st.roleOf[label]
		if mentionIsPhoneLabel(label) || (st.fc.IsGroup && label == st.fc.Title) {
			continue
		}
		st.addNameParts(label, p, 3)
	}
	for _, s := range st.fc.Slots {
		p, ok := dictionary.parse(s.Tag)
		if !ok || p.Kind != tagPerson {
			continue
		}
		switch p.Attribute {
		case "FIRST_NAME", "LAST_NAME", "FULL_NAME", "MIDDLE_NAME", "NICKNAME", "MAIDEN_NAME":
			if st.commonPhrase(s.Value) {
				continue
			}
			st.addNameParts(s.Value, p, 2)
		}
	}
}

func (st *mentionState) free(line int, t mentionToken) bool {
	for i := t.start; i < t.end; i++ {
		if st.covered[line][i] {
			return false
		}
	}
	return true
}

func (st *mentionState) add(line int, t mentionToken, tag string) {
	if tag == "" || !st.free(line, t) {
		return
	}
	for i := t.start; i < t.end; i++ {
		st.covered[line][i] = true
	}
	st.used[tag] = true
	st.slots = append(st.slots, slot{Line: line, Start: t.start, End: t.end, Tag: tag, Value: t.text, Category: "person", Source: "s02:" + st.rule, Confidence: 0.3})
}

func (st *mentionState) freshPerson(role string) parsedTag {
	base, _ := dictionary.parse("{{" + role + "_FIRST_NAME}}")
	n := st.nextNums[role]
	if n == 0 {
		n = 1
	}
	for {
		q := base.withNumber(n)
		taken := false
		for tag := range st.used {
			if p, ok := dictionary.parse(tag); ok && p.Kind == tagPerson && p.role() == role && p.Number == q.Number {
				taken = true
				break
			}
			if p, ok := dictionary.parse(tag); ok && p.Kind == tagPerson && p.role() == role && p.Number == 0 && n == 1 {
				taken = true
				break
			}
		}
		if !taken {
			st.nextNums[role] = n + 1
			return q
		}
		n++
	}
}

func (st *mentionState) senderRole(line int) (parsedTag, bool) {
	if !st.hasSend[line] {
		return parsedTag{}, false
	}
	p, ok := st.roleOf[st.senders[line]]
	return p, ok
}

func (st *mentionState) addresseeRole(line int, name string) (parsedTag, bool) {
	me := st.senders[line]
	others := []string{}
	for _, label := range sortedKeys(st.roleOf) {
		if label != me && !(st.fc.IsGroup && label == st.fc.Title) {
			others = append(others, label)
		}
	}
	if !st.fc.IsGroup && len(others) == 1 {
		p := st.roleOf[others[0]]
		if st.roleHasName(p) {
			return parsedTag{}, false
		}
		return p, true
	}
	first, _ := utf8.DecodeRuneInString(name)
	for _, label := range others {
		if utf8.RuneCountInString(strings.TrimSpace(label)) == 1 {
			r, _ := utf8.DecodeRuneInString(label)
			if unicode.ToLower(r) == unicode.ToLower(first) && !st.roleHasName(st.roleOf[label]) {
				return st.roleOf[label], true
			}
		}
	}
	for back := line - 1; back >= 0 && back >= line-4; back-- {
		if !st.hasSend[back] || st.senders[back] == me {
			continue
		}
		label := st.senders[back]
		if mentionIsPhoneLabel(label) {
			if p, ok := st.roleOf[label]; ok && !st.roleHasName(p) {
				return p, true
			}
		}
		break
	}
	return parsedTag{}, false
}

func (st *mentionState) roleHasName(p parsedTag) bool {
	for _, e := range st.known {
		if q, ok := dictionary.parse(e.tag); ok && q.role() == p.role() && q.Number == p.Number {
			return true
		}
	}
	return false
}

func (st *mentionState) bind(line int, t mentionToken, self bool) {
	if st.third {
		f := mentionFold(t.text)
		if e, ok := st.known[f]; ok {
			st.add(line, t, e.tag)
			return
		}
		tag := st.personTag(st.freshPerson("PERSON"), "FIRST_NAME")
		if tag == "" {
			return
		}
		st.known[f] = mentionEntity{tag: tag, rank: 1}
		st.add(line, t, tag)
		return
	}
	f := mentionFold(t.text)
	if e, ok := st.known[f]; ok {
		st.add(line, t, e.tag)
		return
	}
	var p parsedTag
	ok := false
	if self {
		p, ok = st.senderRole(line)
		if ok && st.roleHasName(p) {
			ok = false
		}
	} else {
		p, ok = st.addresseeRole(line, t.text)
	}
	if !ok {
		p = st.freshPerson("PERSON")
	}
	tag := st.personTag(p, "FIRST_NAME")
	if tag == "" {
		return
	}
	st.known[f] = mentionEntity{tag: tag, rank: 1}
	st.add(line, t, tag)
}

var mentionGreet = mentionSet(`hi hey hello hallo hola heya hiya yo dear thanks thank thx ty cheers bye congrats congratulations welcome morning evening night merhaba selam slm günaydın gunaydin teşekkürler tesekkurler teşekkür tesekkur sağol sagol sağolun sagolun hoşgeldin hosgeldin hoşgeldiniz hosgeldiniz kolay tebrikler canım canim sevgili değerli degerli sayın sayin selamnew tenayistilign ameseginalehu amesegnalehu ሰላም ሰላምታ ውድ አመሰግናለሁ እናመሰግናለን ጤና`)

var mentionPostVoc = mentionSet(`bey hanım hanim hocam hoca abi abla abey efendi kardeşim kardesim ağabey agabey`)

var mentionSelfLead = [][]string{
	{"i'm"}, {"i", "am"}, {"im"}, {"this", "is"}, {"it's"}, {"its"}, {"my", "name", "is"}, {"my", "name's"}, {"name", "is"}, {"benim", "adım"}, {"adım"}, {"ben"}, {"je", "suis"}, {"እኔ"}, {"ስሜ"}, {"me", "llamo"}, {"soy"}, {"ich", "bin"},
}

func (st *mentionState) findPatterns() {
	for line, toks := range st.tokens {
		body := st.bodies[line]
		words := st.wordsWithApos(body)
		for i, w := range words {
			lw := strings.ToLower(strings.ReplaceAll(w.text, "’", "'"))
			if mentionGreet[mentionFold(lw)] || mentionGreet[lw] {
				j := i + 1
				if lw == "thank" && j < len(words) && strings.ToLower(words[j].text) == "you" {
					j++
				}
				if (lw == "good" || lw == "iyi") && j < len(words) {
					j++
				}
				if j < len(words) && (strings.ToLower(words[j].text) == "so" || strings.ToLower(words[j].text) == "much") {
					continue
				}
				if j < len(words) && words[j].start-words[j-1].end <= 3 && !strings.ContainsAny(body[words[j-1].end:words[j].start], ".!?\n") {
					st.rule = "greet"
					st.tryName(line, words, j, false, true)
				}
			}
			if mentionPostVoc[mentionFold(lw)] || mentionPostVoc[lw] {
				if i > 0 && words[i].start-words[i-1].end <= 1 {
					st.rule = "postvoc"
					st.tryName(line, words, i-1, false, false)
				}
			}
			for _, lead := range mentionSelfLead {
				if i+len(lead) >= len(words) {
					continue
				}
				match := true
				for k, lw2 := range lead {
					if strings.ToLower(strings.ReplaceAll(words[i+k].text, "’", "'")) != lw2 {
						match = false
						break
					}
				}
				if !match {
					continue
				}
				if (lead[0] == "this" || lead[0] == "it's" || lead[0] == "its" || lead[0] == "ben") && !st.messageStart(body, words[i].start) {
					continue
				}
				j := i + len(lead)
				if strings.ContainsAny(body[words[j-1].end:words[j].start], ".!?\n,") {
					continue
				}
				st.rule = "self"
				st.tryName(line, words, j, true, true)
			}
			if role, ok := mentionHonor[strings.TrimSuffix(w.text, ".")]; ok && (i == 0 || !st.isHonor(body, words, i-1)) {
				st.rule = "honor"
				st.honorific(line, words, i, role)
			}
			if words[i].start > 0 && body[words[i].start-1] == '@' {
				st.rule = "at"
				st.tryName(line, words, i, false, false)
			}
		}
		_ = toks
		st.rule = "signoff"
		st.signOff(line, words)
		st.rule = "verb"
		st.verbCues(line, words)
		st.rule = "list"
		st.listNames(line)
		st.rule = "sys"
		st.systemNames(line)
	}
}

var mentionHonor = map[string]string{
	"Dr": "DOCTOR", "dr": "DOCTOR", "DR": "DOCTOR", "Prof": "DOCTOR", "Doç": "DOCTOR", "Doc": "DOCTOR", "Uzm": "DOCTOR", "Op": "DOCTOR", "Dt": "DOCTOR", "Dra": "DOCTOR", "Dott": "DOCTOR",
	"Mr": "PERSON", "Mrs": "PERSON", "Ms": "PERSON", "Mx": "PERSON", "Mister": "PERSON", "Miss": "PERSON", "Hon": "PERSON", "Amb": "PERSON", "Sn": "PERSON", "Sayın": "PERSON", "Sr": "PERSON", "Sra": "PERSON", "Herr": "PERSON", "Frau": "PERSON", "Mme": "PERSON", "Mlle": "PERSON", "Ato": "PERSON", "Woizero": "PERSON", "Wro": "PERSON", "Sheikh": "PERSON", "Sir": "PERSON", "Madam": "PERSON",
}

func (st *mentionState) isHonor(body string, words []mentionToken, i int) bool {
	_, ok := mentionHonor[words[i].text]
	if !ok || i+1 >= len(words) {
		return false
	}
	gap := body[words[i].end:words[i+1].start]
	return gap == " " || gap == ". " || gap == "." || gap == "  "
}

func (st *mentionState) honorific(line int, words []mentionToken, i int, role string) {
	body := st.bodies[line]
	j := i
	for j < len(words) && st.isHonor(body, words, j) {
		j++
	}
	if j == i || j >= len(words) {
		return
	}
	var names []mentionToken
	for k := j; k < len(words) && len(names) < 2; k++ {
		t := words[k]
		if cut := strings.IndexAny(t.text, "'’"); cut > 0 {
			t.end = t.start + cut
			t.text = t.text[:cut]
		}
		if k > j {
			gap := body[words[k-1].end:t.start]
			if gap != " " {
				break
			}
		}
		if !mentionIsCap(t.text) || (mentionAllCaps(t.text) && utf8.RuneCountInString(t.text) > 1) || utf8.RuneCountInString(t.text) < 2 {
			break
		}
		if k > j && (st.commonWord(t.text) || !mentionNameLike(t.text)) {
			break
		}
		if k == j && (mentionStop[mentionFold(t.text)] || mentionGroup[mentionFold(t.text)] || st.lowerCnt[mentionFold(t.text)] > 2*st.midCap[mentionFold(t.text)]+2) {
			if _, ok := st.known[mentionFold(t.text)]; !ok {
				break
			}
		}
		names = append(names, t)
	}
	if len(names) == 0 {
		return
	}
	var p parsedTag
	found := false
	for _, n := range names {
		if e, ok := st.known[mentionFold(n.text)]; ok {
			if q, ok2 := dictionary.parse(e.tag); ok2 {
				p, found = q, true
				break
			}
		}
	}
	if !found {
		p = st.freshPerson(role)
	}
	for k, n := range names {
		if e, ok := st.known[mentionFold(n.text)]; ok {
			st.add(line, n, e.tag)
			continue
		}
		attr := "FIRST_NAME"
		if k == 1 || (len(names) == 1 && role == "DOCTOR") {
			attr = "LAST_NAME"
		}
		tag := st.personTag(p, attr)
		if tag == "" {
			continue
		}
		st.known[mentionFold(n.text)] = mentionEntity{tag: tag, rank: 1}
		st.add(line, n, tag)
	}
}

var mentionAfterVerb = mentionSet(`said says mentioned mentions told tells asked asks wants wanted thinks thought called calls texted replied replies responded confirmed confirms agreed agrees sent sends shared shares introduced directed recommended recommends suggested suggests knows knew met joined left added messaged emailed promised explained wrote writes will would can could should has had is was isn't wasn't hasn't didn't doesn't won't and`)

var mentionBeforeVerb = mentionSet(`ask asked tell told cc ping pinged call called text texted email emailed met meet introduce introduced intro thank thanked remind reminded inform informed contact contacted message messaged invite invited`)

var mentionPersonAfter = mentionSet(`said says mentioned mentions told tells asked asks wants wanted thinks thought texted replied responded confirmed agreed introduced directed recommended suggested knows knew promised explained`)

func (st *mentionState) strongName(tok string) bool {
	f := mentionFold(tok)
	if !mentionNameLike(tok) || !mentionIsCap(tok) || mentionAllCaps(tok) || mentionGroup[f] || mentionCommonLower[strings.ToLower(tok)] {
		return false
	}
	lw := strings.ToLower(tok)
	for _, suf := range []string{"ly", "tion", "sion", "ment", "ness", "ing", "ity", "ive", "ful", "ous", "able", "ible", "ance", "ence", "ship", "ism", "ware", "base", "ers", "ics", "ies"} {
		if strings.HasSuffix(lw, suf) {
			return false
		}
	}
	return st.lowerCnt[f] == 0
}

func (st *mentionState) verbCues(line int, words []mentionToken) {
	body := st.bodies[line]
	for i, w := range words {
		if !st.strongName(w.text) || !st.free(line, w) {
			continue
		}
		if _, ok := st.known[mentionFold(w.text)]; ok {
			continue
		}
		hit := false
		if i+1 < len(words) && body[w.end:words[i+1].start] == " " {
			nx := strings.ToLower(strings.ReplaceAll(words[i+1].text, "’", "'"))
			if mentionPersonAfter[nx] {
				hit = true
			}
			if (nx == "is" || nx == "will" || nx == "was" || nx == "has") && i > 0 && !st.messageStart(body, w.start) {
				pv := strings.ToLower(words[i-1].text)
				if (pv == "and" || pv == "but" || pv == "because" || pv == "that" || pv == "since" || pv == "ki") && (i < 2 || !st.strongName(words[i-2].text)) {
					hit = true
				}
			}
			if (nx == "is" || nx == "will") && st.messageStart(body, w.start) && i+2 < len(words) {
				n2 := strings.ToLower(words[i+2].text)
				if n2 == "doing" || n2 == "preoccupied" || n2 == "busy" || n2 == "coming" || n2 == "going" || n2 == "joining" || n2 == "travelling" || n2 == "traveling" || n2 == "sick" || n2 == "available" || n2 == "out" || n2 == "on" || n2 == "be" || n2 == "handle" || n2 == "send" || n2 == "call" {
					hit = true
				}
			}
		}
		if i > 0 && body[words[i-1].end:w.start] == " " {
			pv := strings.ToLower(words[i-1].text)
			if mentionBeforeVerb[pv] {
				hit = true
			}
			if i > 1 && (pv == "on" || pv == "for") && strings.ToLower(words[i-2].text) == "waiting" {
				hit = true
			}
			if i > 1 && pv == "and" && strings.ToLower(words[i-2].text) == "me" {
				hit = true
			}
			if i > 1 && pv == "is" && strings.ToLower(words[i-2].text) == "who" {
				hit = true
			}
		}
		if hit {
			st.rule = "verb2"
			st.third = true
			st.tryName(line, words, i, false, false)
			st.third = false
		}
	}
}

var mentionSysVerb = mentionSet(`added removed made created left joined changed`)

func (st *mentionState) systemNames(line int) {
	body := st.bodies[line]
	if !strings.HasPrefix(body, "\u200e") {
		return
	}
	toks := mentionTokenize(body)
	var groups [][]mentionToken
	var cur []mentionToken
	flush := func() {
		if len(cur) > 0 {
			groups = append(groups, cur)
		}
		cur = nil
	}
	seenVerb := false
	for k, t := range toks {
		lw := strings.ToLower(t.text)
		if mentionSysVerb[lw] {
			flush()
			if seenVerb {
				break
			}
			seenVerb = true
			continue
		}
		if lw == "and" || (k > 0 && strings.Contains(body[toks[k-1].end:t.start], ",")) {
			flush()
			if lw == "and" {
				continue
			}
		}
		if mentionIsCap(t.text) && !mentionAllCaps(t.text) && (mentionNameLike(t.text) || st.known[mentionFold(t.text)].tag != "") && len(cur) < 4 {
			cur = append(cur, t)
			continue
		}
		flush()
		if !seenVerb || lw == "group" || lw == "their" || lw == "using" {
			break
		}
	}
	flush()
	if !seenVerb {
		return
	}
	for _, g := range groups {
		var p parsedTag
		found := false
		for _, t := range g {
			if e, ok := st.known[mentionFold(t.text)]; ok {
				if q, ok2 := dictionary.parse(e.tag); ok2 {
					p, found = q, true
					break
				}
			}
		}
		if !found {
			p = st.freshPerson("PERSON")
		}
		for k, t := range g {
			if e, ok := st.known[mentionFold(t.text)]; ok {
				st.add(line, t, e.tag)
				continue
			}
			attr := "FIRST_NAME"
			if k > 0 {
				attr = "LAST_NAME"
			}
			tag := st.personTag(p, attr)
			if tag == "" {
				continue
			}
			st.known[mentionFold(t.text)] = mentionEntity{tag: tag, rank: 1}
			st.add(line, t, tag)
		}
	}
}

func (st *mentionState) listNames(line int) {
	body := st.bodies[line]
	off := 0
	for _, phys := range strings.Split(body, "\n") {
		start := off
		off += len(phys) + 1
		trimmed := strings.TrimSpace(phys)
		if trimmed == "" {
			continue
		}
		toks := mentionTokenize(phys)
		var name []mentionToken
		for _, t := range toks {
			if len(name) == len(toks) {
				break
			}
			name = append(name, t)
			if len(name) == 3 {
				break
			}
		}
		n := 0
		for n < len(toks) && n < 3 {
			if n > 0 && phys[toks[n-1].end:toks[n].start] != " " {
				break
			}
			if !st.strongName(toks[n].text) {
				break
			}
			n++
		}
		if n < 2 {
			continue
		}
		lead := strings.TrimSpace(phys[:toks[0].start])
		if strings.Trim(lead, "-•*·–—0123456789.) \u2060") != "" {
			continue
		}
		rest := strings.TrimSpace(phys[toks[n-1].end:])
		okEnd := rest == "" || strings.HasPrefix(rest, ":") || strings.HasPrefix(rest, "-") || strings.HasPrefix(rest, "–") || strings.HasPrefix(rest, ",") || strings.HasPrefix(rest, "(") || strings.HasPrefix(rest, "CV")
		if !okEnd {
			continue
		}
		p := st.freshPerson("PERSON")
		for k := 0; k < n; k++ {
			t := toks[k]
			t.start += start
			t.end += start
			if e, ok := st.known[mentionFold(t.text)]; ok {
				st.add(line, t, e.tag)
				continue
			}
			attr := "MIDDLE_NAME"
			if k == 0 {
				attr = "FIRST_NAME"
			} else if k == n-1 {
				attr = "LAST_NAME"
			}
			tag := st.personTag(p, attr)
			st.known[mentionFold(t.text)] = mentionEntity{tag: tag, rank: 1}
			st.add(line, t, tag)
		}
		st.used[st.personTag(p, "FIRST_NAME")] = true
	}
}

func (st *mentionState) messageStart(body string, pos int) bool {
	pre := strings.TrimSpace(body[:pos])
	if pre == "" {
		return true
	}
	last, _ := utf8.DecodeLastRuneInString(pre)
	return strings.ContainsRune(".!?,:\n", last) || unicode.IsSymbol(last) || last > 0x2000
}

func (st *mentionState) signOff(line int, words []mentionToken) {
	if len(words) == 0 {
		return
	}
	body := st.bodies[line]
	last := words[len(words)-1]
	pre := strings.TrimRight(body[:last.start], " \t")
	tail := strings.TrimSpace(body[last.end:])
	if tail != "" && strings.Trim(tail, ".!") != "" {
		return
	}
	if strings.HasSuffix(pre, "—") || strings.HasSuffix(pre, "–") || strings.HasSuffix(pre, "\n-") || strings.HasSuffix(pre, "\n~") {
		st.rule = "dash"
		st.tryName(line, words, len(words)-1, true, false)
		return
	}
	if strings.HasSuffix(pre, ",") && len(words) >= 2 {
		prev := strings.ToLower(words[len(words)-2].text)
		if mentionGreet[prev] || prev == "you" {
			st.rule = "comma"
			st.tryName(line, words, len(words)-1, false, true)
		}
	}
}

func (st *mentionState) wordsWithApos(body string) []mentionToken {
	toks := mentionTokenize(body)
	var out []mentionToken
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if i+1 < len(toks) {
			gap := body[t.end:toks[i+1].start]
			nxt := strings.ToLower(toks[i+1].text)
			if (gap == "'" || gap == "’") && (nxt == "m" || nxt == "s" || nxt == "re" || nxt == "ll" || nxt == "ve" || nxt == "d" || nxt == "t") {
				out = append(out, mentionToken{start: t.start, end: toks[i+1].end, text: body[t.start:toks[i+1].end], transcript: t.transcript})
				i++
				continue
			}
		}
		out = append(out, t)
	}
	return out
}

func (st *mentionState) tryName(line int, words []mentionToken, j int, self bool, allowLowerTranscript bool) {
	if j < 0 || j >= len(words) {
		return
	}
	t := words[j]
	if strings.ContainsAny(t.text, "'’") {
		return
	}
	if e, ok := st.known[mentionFold(t.text)]; ok && e.risky {
		if mentionIsCap(t.text) && !mentionAllCaps(t.text) {
			st.add(line, t, e.tag)
		}
		return
	}
	if !mentionNameLike(t.text) {
		return
	}
	cap := mentionIsCap(t.text) && !(mentionAllCaps(t.text) && utf8.RuneCountInString(t.text) > 1)
	if !cap {
		if !(t.transcript && allowLowerTranscript) {
			if _, ok := st.known[mentionFold(t.text)]; !ok {
				return
			}
		}
		if mentionCommonLower[strings.ToLower(t.text)] {
			return
		}
	}
	if !st.free(line, t) {
		return
	}
	if _, ok := st.known[mentionFold(t.text)]; !ok && st.commonWord(t.text) {
		return
	}
	st.bind(line, t, self)
	if j+1 < len(words) {
		n := words[j+1]
		if n.start-t.end == 1 && st.bodies[line][t.end] == ' ' && mentionIsCap(n.text) && !mentionAllCaps(n.text) && mentionNameLike(n.text) && cap && !st.commonWord(n.text) {
			if p, ok := dictionary.parse(st.known[mentionFold(t.text)].tag); ok {
				if _, exists := st.known[mentionFold(n.text)]; !exists {
					st.known[mentionFold(n.text)] = mentionEntity{tag: st.personTag(p, "LAST_NAME"), rank: 1}
				}
				st.add(line, n, st.known[mentionFold(n.text)].tag)
			}
		}
	}
}

func (st *mentionState) propagate() {
	for line, toks := range st.tokens {
		for _, t := range toks {
			e, ok := st.known[mentionFold(t.text)]
			if !ok || e.tag == "" {
				continue
			}
			cap := mentionIsCap(t.text)
			if e.risky {
				if cap && !mentionAllCaps(t.text) && (!st.messageStart(st.bodies[line], t.start) || strings.HasPrefix(st.bodies[line][t.end:], ",")) {
					st.add(line, t, e.tag)
				}
				continue
			}
			common := st.commonLoose(t.text)
			if !cap && (common || utf8.RuneCountInString(t.text) < 4) {
				continue
			}
			if cap && common && st.messageStart(st.bodies[line], t.start) {
				continue
			}
			if cap && common && mentionStop[mentionFold(t.text)] {
				continue
			}
			st.add(line, t, e.tag)
		}
	}
	sort.SliceStable(st.slots, func(i, j int) bool {
		if st.slots[i].Line != st.slots[j].Line {
			return st.slots[i].Line < st.slots[j].Line
		}
		return st.slots[i].Start < st.slots[j].Start
	})
}
