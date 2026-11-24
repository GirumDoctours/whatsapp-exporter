package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadDotEnv(t *testing.T) {
	data, err := os.ReadFile(".env")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		key = strings.TrimSpace(strings.TrimPrefix(key, "export "))
		if !ok || key == "" || strings.HasPrefix(key, "#") {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if value != "" && os.Getenv(key) == "" {
			t.Setenv(key, value)
		}
	}
}

const liveChat = "[03/03/2026, 09:00:00] Lena Hartwell: ‎Messages and calls are end-to-end encrypted. Only people in this chat can read, listen to, or share them.\r\n" +
	"[03/03/2026, 09:01:00] Lena Hartwell: Hello! I'm Lena, 34 years old, from Leeds. I'd like 3000 grafts.\r\n" +
	"[03/03/2026, 09:02:00] Halcyon Team: Welcome Lena! Dr. Okonkwo at Asteria Hair Clinic can do 14 March.\r\n" +
	"[03/03/2026, 09:03:00] Lena Hartwell: Perfect. My sister Zelda Hartwell will come with me, her number is +44 7911 123456\r\n" +
	"My email is lena.h@example.org and my insta is @lena.hartwell\r\n" +
	"[03/03/2026, 09:04:00] Halcyon Team: Please send your passport number and flight.\r\n" +
	"[03/03/2026, 09:05:00] Lena Hartwell: Passport no: X1234567, flight TK1980 landing 13 March. Home address 12 Rosebank Road, Leeds LS6 2AB\r\n" +
	"[03/03/2026, 09:06:00] Halcyon Team: Thanks! You'll stay at Hotel Marisol near the clinic. Deposit to GB82 WEST 1234 5698 7654 32 please.\r\n" +
	"[03/03/2026, 09:07:00] Lena Hartwell: Done, and Zelda says thank you!\r\n"

func TestLiveRedaction(t *testing.T) {
	loadDotEnv(t)
	if os.Getenv("EXPORTER_AI_EVAL") != "1" {
		t.Skip("set EXPORTER_AI_EVAL=1 to run a real Haiku redaction (costs a fraction of a cent)")
	}
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Fatal("put ANTHROPIC_API_KEY in .env")
	}
	ai := newHaikuClient(key)
	ctx := context.Background()
	if err := ai.check(ctx); err != nil {
		t.Fatalf("the key didn't work: %v", err)
	}
	r := newRedactor(ai, newAICache(filepath.Join(t.TempDir(), "cache")), dictionary, sampleBook())
	out := r.redactChat(ctx, chatInput{Folder: "Lena Hartwell", Title: "Lena Hartwell", Text: liveChat})
	t.Logf("redacted chat:\n%s", out.Text)
	t.Logf("roles: %v", out.Roles)
	t.Logf("stats: %+v", out.Stats)
	t.Logf("usage: %s", ai.usage.summary())
	if !out.AIUsed {
		t.Fatalf("the AI wasn't used (stopped: %s)", r.aiStopReason())
	}
	doc := parseChat(liveChat, []string{"Lena Hartwell", "Halcyon Team"})
	for i := range doc.Lines {
		if !checkTemplate(doc.Lines[i].text(), out.Lines[i].text(), dictionary) {
			t.Errorf("line %d failed the template check", i)
		}
	}
	for _, secret := range []string{"Lena", "Hartwell", "Zelda", "7911 123456", "lena.h@example.org", "@lena.hartwell", "X1234567", "Rosebank", "LS6 2AB", "GB82"} {
		if strings.Contains(out.Text, secret) {
			t.Errorf("still contains %q", secret)
		}
	}
	for _, soft := range []string{"Okonkwo", "Asteria", "Marisol", "TK1980", "14 March", "Leeds"} {
		if strings.Contains(out.Text, soft) {
			t.Logf("kept %q (judgement call)", soft)
		}
	}
}

const liveGroupChat = "[05/03/2026, 10:00:00] Founders Club: ‎Messages and calls are end-to-end encrypted. Only people in this chat can read, listen to, or share them.\r\n" +
	"[05/03/2026, 10:00:30] Founders Club: ‎+44 7911 222333 joined using this group's invite link\r\n" +
	"[05/03/2026, 10:01:00] Halcyon Team: Welcome Priya! Can you share your deck? RSVP for Friday: https://lu.ma/founders-night\r\n" +
	"[05/03/2026, 10:02:00] +44 7911 222333: Thanks! Inshallah I'll send it. Our site is brightpath.ai and my email is priya@brightpath.ai\r\n" +
	"[05/03/2026, 10:03:00] Halcyon Team: The Consultant agreement for Brightpath Labs is attached. @123456789012345 please review\r\n" +
	"[05/03/2026, 10:04:00] +44 7911 222333: Priya_Raman_CV.pdf ‎<attached: 00000001.pdf>\r\n" +
	"[05/03/2026, 10:05:00] Halcyon Team: Great, thanks Priya\r\n"

func TestLiveGroupRedaction(t *testing.T) {
	loadDotEnv(t)
	if os.Getenv("EXPORTER_AI_EVAL") != "1" {
		t.Skip("set EXPORTER_AI_EVAL=1 to run a real Haiku redaction")
	}
	ai := newHaikuClient(os.Getenv("ANTHROPIC_API_KEY"))
	r := newRedactor(ai, newAICache(filepath.Join(t.TempDir(), "cache")), dictionary, sampleBook())
	out := r.redactChat(context.Background(), chatInput{Folder: "Founders Club", Title: "Founders Club", IsGroup: true, Text: liveGroupChat})
	t.Logf("redacted chat:\n%s", out.Text)
	t.Logf("roles: %v stats: %+v usage: %s", out.Roles, out.Stats, ai.usage.summary())
	for _, secret := range []string{"Priya", "Raman", "7911 222333", "lu.ma", "brightpath.ai", "Brightpath", "123456789012345"} {
		if strings.Contains(out.Text, secret) {
			t.Errorf("still contains %q", secret)
		}
	}
	for _, keep := range []string{"Can you share", "Consultant agreement", "Inshallah", "joined using this group's invite link"} {
		if !strings.Contains(out.Text, keep) {
			t.Errorf("over-redacted %q", keep)
		}
	}
	if strings.Contains(out.Text, "{{CONTACT_") {
		t.Error("CONTACT must not appear in a group")
	}
	doc := parseChat(liveGroupChat, nil)
	for i := range doc.Lines {
		if !checkTemplate(doc.Lines[i].text(), out.Lines[i].text(), dictionary) {
			t.Errorf("line %d failed the template check", i)
		}
	}
}
