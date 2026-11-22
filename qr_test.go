package main

import (
	"strings"
	"testing"
)

func TestQRArtFitsAWhatsAppCode(t *testing.T) {
	code := "2@" + strings.Repeat("A", 86) + "," + strings.Repeat("B", 44) + "," + strings.Repeat("C", 44) + "," + strings.Repeat("D", 44) + ",1"
	art, rows, cols := qrArt(code)
	if rows < 20 || rows > 40 {
		t.Fatalf("QR is %d rows tall, expected a half-block code of 20 to 40 rows", rows)
	}
	if cols > 80 {
		t.Fatalf("QR is %d columns wide, wider than a standard 80-column window", cols)
	}
	if strings.Count(art, "\r\n") != rows || strings.Contains(art, "\033") {
		t.Fatalf("QR art should be %d CRLF lines with no escape codes", rows)
	}
}

func TestPickQRLayout(t *testing.T) {
	const artRows, artCols = 31, 63
	cases := []struct {
		name       string
		cols, rows int
		known      bool
		want       qrLayout
	}{
		{"unknown size", 0, 0, false, qrFull},
		{"roomy Mac window", 84, 46, true, qrFull},
		{"just fits with instructions", 80, artRows + qrFullExtraRows, true, qrFull},
		{"default Windows window", 120, 30, true, qrTooSmall},
		{"only the code fits", 120, artRows + 1, true, qrCompact},
		{"too narrow", 60, 60, true, qrTooSmall},
	}
	for _, c := range cases {
		if got := pickQRLayout(c.cols, c.rows, c.known, artRows, artCols); got != c.want {
			t.Errorf("%s: got layout %d, want %d", c.name, got, c.want)
		}
	}
}
