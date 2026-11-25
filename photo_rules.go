package main

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	documentMinLines   = 12
	documentMinSignals = 2
	birthYearGap       = 15
)

var (
	documentLabels = compileKeywords(
		"name and surname", "surname and name", "first and last name", "given name", "given names", "family name", "maiden name",
		"mother's name", "father's name", "place of birth", "date of birth", "marital status", "nationality", "citizenship",
		"passport no", "passport number", "identity no", "identity number", "id number", "tax number", "tax id", "tax identification number",
		"registered address", "registered office", "residential address", "home address", "permanent address", "place of residence",
		"trade registry", "registration number", "shareholder", "shareholders", "founder's name", "legal representative", "power of attorney",
		"adi soyadi", "ad soyad", "adi ve soyadi", "anne adi", "baba adi", "dogum yeri", "dogum tarihi", "medeni hali", "uyrugu",
		"t.c. kimlik no", "kimlik no", "vergi no", "vergi kimlik no", "mersis no", "ikametgah", "ikamet adresi", "ticaret sicil", "vekaletname",
	)
	residenceLabel   = regexp.MustCompile(`^(?:residences?|residing at|ikamet)(?:$|[\s:])`)
	turkishAddress   = regexp.MustCompile(`(?:^|[^\p{L}])(?:mah|mahallesi|sok|sokak|sk|cad|caddesi|cd|blv|bulvari|apt|daire)(?:$|[^\p{L}])`)
	documentNumber   = regexp.MustCompile(`\b(?:\d{10,16}|[A-Z]{1,3}\d{6,9})\b`)
	groupedIDNumber  = regexp.MustCompile(`\b\d{4}(?:[ \-]\d{4}){2,3}\b`)
	numericDate      = regexp.MustCompile(`\b\d{1,2}[./\-]\d{1,2}[./\-](\d{4})\b|\b(\d{4})[./\-]\d{1,2}[./\-]\d{1,2}\b`)
	contiguousDigits = regexp.MustCompile(`(?:^|[^\d+])([1-9]\d{8,15})(?:$|\D)`)
	letteredIDNumber = regexp.MustCompile(`\b[A-Z]{1,3}\d{6,9}\b`)
	idCardWords      = compileKeywords(
		"republic", "republique", "république", "cumhuriyeti", "federal democratic", "kingdom of", "national id", "id no", "id number", "id card",
		"citizen", "citizenship", "licence", "license", "permit", "sex", "date of expiry", "expiry", "expires", "valid until", "valid thru",
		"student id", "employee id", "member no", "card no", "blood group", "blood type",
	)
	maskedCardTail = regexp.MustCompile(`(?i)\b(?:visa|master ?card|amex|american express|maestro|troy|discover)\b[^\n\d]{0,24}?(?:[•●∙·*xX.…]{2,}[ \-]*|ending(?: in| with)? )(\d{4})\b`)
	maskedTail     = regexp.MustCompile(`(?:[•●∙*]{4}[ \-]*)+(\d{4})\b`)
)

var foldTurkish = strings.NewReplacer("ı", "i", "İ", "i", "ş", "s", "ğ", "g", "ü", "u", "ö", "o", "ç", "c", "â", "a", "î", "i", "’", "'", "‘", "'", "̇", "")

func foldLabelText(s string) string {
	return foldTurkish.Replace(strings.ToLower(s))
}

func labelHits(text string) int {
	hits := keywordHits(text, documentLabels)
	for _, line := range strings.Split(text, "\n") {
		if residenceLabel.MatchString(strings.TrimSpace(line)) {
			hits++
			break
		}
	}
	return hits
}

func hasDocumentLabel(line string) bool {
	return labelHits(foldLabelText(line)) > 0
}

func (m *mediaChecker) looksLikePersonalDocument(lines []ocrLine) bool {
	if len(lines) < documentMinLines {
		return false
	}
	var folded strings.Builder
	address, number := false, false
	det := m.r.det()
	for _, l := range lines {
		text := foldLabelText(l.Text)
		folded.WriteString(text + "\n")
		address = address || turkishAddress.MatchString(text)
		number = number || documentNumber.MatchString(l.Text)
		for _, d := range det.detect(l.Text, nil) {
			address = address || d.Category == catAddress || d.Category == catPostcode
			number = number || d.Category == catID || d.Category == catIBAN
		}
	}
	labels := labelHits(folded.String())
	if labels == 0 {
		return false
	}
	signals := labels
	if address {
		signals++
	}
	if number {
		signals++
	}
	return signals >= documentMinSignals
}

func hasIDCardSign(lines []ocrLine, now time.Time) bool {
	if idScore(lines) >= idBorderline {
		return true
	}
	var all strings.Builder
	dates := 0
	for _, l := range lines {
		all.WriteString(l.Text + "\n")
		for _, match := range numericDate.FindAllStringSubmatch(l.Text, -1) {
			dates++
			year, err := strconv.Atoi(match[1] + match[2])
			if err == nil && (year <= now.Year()-birthYearGap || year > now.Year()) {
				return true
			}
		}
		if letteredIDNumber.MatchString(l.Text) || groupedIDNumber.MatchString(l.Text) || contiguousDigits.MatchString(l.Text) {
			return true
		}
	}
	return dates >= 2 || keywordHits(all.String(), idCardWords) > 0
}

func cardTails(text string) [][]int {
	var spans [][]int
	for _, re := range []*regexp.Regexp{maskedCardTail, maskedTail} {
		for _, m := range re.FindAllStringSubmatchIndex(text, -1) {
			spans = append(spans, []int{m[2], m[3]})
		}
	}
	return spans
}

func withLabelledValues(lines []ocrLine, boxed map[int]bool) {
	for i, l := range lines {
		if !hasDocumentLabel(l.Text) {
			continue
		}
		boxed[i] = true
		if i+1 < len(lines) && strings.IndexFunc(lines[i+1].Text, unicode.IsLetter) >= 0 {
			boxed[i+1] = true
		}
	}
}
