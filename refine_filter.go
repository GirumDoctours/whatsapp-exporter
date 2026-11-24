package main

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

var refinePlaces = setOf(strings.Fields(`
africa african africans asia asian asians europe european europeans america american americans antarctica oceania
middle east west north south central eastern western northern southern
afghanistan albania algeria andorra angola argentina armenia australia austria azerbaijan bahamas bahrain bangladesh
barbados belarus belgium belize benin bhutan bolivia bosnia botswana brazil brunei bulgaria burundi cambodia cameroon
canada chad chile china colombia congo croatia cuba cyprus czechia denmark djibouti dominica ecuador egypt eritrea
estonia eswatini ethiopia fiji finland france gabon gambia georgia germany ghana greece grenada guatemala guinea
guyana haiti honduras hungary iceland india indonesia iran iraq ireland israel italy jamaica japan jordan kazakhstan
kenya kosovo kuwait kyrgyzstan laos latvia lebanon lesotho liberia libya lithuania luxembourg madagascar malawi
malaysia maldives mali malta mauritania mauritius mexico moldova monaco mongolia montenegro morocco mozambique
myanmar namibia nepal netherlands nicaragua niger nigeria norway oman pakistan palestine panama paraguay peru
philippines poland portugal qatar romania russia rwanda senegal serbia singapore slovakia slovenia somalia spain
sudan suriname sweden switzerland syria taiwan tajikistan tanzania thailand togo tunisia turkey türkiye turkiye
turkmenistan uganda ukraine emirates kingdom states uruguay uzbekistan venezuela vietnam yemen zambia zimbabwe
endonezya endonesia almanya fransa ingiltere amerika avrupa afrika rusya ispanya italya hindistan istanbul
`))

const (
	ordinaryCapitalWeight = 2
	ordinaryTaggedWeight  = 4
	properNameWords       = 3
)

type refineFilter struct {
	lower   map[string]int
	capital map[string]int
	tagged  map[string]int
	person  map[string]int
	trusted map[string]bool
}

func newRefineFilter(in refineInput) *refineFilter {
	f := &refineFilter{lower: map[string]int{}, capital: map[string]int{}, tagged: map[string]int{}, person: map[string]int{}, trusted: map[string]bool{}}
	for _, s := range in.Slots {
		words := nameWords(s.Value)
		tag, ok := dictionary.parse(s.Tag)
		isPerson := ok && tag.Kind == tagPerson
		for _, w := range words {
			f.tagged[strings.ToLower(w)]++
			if isPerson {
				f.person[strings.ToLower(w)]++
			}
		}
	}
	for _, l := range in.parsed().Lines {
		f.count(l.Body)
	}
	trust := func(label string) {
		for _, w := range nameWords(label) {
			f.trusted[strings.ToLower(w)] = true
		}
	}
	for label := range in.SenderTags {
		if !in.IsGroup || label != in.Title {
			trust(label)
		}
	}
	if !in.IsGroup {
		trust(in.Title)
	}
	return f
}

func nameWords(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsMark(r)
	})
}

func (f *refineFilter) count(body string) {
	sentenceStart := true
	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		word := body[start:end]
		key := strings.ToLower(word)
		first, _ := utf8.DecodeRuneInString(word)
		switch {
		case !unicode.IsUpper(first):
			f.lower[key]++
		case !sentenceStart:
			f.capital[key]++
		}
		sentenceStart = false
		start = -1
	}
	for i, r := range body {
		if unicode.IsLetter(r) || unicode.IsMark(r) {
			if start < 0 {
				start = i
			}
			continue
		}
		flush(i)
		if strings.ContainsRune("\n.!?:•*_", r) {
			sentenceStart = true
		}
	}
	flush(len(body))
}

func (f *refineFilter) ordinary(word string) bool {
	key := strings.ToLower(word)
	if f.trusted[key] && (hasUpper(word) || !isCommonWord(word)) {
		return false
	}
	if f.person[key] > f.lower[key] {
		return false
	}
	if isCommonWord(key) || cueNotNames[key] || refinePlaces[key] {
		return true
	}
	return f.lower[key] >= 2 && f.lower[key] >= ordinaryCapitalWeight*f.capital[key]+ordinaryTaggedWeight*f.tagged[key]
}

func (f *refineFilter) keep(s slot) bool {
	if s.Line < 0 || !strings.Contains(s.Tag, "_NAME") && !strings.HasPrefix(s.Tag, "{{ORGANIZATION") {
		return true
	}
	if strings.HasSuffix(s.Tag, "_FILENAME}}") || strings.ContainsAny(s.Value, "0123456789@") {
		return true
	}
	words := nameWords(s.Value)
	capitalised := 0
	for _, w := range words {
		if !f.ordinary(w) {
			return true
		}
		if first, _ := utf8.DecodeRuneInString(w); unicode.IsUpper(first) && hasLower(w) {
			capitalised++
		}
	}
	return capitalised >= properNameWords
}

func hasLower(s string) bool {
	return strings.IndexFunc(s, unicode.IsLower) >= 0
}
