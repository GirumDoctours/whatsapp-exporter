package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDictionaryGrammar(t *testing.T) {
	valid := []string{
		"{{PATIENT_FIRST_NAME}}", "{{PATIENT_SISTER_PHONE}}", "{{PATIENT_FRIEND_2_FIRST_NAME}}",
		"{{HOTEL_2_NAME}}", "{{CLINIC_ADDRESS}}", "{{BOOKING_REF_2}}", "{{CLINIC_OWNER_LAST_NAME}}",
		"{{PERSON_FIRST_NAME}}", "{{ORGANIZATION_NAME}}", "{{OTHER_PII}}", "{{OWNER_MOTHER_IN_LAW_PHONE}}",
		"{{DOCTOR_2_LAST_NAME}}", "{{PATIENT_CLINIC_PATIENT_ID}}", "{{HOTEL_STAFF_PHONE}}", "{{GROUP_NAME}}",
	}
	for _, tag := range valid {
		if !dictionary.valid(tag) {
			t.Errorf("%s should be valid", tag)
		}
	}
	invalid := []string{"{{PATIENT_SARAH}}", "{{PHONE}}", "{{PATIENT_1_FIRST_NAME}}", "{{HOTEL_FIRST_NAME}}", "{{}}", "{{SISTER_PHONE}}", "{{PATIENT_SISTER_BROTHER_PHONE}}"}
	for _, tag := range invalid {
		if dictionary.valid(tag) {
			t.Errorf("%s should be invalid", tag)
		}
	}
	p, _ := dictionary.parse("{{PATIENT_FRIEND_FIRST_NAME}}")
	if got := p.withNumber(2).String(); got != "{{PATIENT_FRIEND_2_FIRST_NAME}}" {
		t.Errorf("renumbered to %s", got)
	}
	p, _ = dictionary.parse("{{HOTEL_NAME}}")
	if got := p.withNumber(3).String(); got != "{{HOTEL_3_NAME}}" {
		t.Errorf("renumbered to %s", got)
	}
	if fb := dictionary.fallbackFor("{{PATIENT_SARAH_PHONE}}"); fb != "{{PERSON_PHONE}}" {
		t.Errorf("fallback %s", fb)
	}
	if !strings.Contains(dictionary.promptText(), "PATIENT") {
		t.Error("prompt text is missing roles")
	}
}

const sampleChat = "[01/02/2026, 10:00:00] Lena Hartwell: \u200eMessages and calls are end-to-end encrypted. Only people in this chat can read, listen to, or share them.\r\n" +
	"[01/02/2026, 10:01:00] Lena Hartwell: Hi, I'm Lena, 34 years old. My sister Zelda will travel with me.\r\n" +
	"Her number is +44 7911 123456 and my email is lena.h@example.org\r\n" +
	"[01/02/2026, 10:02:00] Halcyon Team: Great Lena! Please send your passport no: X1234567 and join chat.whatsapp.com/AbCdEfGh123\r\n" +
	"[01/02/2026, 10:03:00] Lena Hartwell: Done. Born on 12/03/1991. Card 4111 1111 1111 1111\r\n"

func sampleBook() *peopleBook {
	b := newPeopleBook()
	b.OwnerKey = "15550001111"
	b.OwnerCode = "44"
	b.addLabel("15550001111", "Halcyon Team")
	b.Persons["15550001111"].Own = true
	b.addLabel("447700900999", "Lena Hartwell")
	b.addPhone("447700900999", "447700900999")
	return b
}

func TestChatRoundTrip(t *testing.T) {
	doc := parseChat(sampleChat, []string{"Lena Hartwell", "Halcyon Team"})
	if got := doc.render(); got != sampleChat {
		t.Fatalf("round trip changed the text:\n%q\n%q", got, sampleChat)
	}
	if len(doc.Lines) != 4 || !strings.Contains(doc.Lines[1].Body, "\nHer number") {
		t.Fatalf("parsed %d lines, second body %q", len(doc.Lines), doc.Lines[1].Body)
	}
	tricky := "[01/02/2026, 10:00:00] A: B: c\n[01/02/2026, 10:00:01] \u200eSystem notice"
	if got := parseChat(tricky, []string{"A: B"}).render(); got != tricky {
		t.Fatalf("round trip changed %q to %q", tricky, got)
	}
}

func TestTemplateCheck(t *testing.T) {
	orig := "call me at +44 7911 123456 tomorrow"
	if !checkTemplate(orig, "call me at {{PATIENT_PHONE}} tomorrow", dictionary) {
		t.Error("a single slot should pass")
	}
	if checkTemplate(orig, "call me on {{PATIENT_PHONE}} tomorrow", dictionary) {
		t.Error("a changed word outside a slot must fail")
	}
	if checkTemplate(orig, "call me at {{PATIENT_NUMBER}} tomorrow", dictionary) {
		t.Error("a tag outside the dictionary must fail")
	}
	if !checkTemplate("Ann Lee", "{{PATIENT_FIRST_NAME}} {{PATIENT_LAST_NAME}}", dictionary) {
		t.Error("two slots should pass")
	}
	if checkTemplate(orig, "call me at  tomorrow", dictionary) {
		t.Error("deleting text must fail")
	}
	if !checkTemplate("x.y", "{{PATIENT_FIRST_NAME}}.y", dictionary) {
		t.Error("regex characters must be escaped")
	}
}

func TestDetectors(t *testing.T) {
	d := newDetector(sampleBook())
	cases := []struct {
		text, cat, value string
	}{
		{"mail me at sam@example.com please", catEmail, "sam@example.com"},
		{"ring +44 7700 900456 later", catPhone, "+44 7700 900456"},
		{"iban GB82 WEST 1234 5698 7654 32 thanks", catIBAN, "GB82 WEST 1234 5698 7654 32"},
		{"card 4111 1111 1111 1111 exp", catCard, "4111 1111 1111 1111"},
		{"I am 34 years old", catAge, "34"},
		{"date of birth: 12/03/1990", catBirthDate, "12/03/1990"},
		{"join chat.whatsapp.com/AbCd123", catInvite, "chat.whatsapp.com/AbCd123"},
		{"https://maps.app.goo.gl/xyz", catMaps, "https://maps.app.goo.gl/xyz"},
		{"see https://instagram.com/some.person", catProfile, "https://instagram.com/some.person"},
		{"passport no: X1234567 ok", catID, "X1234567"},
		{"post to SW1A 1AA", catPostcode, "SW1A 1AA"},
		{"\u200eLocation: https://maps.google.com/?q=41.008200,28.978400", catLocation, "https://maps.google.com/?q=41.008200,28.978400"},
		{"Lena said hi", catName, "Lena"},
	}
	for _, c := range cases {
		found := d.detect(c.text, map[string]bool{"447700900999": true})
		ok := false
		for _, f := range found {
			if f.Category == c.cat && f.Value == c.value {
				ok = true
			}
		}
		if !ok {
			t.Errorf("%q: wanted %s %q, got %+v", c.text, c.cat, c.value, found)
		}
	}
	for _, text := range []string{"see you 12.05.2024", "it costs €15000000", "at 10:30 tomorrow", "may I come in", "PHOTO-2026-11-20-03-39-54.jpg", "id 1763612345678 ok", "ref 4111111111111111"} {
		for _, f := range d.detect(text, nil) {
			if f.Confidence >= maskThreshold {
				t.Errorf("%q: unexpected %+v", text, f)
			}
		}
	}
}

func TestEnforceConsistency(t *testing.T) {
	slots := []slot{
		{Tag: "{{PATIENT_FRIEND_FIRST_NAME}}", Value: "Ann", Confidence: 0.9},
		{Tag: "{{PATIENT_FRIEND_FIRST_NAME}}", Value: "Bea", Confidence: 0.9},
		{Tag: "{{PATIENT_FRIEND_FIRST_NAME}}", Value: "ann", Confidence: 0.9},
		{Tag: "{{PATIENT_SISTER_FIRST_NAME}}", Value: "Ann", Confidence: 0.4},
	}
	enforceConsistency(slots, dictionary)
	if slots[3].Tag != "{{PATIENT_FRIEND_FIRST_NAME}}" {
		t.Errorf("same value should keep one tag, got %s", slots[3].Tag)
	}
	if slots[1].Tag != "{{PATIENT_FRIEND_2_FIRST_NAME}}" {
		t.Errorf("second person should be numbered, got %s", slots[1].Tag)
	}
}

func assertRedacted(t *testing.T, out chatOutput, original string, secrets []string) {
	t.Helper()
	for _, secret := range secrets {
		if strings.Contains(out.Text, secret) {
			t.Errorf("redacted text still contains %q:\n%s", secret, out.Text)
		}
	}
	origDoc := parseChat(original, []string{"Lena Hartwell", "Halcyon Team"})
	if len(origDoc.Lines) != len(out.Lines) {
		t.Fatalf("line count changed: %d vs %d", len(origDoc.Lines), len(out.Lines))
	}
	for i := range origDoc.Lines {
		if !checkTemplate(origDoc.Lines[i].text(), out.Lines[i].text(), dictionary) {
			t.Errorf("line %d failed the template check:\n%s\n%s", i, origDoc.Lines[i].text(), out.Lines[i].text())
		}
	}
}

func TestRedactChatLocalOnly(t *testing.T) {
	r := newRedactor(nil, nil, dictionary, sampleBook())
	out := r.redactChat(context.Background(), chatInput{Folder: "Lena Hartwell", Text: sampleChat})
	assertRedacted(t, out, sampleChat, []string{"Lena", "Hartwell", "+44 7911 123456", "lena.h@example.org", "X1234567", "AbCdEfGh123", "12/03/1991", "4111 1111 1111 1111", "Halcyon Team"})
	if !strings.Contains(out.Text, "{{CONTACT_FULL_NAME}}: Hi") || !strings.Contains(out.Text, "{{OWNER_FULL_NAME}}: Great") {
		t.Errorf("sender tags missing:\n%s", out.Text)
	}
	if out.Stats.Flagged == 0 {
		t.Error("local-only slots should be flagged")
	}
}

type fakeAI struct {
	calls atomic.Int64
}

var fakeMarker = regexp.MustCompile(`⟦(m\d+):([A-Z_]+)(?::(P\d+|S\d+))?(?::([A-Z_]+))?⟧`)

func (f *fakeAI) generate(ctx context.Context, req aiRequest, out any) error {
	f.calls.Add(1)
	var reply any
	switch {
	case strings.HasPrefix(req.System, castSystemPrompt[:40]):
		roles := []castRole{}
		for _, ref := range regexp.MustCompile(`(?m)^(S\d+|P\d+): (.*)$`).FindAllStringSubmatch(req.Prompt, -1) {
			role := "PATIENT"
			if strings.Contains(ref[2], "owner") {
				role = "COORDINATOR"
			}
			roles = append(roles, castRole{Ref: ref[1], Role: role, Confidence: 0.9})
		}
		reply = castReply{Roles: roles}
	case strings.HasPrefix(req.System, pass1SystemPrompt[:40]):
		r := pass1Reply{Masked: []maskedTag{}, Candidates: []candidateTag{}, Found: []foundTag{}}
		chunk := req.Prompt[strings.Index(req.Prompt, "CHUNK"):]
		for _, m := range fakeMarker.FindAllStringSubmatch(chunk, -1) {
			tag := "{{PATIENT_" + m[2] + "}}"
			switch m[2] {
			case "PHONE":
				tag = "{{PATIENT_SISTER_PHONE}}"
			case "EMAIL":
				tag = "{{PATIENT_EMAIL}}"
			case "ID_NUMBER":
				tag = "{{PATIENT_PASSPORT_NUMBER}}"
			case "INVITE_LINK":
				tag = "{{GROUP_INVITE_LINK}}"
			}
			r.Masked = append(r.Masked, maskedTag{ID: m[1], Tag: tag, Confidence: 0.9})
		}
		for _, line := range strings.Split(chunk, "\n") {
			if strings.Contains(line, "Zelda") {
				var n int
				fmt.Sscanf(line, "#%d", &n)
				r.Found = append(r.Found, foundTag{Line: n, Text: "Zelda", Tag: "PATIENT_SISTER_FIRST_NAME", Confidence: 0.95})
			}
		}
		reply = r
	case strings.HasPrefix(req.System, pass2SystemPrompt[:40]):
		r := pass2Reply{Verdicts: []slotVerdict{}, Leaks: []foundTag{}}
		for _, m := range regexp.MustCompile(`(?m)^(s\d+) line`).FindAllStringSubmatch(req.Prompt, -1) {
			r.Verdicts = append(r.Verdicts, slotVerdict{Slot: m[1], Correct: true, IsPII: true, Confidence: 0.95})
		}
		reply = r
	default:
		return fmt.Errorf("unexpected prompt")
	}
	data, _ := json.Marshal(reply)
	return json.Unmarshal(data, out)
}

func TestRedactChatWithAI(t *testing.T) {
	ai := &fakeAI{}
	r := newRedactor(ai, nil, dictionary, sampleBook())
	out := r.redactChat(context.Background(), chatInput{Folder: "Lena Hartwell", Text: sampleChat})
	assertRedacted(t, out, sampleChat, []string{"Lena", "Hartwell", "Zelda", "+44 7911 123456", "lena.h@example.org", "X1234567", "AbCdEfGh123", "12/03/1991", "4111 1111 1111 1111"})
	for _, want := range []string{"{{PATIENT_SISTER_FIRST_NAME}}", "{{PATIENT_SISTER_PHONE}}", "{{PATIENT_FULL_NAME}}: Hi", "{{COORDINATOR_FULL_NAME}}: Great", "{{PATIENT_PASSPORT_NUMBER}}"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("missing %s in:\n%s", want, out.Text)
		}
	}
	if !out.AIUsed || ai.calls.Load() < 3 {
		t.Errorf("AI used %v with %d calls", out.AIUsed, ai.calls.Load())
	}
	if out.Stats.Rebuilt != 0 || out.Stats.WholeBody != 0 {
		t.Errorf("unexpected rebuilds: %+v", out.Stats)
	}
}
