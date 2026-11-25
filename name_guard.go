package main

import "strings"

var nameLikePlaces = setOf(strings.Fields(`
north south east west central middle china chad cuba cyprus georgia india ireland israel jordan kenya malta
america asia africa england francia lebanon mali ocean palestine paris peru samoa senegal singapore spain
syria troy kingdom states niger gambia grenada haiti jamaica dominica germany italia holland brazil egypt poland
african asian european danish irish french german
`))

var extraPlaces = setOf(strings.Fields(`
usa uk uae nyc sf ankara antalya izmir bursa dubai ababa
turkish ethiopian kenyan nigerian egyptian moroccan tunisian saudi emirati qatari kuwaiti iraqi iranian pakistani indian bangladeshi
chinese japanese korean vietnamese indonesian malaysian filipino
british scottish spanish portuguese italian greek polish romanian bulgarian ukrainian russian swedish norwegian finnish dutch belgian swiss austrian
american canadian mexican brazilian argentinian colombian australian
`))

var genericNameWords = setOf(strings.Fields(`
everyone everybody logo creator creators content tech digital concierge internship estate upcoming original primary average exclusive
happening showing founder founders community marketing agency design designer developer video videographer photographer editor
events restaurant cafe apartment landlord realtor plumber electrician cleaner delivery courier
support services service solutions studio media group chat official transplant dental dentist welfare healthcare
`))

var groupMentions = setOf(strings.Fields("everyone everybody all here channel"))

func placeWord(word string) bool {
	key := strings.ToLower(word)
	return extraPlaces[key] || refinePlaces[key] && !nameLikePlaces[key]
}

func notANameAlone(word string) bool {
	return placeWord(word) || genericNameWords[strings.ToLower(word)]
}

func onlyNonNames(value string) bool {
	words := nameWords(value)
	if len(words) == 0 || strings.ContainsAny(value, "0123456789") {
		return false
	}
	for _, w := range words {
		if !notANameAlone(w) {
			return false
		}
	}
	return true
}

func isGroupMention(body string, start, end int) bool {
	word := strings.ToLower(strings.TrimPrefix(body[start:end], "@"))
	if !groupMentions[word] {
		return false
	}
	return body[start] == '@' || start > 0 && body[start-1] == '@'
}

func (j *chatJob) chatNameWords() map[string]bool {
	if j.nameWords != nil {
		return j.nameWords
	}
	words := map[string]bool{}
	trust := func(label string) {
		for _, w := range nameWords(label) {
			words[strings.ToLower(w)] = true
		}
	}
	for label := range j.senderRefs {
		trust(label)
	}
	if !j.in.IsGroup {
		trust(j.titleLabel)
	}
	j.nameWords = words
	return words
}

func (j *chatJob) namesSomeoneHere(s slot, body string) bool {
	known := j.chatNameWords()
	for _, w := range nameWords(s.Value) {
		if !known[strings.ToLower(w)] || !hasUpper(w) {
			return false
		}
	}
	return !j.in.IsGroup || vocativeUse(body, s.Start, s.End)
}

func (j *chatJob) wrongNameTag(s slot) bool {
	if s.Line < 0 || s.Line >= len(j.doc.Lines) {
		return false
	}
	body := j.doc.Lines[s.Line].Body
	if s.Start < 0 || s.Start >= s.End || s.End > len(body) {
		return false
	}
	if isGroupMention(body, s.Start, s.End) {
		return true
	}
	if !onlyNonNames(s.Value) || j.namesSomeoneHere(s, body) {
		return false
	}
	tag, ok := j.r.dict.parse(s.Tag)
	if !ok {
		return false
	}
	switch tag.Kind {
	case tagPerson:
		return identityAttributes[tag.Attribute] || tag.Attribute == "EMPLOYER"
	case tagEntity:
		return identityAttributes[tag.Attribute] && len(nameWords(s.Value)) == 1
	}
	return false
}

func (j *chatJob) dropWrongNames() {
	var kept []slot
	for _, s := range j.slots {
		if !j.wrongNameTag(s) {
			kept = append(kept, s)
		}
	}
	j.slots = kept
}
