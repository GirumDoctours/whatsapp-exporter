package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestShowMisses(t *testing.T) {
	loadDotEnv(t)
	label, run := os.Getenv("EXPORTER_MISSES"), firstNonEmpty(os.Getenv("EXPORTER_MISSES_RUN"), "3")
	if label == "" {
		t.Skip("set EXPORTER_MISSES=<label> to list missed details from a cached scoring run")
	}
	data, err := os.ReadFile(goldPath)
	must(t, err)
	var gold goldSet
	must(t, json.Unmarshal(data, &gold))
	chats := loadEvalSet(t, ".eval/train")
	book := ownerBookFor(chats)
	r := newRedactor(newHaikuClient(os.Getenv("ANTHROPIC_API_KEY")), newAICache(filepath.Join(".eval", "score-cache", label, run)), dictionary, book)
	outputs := map[string]chatOutput{}
	for _, c := range chats {
		outputs[c.input.Folder] = r.redactChat(context.Background(), c.input)
	}
	shown := 0
	for _, gl := range gold.Lines {
		out := outputs[gl.Chat]
		if gl.Line >= len(out.Lines) {
			continue
		}
		red := out.Lines[gl.Line].text()
		for _, s := range gl.Spans {
			if len([]rune(s.Text)) < 2 || spanCovered(red, s.Text, s.Kind) || shown >= 10 {
				continue
			}
			orig := parseChat(chatText(chats, gl.Chat), nil).Lines[gl.Line].text()
			t.Logf("\nMISSED %s %q\n  original: %s\n  redacted: %s", s.Kind, s.Text, around(orig, s.Text), around(red, s.Text))
			shown++
		}
	}
}

func chatText(chats []evalChat, folder string) string {
	for _, c := range chats {
		if c.input.Folder == folder {
			return c.input.Text
		}
	}
	return ""
}

func around(text, needle string) string {
	i := strings.Index(text, needle)
	if i < 0 {
		if len(text) > 240 {
			return text[:240] + "…"
		}
		return text
	}
	start, end := max(0, i-120), min(len(text), i+len(needle)+120)
	for start > 0 && !utf8.RuneStart(text[start]) {
		start--
	}
	for end < len(text) && !utf8.RuneStart(text[end]) {
		end++
	}
	return strings.ReplaceAll("…"+text[start:end]+"…", "\n", " ⏎ ")
}
