package main

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var dateMonths = []string{
	"january", "february", "march", "april", "may", "june", "july", "august", "september", "october", "november", "december",
	"jan", "feb", "mar", "apr", "jun", "jul", "aug", "sep", "sept", "oct", "nov", "dec",
	"ocak", "şubat", "subat", "mart", "nisan", "mayıs", "mayis", "haziran", "temmuz", "ağustos", "agustos", "eylül", "eylul", "ekim", "kasım", "kasim", "aralık", "aralik",
	"januar", "jänner", "februar", "märz", "maerz", "juni", "juli", "oktober", "dezember",
	"janvier", "février", "fevrier", "mars", "avril", "mai", "juin", "juillet", "août", "aout", "septembre", "octobre", "novembre", "décembre", "decembre",
	"enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre", "setiembre", "octubre", "noviembre", "diciembre",
	"yanayir", "yanayer", "fibrayir", "fabrayir", "maris", "yunyu", "yulyu", "ughustus", "aghustus", "sibtambir", "uktubar", "nufambir", "nofembir", "disambir",
	"kanun al-thani", "shubat", "adhar", "nisan", "ayyar", "huzayran", "tammuz", "aylul", "tishrin al-awwal", "tishrin al-thani", "kanun al-awwal",
	"يناير", "فبراير", "مارس", "أبريل", "ابريل", "مايو", "يونيو", "يوليو", "أغسطس", "اغسطس", "سبتمبر", "أكتوبر", "اكتوبر", "نوفمبر", "ديسمبر",
	"meskerem", "tikimt", "tikemt", "tekemt", "hidar", "hedar", "tahsas", "tahesas", "yekatit", "megabit", "miyazia", "miyazya", "ginbot", "genbot", "hamle", "nehase", "nehasse", "pagume", "pagumen",
	"መስከረም", "ጥቅምት", "ኅዳር", "ህዳር", "ታኅሣሥ", "ታህሳስ", "ጥር", "የካቲት", "መጋቢት", "ሚያዝያ", "ግንቦት", "ሰኔ", "ሐምሌ", "ሀምሌ", "ነሐሴ", "ጳጉሜ",
}

var dateWeekdays = []string{
	"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday",
	"mon", "tue", "tues", "wed", "thu", "thur", "thurs", "fri", "sat", "sun",
	"pazartesi", "salı", "sali", "çarşamba", "carsamba", "perşembe", "persembe", "cuma", "cumartesi", "pazar",
	"montag", "dienstag", "mittwoch", "donnerstag", "freitag", "samstag", "sonntag",
	"lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi", "dimanche",
	"lunes", "martes", "miércoles", "miercoles", "jueves", "viernes", "sábado", "sabado", "domingo",
	"ሰኞ", "ማክሰኞ", "ረቡዕ", "ሐሙስ", "ዓርብ", "ቅዳሜ", "እሁድ",
}

func dateAlt(words []string) string {
	seen := map[string]bool{}
	var list []string
	for _, w := range words {
		if !seen[w] {
			seen[w] = true
			list = append(list, regexp.QuoteMeta(w))
		}
	}
	sort.Slice(list, func(i, j int) bool { return len(list[i]) > len(list[j]) })
	return "(?:" + strings.Join(list, "|") + ")"
}

var (
	dateMon       = dateAlt(dateMonths)
	dateWd        = dateAlt(dateWeekdays)
	dateDay       = `[0-3]?\d(?:st|nd|rd|th|er|º)?\b\.?`
	dateYear      = `(?:\d{4}|'\d{2})`
	dateTrSuffix  = `(?:['’]?(?:nda|nde|ında|inde|unda|ünde|da|de|ta|te|ı|i|u|ü|a|e|ya|ye))?`
	dateWdLead    = `(?:` + dateWd + `\.?,?\s+(?:the\s+)?)?`
	dateTrDayTail = `(?:\s+` + dateWd + `(?:\s+(?:günü|gunu|günü))?)?`

	dateDayMonth = regexp.MustCompile(`(?i)` + dateWdLead + `(?:the\s+)?` + dateDay + `(?:\s*(?:[-–]|to|bis|au|al)\s*` + dateDay + `)?(?:\s+(?:of|de|del))?\s+` + dateMon + dateTrSuffix + `\.?(?:,?\s+` + dateYear + `)?` + dateTrDayTail)
	dateMonthDay = regexp.MustCompile(`(?i)` + dateWdLead + dateMon + `\.?\s+(?:the\s+)?` + dateDay + `(?:(?:\s*[-–]\s*|\s+to\s+)(?:` + dateMon + `\.?\s+)?` + dateDay + `|(?:,?\s+(?:and\s+|&\s+)?` + dateDay + `)+)?(?:,?\s+` + dateYear + `)?`)
	dateWdOrd    = regexp.MustCompile(`(?i)` + dateWd + `,?\s+(?:the\s+)?[0-3]?\d(?:st|nd|rd|th)\b`)
	dateNumFull  = regexp.MustCompile(`\b(\d{1,2})[./-](\d{1,2})[./-](\d{4}|\d{2})\b`)
	dateISO      = regexp.MustCompile(`\b(\d{4})-(\d{2})-(\d{2})\b`)
	dateNumShort = regexp.MustCompile(`\b(\d{1,2})[./](\d{1,2})\b`)
	dateOrdinal  = regexp.MustCompile(`(?i)\b(?:on\s+)?the\s+[0-3]?\d(?:st|nd|rd|th)\b`)
	dateTrCase   = regexp.MustCompile(`(?i)(?:^|[^\d.,:/])([0-3]?\d)\s?['’]?\s?(?:sinde|sında|inde|ında|ünde|unda|nde|nda|si|sı)\b`)
	dateAge      = regexp.MustCompile(`(?i)\b(\d{1,3})\s*(?:-|\s)?(?:years?[\s-]old|yrs?[\s-]old|y/o|yo\b|yaşında|yasinda|yaşındayım|yasindayim|yaşında|yaş\b|yas\b|jahre\s+alt|ans\b|años|anos|ዓመት|አመት)`)
	dateAgeLead  = regexp.MustCompile(`(?i)\b(?:age|aged|yaş|yaşı|yasi|alter|âge|edad)\s*:?\s*(\d{1,3})\b`)
	dateBirth    = regexp.MustCompile(`(?i)\b(?:born|birthday|birth\s*date|date\s+of\s+birth|dob|d\.o\.b|doğum\s+tarihi|dogum\s+tarihi|doğum|geboren|né|née|nacido|nacida)\b`)
	dateURL      = regexp.MustCompile(`(?i)(?:https?://|www\.)\S+|\S+\.(?:com|co|org|net|io|ai|africa|ng|tr|me|ly|app|gg)/\S*`)
	dateUnit     = regexp.MustCompile(`(?i)^\s*(?:%|k\b|m\b|months?|hours?|hrs?|mins?|minutes?|days?|weeks?|years?|stars?|out|usd|\$|tl|eur|ay\b|saat|gün|gun|yıl)`)
)

type dateCue struct {
	re  *regexp.Regexp
	tag string
}

var dateCues = []dateCue{
	{regexp.MustCompile(`(?i)surgery|işlem|islem|operation|operasyon|ameliyat|procedure|transplant|ekim\s+günü|sac ekimi|saç ekimi|rhinoplasty|implant|treatment|tedavi|op\b|Operation|opération|cirugía|ቀዶ`), "{{SURGERY_DATE}}"},
	{regexp.MustCompile(`(?i)consult|konsültasyon|muayene|online görüşme|video call`), "{{CONSULTATION_DATE}}"},
	{regexp.MustCompile(`(?i)check[\s-]?in|giriş`), "{{CHECK_IN_DATE}}"},
	{regexp.MustCompile(`(?i)check[\s-]?out|çıkış`), "{{CHECK_OUT_DATE}}"},
	{regexp.MustCompile(`(?i)arriv|land(?:ing|s)?\b|pick[\s-]?up|airport|varış|varis|geliş|gelis|geliyor|gelecek|ankunft|arrivée|llegada|come to|coming to|fly(?:ing)? in|flight in`), "{{ARRIVAL_DATE}}"},
	{regexp.MustCompile(`(?i)depart|leav(?:e|ing)|return(?:ing)? (?:home|flight)|fly(?:ing)? back|dönüş|donus|gidiyor|abflug|départ|salida|flight back`), "{{DEPARTURE_DATE}}"},
	{regexp.MustCompile(`(?i)follow[\s-]?up|kontrol|control|check[\s-]?up|post[\s-]?op|wash`), "{{FOLLOW_UP_DATE}}"},
	{regexp.MustCompile(`(?i)\bpa(?:y|id|yment)|ödeme|odeme|invoice|fatura|transfer(?:red)? (?:the )?(?:money|funds)|deposit|salary|maaş|commission`), "{{PAYMENT_DATE}}"},
	{regexp.MustCompile(`(?i)appointment|randevu|termin|rendez|cita|meeting|call\b|interview|toplantı|görüşme`), "{{APPOINTMENT_DATE}}"},
	{regexp.MustCompile(`(?i)event|party|rsvp|demo day|lunch|dinner|breakfast|meetup|picnic|tournament|summit|conference|webinar|workshop|hackathon|gala|festival|launch|ticket|register|game night|poker|class|session|\btura?\b|çekim|shooting`), "{{EVENT_DATE}}"},
}

var dateStrongCue = regexp.MustCompile(`(?i)surgery|operation|operasyon|ameliyat|procedure|arriv|landing|pick[\s-]?up|airport|flight|uçuş|ucus|varış|geliş|dönüş|donus|depart|check[\s-]?in|check[\s-]?out|appointment|randevu|consult|muayene|hotel|otel|transfer|travel|seyahat|trip|rendez|termin|cita|cirug|ankunft|abflug|llegada|vuelo|vol\b|arrivée|départ|treatment|tedavi|işlem|islem|procedure`)

type dateSpan struct {
	start, end int
	tag        string
}

func dateIsWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func dateBounded(body string, start, end int) bool {
	if start > 0 {
		r, _ := utf8.DecodeLastRuneInString(body[:start])
		if dateIsWord(r) || r == '/' || r == '-' || r == '_' || r == '=' {
			return false
		}
	}
	if end < len(body) {
		r, _ := utf8.DecodeRuneInString(body[end:])
		if dateIsWord(r) || r == '/' || r == '_' || r == '=' {
			return false
		}
		if r == '-' && end+1 < len(body) {
			n, _ := utf8.DecodeRuneInString(body[end+1:])
			if dateIsWord(n) {
				return false
			}
		}
	}
	return true
}

func dateClause(body string, start, end int) (int, int) {
	lo := start
	for lo > 0 {
		c := body[lo-1]
		if c == '\n' || c == '.' || c == '!' || c == '?' || c == ';' {
			break
		}
		lo--
	}
	hi := end
	for hi < len(body) {
		c := body[hi]
		if c == '\n' || c == '!' || c == '?' || c == ';' {
			break
		}
		if c == '.' && (hi+1 >= len(body) || body[hi+1] == ' ') {
			break
		}
		hi++
	}
	return lo, hi
}

func datePickTag(body string, start, end int, fallback string) string {
	lo, hi := dateClause(body, start, end)
	if start-lo > 80 {
		lo = start - 80
	}
	if hi-end > 60 {
		hi = end + 60
	}
	for lo > 0 && !utf8.RuneStart(body[lo]) {
		lo--
	}
	for hi < len(body) && !utf8.RuneStart(body[hi]) {
		hi++
	}
	best, bestDist := "", 1<<30
	for _, c := range dateCues {
		for _, m := range c.re.FindAllStringIndex(body[lo:hi], -1) {
			ms, me := m[0]+lo, m[1]+lo
			if ms < end && start < me {
				continue
			}
			d := start - me
			if ms >= end {
				d = 2*(ms-end) + 1
			}
			if d < bestDist {
				best, bestDist = c.tag, d
			}
		}
	}
	if best == "" {
		return fallback
	}
	return best
}

func dateHasStrongCue(body string, start, end int) bool {
	lo, hi := dateClause(body, start, end)
	return dateStrongCue.MatchString(body[lo:start]) || dateStrongCue.MatchString(body[end:hi])
}

func dateValidDM(d, m string) bool {
	di, _ := strconv.Atoi(d)
	mi, _ := strconv.Atoi(m)
	return di >= 1 && di <= 31 && mi >= 1 && mi <= 12
}

func dateTrimTail(body string, s, e int) (int, int) {
	for e > s {
		r, size := utf8.DecodeLastRuneInString(body[:e])
		if r == ',' || r == '.' || unicode.IsSpace(r) {
			e -= size
			continue
		}
		break
	}
	for s < e {
		r, size := utf8.DecodeRuneInString(body[s:])
		if r == ',' || unicode.IsSpace(r) {
			s += size
			continue
		}
		break
	}
	return s, e
}

func dateDetect(body string) []dateSpan {
	if !dateDigit.MatchString(body) {
		return nil
	}
	var spans []dateSpan
	urls := dateURL.FindAllStringIndex(body, -1)
	inURL := func(s, e int) bool {
		for _, u := range urls {
			if s < u[1] && u[0] < e {
				return true
			}
		}
		return false
	}
	loose := false
	add := func(s, e int, tag string) {
		s, e = dateTrimTail(body, s, e)
		bounded := dateBounded(body, s, e) || (loose && dateBounded(body[:e], 0, e))
		if s >= e || inURL(s, e) || !bounded {
			return
		}
		spans = append(spans, dateSpan{s, e, tag})
	}
	addDate := func(s, e int, fallback string) {
		ts, te := dateTrimTail(body, s, e)
		if ts >= te {
			return
		}
		tag := fallback
		if dateBirth.MatchString(body[max(0, ts-40):ts]) {
			tag = "{{PERSON_BIRTH_DATE}}"
		} else {
			tag = datePickTag(body, ts, te, fallback)
		}
		add(ts, te, tag)
	}
	for _, m := range dateDayMonth.FindAllStringIndex(body, -1) {
		if !dateDigit.MatchString(body[m[0]:m[1]]) || dateDayMonthNoise(body, m[0], m[1]) {
			continue
		}
		addDate(m[0], m[1], "{{APPOINTMENT_DATE}}")
	}
	for _, m := range dateMonthDay.FindAllStringIndex(body, -1) {
		if dateMonthDayNoise(body, m[0], m[1]) {
			continue
		}
		addDate(m[0], m[1], "{{APPOINTMENT_DATE}}")
	}
	for _, m := range dateWdOrd.FindAllStringIndex(body, -1) {
		addDate(m[0], m[1], "{{APPOINTMENT_DATE}}")
	}
	for _, m := range dateNumFull.FindAllStringSubmatchIndex(body, -1) {
		a, b := body[m[2]:m[3]], body[m[4]:m[5]]
		if dateValidDM(a, b) || dateValidDM(b, a) {
			addDate(m[0], m[1], "{{APPOINTMENT_DATE}}")
		}
	}
	for _, m := range dateISO.FindAllStringSubmatchIndex(body, -1) {
		if dateValidDM(body[m[6]:m[7]], body[m[4]:m[5]]) {
			addDate(m[0], m[1], "{{APPOINTMENT_DATE}}")
		}
	}
	for _, m := range dateNumShort.FindAllStringSubmatchIndex(body, -1) {
		a, b := body[m[2]:m[3]], body[m[4]:m[5]]
		if !(dateValidDM(a, b) || dateValidDM(b, a)) || a == b {
			continue
		}
		if dateUnit.MatchString(body[m[1]:]) || !dateHasStrongCue(body, m[0], m[1]) {
			continue
		}
		if m[0] > 0 && (body[m[0]-1] == '.' || body[m[0]-1] == '$' || body[m[0]-1] == ':') {
			continue
		}
		addDate(m[0], m[1], "{{APPOINTMENT_DATE}}")
	}
	for _, m := range dateOrdinal.FindAllStringIndex(body, -1) {
		if dateHasStrongCue(body, m[0], m[1]) {
			addDate(m[0], m[1], "{{APPOINTMENT_DATE}}")
		}
	}
	for _, m := range dateTrCase.FindAllStringSubmatchIndex(body, -1) {
		d, _ := strconv.Atoi(body[m[2]:m[3]])
		if d < 1 || d > 31 || !dateHasStrongCue(body, m[2], m[1]) {
			continue
		}
		loose = true
		addDate(m[2], m[1], "{{APPOINTMENT_DATE}}")
		loose = false
	}
	for _, m := range dateAge.FindAllStringSubmatchIndex(body, -1) {
		n, _ := strconv.Atoi(body[m[2]:m[3]])
		if n < 1 || n > 110 {
			continue
		}
		add(m[0], m[1], "{{PERSON_AGE}}")
	}
	for _, m := range dateAgeLead.FindAllStringSubmatchIndex(body, -1) {
		n, _ := strconv.Atoi(body[m[2]:m[3]])
		if n < 1 || n > 110 {
			continue
		}
		add(m[2], m[3], "{{PERSON_AGE}}")
	}
	return spans
}

var (
	dateDigit       = regexp.MustCompile(`\d`)
	dateClockTail   = regexp.MustCompile(`^[:.]\d`)
	dateAmbiguous   = regexp.MustCompile(`(?i)^(?:may|mar|mars|mai|sun|sat)$`)
	dateStrongish   = regexp.MustCompile(`\d{4}|\d(?:st|nd|rd|th)\b`)
	dateMonthWord   = regexp.MustCompile(`(?i)^(?:` + dateWd + `\.?,?\s+(?:the\s+)?)?(\pL+)`)
	dateLeadDecimal = regexp.MustCompile(`^\d+[.,]\d`)
)

func dateAmbiguousOK(body string, s, e int, month string) bool {
	if !dateAmbiguous.MatchString(month) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(month)
	return unicode.IsUpper(r) || dateStrongish.MatchString(body[s:e]) || dateHasStrongCue(body, s, e)
}

func dateDayMonthNoise(body string, s, e int) bool {
	v := body[s:e]
	if dateLeadDecimal.MatchString(v) {
		return true
	}
	if s > 0 {
		r, _ := utf8.DecodeLastRuneInString(body[:s])
		if r == '$' || r == '€' || r == '£' || r == '.' || r == ',' || r == '+' {
			return true
		}
	}
	for _, w := range strings.FieldsFunc(v, func(r rune) bool { return !unicode.IsLetter(r) }) {
		if dateAmbiguous.MatchString(w) && !dateAmbiguousOK(body, s, e, w) {
			return true
		}
	}
	return false
}

func dateMonthDayNoise(body string, s, e int) bool {
	head := dateMonthWord.FindStringSubmatch(body[s:e])
	if head != nil && !dateAmbiguousOK(body, s, e, head[1]) {
		return true
	}
	if e < len(body) {
		rest := body[e:]
		if dateUnit.MatchString(rest) || dateClockTail.MatchString(rest) {
			return true
		}
	}
	if s > 0 {
		r, _ := utf8.DecodeLastRuneInString(body[:s])
		if r == '$' || r == '€' || r == '£' {
			return true
		}
	}
	return false
}

func dateTemporal(fc refineInput) []slot {
	doc := fc.parsed()
	out := append([]slot(nil), fc.Slots...)
	byLine := map[int][]slot{}
	for _, s := range fc.Slots {
		byLine[s.Line] = append(byLine[s.Line], s)
	}
	for i, l := range doc.Lines {
		body := l.Body
		taken := byLine[i]
		spans := dateDetect(body)
		sort.SliceStable(spans, func(a, b int) bool {
			if spans[a].start != spans[b].start {
				return spans[a].start < spans[b].start
			}
			return spans[a].end-spans[a].start > spans[b].end-spans[b].start
		})
		var accepted []dateSpan
		for _, sp := range spans {
			clash := false
			for _, t := range taken {
				if sp.start < t.End && t.Start < sp.end {
					clash = true
					break
				}
			}
			for _, a := range accepted {
				if sp.start < a.end && a.start < sp.end {
					clash = true
					break
				}
			}
			if clash || !dictionary.valid(sp.tag) {
				continue
			}
			accepted = append(accepted, sp)
		}
		for _, sp := range accepted {
			cat := "DATE"
			if strings.Contains(sp.tag, "AGE") {
				cat = "PERSON"
			}
			out = append(out, slot{Line: i, Start: sp.start, End: sp.end, Tag: sp.tag, Value: body[sp.start:sp.end], Category: cat, Source: "s04", Confidence: 0.6})
		}
	}
	return out
}
