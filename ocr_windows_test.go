//go:build windows

package main

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

func TestWindowsTextRecognition(t *testing.T) {
	recognizer, err := newTextRecognizer()
	if err != nil {
		t.Skipf("text recognition isn't available here: %v", err)
	}
	defer recognizer.close()
	small := image.NewRGBA(image.Rect(0, 0, 220, 40))
	draw.Draw(small, small.Bounds(), image.White, image.Point{}, draw.Src)
	pen := font.Drawer{Dst: small, Src: image.NewUniform(color.Black), Face: basicfont.Face7x13, Dot: fixed.P(10, 25)}
	pen.DrawString("Passport number X1234567")
	big := image.NewRGBA(image.Rect(0, 0, 880, 160))
	xdraw.CatmullRom.Scale(big, big.Bounds(), small, small.Bounds(), xdraw.Src, nil)
	path := filepath.Join(t.TempDir(), "fotoğraf 1.png")
	f, err := os.Create(path)
	must(t, err)
	must(t, png.Encode(f, big))
	must(t, f.Close())
	result, err := recognizer.recognize(path)
	must(t, err)
	var text strings.Builder
	for _, l := range result.Lines {
		text.WriteString(l.Text + " ")
	}
	if !strings.Contains(strings.ToLower(text.String()), "passport") {
		t.Errorf("read %q, want the passport line", text.String())
	}
}
