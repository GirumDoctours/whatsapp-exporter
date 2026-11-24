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
			t.say("  %s", t.paint(ansiDim, "Create a key at platform.claude.com, under Settings, then API keys."))
			key = t.askSecret("  " + t.paint(ansiBold, "Paste your Anthropic API key, or press Enter to skip: "))
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
			t.say("  %s", t.paint(ansiDim, "Create a key at console.deepgram.com, under API Keys."))
			key = t.askSecret("  " + t.paint(ansiBold, "Paste your Deepgram API key, or press Enter to skip voice notes: "))
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
