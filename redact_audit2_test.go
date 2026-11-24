package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOwnerVariantsAndForeignData(t *testing.T) {
	b := sampleBook()
	b.addNames("15550001111", "Samuel Okafor")
	r := newRedactor(nil, nil, dictionary, b)
	text := "[01/02/2026, 10:00:00] Halcyon Team: hi\r\n"
	j := &chatJob{r: r, in: chatInput{Title: "x", Text: text}, doc: parseChat(text, nil), roles: map[string]string{}}
	j.indexPeople()
	if got := j.guardTag("{{OWNER_NICKNAME}}", "Sammuel", 0); got != "{{OWNER_NICKNAME}}" {
		t.Errorf("a spelling variant of the owner's name became %s", got)
	}
	if got := j.guardTag("{{OWNER_EMAIL}}", "crm@vendor.com", 0); got != "{{PERSON_EMAIL}}" {
		t.Errorf("someone else's email sent by the owner became %s", got)
	}
}

func TestVocativeSpreadOfCommonWordNames(t *testing.T) {
	r := newRedactor(nil, nil, dictionary, sampleBook())
	text := "[01/02/2026, 10:00:00] Lena Hartwell: Hi Can, how are you?\r\n" +
		"[01/02/2026, 10:01:00] Lena Hartwell: Can you send it?\r\n" +
		"[01/02/2026, 10:02:00] Lena Hartwell: Thanks Can\r\n" +
		"[01/02/2026, 10:03:00] Lena Hartwell: We can meet at a clinic\r\n"
	j := &chatJob{r: r, in: chatInput{Title: "Lena Hartwell", Text: text}, doc: parseChat(text, nil), roles: map[string]string{}}
	j.indexPeople()
	j.slots = []slot{
		{Line: 0, Start: 3, End: 6, Tag: "{{CONTACT_FIRST_NAME}}", Value: "Can", Confidence: 0.95},
		{Line: 3, Start: 16, End: 24, Tag: "{{CLINIC_NAME}}", Value: "a clinic", Confidence: 0.9},
	}
	j.propagate(nil)
	lines := map[int]bool{}
	for _, s := range j.slots {
		if s.Source == "propagated" {
			lines[s.Line] = true
		}
	}
	if !lines[2] || lines[1] || lines[3] {
		t.Errorf("spread to lines %v; want only the greeting on line 2", lines)
	}
}

func TestSenderPhonesPinTags(t *testing.T) {
	r := newRedactor(nil, nil, dictionary, sampleBook())
	text := "[01/02/2026, 10:00:00] Club: \u200eMessages and calls are end-to-end encrypted.\r\n" +
		"[01/02/2026, 10:01:00] +44 7911 222333: hello\r\n" +
		"[01/02/2026, 10:02:00] +44 7911 999888: my old number was +44 7911 555444\r\n"
	out := r.redactChat(context.Background(), chatInput{Title: "Club", IsGroup: true, Text: text})
	if strings.Contains(out.Text, "555444") {
		t.Fatalf("number leaked:\n%s", out.Text)
	}
	tags := map[string]bool{}
	for _, tag := range out.SenderTags {
		tags[tag] = true
	}
	for _, s := range out.Slots {
		if strings.HasSuffix(s.Tag, "_PHONE}}") && tags[s.Tag] {
			t.Errorf("a different number reused a sender's tag %s:\n%s", s.Tag, out.Text)
		}
	}
	if fix := tagCollisions(out.Slots, dictionary); fix != 0 {
		t.Errorf("%d tags hold two values", fix)
	}
}

func TestNoNameMatchesInNotices(t *testing.T) {
	b := sampleBook()
	b.addLabel("447911000777", "Group Admin")
	d := newDetector(b)
	for _, f := range d.detect("\u200e+44 7911 222333 joined using this group's invite link", map[string]bool{"447911000777": true}) {
		if f.Category == catName {
			t.Errorf("name matched inside a notice: %+v", f)
		}
	}
	if !allGeneric("a clinic") || allGeneric("Asteria Clinic") {
		t.Error("generic phrase check is wrong")
	}
}

func TestDeletePreviousTests(t *testing.T) {
	dir := t.TempDir()
	names := []string{"15551234567-11-26-test.zip", "15551234567-11-26-test-messages-only-2.zip", "WhatsApp-11-26-test-redacted-messages-only.zip", "WhatsApp-11-26-test-redacted.zip", "15551234567-11-26.zip", "WhatsApp-11-26-redacted.zip", "holiday.zip"}
	for _, n := range names {
		must(t, os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600))
	}
	keep := filepath.Join(dir, "15551234567-11-26-test.zip")
	if n := deletePreviousTests(map[string]bool{keep: true}, dir); n != 3 {
		t.Errorf("deleted %d, want 3", n)
	}
	left, _ := os.ReadDir(dir)
	if len(left) != 4 {
		t.Errorf("left %d files", len(left))
	}
}
