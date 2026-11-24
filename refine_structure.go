package main

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const structureConfidence = 0.3

var (
	structureEmailRe = regexp.MustCompile(`[\p{L}\p{N}._%+\-]+@[\p{L}\p{N}\-]+(?:\.[\p{L}\p{N}\-]+)*\.\p{L}{2,}`)
	structureURLRe   = regexp.MustCompile(`(?i)(?:https?://|www\.)[^\s<>"'‎]+`)
	structurePhoneRe = regexp.MustCompile(`\+\d[\d \-().]{6,}\d`)
	structureWordRe  = regexp.MustCompile(`[\p{L}\p{N}]+`)
	structureFileRe  = regexp.MustCompile(`(?i)([^\n<>:/\\]+?)\s*\.(?:pdf|docx?|xlsx?|pptx?|vcf|key|pages|csv)\b`)
)

var structureGenericLocal = structureSet("info", "hello", "hi", "hey", "contact", "contacts", "legal", "partnerships", "partnership", "partners", "partner", "finans", "finance", "bilgi", "admin", "support", "sales", "team", "office", "mail", "email", "booking", "bookings", "reservations", "reservation", "careers", "career", "jobs", "job", "hr", "press", "media", "marketing", "billing", "accounts", "account", "noreply", "reply", "no", "enquiries", "enquiry", "inquiries", "inquiry", "help", "service", "services", "customer", "care", "general", "turkey", "turkiye", "invest", "investors", "founders", "founder", "ceo", "hq", "patient", "patients", "clinic", "doctor", "dr", "the", "and", "com", "www", "http", "https", "official", "real", "app", "web", "site", "online", "test", "testing", "demo", "new", "group", "global", "intl", "international")

var structureFreeMail = structureSet("gmail", "googlemail", "hotmail", "outlook", "live", "yahoo", "icloud", "me", "mac", "aol", "proton", "protonmail", "pm", "yandex", "mail", "gmx", "msn", "ymail", "zoho", "qq", "163", "web", "mynet")

var structurePlatforms = structureSet("instagram", "linkedin", "lnkd", "x", "twitter", "youtube", "youtu", "google", "goo", "gle", "docs", "drive", "cal", "calendly", "luma", "lu", "partiful", "maps", "apple", "figma", "trustpilot", "facebook", "fb", "tiktok", "wa", "whatsapp", "bit", "zoom", "notion", "dropbox", "airbnb", "booking", "amazon", "github", "medium", "substack", "gov", "wikipedia", "t", "telegram", "spotify", "forms", "typeform", "canva", "loom", "vimeo", "threads", "reddit", "pinterest", "meet", "teams", "microsoft", "office", "live", "share", "app", "apps", "play", "itunes", "tinyurl", "eventbrite", "meetup", "stripe", "paypal", "venmo", "wise", "revolut", "uber", "lyft", "yelp", "tripadvisor", "opentable", "resy", "shopify", "wix", "squarespace", "wordpress", "blogspot", "gstatic", "googleusercontent", "cloudfront", "amazonaws", "slack", "discord", "snapchat", "zillow", "vercel", "netlify", "github", "gitlab", "bitly", "linktr", "linktree", "beacons", "tally", "hubspot", "mailchimp", "zoominfo", "crunchbase", "angel", "wellfound", "ycombinator", "producthunt", "news", "bbc", "cnn", "nytimes", "forbes", "techcrunch", "bloomberg", "reuters", "co", "com", "www", "m", "mobile", "l", "mail", "gmail", "hotmail", "outlook", "yahoo", "icloud", "company", "information", "service", "find", "and", "update")

var structureOrgAffixes = []string{"healthtravel", "hairtransplant", "transplant", "healthcare", "health", "clinics", "clinic", "klinik", "poliklinik", "poliklinigi", "klinigi", "klinikleri", "hastanesi", "hastane", "saglik", "tip", "estetik", "hospital", "medical", "medicine", "group", "digital", "hair", "invest", "investments", "travel", "tourism", "labs", "lab", "tech", "global", "international", "center", "centre", "merkezi", "studio", "agency", "media", "capital", "ventures", "vc", "partners", "consulting", "solutions", "official", "turkey", "turkiye", "istanbul", "dental", "beauty", "aesthetic", "aesthetics", "inc", "llc", "ltd", "hq", "app", "co", "the", "get", "try", "join", "use", "my", "hello", "team"}

var structureFileGeneric = structureSet("cv", "resume", "résumé", "ozgecmis", "özgeçmiş", "proposal", "final", "draft", "copy", "deck", "invoice", "contract", "agreement", "pdf", "doc", "docx", "page", "pages", "attached", "omitted", "document", "file", "scan", "scanned", "image", "photo", "img", "professional", "design", "turkish", "türkçe", "turkce", "english", "tr", "en", "updated", "new", "version", "v1", "v2", "v3", "report", "plan", "presentation", "pitch", "one", "pager", "onepager", "profile", "portfolio", "letter", "cover", "offer", "quote", "nda", "mou", "terms", "sheet", "budget", "list", "data", "info", "form", "application", "marketing", "official", "price", "prices", "menu", "brochure", "catalog", "catalogue", "sample", "template", "notes", "summary", "minutes", "agenda", "schedule", "itinerary", "ticket", "tickets", "receipt", "statement", "and", "the", "for", "of", "with", "dm", "com", "benim", "profesyonel", "single", "contact", "card", "whatsapp")

func structureSet(words ...string) map[string]bool {
	m := map[string]bool{}
	for _, w := range words {
		m[w] = true
	}
	return m
}

var structureFoldMap = map[rune]rune{'ğ': 'g', 'ş': 's', 'ı': 'i', 'ö': 'o', 'ü': 'u', 'ç': 'c', 'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e', 'á': 'a', 'à': 'a', 'â': 'a', 'ä': 'a', 'ã': 'a', 'å': 'a', 'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i', 'ó': 'o', 'ò': 'o', 'ô': 'o', 'õ': 'o', 'ø': 'o', 'ú': 'u', 'ù': 'u', 'û': 'u', 'ñ': 'n', 'ý': 'y', 'ÿ': 'y', 'ž': 'z', 'š': 's', 'č': 'c', 'ć': 'c', 'ł': 'l', 'ń': 'n', 'ś': 's', 'ź': 'z', 'ż': 'z', 'ř': 'r', 'đ': 'd', 'ß': 's', 'İ': 'i', 'I': 'i'}

func structureFold(s string) string {
	var b strings.Builder
	for _, r := range s {
		if f, ok := structureFoldMap[r]; ok {
			b.WriteRune(f)
			continue
		}
		r = unicode.ToLower(r)
		if f, ok := structureFoldMap[r]; ok {
			r = f
		}
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

type structureWord struct {
	start, end int
	text, fold string
}

type structureCandidate struct {
	compound string
	parts    []string
	isOrg    bool
	srcTag   string
}

func structureWords(body string) []structureWord {
	var out []structureWord
	for _, m := range structureWordRe.FindAllStringIndex(body, -1) {
		out = append(out, structureWord{m[0], m[1], body[m[0]:m[1]], structureFold(body[m[0]:m[1]])})
	}
	return out
}

func structureUsable(token string) bool {
	if len([]rune(token)) < 3 {
		return false
	}
	for _, r := range token {
		if unicode.IsDigit(r) {
			return false
		}
	}
	return !isCommonWord(token) && !structureGenericLocal[token] && !structurePlatforms[token] && !structureAffixSet[token] && !structureFileGeneric[token]
}

var structureAffixSet = structureSet(structureOrgAffixes...)

func structureSplitParts(s string) []string {
	var parts []string
	for _, p := range structurePattern1.Split(structureFold(s), -1) {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func structureCamelSplit(s string) []string {
	var parts []string
	var cur []rune
	rs := []rune(s)
	for i, r := range rs {
		if i > 0 && unicode.IsUpper(r) && unicode.IsLower(rs[i-1]) && len(cur) > 0 {
			parts = append(parts, string(cur))
			cur = nil
		}
		cur = append(cur, r)
	}
	if len(cur) > 0 {
		parts = append(parts, string(cur))
	}
	return parts
}

func structureOrgCores(label string) []string {
	label = structureFold(label)
	out := []string{label}
	cur := label
	for changed := true; changed; {
		changed = false
		for _, a := range structureOrgAffixes {
			if strings.HasSuffix(cur, a) && len(cur)-len(a) >= 4 {
				cur = strings.TrimSuffix(cur, a)
				out = append(out, cur)
				changed = true
			}
			if strings.HasPrefix(cur, a) && len(cur)-len(a) >= 4 {
				cur = strings.TrimPrefix(cur, a)
				out = append(out, cur)
				changed = true
			}
		}
	}
	return out
}

func structureDomainLabel(host string) string {
	host = strings.ToLower(strings.TrimPrefix(strings.ToLower(host), "www."))
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return ""
	}
	i := len(labels) - 2
	for i > 0 && len(labels[i]) <= 3 && (labels[i] == "com" || labels[i] == "co" || labels[i] == "org" || labels[i] == "net" || labels[i] == "gov" || labels[i] == "ac" || labels[i] == "edu") {
		i--
	}
	return labels[i]
}

func structurePersonTag(srcTag, attr string) string {
	p, ok := dictionary.parse(srcTag)
	if ok && p.Kind == tagPerson {
		return "{{" + structureRoleName(p) + "_" + attr + "}}"
	}
	return "{{PERSON_" + attr + "}}"
}

func structureRoleName(p parsedTag) string {
	r := p.role()
	if p.Number >= 2 {
		r += "_" + structureItoa(p.Number)
	}
	return r
}

func structureItoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func structureOrgTag(srcTag string) string {
	p, ok := dictionary.parse(srcTag)
	if ok && p.Kind == tagEntity {
		name := p.Base
		if p.Number >= 2 {
			name += "_" + structureItoa(p.Number)
		}
		t := "{{" + name + "_NAME}}"
		if dictionary.valid(t) {
			return t
		}
	}
	if ok && p.Kind == tagPerson && !p.Fallback {
		return "{{" + structureRoleName(p) + "_EMPLOYER}}"
	}
	return "{{ORGANIZATION_NAME}}"
}

func structureIsURLish(s slot) bool {
	v := strings.ToLower(s.Value)
	return strings.Contains(v, "://") || strings.HasPrefix(v, "www.") || strings.Contains(s.Tag, "LINK") || strings.Contains(s.Tag, "WEBSITE") || s.Category == "LINK" || s.Category == "WEBSITE"
}

func structureMine(s slot) []structureCandidate {
	v := strings.TrimSpace(s.Value)
	var out []structureCandidate
	addPerson := func(raw string) {
		parts := structureSplitParts(raw)
		var keep []string
		for _, p := range parts {
			if structureUsable(p) {
				keep = append(keep, p)
			}
		}
		if len(keep) == 0 {
			return
		}
		out = append(out, structureCandidate{compound: strings.Join(keep, ""), parts: keep, srcTag: s.Tag})
	}
	addOrg := func(label string) {
		label = structureFold(label)
		if label == "" || structurePlatforms[label] || structureFreeMail[label] || len(label) < 3 {
			return
		}
		out = append(out, structureCandidate{compound: label, isOrg: true, srcTag: s.Tag})
	}
	if m := structureEmailRe.FindString(v); m != "" && !strings.Contains(v, "://") {
		at := strings.LastIndexByte(m, '@')
		local, domain := m[:at], m[at+1:]
		if plus := strings.IndexByte(local, '+'); plus >= 0 {
			addPerson(local[plus+1:])
			local = local[:plus]
		}
		label := structureDomainLabel(domain)
		if !structureGenericLocal[structureFold(local)] && structureFold(local) != label {
			addPerson(local)
		}
		if !structureFreeMail[label] {
			addOrg(label)
		}
		return out
	}
	if strings.HasPrefix(v, "@") && !strings.Contains(v[1:], "@") {
		h := strings.Trim(v[1:], ".")
		if dot := strings.IndexByte(h, '.'); dot > 0 {
			addOrg(structureDomainLabel(h))
			return out
		}
		if strings.Contains(s.Tag, "CLINIC") || strings.Contains(s.Tag, "COMPANY") || strings.Contains(s.Tag, "ORGANIZATION") {
			addOrg(h)
		} else {
			addPerson(h)
			addOrg(h)
		}
		return out
	}
	if !structureIsURLish(s) {
		return out
	}
	raw := v
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return out
	}
	host := strings.ToLower(u.Host)
	label := structureDomainLabel(host)
	segs := []string{}
	for _, p := range strings.Split(u.Path, "/") {
		if p != "" {
			segs = append(segs, p)
		}
	}
	switch label {
	case "linkedin":
		if len(segs) >= 2 && segs[0] == "in" {
			addPerson(structurePattern2.ReplaceAllString(segs[1], ""))
		}
		if len(segs) >= 2 && (segs[0] == "company" || segs[0] == "school") {
			addOrg(strings.ReplaceAll(segs[1], "-", ""))
		}
		return out
	case "instagram", "tiktok", "facebook", "x", "twitter", "threads", "cal", "calendly", "linktr", "medium", "substack", "youtube":
		if len(segs) == 1 || (len(segs) >= 1 && (label == "cal" || label == "calendly")) {
			h := strings.TrimPrefix(segs[0], "@")
			if structurePlatforms[structureFold(h)] || h == "p" || h == "reel" || h == "reels" || h == "stories" || h == "watch" || h == "booking" || h == "share" {
				return out
			}
			addPerson(h)
			addOrg(h)
		}
		return out
	}
	if structurePlatforms[label] {
		return out
	}
	addOrg(label)
	for i := 1; i < len(segs); i++ {
		kind := structurePathKinds[strings.ToLower(segs[i-1])]
		if kind == "" {
			continue
		}
		slug := strings.ToLower(segs[i])
		if structurePattern3.MatchString(slug) {
			continue
		}
		words := structureSplitParts(slug)
		if len(words) == 0 || len(words) > 4 {
			continue
		}
		if kind == "PERSON" {
			out = append(out, structureCandidate{compound: strings.Join(words, ""), parts: words, srcTag: "{{PERSON_FULL_NAME}}"})
			continue
		}
		tag := "{{" + kind + "_NAME}}"
		for _, w := range words {
			if structureUsable(w) && len(w) >= 4 {
				out = append(out, structureCandidate{compound: w, isOrg: true, srcTag: tag})
			}
		}
		out = append(out, structureCandidate{compound: strings.Join(words, ""), isOrg: true, srcTag: tag})
	}
	return out
}

var structurePathKinds = map[string]string{"clinic": "CLINIC", "clinics": "CLINIC", "klinik": "CLINIC", "hospital": "HOSPITAL", "hospitals": "HOSPITAL", "hotel": "HOTEL", "hotels": "HOTEL", "company": "COMPANY", "companies": "COMPANY", "org": "ORGANIZATION", "organization": "ORGANIZATION", "doctor": "PERSON", "doctors": "PERSON", "dr": "PERSON", "team": "PERSON", "people": "PERSON", "speaker": "PERSON", "speakers": "PERSON", "author": "PERSON", "authors": "PERSON", "member": "PERSON", "members": "PERSON", "profile": "PERSON", "user": "PERSON", "users": "PERSON"}

type structureRef struct {
	tag  string
	attr string
}

func structureStructMine(fc refineInput) []slot {
	doc := fc.parsed()
	bodies := make([]string, len(doc.Lines))
	for i, l := range doc.Lines {
		bodies[i] = l.Body
	}
	slots := append([]slot(nil), fc.Slots...)
	slots = structureCover(bodies, slots)

	known := map[string]string{}
	for _, s := range slots {
		p, ok := dictionary.parse(s.Tag)
		if !ok {
			continue
		}
		words := structureWordRe.FindAllString(s.Value, -1)
		if p.Kind == tagPerson && (p.Attribute == "FULL_NAME" || p.Attribute == "FIRST_NAME" || p.Attribute == "LAST_NAME" || p.Attribute == "NICKNAME" || p.Attribute == "MIDDLE_NAME") {
			for i, w := range words {
				f := structureFold(w)
				if !structureUsable(f) {
					continue
				}
				attr := p.Attribute
				if attr == "FULL_NAME" {
					attr = "MIDDLE_NAME"
					if i == 0 {
						attr = "FIRST_NAME"
					} else if i == len(words)-1 {
						attr = "LAST_NAME"
					}
				}
				if _, seen := known[f]; !seen {
					known[f] = "{{" + structureRoleName(p) + "_" + attr + "}}"
				}
			}
		}
		if (p.Kind == tagEntity && p.Attribute == "NAME") || (p.Kind == tagPerson && p.Attribute == "EMPLOYER") {
			f := structureFold(strings.Join(words, ""))
			if _, seen := known[f]; !seen && len(f) >= 3 {
				known[f] = s.Tag
			}
			if len(words) == 1 || (len(words) > 1 && !isCommonWord(words[0])) {
				f0 := structureFold(words[0])
				if _, seen := known[f0]; !seen && structureUsable(f0) && len(words) == 1 {
					known[f0] = s.Tag
				}
			}
		}
	}

	var cands []structureCandidate
	for _, s := range slots {
		cands = append(cands, structureMine(s)...)
	}
	cands = append(cands, structureFileCandidates(bodies, fc)...)
	cands = append(cands, structureSenderCandidates(fc, bodies)...)
	slots = structurePhones(bodies, slots, fc)

	personTok := map[string]string{}
	orgTok := map[string]string{}
	compounds := map[string]structureCandidate{}
	for _, c := range cands {
		if c.isOrg {
			for _, core := range structureOrgCores(c.compound) {
				if !structureUsable(core) {
					continue
				}
				if _, ok := orgTok[core]; !ok {
					orgTok[core] = structureOrgTag(c.srcTag)
				}
			}
			continue
		}
		for i, p := range c.parts {
			if !structureUsable(p) && len(c.parts) == 1 {
				continue
			}
			if !structureUsable(p) {
				continue
			}
			attr := "FIRST_NAME"
			if i > 0 && i == len(c.parts)-1 {
				attr = "LAST_NAME"
			} else if i > 0 {
				attr = "MIDDLE_NAME"
			}
			if _, ok := personTok[p]; !ok {
				personTok[p] = structurePersonTag(c.srcTag, attr)
			}
		}
		if len(c.compound) >= 6 {
			compounds[c.compound] = c
		}
	}

	var added []slot
	for li, body := range bodies {
		words := structureWords(body)
		for i := 0; i < len(words); i++ {
			matched := false
			for k := 3; k >= 2 && !matched; k-- {
				if i+k > len(words) {
					continue
				}
				if !structureAdjacent(body, words[i:i+k]) {
					continue
				}
				var cat strings.Builder
				for _, w := range words[i : i+k] {
					cat.WriteString(w.fold)
				}
				key := cat.String()
				if c, ok := compounds[key]; ok {
					for j, w := range words[i : i+k] {
						attr := "FIRST_NAME"
						if j == k-1 {
							attr = "LAST_NAME"
						} else if j > 0 {
							attr = "MIDDLE_NAME"
						}
						tag := known[w.fold]
						if tag == "" {
							tag = structurePersonTag(c.srcTag, attr)
						}
						added = append(added, structureNewSlot(li, w.start, w.end, body, tag, "NAME"))
					}
					i += k - 1
					matched = true
					continue
				}
				if tag, ok := orgTok[key]; ok {
					if kt := known[key]; kt != "" {
						tag = kt
					}
					added = append(added, structureNewSlot(li, words[i].start, words[i+k-1].end, body, tag, "ORG"))
					i += k - 1
					matched = true
				}
			}
			if matched {
				continue
			}
			w := words[i]
			if tag, ok := orgTok[w.fold]; ok {
				if kt := known[w.fold]; kt != "" {
					tag = kt
				}
				added = append(added, structureNewSlot(li, w.start, w.end, body, tag, "ORG"))
				continue
			}
			if tag, ok := personTok[w.fold]; ok {
				if kt := known[w.fold]; kt != "" {
					tag = kt
				}
				added = append(added, structureNewSlot(li, w.start, w.end, body, tag, "NAME"))
				continue
			}
			if c, ok := compounds[w.fold]; ok && len(c.parts) > 0 {
				tag := known[w.fold]
				if tag == "" {
					tag = structurePersonTag(c.srcTag, "FULL_NAME")
				}
				added = append(added, structureNewSlot(li, w.start, w.end, body, tag, "NAME"))
				continue
			}
			if tag, ok := orgTok[w.fold]; ok {
				if kt := known[w.fold]; kt != "" {
					tag = kt
				}
				added = append(added, structureNewSlot(li, w.start, w.end, body, tag, "ORG"))
			}
		}
	}
	return structureMerge(slots, added)
}

func structureAdjacent(body string, ws []structureWord) bool {
	for i := 1; i < len(ws); i++ {
		gap := body[ws[i-1].end:ws[i].start]
		if len(gap) > 2 || strings.TrimLeft(gap, " -_.&") != "" {
			return false
		}
	}
	return true
}

func structureNewSlot(line, start, end int, body, tag, cat string) slot {
	if !dictionary.valid(tag) {
		if cat == "ORG" {
			tag = "{{ORGANIZATION_NAME}}"
		} else {
			tag = "{{PERSON_FULL_NAME}}"
		}
	}
	return slot{Line: line, Start: start, End: end, Tag: tag, Value: body[start:end], Category: cat, Source: "s06", Confidence: structureConfidence}
}

func structureMerge(slots, added []slot) []slot {
	byLine := map[int][]slot{}
	for _, s := range slots {
		byLine[s.Line] = append(byLine[s.Line], s)
	}
	out := append([]slot(nil), slots...)
	for _, a := range added {
		clash := false
		for _, s := range byLine[a.Line] {
			if a.Start < s.End && s.Start < a.End {
				clash = true
				break
			}
		}
		if !clash {
			out = append(out, a)
			byLine[a.Line] = append(byLine[a.Line], a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Start < out[j].Start
	})
	return out
}

func structureCover(bodies []string, slots []slot) []slot {
	valueTag := map[string]string{}
	for _, s := range slots {
		k := strings.ToLower(strings.TrimSpace(s.Value))
		if _, ok := valueTag[k]; !ok {
			valueTag[k] = s.Tag
		}
	}
	byLine := map[int][]int{}
	for i, s := range slots {
		byLine[s.Line] = append(byLine[s.Line], i)
	}
	drop := map[int]bool{}
	var extra []slot
	for li, body := range bodies {
		type span struct {
			a, b int
			def  string
		}
		var spans []span
		for _, m := range structureURLRe.FindAllStringIndex(body, -1) {
			b := m[1]
			for b > m[0] && strings.ContainsRune(".,;:!?)]}'\"*", rune(body[b-1])) {
				b--
			}
			spans = append(spans, span{m[0], b, "{{LINK}}"})
		}
		for _, m := range structureEmailRe.FindAllStringIndex(body, -1) {
			inside := false
			for _, s := range spans {
				if m[0] >= s.a && m[1] <= s.b {
					inside = true
				}
			}
			if !inside {
				spans = append(spans, span{m[0], m[1], "{{EMAIL_ADDRESS}}"})
			}
		}
		for _, m := range structurePhoneRe.FindAllStringIndex(body, -1) {
			if len(onlyDigits(body[m[0]:m[1]])) >= 8 {
				spans = append(spans, span{m[0], m[1], "{{PHONE_NUMBER}}"})
			}
		}
		for _, sp := range spans {
			if sp.b <= sp.a {
				continue
			}
			var over []int
			beyond := false
			covered := false
			for _, idx := range byLine[li] {
				s := slots[idx]
				if drop[idx] {
					continue
				}
				if s.Start < sp.b && sp.a < s.End {
					over = append(over, idx)
					if s.Start < sp.a || s.End > sp.b {
						beyond = true
					}
					if s.Start <= sp.a && s.End >= sp.b {
						covered = true
					}
				}
			}
			if covered || beyond {
				continue
			}
			tag := valueTag[strings.ToLower(body[sp.a:sp.b])]
			best := -1
			for _, idx := range over {
				if best < 0 || slots[idx].End-slots[idx].Start > slots[best].End-slots[best].Start {
					best = idx
				}
			}
			if tag == "" && best >= 0 {
				tag = slots[best].Tag
			}
			if tag == "" || !dictionary.valid(tag) {
				tag = sp.def
			}
			conf := 0.9
			for _, idx := range over {
				drop[idx] = true
				if slots[idx].Confidence > conf {
					conf = slots[idx].Confidence
				}
			}
			extra = append(extra, slot{Line: li, Start: sp.a, End: sp.b, Tag: tag, Value: body[sp.a:sp.b], Category: "COVER", Source: "s06", Confidence: conf})
		}
	}
	var out []slot
	for i, s := range slots {
		if !drop[i] {
			out = append(out, s)
		}
	}
	return append(out, extra...)
}

func structureFileCandidates(bodies []string, fc refineInput) []structureCandidate {
	var out []structureCandidate
	for _, body := range bodies {
		if !strings.Contains(body, "attached:") && !strings.Contains(body, "document omitted") && !structurePattern4.MatchString(strings.TrimSpace(body)) {
			continue
		}
		for _, m := range structureFileRe.FindAllStringSubmatch(body, -1) {
			name := strings.TrimSpace(m[1])
			if strings.HasPrefix(name, "<attached") || structurePattern5.MatchString(name) {
				continue
			}
			var parts []string
			for _, chunk := range structurePattern6.Split(name, -1) {
				for _, p := range structureCamelSplit(chunk) {
					f := structureFold(p)
					if f == "" || structureFileGeneric[f] || isCommonWord(f) || len([]rune(f)) < 3 {
						continue
					}
					if structurePattern3.MatchString(f) {
						continue
					}
					parts = append(parts, f)
				}
			}
			if len(parts) == 0 || len(parts) > 4 {
				continue
			}
			out = append(out, structureCandidate{compound: strings.Join(parts, ""), parts: parts, srcTag: "{{PERSON_FULL_NAME}}"})
		}
	}
	return out
}

func structureNameLike(token string, bodies []string) bool {
	upper, lower := 0, 0
	for _, b := range bodies {
		for _, w := range structureWordRe.FindAllString(b, -1) {
			if structureFold(w) != token {
				continue
			}
			r := []rune(w)[0]
			if unicode.IsUpper(r) {
				upper++
			} else {
				lower++
			}
		}
	}
	return lower*4 <= upper
}

func structureSenderCandidates(fc refineInput, bodies []string) []structureCandidate {
	var out []structureCandidate
	names := make([]string, 0, len(fc.SenderTags))
	for n := range fc.SenderTags {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		tag := fc.SenderTags[name]
		if onlyDigits(name) != "" && len(onlyDigits(name)) >= 7 {
			continue
		}
		p, ok := dictionary.parse(tag)
		if ok && p.Kind == tagPerson {
			var parts []string
			for _, w := range structureWordRe.FindAllString(name, -1) {
				f := structureFold(w)
				if structureUsable(f) {
					parts = append(parts, f)
				}
			}
			if len(parts) > 0 {
				out = append(out, structureCandidate{compound: strings.Join(parts, ""), parts: parts, srcTag: tag})
			}
			continue
		}
		for _, w := range structureWordRe.FindAllString(name, -1) {
			f := structureFold(w)
			if structureUsable(f) && len(f) >= 4 && structureNameLike(f, bodies) {
				out = append(out, structureCandidate{compound: f, isOrg: true, srcTag: "{{ORGANIZATION_NAME}}"})
			}
		}
	}
	return out
}

var structureDigitRunRe = regexp.MustCompile(`\+?\d[\d \-().]{5,}\d`)

func structurePhones(bodies []string, slots []slot, fc refineInput) []slot {
	known := map[string]string{}
	for n, tag := range fc.SenderTags {
		if d := onlyDigits(n); len(d) >= 7 && strings.HasPrefix(strings.TrimSpace(n), "+") {
			known[d] = tag
		}
	}
	for _, s := range slots {
		if d := onlyDigits(s.Value); len(d) >= 8 && (strings.Contains(s.Tag, "PHONE") || s.Category == "PHONE") {
			if _, ok := known[d]; !ok {
				known[d] = s.Tag
			}
		}
	}
	if len(known) == 0 {
		return slots
	}
	byLine := map[int][]slot{}
	for _, s := range slots {
		byLine[s.Line] = append(byLine[s.Line], s)
	}
	var added []slot
	for li, body := range bodies {
		for _, m := range structureDigitRunRe.FindAllStringIndex(body, -1) {
			d := onlyDigits(body[m[0]:m[1]])
			if len(d) < 7 {
				continue
			}
			tag := ""
			for kd, kt := range known {
				if kd == d || (len(d) >= 9 && strings.HasSuffix(kd, d)) || (len(kd) >= 9 && strings.HasSuffix(d, kd)) {
					if tag == "" || kt < tag {
						tag = kt
					}
				}
			}
			if tag == "" || !dictionary.valid(tag) {
				continue
			}
			clash := false
			for _, s := range byLine[li] {
				if m[0] < s.End && s.Start < m[1] {
					clash = true
				}
			}
			if !clash {
				added = append(added, slot{Line: li, Start: m[0], End: m[1], Tag: tag, Value: body[m[0]:m[1]], Category: "PHONE", Source: "s06", Confidence: 0.9})
			}
		}
	}
	return append(slots, added...)
}

var (
	structurePattern1 = regexp.MustCompile(`[^\p{L}]+`)
	structurePattern2 = regexp.MustCompile(`-[0-9a-z]*\d[0-9a-z]*$`)
	structurePattern3 = regexp.MustCompile(`\d`)
	structurePattern4 = regexp.MustCompile(`(?i)\.(pdf|docx?|vcf)\s*$`)
	structurePattern5 = regexp.MustCompile(`^\d+$`)
	structurePattern6 = regexp.MustCompile(`[\s_\-–.()]+`)
)
