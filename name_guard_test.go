package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type foundAI struct {
	found []foundTag
}

func (f foundAI) generate(_ context.Context, req aiRequest, out any) error {
	var reply any
	switch {
	case strings.HasPrefix(req.System, castSystemPrompt[:40]):
		reply = castReply{Roles: []castRole{}}
	case strings.HasPrefix(req.System, pass1SystemPrompt[:40]):
		reply = pass1Reply{Masked: []maskedTag{}, Candidates: []candidateTag{}, Found: f.found}
	case strings.HasPrefix(req.System, pass2SystemPrompt[:40]):
		reply = pass2Reply{Verdicts: []slotVerdict{}, Leaks: []foundTag{}}
	case strings.HasPrefix(req.System, roleSystemPrompt[:40]):
		reply = roleReply{People: []roleAssignment{}}
	default:
		reply = confirmReply{Decisions: []confirmDecision{}}
	}
	data, err := json.Marshal(reply)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func bookWithDescriptiveContacts() *peopleBook {
	b := sampleBook()
	b.addLabel("905550000001", "Zeynep Turkey")
	b.addLabel("905550000002", "Marlon NYC")
	b.addLabel("905550000003", "Tobias Logo")
	b.addLabel("905550000004", "Selin Creator")
	b.addLabel("905550000005", "Abel Ethiopian Coffee")
	return b
}

const ordinaryWordsChat = "[01/02/2026, 10:00:00] Lena Hartwell: Flights to Turkey are cheap, NYC is not\r\n" +
	"[01/02/2026, 10:01:00] Halcyon Team: The Logo is ready and the Creator economy is big in Turkey\r\n" +
	"[01/02/2026, 10:02:00] Lena Hartwell: I love Ethiopian food. Zeynep Turkey told me about it\r\n" +
	"[01/02/2026, 10:03:00] Halcyon Team: Hi @everyone please meet Zeynep\r\n"

func requireOrdinaryWords(t *testing.T, text string) {
	t.Helper()
	for _, want := range []string{"Flights to Turkey are cheap, NYC is not", "The Logo is ready and the Creator economy is big in Turkey", "I love Ethiopian food", "@everyone"} {
		if !strings.Contains(text, want) {
			t.Errorf("should stay readable: %q in\n%s", want, text)
		}
	}
	for _, secret := range []string{"Zeynep", "Lena", "Hartwell"} {
		if strings.Contains(text, secret) {
			t.Errorf("%q leaked in\n%s", secret, text)
		}
	}
}

func TestOrdinaryWordsFromContactNames(t *testing.T) {
	r := newRedactor(nil, nil, dictionary, bookWithDescriptiveContacts())
	out := r.redactChat(context.Background(), chatInput{Folder: "Lena Hartwell", Title: "Lena Hartwell", Text: ordinaryWordsChat})
	requireOrdinaryWords(t, out.Text)
}

func TestContactNamePartsThatAreOrdinaryWordsAreNotDetected(t *testing.T) {
	det := newRedactor(nil, nil, dictionary, bookWithDescriptiveContacts()).det()
	for _, text := range []string{"Flights to Turkey", "back in NYC", "a new Logo", "Ethiopian food"} {
		if found := det.detect(text, nil); len(found) != 0 {
			t.Errorf("%q: found %+v", text, found)
		}
	}
	for _, text := range []string{"ask Zeynep Turkey", "ask Zeynep"} {
		if found := det.detect(text, nil); len(found) != 1 {
			t.Errorf("%q: found %+v", text, found)
		}
	}
}

func TestOrdinaryWordsTaggedByTheAI(t *testing.T) {
	ai := foundAI{found: []foundTag{
		{Line: 1, Text: "Turkey", Tag: "PERSON_LAST_NAME", Confidence: 0.95},
		{Line: 1, Text: "NYC", Tag: "PERSON_2_LAST_NAME", Confidence: 0.95},
		{Line: 2, Text: "Logo", Tag: "PERSON_3_LAST_NAME", Confidence: 0.95},
		{Line: 2, Text: "Creator", Tag: "GROUP_2_NAME", Confidence: 0.95},
		{Line: 3, Text: "Ethiopian", Tag: "PERSON_FIRST_NAME", Confidence: 0.95},
		{Line: 3, Text: "Zeynep Turkey", Tag: "PERSON_FULL_NAME", Confidence: 0.95},
		{Line: 4, Text: "everyone", Tag: "EVENT_HOST_PARTNER_EMPLOYER", Confidence: 0.95},
		{Line: 4, Text: "Zeynep", Tag: "PERSON_FIRST_NAME", Confidence: 0.95},
	}}
	r := newRedactor(ai, nil, dictionary, bookWithDescriptiveContacts())
	out := r.redactChat(context.Background(), chatInput{Folder: "Lena Hartwell", Title: "Lena Hartwell", Text: ordinaryWordsChat})
	if !out.AIUsed {
		t.Fatal("the AI should have been used")
	}
	requireOrdinaryWords(t, out.Text)
}

func TestNamesOfPeopleInTheChatAreStillTagged(t *testing.T) {
	ai := foundAI{found: []foundTag{{Line: 2, Text: "Turkey", Tag: "CONTACT_LAST_NAME", Confidence: 0.95}}}
	chat := "[01/02/2026, 10:00:00] Zeynep Turkey: Hello\r\n[01/02/2026, 10:01:00] Halcyon Team: Welcome Ms Turkey\r\n"
	for name, client := range map[string]aiClient{"with AI": ai, "local checks only": nil} {
		r := newRedactor(client, nil, dictionary, bookWithDescriptiveContacts())
		out := r.redactChat(context.Background(), chatInput{Folder: "Zeynep Turkey", Title: "Zeynep Turkey", Text: chat})
		if strings.Contains(out.Text, "Turkey") {
			t.Errorf("%s: the surname of someone in the chat must stay tagged:\n%s", name, out.Text)
		}
	}
}

func TestGroupMembersNamedLikeOrdinaryWords(t *testing.T) {
	b := sampleBook()
	b.addLabel("905550000006", "Ethiopian Abel")
	r := newRedactor(nil, nil, dictionary, b)
	chat := "[01/02/2026, 10:00:00] Ethiopian Abel: Hello all\r\n" +
		"[01/02/2026, 10:01:00] Halcyon Team: I love Ethiopian food\r\n" +
		"[01/02/2026, 10:02:00] Lena Hartwell: Thanks Ethiopian!\r\n"
	out := r.redactChat(context.Background(), chatInput{Folder: "GP-Food", Title: "Food", IsGroup: true, Text: chat})
	if !strings.Contains(out.Text, "I love Ethiopian food") {
		t.Errorf("an ordinary use should stay readable:\n%s", out.Text)
	}
	if strings.Contains(out.Text, "Thanks Ethiopian") || strings.Contains(out.Text, "Abel") {
		t.Errorf("addressing the member by name must stay tagged:\n%s", out.Text)
	}
}

func TestPlacesThatDoubleAsNamesAreLeftAlone(t *testing.T) {
	for _, name := range []string{"Jordan", "Georgia", "Chad", "India", "Paris", "Israel", "West", "Danish"} {
		if notANameAlone(name) {
			t.Errorf("%s can be someone's name", name)
		}
	}
	for _, word := range []string{"Turkey", "NYC", "everyone", "Logo", "Ethiopian"} {
		if !notANameAlone(word) {
			t.Errorf("%s isn't a name on its own", word)
		}
	}
	if onlyNonNames("Turkey 2") || onlyNonNames("Ali Turkey") || onlyNonNames("") {
		t.Error("only plain places and descriptive words count")
	}
}

func TestGroupMentions(t *testing.T) {
	body := "hey @all and @ali, all good"
	if !isGroupMention(body, 5, 8) || !isGroupMention(body, 4, 8) {
		t.Error("@all is a group mention")
	}
	at := strings.Index(body, "ali")
	if isGroupMention(body, at, at+3) {
		t.Error("@ali is a person")
	}
	last := strings.LastIndex(body, "all")
	if isGroupMention(body, last, last+3) {
		t.Error("a plain word isn't a mention")
	}
}

func TestSameValueKeepsOneTag(t *testing.T) {
	slots := []slot{
		{Tag: "{{PERSON_5_FIRST_NAME}}", Value: "Aster", Confidence: 0.9},
		{Tag: "{{PERSON_2_FIRST_NAME}}", Value: "Actor", Confidence: 0.9},
		{Tag: "{{PERSON_2_FIRST_NAME}}", Value: "Astor", Confidence: 0.9},
		{Tag: "{{PERSON_3_FIRST_NAME}}", Value: "Actor", Confidence: 0.5},
		{Tag: "{{PERSON_5_FIRST_NAME}}", Value: "Aster", Confidence: 0.9},
	}
	enforceConsistency(slots, dictionary)
	if slots[0].Tag != slots[4].Tag {
		t.Errorf("Aster got %s and %s", slots[0].Tag, slots[4].Tag)
	}
	if slots[1].Tag != slots[3].Tag {
		t.Errorf("Actor got %s and %s", slots[1].Tag, slots[3].Tag)
	}
}
