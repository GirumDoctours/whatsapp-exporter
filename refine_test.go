package main

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

const refineStamp = "[02/01/2026, 15:04:05] "

func refineChat(lines ...string) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(refineStamp + l + "\n")
	}
	return b.String()
}

func tagOf(slots []slot, value string) string {
	for _, s := range slots {
		if s.Value == value {
			return s.Tag
		}
	}
	return ""
}

func TestRefineFindsNamesDatesAndOrganisations(t *testing.T) {
	in := refineInput{
		Title:      "Nadia Brennan",
		SenderTags: map[string]string{"Nadia Brennan": "{{CONTACT_FULL_NAME}}"},
		Text: refineChat(
			"Nadia Brennan: Dr. Oswaldt will see you at Verimont Clinic",
			"Nadia Brennan: your surgery is on November 25",
			"Nadia Brennan: Thanks Torvald, see you soon",
		),
	}
	slots := refineSlots(in)
	for _, value := range []string{"Oswaldt", "Torvald", "November 25"} {
		if tagOf(slots, value) == "" {
			t.Errorf("%q was not tagged: %+v", value, slots)
		}
	}
	for _, s := range slots {
		if !dictionary.valid(s.Tag) {
			t.Errorf("invalid tag %s for %q", s.Tag, s.Value)
		}
	}
}

func TestRefineFilterLeavesOrdinaryWordsAndPlaces(t *testing.T) {
	in := refineInput{
		Title:      "Mark Ferro",
		SenderTags: map[string]string{"Mark Ferro": "{{CONTACT_FULL_NAME}}"},
		Text: refineChat(
			"Mark Ferro: the airport is far, airport traffic is bad",
			"Mark Ferro: go to the airport early",
			"Mark Ferro: Verimont was good, Verimont again, at Verimont",
		),
	}
	f := newRefineFilter(in)
	for _, word := range []string{"Airport", "airport", "Indonesia", "African"} {
		if !f.ordinary(word) {
			t.Errorf("%q should count as ordinary", word)
		}
	}
	for _, word := range []string{"Verimont", "Mark", "Ferro"} {
		if f.ordinary(word) {
			t.Errorf("%q should not count as ordinary", word)
		}
	}
	if f.ordinary("mark") != isCommonWord("mark") {
		t.Error("a lowercase common word should not be trusted as a name")
	}
}

func TestRefineNeverLosesAnEarlierReplacement(t *testing.T) {
	before := []slot{{Line: 0, Start: 0, End: 10, Value: "Nadia Bren"}, {Line: 2, Start: 4, End: 8, Value: "Verm"}}
	if _, missing := uncovered(before, []slot{{Line: 0, Start: 0, End: 5}, {Line: 0, Start: 5, End: 12}, {Line: 2, Start: 4, End: 8}}); missing {
		t.Error("adjacent slots cover the earlier one")
	}
	if gap, missing := uncovered(before, []slot{{Line: 0, Start: 0, End: 5}, {Line: 0, Start: 6, End: 12}}); !missing || gap.Line != 0 {
		t.Error("a one-character gap must be reported")
	}
	restored := restoreUncovered(slotsByLine(before), []slot{{Line: 0, Start: 0, End: 10, Value: "kept"}, {Line: 2, Start: 5, End: 8, Value: "cut"}})
	if tagged := slotsByLine(restored); tagged[0][0].Value != "kept" || len(tagged[2]) != 1 || tagged[2][0].Value != "Verm" {
		t.Errorf("only the broken line should go back: %+v", restored)
	}
}

func TestRefinedChatKeepsTemplateAndIsRepeatable(t *testing.T) {
	in := chatInput{Folder: "Nadia Brennan", Title: "Nadia Brennan", Text: refineChat(
		"Nadia Brennan: Dr. Oswaldt sends his regards, call +1 415 555 2671",
		"Nadia Brennan: ‎Transcript: Hello Torvald, your surgery is on November 25",
		"Nadia Brennan: Dr. Oswaldt will be there",
	)}
	run := func() chatOutput {
		return newRedactor(nil, newAICache(t.TempDir()), dictionary, newPeopleBook()).redactChat(context.Background(), in)
	}
	first := run()
	doc := parseChat(in.Text, nil)
	for i, l := range first.Lines {
		if !checkTemplate(doc.Lines[i].text(), l.text(), dictionary) {
			t.Errorf("line %d changed outside its tags: %s", i, l.text())
		}
	}
	for _, leak := range []string{"Oswaldt", "Torvald", "November 25", "555"} {
		if strings.Contains(first.Text, leak) {
			t.Errorf("%q is still visible:\n%s", leak, first.Text)
		}
	}
	for range 5 {
		if again := run(); !reflect.DeepEqual(first.Text, again.Text) {
			t.Fatalf("two runs differ:\n%s\n%s", first.Text, again.Text)
		}
	}
}
