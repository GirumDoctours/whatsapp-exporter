package main

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

func TestIDSignals(t *testing.T) {
	m := &mediaChecker{r: newRedactor(nil, nil, dictionary, sampleBook())}
	if m.idSignal(ocrResult{Barcodes: []ocrBarcode{{Kind: "VNBarcodeSymbologyPDF417"}}}) == "" {
		t.Error("a PDF417 barcode should count as an ID")
	}
	card := ocrResult{
		Faces: []ocrLine{{X: 0.05, Y: 0.2, W: 0.25, H: 0.4}},
		Lines: []ocrLine{{Text: "SMITH"}, {Text: "JOHN A"}, {Text: "123 MAIN ST"}, {Text: "01/02/1990"}, {Text: "D1234567"}, {Text: "RESTR NONE"}},
	}
	if m.idSignal(card) == "" {
		t.Error("a face next to card-shaped text should count as an ID")
	}
	selfie := ocrResult{Faces: []ocrLine{{W: 0.6, H: 0.7}}, Lines: card.Lines}
	if cardLayout(selfie) {
		t.Error("a big face filling the photo isn't an ID card")
	}
	if m.idSignal(ocrResult{Faces: []ocrLine{{W: 0.1, H: 0.1}}, Lines: []ocrLine{{Text: "Happy birthday!"}}}) != "" {
		t.Error("a photo of a person with a caption isn't an ID")
	}
}

func TestUnreadableAndRetryHelpers(t *testing.T) {
	low := ocrResult{Lines: []ocrLine{{Conf: 0.2}, {Conf: 0.3}, {Conf: 0.4}}}
	if !unreadable(low) || unreadable(ocrResult{Lines: []ocrLine{{Conf: 0.9}, {Conf: 0.8}, {Conf: 0.95}}}) || unreadable(ocrResult{Lines: []ocrLine{{Conf: 0.1}}}) {
		t.Error("unreadable() should flag only several low-confidence lines")
	}
	src := image.NewRGBA(image.Rect(0, 0, 4, 2))
	src.Set(0, 0, color.RGBA{100, 100, 100, 255})
	src.Set(3, 1, color.RGBA{120, 120, 120, 255})
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			if (x == 0 && y == 0) || (x == 3 && y == 1) {
				continue
			}
			src.Set(x, y, color.RGBA{110, 110, 110, 255})
		}
	}
	g := contrastBoost(src)
	if g.GrayAt(0, 0).Y != 0 || g.GrayAt(3, 1).Y != 255 {
		t.Errorf("contrast not stretched: %d %d", g.GrayAt(0, 0).Y, g.GrayAt(3, 1).Y)
	}
	r := rotate90(g, 1)
	if r.Bounds().Dx() != 2 || r.Bounds().Dy() != 4 {
		t.Errorf("rotated size %v", r.Bounds())
	}
	if rotate90(g, 2).Bounds().Dx() != 4 {
		t.Error("half turn keeps the size")
	}
}

func TestVoiceNotesNeverInRedactedCopy(t *testing.T) {
	dir := t.TempDir()
	r := newRedactor(nil, nil, dictionary, sampleBook())
	m := newMediaChecker(r, filepath.Join(dir, "out"))
	m.replaceAudio = true
	audio := filepath.Join(dir, "note.opus")
	must(t, os.WriteFile(audio, []byte("OggS"), 0o600))
	m.submit(audio)
	m.wait()
	rp, ok := m.replacement(audio)
	if !ok {
		t.Fatal("voice notes should be replaced in the redacted copy")
	}
	data, _ := os.ReadFile(rp)
	if string(data) != placeholderVoice+"\n" {
		t.Errorf("placeholder %q", data)
	}
}
