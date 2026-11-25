package main

import (
	"math/big"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nyaruka/phonenumbers"
)

const (
	maskThreshold = 0.85
	catName       = "NAME"
	catPhone      = "PHONE"
	catEmail      = "EMAIL"
	catHandle     = "HANDLE"
	catProfile    = "PROFILE_LINK"
	catInvite     = "INVITE_LINK"
	catMaps       = "MAPS_LINK"
	catLocation   = "SHARED_LOCATION"
	catPayment    = "PAYMENT_LINK"
	catMeeting    = "MEETING_LINK"
	catCalendar   = "CALENDAR_LINK"
	catForm       = "FORM_LINK"
	catDocument   = "DOCUMENT_LINK"
	catPhotos     = "PHOTO_ALBUM_LINK"
	catLink       = "LINK"
	catEventLink  = "EVENT_INVITE_LINK"
	catVideoLink  = "VIDEO_LINK"
	catPostLink   = "SOCIAL_POST_LINK"
	catDomain     = "WEBSITE"
	catWhatsAppID = "WHATSAPP_ID"
	catCard       = "CARD_NUMBER"
	catCVV        = "CARD_CVV"
	catIBAN       = "IBAN"
	catID         = "ID_NUMBER"
	catAge        = "AGE"
	catBirthDate  = "BIRTH_DATE"
	catBirthYear  = "BIRTH_YEAR"
	catPostcode   = "POSTCODE"
	catAddress    = "ADDRESS"
)

type detection struct {
	Start      int
	End        int
	Value      string
	Category   string
	Hint       string
	Confidence float64
	Person     string
}

type nameEntry struct {
	text   string
	person string
	part   string
	common bool
	plain  bool
	own    bool
}

type detector struct {
	names       *regexp.Regexp
	nameEntries map[string][]nameEntry
	region      string
	book        *peopleBook
}

var (
	phoneCandidate = regexp.MustCompile(`(?:\+|00)?\d[\d \t().\-]{5,18}\d`)
	datePattern    = regexp.MustCompile(`^\d{1,4}[./\-]\d{1,2}[./\-]\d{1,4}$`)
	timePattern    = regexp.MustCompile(`^\d{1,2}[:.]\d{2}`)
	emailPattern   = regexp.MustCompile(`[\p{L}\p{N}._%+\-]+@[\p{L}\p{N}.\-]+\.\p{L}{2,}`)
	domainPattern  = regexp.MustCompile(`(?i)\b(?:[\p{L}\p{N}][\p{L}\p{N}\-]*\.)+(?:com|net|org|io|ai|co|app|dev|me|health|clinic|tr|uk|de|fr|es|it|nl|ca|us|info|biz|xyz|so|gg|tv|ly|to|eu|ch|at|be|se|no|dk|pl|ru|ae|sa|et|ke|ng|za|in|au|nz|br|mx|jp|cn|kr)\b(?:/[^\s<>"']*)?`)
	urlPattern     = regexp.MustCompile(`(?i)\b(?:https?://|www\.)[^\s<>"'‎]+|\b(?:instagram\.com|facebook\.com|fb\.com|tiktok\.com|twitter\.com|x\.com|linkedin\.com|t\.me|wa\.me|snapchat\.com|youtube\.com|chat\.whatsapp\.com|calendly\.com|paypal\.me|revolut\.me|maps\.app\.goo\.gl|goo\.gl|forms\.gle)/[^\s<>"'‎]+`)
	handlePattern  = regexp.MustCompile(`(?:^|[\s(,;:])@([\p{L}\p{N}_.]{2,30})`)
	cardPattern    = regexp.MustCompile(`\b(?:\d[ \-]?){12,18}\d\b`)
	cvvPattern     = regexp.MustCompile(`(?i)\b(?:cvv|cvc|cv2|security code)\W{0,6}(\d{3,4})\b`)
	ibanPattern    = regexp.MustCompile(`\b[A-Z]{2}\d{2}(?: ?[A-Z0-9]){11,30}\b`)
	idPattern      = regexp.MustCompile(`(?i)\b(passport|pasaport|passeport|pasaporte|reisepass|passaporto|id card|identity card|national id|kimlik(?: no)?|t\.?c\.? ?(?:no|kimlik(?: no)?)|tckn|driver'?s? licen[cs]e|driving licen[cs]e|ehliyet|ssn|social security(?: number)?|nhs (?:no|number)|insurance (?:no|number)|policy (?:no|number)|visa (?:no|number)|residence permit|ikamet|medical record(?: number)?|patient (?:id|no|number)|booking (?:ref|reference|no|number)|reservation (?:code|no|number)|pnr)\b(?:\s*(?:no|number|num|nr|#)\.?)?\s*[:#=\-]?\s*([A-Z0-9][A-Z0-9\-]{3,20}\d[A-Z0-9\-]*|\d[A-Z0-9\-]{4,20})`)
	agePatterns    = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(\d{1,2}) ?(?:years? old|yrs? old|y/o|yo|year-old)\b`),
		regexp.MustCompile(`(?i)\b(?:aged?|age:)\s?(\d{1,2})\b`),
		regexp.MustCompile(`(?i)\b(\d{1,2}) ?yaş(?:ında|indayım|ındayım|inda)?\b`),
		regexp.MustCompile(`(?i)\b(\d{1,2}) ?jahre alt\b`),
		regexp.MustCompile(`(?i)\b(\d{1,2}) ?años\b`),
		regexp.MustCompile(`(?i)\bj'ai (\d{1,2}) ?ans\b`),
	}
	softAgePattern   = regexp.MustCompile(`(?i)\b(?:i'?m|i am|she'?s|he'?s|she is|he is)\s(\d{2})\b(?:\s|[.,!]|$)`)
	birthDatePrefix  = `(?i)\b(?:born(?: on)?|date of birth|d\.?o\.?b\.?|birthday(?: is)?|doğum tarihi(?:m)?|geboren(?: am)?|née? le|fecha de nacimiento)\W{0,10}`
	dateValue        = `(\d{1,2}[./\- ]\d{1,2}[./\- ]\d{2,4}|\d{4}-\d{2}-\d{2}|\d{1,2}(?:st|nd|rd|th)? (?:of )?(?:jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-zçğıöşü]*,? \d{2,4}|(?:jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]* \d{1,2}(?:st|nd|rd|th)?,? \d{4})`
	birthDatePattern = regexp.MustCompile(birthDatePrefix + dateValue)
	birthYearPattern = regexp.MustCompile(`(?i)\b(?:born in|born|doğum yılı|jahrgang)\s(19\d{2}|20[0-2]\d)\b`)
	ukPostcode       = regexp.MustCompile(`\b[A-Z]{1,2}\d[A-Z\d]? ?\d[A-Z]{2}\b`)
	usZip            = regexp.MustCompile(`\b(?:A[LKZR]|C[AOT]|D[EC]|FL|GA|HI|I[ADLN]|K[SY]|LA|M[EDAINSOT]|N[EVHJMYCD]|O[HKR]|PA|RI|S[CD]|T[NX]|UT|V[TA]|W[AVIY]) (\d{5}(?:-\d{4})?)\b`)
	stateNames       = `Alabama|Alaska|Arizona|Arkansas|California|Colorado|Connecticut|Delaware|Florida|Georgia|Hawaii|Idaho|Illinois|Indiana|Iowa|Kansas|Kentucky|Louisiana|Maine|Maryland|Massachusetts|Michigan|Minnesota|Mississippi|Missouri|Montana|Nebraska|Nevada|New Hampshire|New Jersey|New Mexico|New York|North Carolina|North Dakota|Ohio|Oklahoma|Oregon|Pennsylvania|Rhode Island|South Carolina|South Dakota|Tennessee|Texas|Utah|Vermont|Virginia|Washington|West Virginia|Wisconsin|Wyoming`
	spelledStateZip  = regexp.MustCompile(`\b(?:` + stateNames + `),?\s+\d{5}(?:-\d{4})?\b`)
	addressTail      = regexp.MustCompile(`^(?:,?\s*(?i:apt|apartment|unit|suite|ste|flat|floor|fl)\.?\s*[\p{L}\p{N}\-]+|,?\s*#\s*[\p{L}\p{N}\-]+)?(?:,?\s*(?:N|S|E|W|NE|NW|SE|SW)\b)?(?:,?\s*[\p{Lu}][\p{Ll}'’.\-]+(?:\s[\p{Lu}][\p{Ll}'’.\-]+){0,3})?(?:,?\s*[A-Z]{1,2}\d[A-Z\d]?\s?\d[A-Z]{2}\b|(?:,?\s*(?:[A-Z]{2}\b|` + stateNames + `))?(?:,?\s*\d{5}(?:-\d{4})?)?)`)
	streetPattern    = regexp.MustCompile(`(?i)\b\d{1,5}[a-z]?,?\s+(?:[\p{L}'’.\-]+\s){0,4}(?:street|st\.|road|rd\.?|avenue|ave\.?|lane|ln\.?|drive|boulevard|blvd\.?|way|court|ct\.?|close|place|terrace|crescent|sokak|sokağı|sk\.|caddesi|cad\.|cd\.|mahallesi|mah\.|straße|strasse|str\.|calle|rue|via)(?:\s|[.,]|$)|(?i)\b[\p{L}'’.\-]+\s(?:sokak|sokağı|sk\.|caddesi|cad\.|cd\.|mahallesi|mah\.|bulvarı|blv\.)\s?(?:no|numara)?\s?[:.]?\s?\d{1,4}(?:/\d{1,4})?`)
	locationLine     = regexp.MustCompile(`Location: (https://maps\.google\.com/\?q=[\-0-9.]+,[\-0-9.]+)`)
)

func newDetector(book *peopleBook) *detector {
	d := &detector{nameEntries: map[string][]nameEntry{}, book: book}
	if book.OwnerCode != "" {
		d.region = phonenumbers.GetRegionCodeForCountryCode(atoiSafe(book.OwnerCode))
	}
	var alternatives []string
	add := func(text string, e nameEntry) {
		key := strings.ToLower(text)
		if len(d.nameEntries[key]) == 0 {
			alternatives = append(alternatives, regexp.QuoteMeta(text))
		}
		d.nameEntries[key] = append(d.nameEntries[key], e)
	}
	for _, p := range book.snapshot(nil) {
		for _, name := range p.Names {
			name = cleanName(name)
			if utf8.RuneCountInString(name) < 2 || !hasLetter(name) {
				continue
			}
			parts := strings.Fields(name)
			if len(parts) > 1 {
				add(name, nameEntry{text: name, person: p.Key, part: "FULL_NAME", own: p.Own})
			}
			for i, part := range parts {
				if utf8.RuneCountInString(part) < 2 || !hasLetter(part) {
					continue
				}
				role := "FIRST_NAME"
				switch {
				case len(parts) == 1:
					role = "FIRST_NAME"
				case i == len(parts)-1:
					role = "LAST_NAME"
				case i > 0:
					role = "MIDDLE_NAME"
				}
				add(part, nameEntry{text: part, person: p.Key, part: role, common: isCommonWord(part), plain: notANameAlone(part), own: p.Own})
			}
		}
	}
	if len(alternatives) > 0 {
		sort.Slice(alternatives, func(i, j int) bool { return len(alternatives[i]) > len(alternatives[j]) })
		d.names = regexp.MustCompile(`(?i)(?:` + strings.Join(alternatives, "|") + `)`)
	}
	return d
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func cleanName(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' || r == '-' || r == '\'' || r == '’' || r == '.' {
			return r
		}
		return ' '
	}, name)
	return strings.Join(strings.Fields(name), " ")
}

func hasLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func isApostrophe(r rune) bool {
	return r == '\'' || r == '’'
}

func boundaryOK(text string, start, end int) bool {
	if start > 0 {
		r, size := utf8.DecodeLastRuneInString(text[:start])
		if isWordRune(r) {
			return false
		}
		if isApostrophe(r) && start-size > 0 {
			if before, _ := utf8.DecodeLastRuneInString(text[:start-size]); unicode.IsLetter(before) {
				return false
			}
		}
	}
	if end < len(text) {
		r, size := utf8.DecodeRuneInString(text[end:])
		if isWordRune(r) {
			return false
		}
		if isApostrophe(r) && end+size < len(text) {
			next, nsize := utf8.DecodeRuneInString(text[end+size:])
			possessive := (next == 's' || next == 'S') && (end+size+nsize == len(text) || !unicode.IsLetter(firstRune(text[end+size+nsize:])))
			if unicode.IsLetter(next) && !possessive {
				return false
			}
		}
	}
	return true
}

func firstRune(s string) rune {
	r, _ := utf8.DecodeRuneInString(s)
	return r
}

func trimURLEnd(text string, start, end int) int {
	for end > start && strings.ContainsRune(".,!?;:)]}'\"", rune(text[end-1])) {
		end--
	}
	return end
}

func startsUpper(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsUpper(r)
}

func (d *detector) detect(text string, chatPeople map[string]bool) []detection {
	var found []detection
	add := func(start, end int, cat, hint string, conf float64, person string) {
		if start < 0 || end > len(text) || start >= end {
			return
		}
		found = append(found, detection{Start: start, End: end, Value: text[start:end], Category: cat, Hint: hint, Confidence: conf, Person: person})
	}
	for _, m := range locationLine.FindAllStringSubmatchIndex(text, -1) {
		add(m[2], m[3], catLocation, "", 0.99, "")
	}
	for _, m := range emailPattern.FindAllStringIndex(text, -1) {
		add(m[0], m[1], catEmail, "", 0.99, "")
	}
	for _, m := range urlPattern.FindAllStringIndex(text, -1) {
		end := trimURLEnd(text, m[0], m[1])
		cat, conf := classifyURL(text[m[0]:end])
		add(m[0], end, cat, "", conf, "")
	}
	for _, m := range domainPattern.FindAllStringIndex(text, -1) {
		end := trimURLEnd(text, m[0], m[1])
		if m[0] > 0 && strings.ContainsRune("@/.", rune(text[m[0]-1])) {
			continue
		}
		value := text[m[0]:end]
		if strings.Contains(value, "/") {
			cat, conf := classifyURL(value)
			add(m[0], end, cat, "", conf, "")
			continue
		}
		add(m[0], end, catDomain, "", 0.88, "")
	}
	for _, m := range handlePattern.FindAllStringSubmatchIndex(text, -1) {
		start := m[2] - 1
		handle := text[m[2]:m[3]]
		if digits := onlyDigits(handle); len(digits) == len(handle) && len(digits) >= 8 {
			if len(digits) >= 13 {
				add(start, m[3], catWhatsAppID, "", 0.95, d.book.keyForLID(digits))
				continue
			}
			add(start, m[3], catPhone, "", 0.95, d.book.keyForPhone(digits))
			continue
		}
		if m[3] < len(text) && text[m[3]] == '@' || groupMentions[strings.ToLower(handle)] {
			continue
		}
		add(start, m[3], catHandle, "", 0.88, "")
	}
	for _, m := range ibanPattern.FindAllStringIndex(text, -1) {
		if validIBAN(text[m[0]:m[1]]) {
			add(m[0], m[1], catIBAN, "", 0.99, "")
		}
	}
	for _, m := range cardPattern.FindAllStringIndex(text, -1) {
		if plausibleCard(text[m[0]:m[1]], text) {
			add(m[0], m[1], catCard, "", 0.97, "")
		}
	}
	for _, span := range cardTails(text) {
		add(span[0], span[1], catCard, "", 0.9, "")
	}
	for _, m := range cvvPattern.FindAllStringSubmatchIndex(text, -1) {
		add(m[2], m[3], catCVV, "", 0.9, "")
	}
	for _, m := range idPattern.FindAllStringSubmatchIndex(text, -1) {
		add(m[4], m[5], catID, strings.ToLower(text[m[2]:m[3]]), 0.88, "")
	}
	for _, m := range birthDatePattern.FindAllStringSubmatchIndex(text, -1) {
		add(m[2], m[3], catBirthDate, "", 0.92, "")
	}
	for _, m := range birthYearPattern.FindAllStringSubmatchIndex(text, -1) {
		add(m[2], m[3], catBirthYear, "", 0.88, "")
	}
	for _, p := range agePatterns {
		for _, m := range p.FindAllStringSubmatchIndex(text, -1) {
			add(m[2], m[3], catAge, "", 0.9, "")
		}
	}
	for _, m := range softAgePattern.FindAllStringSubmatchIndex(text, -1) {
		add(m[2], m[3], catAge, "", 0.5, "")
	}
	d.detectPhones(text, add)
	for _, m := range ukPostcode.FindAllStringIndex(text, -1) {
		add(m[0], m[1], catPostcode, "", 0.8, "")
	}
	for _, m := range usZip.FindAllStringSubmatchIndex(text, -1) {
		add(m[2], m[3], catPostcode, "", 0.7, "")
	}
	for _, m := range spelledStateZip.FindAllStringIndex(text, -1) {
		add(m[0], m[1], catPostcode, "", 0.8, "")
	}
	for _, m := range streetPattern.FindAllStringIndex(text, -1) {
		end := m[1]
		for end > m[0] && strings.ContainsRune(" .,", rune(text[end-1])) {
			end--
		}
		if tail := addressTail.FindStringIndex(text[end:]); tail != nil && tail[1] > 0 {
			end += tail[1]
		}
		add(m[0], end, catAddress, "", 0.75, "")
	}
	d.detectNames(text, chatPeople, add)
	return resolveOverlaps(found)
}

func (d *detector) detectPhones(text string, add func(int, int, string, string, float64, string)) {
	for _, m := range phoneCandidate.FindAllStringIndex(text, -1) {
		raw := strings.TrimSpace(text[m[0]:m[1]])
		digits := onlyDigits(raw)
		if len(digits) < 7 || len(digits) > 15 || datePattern.MatchString(raw) || timePattern.MatchString(raw) {
			continue
		}
		if m[0] > 0 && strings.ContainsRune("€$£₺¥#", lastRune(text[:m[0]])) {
			continue
		}
		person := d.book.keyForPhone(digits)
		conf := 0.0
		if person != "" {
			conf = 0.98
		} else if d.validPhone(raw) {
			conf = 0.93
		}
		if conf == 0 {
			if strings.HasPrefix(raw, "+") && len(digits) >= 9 {
				conf = 0.6
			} else {
				continue
			}
		}
		add(m[0], m[1], catPhone, "", conf, person)
	}
}

func lastRune(s string) rune {
	s = strings.TrimRight(s, " ")
	r, _ := utf8.DecodeLastRuneInString(s)
	return r
}

func (d *detector) validPhone(raw string) bool {
	normalized := raw
	if strings.HasPrefix(normalized, "00") {
		normalized = "+" + normalized[2:]
	}
	region := d.region
	if strings.HasPrefix(normalized, "+") {
		region = ""
	}
	if region == "" && !strings.HasPrefix(normalized, "+") {
		return false
	}
	num, err := phonenumbers.Parse(normalized, region)
	if err != nil {
		return false
	}
	return phonenumbers.IsValidNumber(num)
}

func (d *detector) detectNames(text string, chatPeople map[string]bool, add func(int, int, string, string, float64, string)) {
	if d.names == nil {
		return
	}
	for _, m := range d.names.FindAllStringIndex(text, -1) {
		if !boundaryOK(text, m[0], m[1]) {
			continue
		}
		value := text[m[0]:m[1]]
		entries := d.nameEntries[strings.ToLower(value)]
		if len(entries) == 0 {
			continue
		}
		inChat := func(x nameEntry) bool { return x.own || chatPeople[x.person] }
		e := entries[0]
		for _, candidate := range entries {
			if inChat(candidate) && !inChat(e) {
				e = candidate
			}
		}
		conf := 0.9
		lower := strings.ToLower(value)
		switch {
		case noticePattern.MatchString(text):
			continue
		case (e.common || isCommonWord(lower)) && !(inChat(e) && startsUpper(value)):
			continue
		case e.plain && !(inChat(e) && startsUpper(value)):
			continue
		case e.common:
			conf = 0.45
		case !inChat(e) && e.part != "FULL_NAME":
			conf = 0.55
		case !inChat(e):
			conf = 0.8
		case !startsUpper(value) && e.part != "FULL_NAME":
			conf = 0.75
		}
		person := e.person
		if len(entries) > 1 {
			people := map[string]bool{}
			for _, x := range entries {
				people[x.person] = true
			}
			if len(people) > 1 {
				person = ""
			}
		}
		add(m[0], m[1], catName, e.part, conf, person)
	}
}

func classifyURL(raw string) (string, float64) {
	link := raw
	if !strings.Contains(strings.ToLower(link), "://") {
		link = "https://" + link
	}
	u, err := url.Parse(link)
	if err != nil {
		return catLink, 0.9
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	path := strings.ToLower(u.Path)
	segments := strings.Split(strings.Trim(path, "/"), "/")
	first := ""
	if len(segments) > 0 {
		first = segments[0]
	}
	switch {
	case host == "lu.ma" || host == "luma.com" || host == "partiful.com" || host == "posh.vip" || strings.Contains(host, "eventbrite.") || host == "meetup.com" || host == "rsvp.withgoogle.com":
		return catEventLink, 0.95
	case host == "youtu.be" || (host == "youtube.com" && (first == "watch" || first == "shorts" || first == "live")) || host == "vimeo.com" || host == "loom.com" || host == "www.loom.com":
		return catVideoLink, 0.92
	case (host == "instagram.com" && (first == "p" || first == "reel" || first == "reels" || first == "stories")) || (host == "tiktok.com" && strings.Contains(path, "/video/")) || ((host == "x.com" || host == "twitter.com") && strings.Contains(path, "/status/")) || (host == "linkedin.com" && (first == "posts" || first == "feed")) || (host == "facebook.com" && (first == "share" || first == "photo" || first == "posts" || strings.Contains(path, "/posts/"))):
		return catPostLink, 0.92
	case host == "figma.com" || strings.HasSuffix(host, ".notion.site") || host == "notion.so" || host == "airtable.com" || host == "canva.com" || host == "miro.com" || host == "docsend.com" || host == "pitch.com":
		return catDocument, 0.92
	case host == "chat.whatsapp.com":
		return catInvite, 0.99
	case host == "wa.me" || host == "api.whatsapp.com":
		return catProfile, 0.97
	case host == "maps.google.com" || strings.HasPrefix(host, "maps.app.goo.gl") || host == "maps.apple.com" || (host == "goo.gl" && first == "maps") || (strings.HasPrefix(host, "google.") && first == "maps"):
		return catMaps, 0.95
	case host == "paypal.me" || host == "revolut.me" || host == "monzo.me" || host == "buy.stripe.com" || host == "checkout.stripe.com" || host == "iyzi.link" || strings.HasPrefix(host, "pay.") || (host == "wise.com" && first == "pay") || (host == "paypal.com" && first == "paypalme"):
		return catPayment, 0.95
	case strings.HasSuffix(host, "zoom.us") || host == "meet.google.com" || host == "teams.microsoft.com" || host == "teams.live.com" || host == "whereby.com":
		return catMeeting, 0.95
	case host == "calendly.com" || host == "cal.com":
		return catCalendar, 0.9
	case host == "forms.gle" || (host == "docs.google.com" && first == "forms") || strings.HasSuffix(host, "typeform.com") || strings.HasSuffix(host, "jotform.com") || host == "tally.so":
		return catForm, 0.92
	case host == "photos.app.goo.gl" || host == "photos.google.com" || (host == "icloud.com" && first == "photos"):
		return catPhotos, 0.92
	case host == "docs.google.com" || host == "drive.google.com" || strings.HasSuffix(host, "dropbox.com") || host == "wetransfer.com" || host == "we.tl" || strings.HasSuffix(host, "onedrive.live.com") || host == "1drv.ms" || host == "icloud.com":
		return catDocument, 0.9
	case (host == "instagram.com" || host == "facebook.com" || host == "fb.com" || host == "m.facebook.com" || host == "twitter.com" || host == "x.com" || host == "snapchat.com" || host == "t.me") && first != "" && !isPublicPath(first):
		return catProfile, 0.9
	case host == "tiktok.com" && strings.HasPrefix(first, "@"):
		return catProfile, 0.9
	case host == "youtube.com" && strings.HasPrefix(first, "@"):
		return catProfile, 0.85
	case host == "linkedin.com" && first == "in":
		return catProfile, 0.92
	}
	return catLink, 0.9
}

func isPublicPath(segment string) bool {
	switch segment {
	case "p", "reel", "reels", "explore", "stories", "tv", "watch", "share", "status", "hashtag", "groups", "events", "photo", "photo.php", "story.php", "permalink.php", "i", "search", "home":
		return true
	}
	return false
}

var cardWords = regexp.MustCompile(`(?i)\b(?:card|kart|kartı|visa|mastercard|master card|amex|american express|credit|debit|kredi|cvv|cvc|expiry|exp)\b`)

func validIIN(d string) bool {
	n := len(d)
	prefix := func(k int) int { return atoiSafe(d[:k]) }
	switch {
	case d[0] == '4':
		return n == 13 || n == 16 || n == 19
	case prefix(2) >= 51 && prefix(2) <= 55, prefix(4) >= 2221 && prefix(4) <= 2720:
		return n == 16
	case prefix(2) == 34 || prefix(2) == 37:
		return n == 15
	case prefix(4) == 6011 || prefix(2) == 65 || (prefix(3) >= 644 && prefix(3) <= 649):
		return n >= 16 && n <= 19
	case prefix(4) >= 3528 && prefix(4) <= 3589:
		return n >= 16 && n <= 19
	case prefix(2) == 36 || prefix(2) == 38 || (prefix(3) >= 300 && prefix(3) <= 305):
		return n == 14
	}
	return false
}

func plausibleCard(raw, context string) bool {
	digits := onlyDigits(raw)
	if len(digits) < 13 || len(digits) > 19 || !luhn(digits) || !validIIN(digits) {
		return false
	}
	groups := strings.FieldsFunc(raw, func(r rune) bool { return r == ' ' || r == '-' })
	var lens []string
	for _, g := range groups {
		lens = append(lens, strconv.Itoa(len(g)))
	}
	switch strings.Join(lens, ",") {
	case "4,4,4,4", "4,4,4,4,3", "4,6,5", "4,6,4", "4,4,4,1":
		return true
	}
	return len(groups) == 1 && cardWords.MatchString(context)
}

func luhn(digits string) bool {
	sum := 0
	double := false
	for i := len(digits) - 1; i >= 0; i-- {
		n := int(digits[i] - '0')
		if double {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
		double = !double
	}
	return sum%10 == 0 && !allSame(digits)
}

func allSame(s string) bool {
	for i := 1; i < len(s); i++ {
		if s[i] != s[0] {
			return false
		}
	}
	return true
}

func validIBAN(raw string) bool {
	iban := strings.ToUpper(strings.ReplaceAll(raw, " ", ""))
	if len(iban) < 15 || len(iban) > 34 {
		return false
	}
	rearranged := iban[4:] + iban[:4]
	var b strings.Builder
	for _, r := range rearranged {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteString(big.NewInt(int64(r - 'A' + 10)).String())
		default:
			return false
		}
	}
	n, ok := new(big.Int).SetString(b.String(), 10)
	return ok && new(big.Int).Mod(n, big.NewInt(97)).Int64() == 1
}

func categoryPriority(cat string) int {
	switch cat {
	case catLocation, catEmail, catInvite, catIBAN, catCard:
		return 5
	case catProfile, catMaps, catPayment, catMeeting, catCalendar, catForm, catDocument, catPhotos, catEventLink, catVideoLink, catPostLink, catLink:
		return 4
	case catWhatsAppID:
		return 3
	case catPhone, catID, catBirthDate:
		return 3
	case catHandle, catDomain, catAddress:
		return 2
	}
	return 1
}

func resolveOverlaps(found []detection) []detection {
	sort.SliceStable(found, func(i, j int) bool {
		a, b := found[i], found[j]
		if pa, pb := categoryPriority(a.Category), categoryPriority(b.Category); pa != pb {
			return pa > pb
		}
		if a.Confidence != b.Confidence {
			return a.Confidence > b.Confidence
		}
		return a.End-a.Start > b.End-b.Start
	})
	var kept []detection
	for _, d := range found {
		overlaps := false
		for _, k := range kept {
			if d.Start < k.End && k.Start < d.End {
				overlaps = true
				break
			}
		}
		if !overlaps {
			kept = append(kept, d)
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Start < kept[j].Start })
	return kept
}

var commonWords = setOf(strings.Fields(`
a about above after again against all am an and any are as at be because been before being below between both but by
can could did do does doing down during each few for from further had has have having he her here hers herself him
himself his how i if in into is it its itself just me more most my myself no nor not now of off on once only or other
our ours ourselves out over own same she should so some such than that the their theirs them themselves then there
these they this those through to too under until up very was we were what when where which while who whom why will
with would you your yours yourself yourselves yes ok okay hi hello hey thanks thank please sorry good great nice
fine sure well love dear sir madam mr mrs ms dr doctor team support admin office info sales clinic hotel hospital
may june april august march will mark bill grace hope joy faith rose lily daisy ruby pearl amber summer autumn
winter spring dawn eve star sky sunny honey angel baby sugar max ray art jack jean mike frank pat sue gene dean
rich bob guy hunter page chase cash bishop miller baker cook king young brown white green black gray grey long short
bear wolf fox rocky storm river ocean brook dale glen cliff stone wood woods field fields hill hills lake moon sun
mom mum dad mama papa bro sis brother sister uncle aunt cousin son daughter wife husband friend boss
monday tuesday wednesday thursday friday saturday sunday january february july september october november december
today tomorrow yesterday morning evening night week month year time day now soon later here there home work
new old big small best better last first next one two three four five six seven eight nine ten
deniz ilk umut ay su
`))

func isCommonWord(word string) bool {
	return commonWords[strings.ToLower(word)]
}
