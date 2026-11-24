package main

import (
	"archive/zip"
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestDebugLogCapturesOwnComponentsOnly(t *testing.T) {
	var file bytes.Buffer
	detach := appLog.attach(&file)
	defer detach()
	enableDebugLog()
	logRedact.Debugf("redaction detail %d", 7)
	appLog.Sub("WA/Client").Debugf("noisy socket frame")
	appLog.Sub("WA/Client").Warnf("library warning")
	logAI.Errorf("request failed")
	got := string(debugLogContents())
	for _, want := range []string{"[Exporter/Redact DEBUG] redaction detail 7", "[Exporter/WA/Client WARN] library warning", "[Exporter/AI ERROR] request failed"} {
		if !strings.Contains(got, want) {
			t.Errorf("debug log missing %q", want)
		}
	}
	if strings.Contains(got, "noisy socket frame") {
		t.Error("library debug lines should stay out of debug.txt unless WHATSAPP_EXPORTER_DEBUG is set")
	}
	if !strings.Contains(file.String(), "request failed") || strings.Contains(file.String(), "noisy socket frame") {
		t.Errorf("file log got %q", file.String())
	}

	out := filepath.Join(t.TempDir(), "with-debug.zip")
	must(t, writeZip(out, nil, addDebugLog, nil))
	zr, err := zip.OpenReader(out)
	must(t, err)
	defer zr.Close()
	if len(zr.File) != 1 || zr.File[0].Name != debugFileName {
		t.Fatalf("zip entries %v", zr.File)
	}
}

func TestMessagesOnlyRendering(t *testing.T) {
	msgs := []storedMessage{
		{ChatJID: "1@s.whatsapp.net", ID: "I1", SenderJID: "1@s.whatsapp.net", Timestamp: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), PushName: "Ann",
			MediaState: mediaDone, MediaFile: "x.jpg",
			Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/jpeg"), Caption: proto.String("look")}}},
	}
	names := &nameResolver{contacts: nil, ownName: "Me", ownUsers: map[string]bool{}, pushNames: map[string]string{}}
	withMedia := renderChatWith("Ann", msgs, names, t.TempDir(), time.UTC, false)
	textOnly := renderChatWith("Ann", msgs, names, t.TempDir(), time.UTC, true)
	if len(withMedia.Attachments) != 1 || len(textOnly.Attachments) != 0 {
		t.Fatalf("attachments %d / %d", len(withMedia.Attachments), len(textOnly.Attachments))
	}
	if len(textOnly.Missing) != 0 {
		t.Errorf("messages-only exports shouldn't list media as missing")
	}
	if !strings.Contains(textOnly.Text, "image omitted\r\nlook") {
		t.Errorf("unexpected text:\n%s", textOnly.Text)
	}
	if got := withNameSuffix("15551234567-11-26-test.zip", messagesOnlySuffix); got != "15551234567-11-26-test-messages-only.zip" {
		t.Errorf("name %s", got)
	}
}
