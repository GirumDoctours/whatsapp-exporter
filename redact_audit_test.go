package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestMediaFailsClosed(t *testing.T) {
	dir := t.TempDir()
	r := newRedactor(nil, nil, dictionary, sampleBook())
	m := newMediaChecker(r, filepath.Join(dir, "out"))
	docx := filepath.Join(dir, "leads.xlsx")
	pdf := filepath.Join(dir, "scan.pdf")
	video := filepath.Join(dir, "clip.mp4")
	must(t, os.WriteFile(docx, []byte("PK fake spreadsheet"), 0o600))
	must(t, os.WriteFile(pdf, []byte("%PDF-1.4 not really a pdf"), 0o600))
	must(t, os.WriteFile(video, []byte("video"), 0o600))
	for _, p := range []string{docx, pdf, video} {
		m.submit(p)
	}
	m.wait()
	for _, p := range []string{docx, pdf} {
		if _, ok := m.replacement(p); !ok {
			t.Errorf("%s should be replaced", filepath.Base(p))
		}
	}
	if _, ok := m.replacement(video); ok {
		t.Error("videos are copied as they are")
	}
	tot := m.totals()
	if tot.Removed != 1 || tot.Unreadable != 1 || tot.Unchecked["videos"] != 1 {
		t.Errorf("totals %+v", tot)
	}
	licence := []ocrLine{{Text: "CALIFORNIA DRIVER LICENSE"}, {Text: "DL 1234567"}, {Text: "EXP 08/31/2029"}, {Text: "DOB 01/02/1990"}, {Text: "CLASS C"}, {Text: "HGT 5-10 EYES BRN"}}
	if idScore(licence) < idScoreThreshold {
		t.Errorf("a US driver's licence scored %d", idScore(licence))
	}
	if idScore([]ocrLine{{Text: "Economy nominal results"}}) != 0 {
		t.Error("keywords must match whole words only")
	}
}

func TestUniqueRolesBeyondNinetyNine(t *testing.T) {
	used := map[string]bool{}
	seen := map[string]bool{}
	for i := 0; i < 150; i++ {
		role := uniqueRole("GROUP_MEMBER", used, dictionary)
		if seen[role] {
			t.Fatalf("role %s given twice", role)
		}
		seen[role] = true
	}
	if !dictionary.valid("{{GROUP_MEMBER_150_PHONE}}") {
		t.Error("numbers above 99 should be valid")
	}
}

func TestConsistencyMergesVariantsAndNeverNumbersOwner(t *testing.T) {
	slots := []slot{
		{Tag: "{{HOTEL_NAME}}", Value: "Hotel Marisol", Confidence: 0.9},
		{Tag: "{{HOTEL_NAME}}", Value: "Hotel Marisoll", Confidence: 0.9},
		{Tag: "{{HOTEL_NAME}}", Value: "Grand Bosphorus", Confidence: 0.9},
		{Tag: "{{OWNER_FIRST_NAME}}", Value: "Sam", Confidence: 0.9},
		{Tag: "{{OWNER_2_FIRST_NAME}}", Value: "Sammy", Confidence: 0.9},
		{Tag: "{{PERSON_FIRST_NAME}}", Value: "Ann", Confidence: 0.9},
		{Tag: "{{PERSON_FIRST_NAME}}", Value: "Bea", Confidence: 0.9},
		{Tag: "{{PERSON_PHONE}}", Value: "+44 7911 000001", Confidence: 0.9},
		{Tag: "{{PERSON_PHONE}}", Value: "+44 7911 000002", Confidence: 0.9},
		{Tag: "{{PERSON_PHONE}}", Value: "07911 000001", Confidence: 0.9},
	}
	fix := enforceConsistency(slots, dictionary)
	want := []string{"{{HOTEL_NAME}}", "{{HOTEL_NAME}}", "{{HOTEL_2_NAME}}", "{{OWNER_FIRST_NAME}}", "{{OWNER_FIRST_NAME}}", "{{PERSON_FIRST_NAME}}", "{{PERSON_2_FIRST_NAME}}", "{{PERSON_PHONE}}"}
	for i, w := range want {
		if slots[i].Tag != w {
			t.Errorf("slot %d (%s) = %s, want %s", i, slots[i].Value, slots[i].Tag, w)
		}
	}
	if slots[8].Tag == "{{PERSON_2_PHONE}}" || slots[8].Tag == "{{PERSON_PHONE}}" {
		t.Errorf("an unlinked second phone must not reuse a number taken by a named person: %s", slots[8].Tag)
	}
	if slots[9].Tag != slots[7].Tag {
		t.Errorf("the same number written locally should keep its tag: %s vs %s", slots[9].Tag, slots[7].Tag)
	}
	if fix.collisions != 0 {
		t.Errorf("%d tags still hold two values", fix.collisions)
	}
}

func TestRoleGuards(t *testing.T) {
	r := newRedactor(nil, nil, dictionary, sampleBook())
	group := "[01/02/2026, 10:00:00] Founders Club: ‎Messages and calls are end-to-end encrypted. Only people in this chat can read, listen to, or share them.\r\n" +
		"[01/02/2026, 10:01:00] Halcyon Team: Welcome all\r\n" +
		"[01/02/2026, 10:02:00] +44 7911 222333: ‎+44 7911 222333 joined using this group's invite link\r\n"
	j := &chatJob{r: r, in: chatInput{Title: "Founders Club", IsGroup: true, Text: group}, doc: parseChat(group, nil), roles: map[string]string{}}
	j.indexPeople()
	if j.ownerRef == "" {
		t.Fatal("owner not found")
	}
	other := "S2"
	if j.roleAllowed(other, "OWNER") || j.roleAllowed(other, "CONTACT") || j.roleAllowed(other, "CONTACT_FRIEND") {
		t.Error("only the owner can be OWNER, and CONTACT is never used in groups")
	}
	if !j.roleAllowed(j.ownerRef, "COORDINATOR") || !j.roleAllowed(other, "OWNER_BROTHER") {
		t.Error("specific roles and owner relations should be allowed")
	}
	if got := j.guardTag("{{OWNER_PHONE}}", "+44 7911 222333", 2); got != "{{PERSON_PHONE}}" {
		t.Errorf("a stranger's phone tagged as the owner's became %s", got)
	}
	if got := j.guardTag("{{OWNER_FIRST_NAME}}", "Halcyon", 1); got != "{{OWNER_FIRST_NAME}}" {
		t.Errorf("the owner's own name should stay OWNER, got %s", got)
	}
	if !isNotice(j.doc.Lines[2].Body) || isNotice(j.doc.Lines[1].Body) {
		t.Error("WhatsApp notices should be recognised")
	}
	for _, v := range []string{"IV", "can", "x", "123"} {
		if !implausibleName(v) {
			t.Errorf("%q should not count as a name", v)
		}
	}
	for _, v := range []string{"Zelda", "Ayşe Yılmaz", "Can"} {
		if implausibleName(v) {
			t.Errorf("%q is a plausible name", v)
		}
	}
}

func TestDetectorsAfterAudit(t *testing.T) {
	b := sampleBook()
	b.addLabel("lid:123456789012345", "Jane Doe")
	b.addLabel("447911444555", "Don Draper")
	b.addPhone("447911444555", "447911444555")
	d := newDetector(b)
	chat := map[string]bool{"447911444555": true, "lid:123456789012345": true}
	cases := []struct{ text, cat, value string }{
		{"rsvp https://lu.ma/abc123 now", catEventLink, "https://lu.ma/abc123"},
		{"see https://www.figma.com/proto/x", catDocument, "https://www.figma.com/proto/x"},
		{"watch https://youtu.be/xyz", catVideoLink, "https://youtu.be/xyz"},
		{"portal https://portal.example-clinic.io/patient/77", catLink, "https://portal.example-clinic.io/patient/77"},
		{"our site is halcyon.com btw", catDomain, "halcyon.com"},
		{"write to İnfo@clinic.com.tr", catEmail, "İnfo@clinic.com.tr"},
		{"thanks @123456789012345", catWhatsAppID, "@123456789012345"},
		{"call 07911 444555 today", catPhone, "07911 444555"},
		{"Don’s car", catName, "Don"},
		{"Jane_Doe_CV.pdf ‎<attached: 00000003.pdf>", catName, "Jane"},
		{"I live at 12 Rosebank Road, Apt 4, Leeds, LS6 2AB thanks", catAddress, "12 Rosebank Road, Apt 4, Leeds, LS6 2AB"},
		{"ship to Austin, Texas 78701 please", catPostcode, "Texas 78701"},
	}
	for _, c := range cases {
		found := d.detect(c.text, chat)
		ok := false
		for _, f := range found {
			if f.Category == c.cat && f.Value == c.value && (c.cat != catLink || f.Confidence >= maskThreshold) {
				ok = true
			}
		}
		if !ok {
			t.Errorf("%q: wanted %s %q, got %+v", c.text, c.cat, c.value, found)
		}
	}
	for _, f := range d.detect("I don’t know, we’ve tried", chat) {
		if f.Category == catName {
			t.Errorf("apostrophe words matched a name: %+v", f)
		}
	}
	if key := b.keyForPhone("07911444555"); key != "447911444555" {
		t.Errorf("local number didn't match the contact: %q", key)
	}
}

func TestPropagationUsesTheBestTag(t *testing.T) {
	r := newRedactor(nil, nil, dictionary, sampleBook())
	text := "[01/02/2026, 10:00:00] Lena Hartwell: Asteria Clinic is great\r\n[01/02/2026, 10:01:00] Lena Hartwell: I booked Asteria Clinic\r\n[01/02/2026, 10:02:00] Lena Hartwell: asteria clinic again\r\n"
	j := &chatJob{r: r, in: chatInput{Title: "Lena Hartwell", Text: text}, doc: parseChat(text, nil), roles: map[string]string{}}
	j.indexPeople()
	j.slots = []slot{
		{Line: 0, Start: 0, End: 14, Tag: "{{ORGANIZATION_NAME}}", Value: "Asteria Clinic", Confidence: 0.95},
		{Line: 1, Start: 9, End: 23, Tag: "{{CLINIC_NAME}}", Value: "Asteria Clinic", Confidence: 0.9},
	}
	j.propagate(nil)
	covered := false
	for _, s := range j.slots {
		if s.Line == 2 && s.Tag == "{{CLINIC_NAME}}" {
			covered = true
		}
	}
	if !covered {
		t.Errorf("the specific tag should spread to every mention: %+v", j.slots)
	}
}

func TestMergeAddressSlots(t *testing.T) {
	r := newRedactor(nil, nil, dictionary, sampleBook())
	text := "[01/02/2026, 10:00:00] Lena Hartwell: 12 Rosebank Road, Leeds LS6 2AB\r\n"
	j := &chatJob{r: r, in: chatInput{Title: "Lena Hartwell", Text: text}, doc: parseChat(text, nil), roles: map[string]string{}}
	body := j.doc.Lines[0].Body
	j.slots = []slot{
		{Line: 0, Start: 0, End: 16, Tag: "{{PATIENT_HOME_ADDRESS}}", Value: body[0:16], Confidence: 0.9},
		{Line: 0, Start: 18, End: 23, Tag: "{{PATIENT_HOME_CITY}}", Value: body[18:23], Confidence: 0.9},
		{Line: 0, Start: 24, End: 31, Tag: "{{PATIENT_POSTCODE}}", Value: body[24:31], Confidence: 0.9},
	}
	j.mergeAddresses()
	if len(j.slots) != 1 || j.slots[0].Tag != "{{PATIENT_HOME_ADDRESS}}" || j.slots[0].Value != body {
		t.Errorf("address pieces should merge into one slot: %+v", j.slots)
	}
}

func TestLiveRedactionStillCleanAfterAudit(t *testing.T) {
	r := newRedactor(&fakeAI{}, nil, dictionary, sampleBook())
	out := r.redactChat(context.Background(), chatInput{Folder: "x", Text: sampleChat})
	if strings.Contains(out.Text, "OWNER_2") {
		t.Error("OWNER must never be numbered")
	}
	for i, l := range parseChat(sampleChat, nil).Lines {
		if !checkTemplate(l.text(), out.Lines[i].text(), dictionary) {
			t.Errorf("line %d failed the template check", i)
		}
	}
	_ = strconv.Itoa
}
