package main

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const cueName = `\p{Lu}[\p{Ll}\p{M}]+(?:-\p{Lu}[\p{Ll}\p{M}]+)?`

var cueNameToken = regexp.MustCompile(cueName)

var cueNotNames = setOf(strings.Fields(`
everyone everybody all team guys folks founders friends family there again both gentlemen ladies people
yall community members sir madam boss bro brother sister man dude buddy mate chief hocam
abi abla canım arkadaşlar herkes herkese ekip partner partners doc doctor so much you for back advance
chat group admin admins world earth god allah lord jesus inshallah mashallah ramadan eid christmas
bayram happy merry new year birthday congrats congratulations welcome sorry ok okay yes no thanks thank
hello hi hey dear morning evening night afternoon guys sirs madams beautiful amazing awesome great
patient patients clinic clinics hospital hotel company consultant client clients user users customer
customers manager director ceo cto coo founder cofounder investor investors sales marketing support
the a an this that these those my our your his her their its we they i he she it me us them
turkey türkiye turkiye istanbul ankara antalya izmir america usa us uk europe africa english turkish
american african european arabic spanish german french russian google meta whatsapp instagram facebook
linkedin youtube tiktok twitter apple microsoft amazon zoom gmail drive sheets docs claude chatgpt gpt
mr mrs ms miss dr prof bey hanım hanim beyi beye beyin hanımı hanıma hanımın hanımla abla abi hoca hocam
also since but however although though because therefore meanwhile anyway besides moreover furthermore
information info details detail media request requests note notes update updates reminder important
hair dental medical health aesthetic aesthetics plastic surgery beauty international private özel test
global smile care doctors este estetik cerrahi tıp merkezi center centre klinik clinic hospital medicine
general city park street road avenue university college school academy institute foundation
cv resume profesyonel professional social omg lol wow haha yay
san santa francisco york los angeles las vegas jersey silicon valley hong kong dhabi tel aviv saudi arabia
united states kingdom emirates south north east west central bay area washington boston chicago miami
seattle austin dallas houston atlanta denver london paris berlin madrid rome dubai cairo lagos nairobi
accra addis ababa kigali johannesburg toronto vancouver mexico bogota tokyo singapore bangkok seoul
business development coordination relations operations partnership partnerships marketing growth product
merhaba selam teşekkürler günaydın tebrikler club
`))

var cueGreeting = regexp.MustCompile(`(?:^|[^\p{L}])((?i:hi|hello|hey|heya|hiya|dear|thanks|thank you|thx|merhaba|selam|selamlar|sayın|günaydın|iyi günler|iyi akşamlar|teşekkürler|teşekkür ederim|sağol|sağolun|hola|gracias|bonjour|merci|hallo|danke|salam|salaam|marhaba|shukran|ciao|grazie|congrats|congratulations|welcome|good morning|good afternoon|good evening|bye|cheers)) (` + cueName + `(?: ` + cueName + `)?)`)

var cueHonorific = regexp.MustCompile(`(?:^|[^\p{L}])((?:Op\.? ?|Uzm\.? ?|Doç\.? ?|Prof\.? ?)?(?:Dr|DR|dr|Dt|Doctor|Doktor|Prof|Mr|Mrs|Ms|Mx|Miss|Mme|Mlle|Herr|Frau|Sr|Sra|Srta|Señor|Señora|Signor|Signora|Monsieur|Madame|Sayın|Ato|Weyzero|W/ro|Wro|Abu|Umm|Sheikh|Shaikh|Sheikha|Hajji|Haji|Ustaz|Ustadh|Pastor|Imam|Rev)\.?(?: (?:Dr|Dt)\.?)?) (` + cueName + `(?: ` + cueName + `){0,2})`)

var cuePostHonorific = regexp.MustCompile(`(` + cueName + `(?: ` + cueName + `)?)(?: |-)(?:Bey|Beyi|Beye|Beyin|Beyle|Hanım|Hanımı|Hanıma|Hanımın|Hanımla|Hanim|Hn|Hnm|Abla|Ablam|Abi|Abim|Ağabey|Hoca|Hocam|Hocaya|Hocanın|Efendi|Usta|Ustam|san|sama|kun|chan|ji|Ji|Bhai|Sahab|Sahib|Saheb)(?:[^\p{L}]|$)`)

var cueRelation = regexp.MustCompile(`(?i:\b(my|our|your|his|her|their)\s+(?:(?:little|big|older|younger|elder|baby|best|dear|beloved|late|ex)\s+)?(sister|brother|wife|husband|son|daughter|mom|mum|mother|mommy|dad|father|daddy|friend|cousin|aunt|auntie|uncle|niece|nephew|grandma|grandmother|grandpa|grandfather|granddaughter|grandson|fiance|fiancee|fiancé|fiancée|partner|boyfriend|girlfriend|spouse|colleague|coworker|co-worker|boss|neighbor|neighbour|roommate|doctor|surgeon|dentist|lawyer|accountant|assistant|driver|translator|coordinator|nurse|cofounder|co-founder|mother-in-law|father-in-law|sister-in-law|brother-in-law|caregiver)\s*,?\s+)(` + cueName + `(?: ` + cueName + `)?)`)

var cueRelationTR = regexp.MustCompile(`(?i:(?:^|[^\p{L}])(kız kardeşim|erkek kardeşim|kardeşim|annem|babam|eşim|karım|kocam|oğlum|kızım|ablam|abim|ağabeyim|teyzem|halam|amcam|dayım|kuzenim|arkadaşım|doktorum|nişanlım|sevgilim|kayınvalidem|kayınpederim|mi hermana|mi hermano|mi esposa|mi esposo|mi marido|mi hijo|mi hija|mi amigo|mi amiga|mi madre|mi padre|mi mamá|mi papá|meine schwester|mein bruder|meine frau|mein mann|mein sohn|meine tochter|mein freund|meine freundin|meine mutter|mein vater|ma sœur|ma soeur|mon frère|mon frere|ma femme|mon mari|mon fils|ma fille|mon ami|mon amie|ma mère|mon père|ukhti|akhi|ahi)\s*,?\s+)(` + cueName + `(?: ` + cueName + `)?)`)

var cueSelfIntro = regexp.MustCompile(`(?i:\b(?:my name is|my name's|name is|named|i am|i'm|i’m|benim adım|adım|ismim|me llamo|mi nombre es|ich heiße|ich heisse|je m'appelle|je m’appelle|je suis)\s+)(` + cueName + `(?: ` + cueName + `)?)`)

var cueOrgCue = regexp.MustCompile(`((?:` + cueName + `|[A-Z]{2,})(?: (?:` + cueName + `|[A-Z]{2,}|&)){0,2}) (Clinic|Clinics|Klinik|Kliniği|Klinigi|Hospital|Hastanesi|Hastane|Medical Center|Medical Centre|Dental|Dent|Estetik|Aesthetics|Aesthetic|Hair Center|Hair Clinic|Hotel|Otel|Oteli|Pharmacy|Eczanesi|Eczane|Lab|Labs|Laboratory|Merkezi|Tıp Merkezi|Polikliniği|Group|Grup|Digital|Tour|Tours|Travel|Turizm|Agency|Ajans|Inc|LLC|Ltd|GmbH|Corp|Holding|Media|Foundation|Capital|Ventures|Partners|Consulting|Studio|Studios)\b`)

var cueHome = regexp.MustCompile(`(?i:\b(?:i|we)\s+(?:live|lived|am living|are living|reside|am based|are based|grew up|was born)\s+in\s+|\b(?:i'm|i’m|i am|we're|we are|originally)\s+(?:originally\s+)?from\s+|\b(?:lives|living|resides|based)\s+in\s+)(` + cueName + `(?: ` + cueName + `)?)`)

var cueAge = regexp.MustCompile(`(?i:\b(\d{1,3})\s*(?:years old|year old|yrs old|yr old|y/o|yaşında|yaşındayım|yaşındadır|años|jahre alt|ans\b)|\b(?:age|aged|yaş|yaşı)\s*:?\s*(\d{1,3})\b)`)

var cueOrgKind = map[string]string{
	"clinic": "CLINIC", "clinics": "CLINIC", "klinik": "CLINIC", "kliniği": "CLINIC", "klinigi": "CLINIC",
	"dental": "CLINIC", "dent": "CLINIC", "estetik": "CLINIC", "aesthetics": "CLINIC",
	"aesthetic": "CLINIC", "hair center": "CLINIC", "hair clinic": "CLINIC",
	"polikliniği": "CLINIC", "merkezi": "CLINIC", "tıp merkezi": "HOSPITAL",
	"hospital": "HOSPITAL", "hastanesi": "HOSPITAL", "hastane": "HOSPITAL", "medical center": "HOSPITAL", "medical centre": "HOSPITAL",
	"hotel": "HOTEL", "otel": "HOTEL", "oteli": "HOTEL",
	"pharmacy": "PHARMACY", "eczanesi": "PHARMACY", "eczane": "PHARMACY",
	"lab": "LAB", "labs": "LAB", "laboratory": "LAB",
	"group": "COMPANY", "grup": "COMPANY", "digital": "COMPANY", "tour": "COMPANY", "tours": "COMPANY", "travel": "COMPANY",
	"turizm": "COMPANY", "agency": "COMPANY", "ajans": "COMPANY", "inc": "COMPANY", "llc": "COMPANY", "ltd": "COMPANY",
	"gmbh": "COMPANY", "corp": "COMPANY", "holding": "COMPANY", "media": "COMPANY", "foundation": "COMPANY", "capital": "FUND",
	"ventures": "FUND", "partners": "COMPANY", "consulting": "COMPANY", "studio": "COMPANY", "studios": "COMPANY",
}

var cueRelationMap = map[string]string{
	"sister": "SISTER", "brother": "BROTHER", "wife": "WIFE", "husband": "HUSBAND", "son": "SON", "daughter": "DAUGHTER",
	"mom": "MOTHER", "mum": "MOTHER", "mother": "MOTHER", "mommy": "MOTHER", "dad": "FATHER", "father": "FATHER", "daddy": "FATHER",
	"friend": "FRIEND", "cousin": "COUSIN", "aunt": "AUNT", "auntie": "AUNT", "uncle": "UNCLE", "niece": "NIECE", "nephew": "NEPHEW",
	"grandma": "GRANDMOTHER", "grandmother": "GRANDMOTHER", "grandpa": "GRANDFATHER", "grandfather": "GRANDFATHER",
	"granddaughter": "GRANDCHILD", "grandson": "GRANDCHILD", "fiance": "FIANCE", "fiancee": "FIANCE", "fiancé": "FIANCE", "fiancée": "FIANCE",
	"partner": "PARTNER", "boyfriend": "PARTNER", "girlfriend": "PARTNER", "spouse": "PARTNER", "colleague": "COLLEAGUE",
	"coworker": "COLLEAGUE", "co-worker": "COLLEAGUE", "boss": "BOSS", "neighbor": "NEIGHBOR", "neighbour": "NEIGHBOR",
	"roommate": "ROOMMATE", "lawyer": "LAWYER", "accountant": "ACCOUNTANT", "cofounder": "BUSINESS_PARTNER", "co-founder": "BUSINESS_PARTNER",
	"mother-in-law": "MOTHER_IN_LAW", "father-in-law": "FATHER_IN_LAW", "sister-in-law": "SISTER_IN_LAW", "brother-in-law": "BROTHER_IN_LAW",
	"caregiver":    "CAREGIVER",
	"kız kardeşim": "SISTER", "erkek kardeşim": "BROTHER", "kardeşim": "SIBLING", "annem": "MOTHER", "babam": "FATHER", "eşim": "PARTNER",
	"karım": "WIFE", "kocam": "HUSBAND", "oğlum": "SON", "kızım": "DAUGHTER", "ablam": "SISTER", "abim": "BROTHER", "ağabeyim": "BROTHER",
	"teyzem": "AUNT", "halam": "AUNT", "amcam": "UNCLE", "dayım": "UNCLE", "kuzenim": "COUSIN", "arkadaşım": "FRIEND", "nişanlım": "FIANCE",
	"sevgilim": "PARTNER", "kayınvalidem": "MOTHER_IN_LAW", "kayınpederim": "FATHER_IN_LAW",
	"mi hermana": "SISTER", "mi hermano": "BROTHER", "mi esposa": "WIFE", "mi esposo": "HUSBAND", "mi marido": "HUSBAND", "mi hijo": "SON",
	"mi hija": "DAUGHTER", "mi amigo": "FRIEND", "mi amiga": "FRIEND", "mi madre": "MOTHER", "mi padre": "FATHER", "mi mamá": "MOTHER", "mi papá": "FATHER",
	"meine schwester": "SISTER", "mein bruder": "BROTHER", "meine frau": "WIFE", "mein mann": "HUSBAND", "mein sohn": "SON", "meine tochter": "DAUGHTER",
	"mein freund": "FRIEND", "meine freundin": "FRIEND", "meine mutter": "MOTHER", "mein vater": "FATHER",
	"ma sœur": "SISTER", "ma soeur": "SISTER", "mon frère": "BROTHER", "mon frere": "BROTHER", "ma femme": "WIFE", "mon mari": "HUSBAND",
	"mon fils": "SON", "ma fille": "DAUGHTER", "mon ami": "FRIEND", "mon amie": "FRIEND", "ma mère": "MOTHER", "mon père": "FATHER",
	"ukhti": "SISTER", "akhi": "BROTHER", "ahi": "BROTHER",
}

var cueRoleNouns = map[string]string{
	"doctor": "DOCTOR", "surgeon": "SURGEON", "dentist": "DOCTOR", "assistant": "ASSISTANT", "driver": "DRIVER",
	"translator": "TRANSLATOR", "coordinator": "COORDINATOR", "nurse": "NURSE", "doktorum": "DOCTOR",
}

var cueDoctorTitles = setOf(strings.Fields("dr dt doctor doktor prof op uzm doç"))

type cueState struct {
	fc        refineInput
	doc       chatDoc
	slots     []slot
	spans     map[int][][2]int
	valueTag  map[string]string
	tokenRole map[string]string
	usedRoles map[string]bool
	ownerRole string
	partner   string
}

func cueCues(fc refineInput) []slot {
	st := &cueState{
		fc:        fc,
		doc:       fc.parsed(),
		spans:     map[int][][2]int{},
		valueTag:  map[string]string{},
		tokenRole: map[string]string{},
		usedRoles: map[string]bool{},
	}
	for _, s := range fc.Slots {
		st.slots = append(st.slots, s)
		st.spans[s.Line] = append(st.spans[s.Line], [2]int{s.Start, s.End})
		st.learn(s.Value, s.Tag)
	}
	st.learnSenders()
	for i, l := range st.doc.Lines {
		st.scanLine(i, l)
	}
	st.propagate()
	for i := range st.doc.Lines {
		st.coordinate(i)
	}
	st.propagate()
	return st.slots
}

func cueRoleKey(p parsedTag) string {
	if p.Kind == tagEntity {
		if p.Number >= 2 {
			return p.Base + "_" + strconv.Itoa(p.Number)
		}
		return p.Base
	}
	if p.Number >= 2 {
		return p.role() + "_" + strconv.Itoa(p.Number)
	}
	return p.role()
}

func (st *cueState) learn(value, tag string) {
	p, ok := dictionary.parse(tag)
	if !ok {
		return
	}
	if p.Kind != tagStandalone {
		st.usedRoles[cueRoleKey(p)] = true
	}
	if !identityAttributes[p.Attribute] {
		return
	}
	v := strings.TrimSpace(value)
	if v == "" {
		return
	}
	if _, seen := st.valueTag[v]; !seen {
		st.valueTag[v] = tag
	}
	if p.Kind != tagPerson || p.Fallback {
		return
	}
	for _, tok := range cueNameToken.FindAllString(v, -1) {
		key := strings.ToLower(tok)
		if _, seen := st.tokenRole[key]; !seen && utf8.RuneCountInString(tok) >= 3 {
			st.tokenRole[key] = cueRoleKey(p)
		}
	}
}

func (st *cueState) learnSenders() {
	for _, sender := range sortedKeys(st.fc.SenderTags) {
		tag := st.fc.SenderTags[sender]
		p, ok := dictionary.parse(tag)
		if !ok || p.Kind != tagPerson {
			continue
		}
		role := cueRoleKey(p)
		st.usedRoles[role] = true
		if p.Base == "OWNER" && p.Relation == "" {
			st.ownerRole = role
		} else if !st.fc.IsGroup && !p.Fallback {
			st.partner = role
		}
		if p.Fallback || !cueLooksLikeName(sender) {
			continue
		}
		toks := cueNameToken.FindAllString(sender, -1)
		if len(toks) == 0 {
			continue
		}
		if len(toks) >= 2 {
			st.setValue(sender, roleTag(role, "FULL_NAME"))
			st.setValue(toks[0], roleTag(role, "FIRST_NAME"))
			st.setValue(toks[len(toks)-1], roleTag(role, "LAST_NAME"))
		} else {
			st.setValue(toks[0], roleTag(role, "FIRST_NAME"))
		}
		for _, tok := range toks {
			key := strings.ToLower(tok)
			if _, seen := st.tokenRole[key]; !seen && utf8.RuneCountInString(tok) >= 3 {
				st.tokenRole[key] = role
			}
		}
	}
	if !st.fc.IsGroup && st.partner == "" {
		st.partner = "CONTACT"
	}
	if st.ownerRole == "" {
		st.ownerRole = "OWNER"
	}
}

func (st *cueState) setValue(v, tag string) {
	if _, seen := st.valueTag[v]; !seen && dictionary.valid(tag) {
		st.valueTag[v] = tag
	}
}

func cueLooksLikeName(s string) bool {
	if strings.ContainsAny(s, "0123456789+@") {
		return false
	}
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func (st *cueState) speakerRole(l chatLine) string {
	if !l.HasSender {
		return ""
	}
	p, ok := dictionary.parse(st.fc.SenderTags[l.Sender])
	if !ok || p.Kind != tagPerson || p.Fallback {
		return ""
	}
	return cueRoleKey(p)
}

func (st *cueState) addresseeRole(l chatLine) string {
	if st.fc.IsGroup {
		return ""
	}
	sp := st.speakerRole(l)
	if sp == st.ownerRole {
		return st.partner
	}
	if sp != "" {
		return st.ownerRole
	}
	return ""
}

func (st *cueState) overlaps(line, start, end int) bool {
	for _, sp := range st.spans[line] {
		if start < sp[1] && sp[0] < end {
			return true
		}
	}
	return false
}

func (st *cueState) emit(line, start, end int, tag string) {
	body := st.doc.Lines[line].Body
	if start < 0 || end > len(body) || start >= end || st.overlaps(line, start, end) || !dictionary.valid(tag) {
		return
	}
	st.slots = append(st.slots, slot{Line: line, Start: start, End: end, Tag: tag, Value: body[start:end], Category: "NAME", Source: "s08", Confidence: 0.6})
	st.spans[line] = append(st.spans[line], [2]int{start, end})
}

func cueSkip(tok string, known bool) bool {
	if known {
		return false
	}
	return isCommonWord(tok) || cueNotNames[strings.ToLower(tok)] || utf8.RuneCountInString(tok) < 2
}

func (st *cueState) knownToken(tok string) bool {
	_, ok := st.tokenRole[strings.ToLower(tok)]
	if ok {
		return true
	}
	_, ok = st.valueTag[tok]
	return ok
}

func (st *cueState) nameTokens(body string, start, end int) [][2]int {
	var toks [][2]int
	for _, loc := range cueNameToken.FindAllStringIndex(body[start:end], -1) {
		a, b := start+loc[0], start+loc[1]
		tok := body[a:b]
		if cueSkip(tok, st.knownToken(tok)) {
			break
		}
		toks = append(toks, [2]int{a, b})
	}
	return toks
}

func (st *cueState) freshRole(base string) string {
	if base == "" || base == "PERSON" {
		return "PERSON"
	}
	if !st.usedRoles[base] {
		st.usedRoles[base] = true
		return base
	}
	for n := 2; ; n++ {
		cand := base + "_" + strconv.Itoa(n)
		if !st.usedRoles[cand] {
			st.usedRoles[cand] = true
			return cand
		}
	}
}

func (st *cueState) addName(line int, start, end int, base string, reuse bool) {
	body := st.doc.Lines[line].Body
	toks := st.nameTokens(body, start, end)
	if len(toks) == 0 {
		return
	}
	full := body[toks[0][0]:toks[len(toks)-1][1]]
	if tag, ok := st.valueTag[full]; ok {
		st.emitTokens(line, toks, tag, "")
		return
	}
	role := ""
	for _, t := range toks {
		if r, ok := st.tokenRole[strings.ToLower(body[t[0]:t[1]])]; ok {
			role = r
			break
		}
	}
	if role == "" {
		if reuse && base != "" && base != "PERSON" {
			role = base
			st.usedRoles[base] = true
		} else {
			role = st.freshRole(base)
		}
	}
	for _, t := range toks {
		tok := body[t[0]:t[1]]
		key := strings.ToLower(tok)
		if _, seen := st.tokenRole[key]; !seen && role != "PERSON" && utf8.RuneCountInString(tok) >= 3 {
			st.tokenRole[key] = role
		}
	}
	if len(toks) >= 2 {
		st.setValue(full, roleTag(role, "FULL_NAME"))
		st.setValue(body[toks[0][0]:toks[0][1]], roleTag(role, "FIRST_NAME"))
		st.setValue(body[toks[len(toks)-1][0]:toks[len(toks)-1][1]], roleTag(role, "LAST_NAME"))
	} else {
		st.setValue(full, roleTag(role, "FIRST_NAME"))
	}
	st.emitTokens(line, toks, "", role)
}

func (st *cueState) emitTokens(line int, toks [][2]int, wholeTag, role string) {
	body := st.doc.Lines[line].Body
	start, end := toks[0][0], toks[len(toks)-1][1]
	if !st.overlaps(line, start, end) {
		tag := wholeTag
		if tag == "" {
			attr := "FIRST_NAME"
			if len(toks) >= 2 {
				attr = "FULL_NAME"
			}
			tag = roleTag(role, attr)
		}
		st.emit(line, start, end, tag)
		return
	}
	for i, t := range toks {
		tok := body[t[0]:t[1]]
		tag, ok := st.valueTag[tok]
		if !ok {
			r := role
			if r == "" {
				if p, ok := dictionary.parse(wholeTag); ok && p.Kind == tagPerson {
					r = cueRoleKey(p)
				}
			}
			if r == "" {
				continue
			}
			attr := "LAST_NAME"
			if i == 0 {
				attr = "FIRST_NAME"
			}
			tag = roleTag(r, attr)
		}
		st.emit(line, t[0], t[1], tag)
	}
}

func (st *cueState) relationRole(l chatLine, possessive, relation string) string {
	if base, ok := cueRoleNouns[relation]; ok {
		return base
	}
	rel, ok := cueRelationMap[relation]
	if !ok {
		return "PERSON"
	}
	owner := ""
	switch possessive {
	case "my", "our", "":
		owner = st.speakerRole(l)
	case "your":
		owner = st.addresseeRole(l)
	}
	base, _ := splitNumber(owner)
	if base == "" {
		return "PERSON"
	}
	if p, ok := dictionary.parse(roleTag(base, "FIRST_NAME")); !ok || p.Relation != "" {
		return "PERSON"
	}
	if !dictionary.valid(roleTag(base+"_"+rel, "FIRST_NAME")) {
		return "PERSON"
	}
	return base + "_" + rel
}

func (st *cueState) scanLine(i int, l chatLine) {
	body := l.Body
	if body == "" {
		return
	}
	for _, m := range cueHonorific.FindAllStringSubmatchIndex(body, -1) {
		title := strings.ToLower(strings.Trim(strings.Fields(body[m[2]:m[3]])[0], "."))
		base := "PERSON"
		if cueDoctorTitles[title] {
			base = "DOCTOR"
		}
		st.addName(i, m[4], m[5], base, false)
	}
	for _, m := range cuePostHonorific.FindAllStringSubmatchIndex(body, -1) {
		st.addName(i, m[2], m[3], "PERSON", false)
	}
	for _, m := range cueRelation.FindAllStringSubmatchIndex(body, -1) {
		role := st.relationRole(l, strings.ToLower(body[m[2]:m[3]]), strings.ToLower(body[m[4]:m[5]]))
		st.addName(i, m[6], m[7], role, false)
	}
	for _, m := range cueRelationTR.FindAllStringSubmatchIndex(body, -1) {
		role := st.relationRole(l, "my", strings.ToLower(body[m[2]:m[3]]))
		st.addName(i, m[4], m[5], role, false)
	}
	for _, m := range cueSelfIntro.FindAllStringSubmatchIndex(body, -1) {
		cue := strings.ToLower(body[m[0]:m[2]])
		role := st.speakerRole(l)
		if strings.Contains(cue, "named") && !strings.Contains(cue, "name is") {
			role = "PERSON"
		}
		if role == "" {
			role = "PERSON"
		}
		st.addName(i, m[2], m[3], role, true)
	}
	for _, m := range cueGreeting.FindAllStringSubmatchIndex(body, -1) {
		role := st.addresseeRole(l)
		if role == "" {
			role = "PERSON"
		}
		st.addName(i, m[4], m[5], role, true)
	}
	for _, m := range cueOrgCue.FindAllStringSubmatchIndex(body, -1) {
		st.addOrg(i, m[2], m[3], m[5])
	}
	if m := cueWholeName.FindStringSubmatchIndex(body); m != nil {
		st.addFull(i, m[2], m[3], "PERSON")
	}
	for _, m := range cueFileName.FindAllStringSubmatchIndex(body, -1) {
		st.addFull(i, m[2], m[3], "PERSON")
	}
	for _, m := range cueTitleList.FindAllStringSubmatchIndex(body, -1) {
		st.addFull(i, m[2], m[3], "PERSON")
	}
	for _, m := range cueRoleNounCue.FindAllStringSubmatchIndex(body, -1) {
		base := cueRoleNounBase[strings.ToLower(body[m[2]:m[3]])]
		if base == "" {
			base = "PERSON"
		}
		st.addName(i, m[4], m[5], base, false)
	}
	for _, m := range cueHome.FindAllStringSubmatchIndex(body, -1) {
		toks := st.nameTokens(body, m[2], m[3])
		if len(toks) == 0 {
			continue
		}
		role := st.speakerRole(l)
		cue := strings.ToLower(body[m[0]:m[2]])
		if !(strings.HasPrefix(cue, "i") || strings.HasPrefix(cue, "we") || strings.HasPrefix(cue, "orig")) || role == "" {
			role = "PERSON"
		}
		st.emit(i, toks[0][0], toks[len(toks)-1][1], roleTag(role, "HOME_CITY"))
	}
	for _, m := range cueAge.FindAllStringSubmatchIndex(body, -1) {
		a, b := m[2], m[3]
		if a < 0 {
			a, b = m[4], m[5]
		}
		n, _ := strconv.Atoi(body[a:b])
		if n < 1 || n > 120 {
			continue
		}
		role := "PERSON"
		if sp := st.speakerRole(l); sp != "" && cuesPattern1.MatchString(body[max(0, a-12):min(len(body), m[1])]) {
			role = sp
		}
		st.emit(i, a, b, roleTag(role, "AGE"))
	}
}

func (st *cueState) addOrg(line, start, end, kwEnd int) {
	body := st.doc.Lines[line].Body
	kw := strings.ToLower(body[end+1 : kwEnd])
	kind := cueOrgKind[kw]
	if kind == "" {
		return
	}
	locs := cuesPattern2.FindAllStringIndex(body[start:end], -1)
	first := -1
	distinct := false
	for _, loc := range locs {
		w := body[start+loc[0] : start+loc[1]]
		if first < 0 && (isCommonWord(w) || cueNotNames[strings.ToLower(w)] || w == "&") {
			continue
		}
		if first < 0 {
			first = start + loc[0]
		}
		if !isCommonWord(w) && !cueNotNames[strings.ToLower(w)] && w != "&" {
			distinct = true
		}
	}
	if first < 0 || !distinct {
		return
	}
	value := body[first:kwEnd]
	tag, ok := st.valueTag[value]
	if !ok {
		head := cueNameToken.FindString(body[first:end])
		if head == "" {
			head = strings.Fields(body[first:end])[0]
		}
		if t, ok := st.valueTag[head]; ok {
			if p, ok := dictionary.parse(t); ok && p.Kind == tagEntity {
				tag = t
			}
		}
	}
	if tag == "" {
		tag = "{{" + st.freshRole(kind) + "_NAME}}"
		st.setValue(value, tag)
		head := strings.Fields(body[first:end])[0]
		if utf8.RuneCountInString(head) >= 4 && !isCommonWord(head) && !cueNotNames[strings.ToLower(head)] {
			st.setValue(head, tag)
		}
	}
	if st.overlaps(line, first, kwEnd) {
		for _, loc := range cuesPattern2.FindAllStringIndex(body[first:end], -1) {
			w := body[first+loc[0] : first+loc[1]]
			if !isCommonWord(w) && !cueNotNames[strings.ToLower(w)] {
				st.emit(line, first+loc[0], first+loc[1], tag)
			}
		}
		return
	}
	st.emit(line, first, kwEnd, tag)
}

func (st *cueState) propagate() {
	values := sortedKeys(st.valueTag)
	sortByLenDesc(values)
	lowered := make([]string, len(st.doc.Lines))
	for i, l := range st.doc.Lines {
		lowered[i] = strings.ToLower(l.Body)
	}
	for _, v := range values {
		tag := st.valueTag[v]
		first, _ := utf8.DecodeRuneInString(v)
		if utf8.RuneCountInString(v) < 3 || !unicode.IsUpper(first) || strings.ContainsAny(v, "@/:") {
			continue
		}
		vocativeOnly := false
		if isCommonWord(v) || cueNotNames[strings.ToLower(v)] {
			p, ok := dictionary.parse(tag)
			if !ok || p.Kind != tagPerson || strings.Contains(v, " ") {
				continue
			}
			vocativeOnly = true
		}
		var fold *regexp.Regexp
		if !vocativeOnly && utf8.RuneCountInString(v) >= 4 && cueIsPerson(tag) {
			fold = regexp.MustCompile(`(?i)` + regexp.QuoteMeta(v))
		}
		needle := strings.ToLower(v)
		for i, l := range st.doc.Lines {
			if !strings.Contains(lowered[i], needle) {
				continue
			}
			locs := cueFindExact(l.Body, v)
			if fold != nil {
				locs = cueFindFold(l.Body, fold)
			}
			for _, loc := range locs {
				if !cueWordBounded(l.Body, loc[0], loc[1]) {
					continue
				}
				if vocativeOnly && !cueVocative(l.Body, loc[0], loc[1]) {
					continue
				}
				st.emit(i, loc[0], loc[1], tag)
			}
		}
	}
}

func cueWordBounded(body string, a, b int) bool {
	if a > 0 {
		r, _ := utf8.DecodeLastRuneInString(body[:a])
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	if b < len(body) {
		r, _ := utf8.DecodeRuneInString(body[b:])
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func sortByLenDesc(values []string) {
	sort.Slice(values, func(i, j int) bool {
		if len(values[i]) != len(values[j]) {
			return len(values[i]) > len(values[j])
		}
		return values[i] < values[j]
	})
}

func cueFindExact(body, text string) [][2]int {
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
	return out
}

func cueVocative(body string, a, b int) bool {
	if b < len(body) && !strings.ContainsRune(".,!?\n", rune(body[b])) {
		return false
	}
	before := strings.TrimRight(body[:a], " ")
	if before == body[:a] && a > 0 {
		return false
	}
	if strings.HasSuffix(before, ",") {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(before)
	return unicode.IsLower(r)
}

var cueCoord = regexp.MustCompile(`(` + cueName + `)(?:['’]s)?(?: and | & | ve | y | und | et )(` + cueName + `)`)

func (st *cueState) personToken(tok string) bool {
	if r, ok := st.tokenRole[strings.ToLower(tok)]; ok && r != "" {
		return true
	}
	if tag, ok := st.valueTag[tok]; ok {
		p, ok := dictionary.parse(tag)
		return ok && p.Kind == tagPerson
	}
	return false
}

func (st *cueState) coordinate(i int) {
	body := st.doc.Lines[i].Body
	for off := 0; off < len(body); {
		m := cueCoord.FindStringSubmatchIndex(body[off:])
		if m == nil {
			break
		}
		a1, b1, a2, b2 := off+m[2], off+m[3], off+m[4], off+m[5]
		left, right := body[a1:b1], body[a2:b2]
		switch {
		case st.personToken(left) && !st.personToken(right):
			st.addName(i, a2, b2, "PERSON", false)
		case st.personToken(right) && !st.personToken(left):
			st.addName(i, a1, b1, "PERSON", false)
		}
		off = a2
	}
}

var cueWholeName = regexp.MustCompile(`\A\s*(` + cueName + `(?: ` + cueName + `){1,2})\s*\z`)

var cueFileName = regexp.MustCompile(`(?:^|[^\p{L}])(` + cueName + `(?: ` + cueName + `){1,2})(?:[ _-](?:CV|Cv|Resume|Résumé|Özgeçmiş)\b|\s?\.(?:pdf|vcf|docx?)\b)`)

var cueTitleList = regexp.MustCompile(`(?m)(?:^|/ |- |• )(` + cueName + `(?: ` + cueName + `){1,2})(?:: |, | - | – | \()(?:[A-Za-z&]+ ){0,4}(?i:consultant|manager|director|coordinator|specialist|officer|founder|co-founder|cofounder|ceo|cto|coo|cfo|president|head|lead|engineer|doctor|surgeon|nurse|assistant|representative|executive|partner|advisor|analyst|designer|developer|intern|associate|vp|chair|owner)\b`)

func (st *cueState) addFull(line, start, end int, base string) {
	body := st.doc.Lines[line].Body
	toks := st.nameTokens(body, start, end)
	if len(toks) < 2 {
		return
	}
	st.addName(line, toks[0][0], toks[len(toks)-1][1], base, false)
}

func cueFindFold(body string, re *regexp.Regexp) [][2]int {
	var out [][2]int
	for _, m := range re.FindAllStringIndex(body, -1) {
		out = append(out, [2]int{m[0], m[1]})
	}
	return out
}

func cueIsPerson(tag string) bool {
	p, ok := dictionary.parse(tag)
	return ok && p.Kind == tagPerson
}

var cueRoleNounCue = regexp.MustCompile(`(?:\b(?:the |our |a |their |new |this )?(patient|coordinator|translator|interpreter|driver|surgeon|nurse|assistant|anesthesiologist|anesthetist|dentist|technician|hasta|hastamız|koordinatör|tercüman|şoför|asistan|hemşire)\s+)(` + cueName + `(?: ` + cueName + `)?)`)

var cueRoleNounBase = map[string]string{
	"patient": "PATIENT", "hasta": "PATIENT", "hastamız": "PATIENT", "coordinator": "COORDINATOR", "koordinatör": "COORDINATOR",
	"translator": "TRANSLATOR", "interpreter": "TRANSLATOR", "tercüman": "TRANSLATOR", "driver": "DRIVER", "şoför": "DRIVER",
	"surgeon": "SURGEON", "nurse": "NURSE", "hemşire": "NURSE", "assistant": "ASSISTANT", "asistan": "ASSISTANT",
	"anesthesiologist": "ANESTHESIOLOGIST", "anesthetist": "ANESTHESIOLOGIST", "dentist": "DOCTOR", "technician": "TECHNICIAN",
}

var (
	cuesPattern1 = regexp.MustCompile(`(?i)\b(i am|i'm|i’m|yaşındayım)`)
	cuesPattern2 = regexp.MustCompile(`\S+`)
)
