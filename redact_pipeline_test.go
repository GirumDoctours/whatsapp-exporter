package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBatchDispatcherOverlapsBatchesOfTen(t *testing.T) {
	release := make(chan struct{})
	var started atomic.Int64
	var mu sync.Mutex
	var finished []string
	d := newBatchDispatcher(context.Background(), redactBatchSize, func(ctx context.Context, in chatInput) chatOutput {
		started.Add(1)
		<-release
		return chatOutput{Folder: in.Folder}
	}, func(out chatOutput) {
		mu.Lock()
		finished = append(finished, out.Folder)
		mu.Unlock()
	})
	waitFor := func(n int64) {
		deadline := time.Now().Add(2 * time.Second)
		for started.Load() < n && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
	}
	for i := 0; i < 9; i++ {
		d.add(chatInput{Folder: string(rune('a' + i))})
	}
	time.Sleep(30 * time.Millisecond)
	if started.Load() != 0 {
		t.Fatalf("started %d before a full batch", started.Load())
	}
	d.add(chatInput{Folder: "j"})
	waitFor(10)
	for i := 0; i < 10; i++ {
		d.add(chatInput{Folder: "b2-" + string(rune('a'+i))})
	}
	d.add(chatInput{Folder: "j"})
	waitFor(20)
	if started.Load() != 20 {
		t.Fatalf("second batch should start while the first is running, started %d", started.Load())
	}
	for i := 0; i < 3; i++ {
		d.add(chatInput{Folder: "tail-" + string(rune('a'+i))})
	}
	close(release)
	d.wait()
	if started.Load() != 23 || len(finished) != 23 {
		t.Fatalf("started %d finished %d, want 23", started.Load(), len(finished))
	}
	if done, queued := d.progress(); done != 23 || queued != 23 {
		t.Fatalf("progress %d/%d", done, queued)
	}
}

func jpegWithGPS(t *testing.T) []byte {
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	tiff := []byte("II*\x00\x08\x00\x00\x00\x01\x00")
	entry := make([]byte, 12)
	binary.LittleEndian.PutUint16(entry[0:], 0x8825)
	binary.LittleEndian.PutUint16(entry[2:], 4)
	binary.LittleEndian.PutUint32(entry[4:], 1)
	tiff = append(tiff, entry...)
	tiff = append(tiff, 0, 0, 0, 0)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	segment := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(segment[2:], uint16(len(payload)+2))
	segment = append(segment, payload...)
	out := append([]byte{}, data[:2]...)
	out = append(out, segment...)
	return append(out, data[2:]...)
}

func TestStripJPEGExifRemovesGPS(t *testing.T) {
	data := jpegWithGPS(t)
	stripped, removed := stripJPEGExif(data)
	if !removed || bytes.Contains(stripped, []byte("Exif\x00\x00")) {
		t.Fatal("GPS EXIF should be removed")
	}
	if _, err := jpeg.Decode(bytes.NewReader(stripped)); err != nil {
		t.Fatalf("stripped JPEG no longer decodes: %v", err)
	}
	var plain bytes.Buffer
	_ = jpeg.Encode(&plain, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil)
	if _, removed := stripJPEGExif(plain.Bytes()); removed {
		t.Fatal("a JPEG without GPS should be left alone")
	}
}

func TestImagePlaceholderAndBlackOut(t *testing.T) {
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 200; x++ {
			img.Set(x, y, color.White)
		}
	}
	_ = jpeg.Encode(&buf, img, nil)
	placeholder, err := placeholderImage(buf.Bytes(), "a.jpg", placeholderIDText)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(placeholder))
	if err != nil || cfg.Width != 200 || cfg.Height != 100 {
		t.Fatalf("placeholder %v %+v", err, cfg)
	}
	blacked, err := blackOut(buf.Bytes(), "a.jpg", []ocrLine{{Text: "x", X: 0.25, Y: 0.25, W: 0.5, H: 0.5}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := jpeg.Decode(bytes.NewReader(blacked))
	if err != nil {
		t.Fatal(err)
	}
	if r, g, b, _ := out.At(100, 50).RGBA(); r > 0x2000 || g > 0x2000 || b > 0x2000 {
		t.Errorf("centre should be black, got %d %d %d", r, g, b)
	}
	if r, _, _, _ := out.At(5, 5).RGBA(); r < 0xd000 {
		t.Errorf("corner should stay white")
	}
	if !bytes.HasPrefix(placeholderPDF(placeholderIDText), []byte("%PDF-1.4")) {
		t.Error("placeholder PDF header")
	}
}

func TestIDScore(t *testing.T) {
	passport := []ocrLine{
		{Text: "PASSPORT"}, {Text: "Surname"}, {Text: "Date of birth"}, {Text: "Nationality"},
		{Text: "P<GBRDOE<<JANE<<<<<<<<<<<<<<<<<<<<<<<<<<<<<"}, {Text: "1234567897GBR8001014F2501017<<<<<<<<<<<<<<04"},
	}
	if idScore(passport) < idScoreThreshold {
		t.Errorf("passport scored %d", idScore(passport))
	}
	menu := []ocrLine{{Text: "Today's menu"}, {Text: "Soup 5"}, {Text: "Salad 7"}}
	if idScore(menu) >= idBorderline {
		t.Errorf("menu scored %d", idScore(menu))
	}
}

func TestRedactVCard(t *testing.T) {
	card := "BEGIN:VCARD\r\nVERSION:3.0\r\nN:Hartwell;Zelda;;;\r\nFN:Zelda Hartwell\r\nitem1.TEL;waid=447911123456:+44 7911 123456\r\nEMAIL:z@example.org\r\nX-WA-BIZ-NAME:Zelda Shop\r\nEND:VCARD\r\n"
	out := chatOutput{Slots: []slot{{Value: "Zelda Hartwell", Tag: "{{PATIENT_SISTER_FULL_NAME}}"}}}
	got := redactVCard(card, out, dictionary, "")
	for _, secret := range []string{"Zelda", "Hartwell", "7911", "example.org"} {
		if strings.Contains(got, secret) {
			t.Errorf("vcard still contains %q:\n%s", secret, got)
		}
	}
	for _, want := range []string{"FN:{{PATIENT_SISTER_FULL_NAME}}", "TEL:{{PATIENT_SISTER_PHONE}}", "BEGIN:VCARD", "END:VCARD"} {
		if !strings.Contains(got, want) {
			t.Errorf("vcard missing %q:\n%s", want, got)
		}
	}
}

func readZipFiles(t *testing.T, path string) map[string]string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	files := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(rc)
		rc.Close()
		files[f.Name] = string(data)
	}
	return files
}

func TestRedactedZipEndToEnd(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, "Lena Hartwell")
	must(t, os.MkdirAll(folder, 0o700))
	must(t, os.WriteFile(filepath.Join(folder, "_chat.txt"), []byte(sampleChat), 0o600))
	must(t, os.WriteFile(filepath.Join(folder, "00000001.jpg"), jpegWithGPS(t), 0o600))
	index := "\xef\xbb\xbfFolder,Chat,Type,Phone number or group ID,WhatsApp profile name,Archived,Messages,First message,Last message,Attachments saved,Attachments missing\r\nLena Hartwell,Lena Hartwell,Individual,+447700900999,Lena,No,4,x,y,1,0\r\n"
	must(t, os.WriteFile(filepath.Join(dir, indexFileName), []byte(index), 0o600))
	must(t, os.WriteFile(filepath.Join(dir, reportFileName), []byte("WhatsApp export for +15550001111\r\n"), 0o600))

	ctx := context.Background()
	r := newRedactor(&fakeAI{}, newAICache(filepath.Join(dir, redactionCacheDir)), dictionary, sampleBook())
	run := newRedactionRun(ctx, r, dir, "15550001111")
	run.media = newMediaChecker(r, filepath.Join(dir, redactedMediaDir))
	run.media.submit(filepath.Join(folder, "00000001.jpg"))
	run.finish(ctx)
	out := filepath.Join(t.TempDir(), "redacted.zip")
	totals, err := run.writeRedactedZip(out, nil)
	must(t, err)
	files := readZipFiles(t, out)
	var chat string
	for name, data := range files {
		if strings.HasSuffix(name, "/_chat.txt") {
			chat = data
			if strings.Contains(name, "Lena") {
				t.Errorf("folder name leaks: %s", name)
			}
		}
		for _, secret := range []string{"Lena", "Hartwell", "Zelda", "7911", "example.org", "X1234567", "+447700900999", "15550001111"} {
			if strings.Contains(data, secret) {
				t.Errorf("%s still contains %q", name, secret)
			}
		}
	}
	if chat == "" || !strings.Contains(files[indexFileName], "{{PATIENT_PHONE}}") || !strings.Contains(files[reportFileName], "{{OWNER_PHONE}}") {
		t.Errorf("unexpected redacted files: %v", files)
	}
	if !strings.Contains(files[redactionReportName], "Details replaced with tags") {
		t.Error("redaction report missing")
	}
	if totals.Slots == 0 || totals.Media.GPSRemoved != 1 {
		t.Errorf("totals %+v", totals)
	}
	for name, data := range files {
		if strings.HasSuffix(name, ".jpg") && bytes.Contains([]byte(data), []byte("Exif\x00\x00")) {
			t.Error("GPS should be stripped from the redacted photo")
		}
	}
}
