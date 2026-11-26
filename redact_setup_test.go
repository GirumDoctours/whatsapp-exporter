package main

import (
	"bufio"
	"errors"
	"strings"
	"testing"
)

func TestAskKeyOpensHelpPageThenReadsKey(t *testing.T) {
	var screen strings.Builder
	term := &terminal{in: bufio.NewReader(strings.NewReader("?\nsk-test\n")), out: &screen}
	var opened []string
	openKeyPage = func(url string) error {
		opened = append(opened, url)
		return nil
	}
	defer func() { openKeyPage = openURL }()

	key := askKey(term, anthropicKeyHelp, "Key: ")

	if key != "sk-test" {
		t.Fatalf("key = %q, want sk-test", key)
	}
	if len(opened) != 1 || opened[0] != anthropicKeyHelp.url {
		t.Fatalf("opened %v, want one visit to %s", opened, anthropicKeyHelp.url)
	}
	for _, want := range []string{"Type ? and press Enter", "Opening the guide", anthropicKeyHelp.url, anthropicKeyHelp.steps[0], "paste the key"} {
		if !strings.Contains(screen.String(), want) {
			t.Errorf("screen is missing %q:\n%s", want, screen.String())
		}
	}
}

func TestAskKeyShowsAddressWhenBrowserWontOpen(t *testing.T) {
	var screen strings.Builder
	term := &terminal{in: bufio.NewReader(strings.NewReader("?\n\n")), out: &screen}
	openKeyPage = func(string) error { return errors.New("no browser") }
	defer func() { openKeyPage = openURL }()

	if key := askKey(term, deepgramKeyHelp, "Key: "); key != "" {
		t.Fatalf("key = %q, want empty so the step is skipped", key)
	}
	if !strings.Contains(screen.String(), "open "+deepgramKeyHelp.url+" in your browser") {
		t.Errorf("screen doesn't give the address to open:\n%s", screen.String())
	}
}
