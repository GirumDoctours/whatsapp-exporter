package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestTranscriberWritesSidecarAndRetries(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Token test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if calls == 1 {
			w.Header().Set("retry-after", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if r.URL.Query().Get("model") != "nova-3" || r.Header.Get("Content-Type") != "audio/ogg" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"results":{"channels":[{"alternatives":[{"transcript":" Hi, I'm Lena, call me on +44 7911 123456. "}]}]}}`))
	}))
	defer server.Close()
	old := deepgramListenURL
	deepgramListenURL = server.URL + "/v1/listen?model=nova-3"
	defer func() { deepgramListenURL = old }()

	dir := t.TempDir()
	audio := filepath.Join(dir, "note.opus")
	must(t, os.WriteFile(audio, []byte("OggS fake"), 0o600))
	tr := newTranscriber("test-key")
	tr.transcribeFile(context.Background(), audio, 7)
	text, ok := readTranscript(audio)
	if !ok || text != "Hi, I'm Lena, call me on +44 7911 123456." || calls != 2 {
		t.Fatalf("transcript %q ok %v after %d calls", text, ok, calls)
	}
	if tr.done.Load() != 1 || !strings.Contains(tr.summary(), "1 voice notes transcribed") {
		t.Errorf("summary %s", tr.summary())
	}
	tr.transcribeFile(context.Background(), audio, 7)
	if calls != 2 {
		t.Error("an existing transcript should be reused, not requested again")
	}

	msg := storedMessage{ChatJID: "1@s.whatsapp.net", ID: "A1", SenderJID: "1@s.whatsapp.net", Timestamp: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		MediaState: mediaDone, MediaFile: "note.opus", Message: &waE2E.Message{AudioMessage: &waE2E.AudioMessage{Mimetype: proto.String("audio/ogg; codecs=opus"), PTT: proto.Bool(true)}}}
	names := &nameResolver{ownName: "Me", ownUsers: map[string]bool{}, pushNames: map[string]string{}}
	out := renderChat("Lena", []storedMessage{msg}, names, dir, time.UTC)
	if !strings.Contains(out.Text, "<attached: 00000001.opus>\r\n‎Transcript: Hi, I'm Lena") {
		t.Errorf("transcript missing from the chat:\n%s", out.Text)
	}
	r := newRedactor(nil, nil, dictionary, sampleBook())
	red := r.redactChat(context.Background(), chatInput{Title: "Lena", Text: out.Text})
	if strings.Contains(red.Text, "7911 123456") {
		t.Errorf("the transcript should be redacted:\n%s", red.Text)
	}
}
