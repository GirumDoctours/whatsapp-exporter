package main

import (
	"context"
	"os"
	"strings"
)

var redactionNotice = []string{
	"To find personal details, the text of your chats is checked by Claude,",
	"Anthropic's AI, using your own API key. Names, numbers and emails are",
	"hidden on this computer first. Photos and documents stay on this computer.",
}

type keyHelp struct {
	service string
	url     string
	where   string
	steps   []string
}

var anthropicKeyHelp = keyHelp{
	service: "Anthropic",
	url:     "https://girumdoctours.github.io/whatsapp-exporter/anthropic-key.html",
	where:   "Create a key at platform.claude.com, under Settings, then API keys.",
	steps: []string{
		"Go to platform.claude.com and sign in, or create a free account.",
		"Add a few dollars of credit under Billing.",
		"Open API keys, click Create key, give it any name and copy it.",
	},
}

var deepgramKeyHelp = keyHelp{
	service: "Deepgram",
	url:     "https://girumdoctours.github.io/whatsapp-exporter/deepgram-key.html",
	where:   "Create a key at console.deepgram.com, under API Keys.",
	steps: []string{
		"Go to console.deepgram.com and sign in, or create an account. New accounts get $200 of free credit.",
		"Open API Keys, create a new key and copy it.",
	},
}

var openKeyPage = openURL

func askKey(t *terminal, help keyHelp, prompt string) string {
	t.say("  %s", t.paint(ansiDim, help.where))
	t.say("  %s", t.paint(ansiDim, "Don't have a key? Type ? and press Enter for a step-by-step guide with pictures."))
	for {
		key := t.askSecret("  " + t.paint(ansiBold, prompt))
		if key != "?" {
			return key
		}
		showKeyHelp(t, help)
	}
}

func showKeyHelp(t *terminal, help keyHelp) {
	logSetup.Infof("Opening the %s key page", help.service)
	opened := openKeyPage(help.url) == nil
	t.say("")
	if opened {
		t.say("  Opening the guide in your browser. If nothing opens, go to %s", t.paint(ansiBold, help.url))
	} else {
		t.say("  For a guide with pictures, open %s in your browser.", t.paint(ansiBold, help.url))
	}
	for i, step := range help.steps {
		t.say("  %d. %s", i+1, step)
	}
	t.say("  %d. Come back to this window and paste the key (%s).", len(help.steps)+1, pasteKeys)
	t.say("  %s", t.paint(ansiDim, "Nothing appears while you paste, so the key stays hidden. Press Enter after pasting."))
	t.say("")
}

func askRedact(t *terminal) bool {
	return t.choose("What should the ZIP contain?", []menuOption{
		{label: "Redact PII, then zip", hint: "personal details replaced · the original isn't kept"},
		{label: "Just zip", hint: "your chats exactly as they are"},
	}) == 0
}

func setupAI(ctx context.Context, t *terminal) *haikuClient {
	t.say("")
	for _, line := range redactionNotice {
		t.say("  %s", line)
	}
	t.say("")
	key := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	logSetup.Infof("Setting up the AI check (key in environment: %v)", key != "")
	if key != "" {
		t.say("  %s", t.paint(ansiDim, "Using the API key from ANTHROPIC_API_KEY."))
	}
	for attempt := 0; attempt < 3; attempt++ {
		if key == "" {
			key = askKey(t, anthropicKeyHelp, "Paste your Anthropic API key, or press Enter to skip: ")
			if key == "" {
				logSetup.Infof("No API key given; local checks only")
				t.say("  %s", t.paint(ansiYellow, "No key, so personal details will be found with local checks only. These miss more."))
				t.say("")
				return nil
			}
		}
		client := newHaikuClient(key)
		err := client.check(ctx)
		if err == nil {
			t.say("  %s API key works", t.paint(ansiGreen, glyphs.check))
			t.say("")
			return client
		}
		if status, _ := apiStatus(err); status == 401 || status == 403 {
			t.say("  %s", t.paint(ansiYellow, "That key didn't work. Check it and paste it again."))
			key = ""
			continue
		}
		t.say("  %s", t.paint(ansiYellow, "Couldn't reach Claude just now. The check will be tried again during the export."))
		t.say("")
		return client
	}
	t.say("  %s", t.paint(ansiYellow, "Continuing with local checks only."))
	t.say("")
	return nil
}

var transcriptionNotice = []string{
	"Voice notes can be turned into text so they're readable and redacted too.",
	"Each voice note is sent to Deepgram, a speech-to-text service, using your",
	"own Deepgram API key. It costs about 26 cents per hour of audio.",
}

func setupTranscription(ctx context.Context, t *terminal) *transcriber {
	t.say("")
	for _, line := range transcriptionNotice {
		t.say("  %s", line)
	}
	t.say("")
	key := strings.TrimSpace(os.Getenv("DEEPGRAM_API_KEY"))
	logSetup.Infof("Setting up transcription (Deepgram key in environment: %v)", key != "")
	if key != "" {
		t.say("  %s", t.paint(ansiDim, "Using the Deepgram key from DEEPGRAM_API_KEY."))
	}
	for attempt := 0; attempt < 3; attempt++ {
		if key == "" {
			key = askKey(t, deepgramKeyHelp, "Paste your Deepgram API key, or press Enter to skip voice notes: ")
			if key == "" {
				logSetup.Infof("No Deepgram key; voice notes won't be transcribed")
				t.say("  %s", t.paint(ansiYellow, "Voice notes won't be transcribed."))
				t.say("")
				return nil
			}
		}
		tr := newTranscriber(key)
		status, err := tr.check(ctx)
		if err == nil {
			logSetup.Infof("Deepgram key works")
			t.say("  %s Deepgram key works", t.paint(ansiGreen, glyphs.check))
			t.say("")
			return tr
		}
		logSetup.Warnf("Deepgram key check failed (status %d): %v", status, err)
		if status == 401 || status == 403 {
			t.say("  %s", t.paint(ansiYellow, "That Deepgram key didn't work. Check it and paste it again."))
			key = ""
			continue
		}
		t.say("  %s", t.paint(ansiYellow, "Couldn't reach Deepgram just now. Transcription will be tried during the export."))
		t.say("")
		return tr
	}
	return nil
}
