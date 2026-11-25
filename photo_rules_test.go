package main

import (
	"context"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type scriptedOCR struct {
	byName map[string]ocrResult
}

func (s scriptedOCR) recognize(path string) (ocrResult, error) {
	return s.byName[filepath.Base(path)], nil
}

func (s scriptedOCR) close() {}

func textLines(text string) []ocrLine {
	var out []ocrLine
	for i, l := range strings.Split(strings.TrimSpace(text), "\n") {
		out = append(out, ocrLine{Text: l, Conf: 1, X: 0.1, Y: 0.05 * float64(i%18), W: 0.6, H: 0.03})
	}
	return out
}

const formationDocument = `
ARTICLES OF ASSOCIATION
Article 1
Establishment
A joint stock company has been established
by the founders whose names
surnames residences and
nationalities are written below
Founder's name and surname
Deniz Yalvac
Residence Papatya Mah
Lale Sokak 1/2
Ornekkoy Ankara Nationality TR
Article 2
Title of the company`

const turkishForm = `
BASVURU FORMU
Lutfen tum alanlari doldurun
Adı Soyadı
Deniz Yalvac
Baba Adı
Kemal
Dogum Yeri
Ankara
Adres
Lale Mah Gul Sok
Tarih
Imza`

const eventFlyer = `
FOUNDERS SUMMIT
Join us for an evening of talks
Speakers
Ada Example
Kofi Sample
Doors open 18:00
Friday 27 November
Tickets 25 USD
Networking and drinks
Limited seats
Register now
Follow us for updates
See you there`

func newTestMediaChecker() *mediaChecker {
	return &mediaChecker{r: newRedactor(nil, nil, dictionary, sampleBook())}
}

func TestPersonalDocumentPhotos(t *testing.T) {
	m := newTestMediaChecker()
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"company formation document", formationDocument, true},
		{"Turkish application form", turkishForm, true},
		{"event flyer", eventFlyer, false},
		{"short caption with one label", "Nationality\nTR", false},
		{"flyer with a street address", eventFlyer + "\nVenue 12 Rosebank Road", false},
		{"flyer with a phone number", eventFlyer + "\nCall 05321234567", false},
	}
	for _, c := range cases {
		if got := m.looksLikePersonalDocument(textLines(c.text)); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestLabelledValuesAreBlackedOutInSmallPhotos(t *testing.T) {
	m := newTestMediaChecker()
	boxes := m.piiBoxes(textLines("Booking form\nName and surname\nDeniz Yalvac\nThank you"))
	if len(boxes) != 2 || boxes[0].Text != "Name and surname" || boxes[1].Text != "Deniz Yalvac" {
		t.Errorf("boxes %+v", boxes)
	}
	if boxes := m.piiBoxes(textLines(eventFlyer)); len(boxes) != 0 {
		t.Errorf("a flyer with no details shouldn't be blacked out: %+v", boxes)
	}
}

func TestCardDigits(t *testing.T) {
	m := newTestMediaChecker()
	for _, text := range []string{"Visa •••• 4821", "VISA **** 4821", "Mastercard ending in 4821", "Amex xx 4821", "•••• •••• •••• 4821", "Paid with visa ****4821"} {
		if len(m.piiBoxes(textLines(text))) != 1 {
			t.Errorf("%q should be blacked out", text)
		}
	}
	for _, text := range []string{"Visa 2026 applications", "Your visa was approved in 2025", "Discover 1000 ideas", "Total 4821", "Cash • Visa • Mastercard", "Open 10:00 ... 2026"} {
		if boxes := m.piiBoxes(textLines(text)); len(boxes) != 0 {
			t.Errorf("%q shouldn't be blacked out", text)
		}
	}
	r := newRedactor(nil, nil, dictionary, sampleBook())
	out := r.redactChat(context.Background(), chatInput{Folder: "Lena Hartwell", Title: "Lena Hartwell", Text: "[01/02/2026, 10:00:00] Lena Hartwell: I paid with Visa ending in 4821\r\n"})
	if strings.Contains(out.Text, "4821") || !strings.Contains(out.Text, "CARD_NUMBER") {
		t.Errorf("card digits left in a message:\n%s", out.Text)
	}
}

func TestIDPhotosNeedMoreThanAFace(t *testing.T) {
	m := newTestMediaChecker()
	face := []ocrLine{{X: 0.05, Y: 0.2, W: 0.25, H: 0.4}}
	now := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	replaced := map[string]string{
		"licence without keywords": "SMITH\nJOHN A\n123 MAIN ST\n01/02/1990\nD1234567\nRESTR NONE",
		"card with a birth date":   "YALVAC\nDENIZ\n14.05.1991\nANKARA\nA 12",
		"card with an expiry date": "ABEBE\nKEBEDE\n01/01/2031\nADDIS\n22 B",
		"card with grouped digits": "ABEBE\nKEBEDE\n1234 5678 9012\nMALE 1\nADDIS",
		"card with a plain number": "ABEBE\nKEBEDE\n482913377\nROW 2\nADDIS",
		"badge":                    "REPUBLIC OF EXAMPLE\nABEBE\nKEBEDE\nUNIT 12\nZONE 3",
	}
	for name, text := range replaced {
		lines := textLines(text)
		if !cardLayout(ocrResult{Faces: face, Lines: lines}) || !hasIDCardSign(lines, now) {
			t.Errorf("%s should still be replaced", name)
		}
	}
	if m.idSignal(ocrResult{Faces: face, Lines: textLines(replaced["licence without keywords"])}) == "" {
		t.Error("the licence should be replaced as an ID")
	}
	kept := map[string]string{
		"event flyer":     eventFlyer,
		"dated poster":    "LAUNCH NIGHT\nAda Example\n27/11/2026\nDoors 18:00\nTickets 25 USD",
		"podcast cover":   "EPISODE 12\nAda Example\nBuilding for 10 years\nListen now\nNew every week",
		"profile screen":  "Ada Example\n120 posts\n4821 followers\n310 following\nFollow",
		"phone on poster": "HAIR STUDIO\nBook today\n0911 234 567\nOpen 9 to 18\nAddis",
	}
	for name, text := range kept {
		if hasIDCardSign(textLines(text), now) {
			t.Errorf("%s shouldn't be replaced as an ID", name)
		}
	}
}

func TestDocumentPhotoIsReplacedEndToEnd(t *testing.T) {
	dir := t.TempDir()
	write := func(name string) string {
		f, err := os.Create(filepath.Join(dir, name))
		must(t, err)
		must(t, jpeg.Encode(f, image.NewGray(image.Rect(0, 0, 400, 300)), nil))
		must(t, f.Close())
		return f.Name()
	}
	document, flyer, receipt := write("document.jpg"), write("flyer.jpg"), write("receipt.jpg")
	m := newMediaChecker(newRedactor(nil, nil, dictionary, sampleBook()), filepath.Join(dir, "out"))
	m.ocr = scriptedOCR{byName: map[string]ocrResult{
		"document.jpg": {Lines: textLines(formationDocument)},
		"flyer.jpg":    {Lines: textLines(eventFlyer), Faces: []ocrLine{{X: 0.1, Y: 0.1, W: 0.2, H: 0.2}}},
		"receipt.jpg":  {Lines: textLines("Receipt\nTotal 12.00\nVisa •••• 4821")},
	}}
	for _, p := range []string{document, flyer, receipt} {
		m.submit(p)
	}
	m.wait()
	if _, ok := m.replacement(document); !ok {
		t.Error("the document photo should be replaced")
	}
	if _, ok := m.replacement(flyer); ok {
		t.Error("the flyer should be copied as it is")
	}
	if _, ok := m.replacement(receipt); !ok {
		t.Error("the card digits should be blacked out")
	}
	if tot := m.totals(); tot.PhotoDocs != 1 || tot.IDDocuments != 0 || tot.Blacked != 1 {
		t.Errorf("totals %+v", tot)
	}
}
