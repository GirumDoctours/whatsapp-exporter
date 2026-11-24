package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"google.golang.org/protobuf/proto"
)

func TestHumanETA(t *testing.T) {
	cases := map[time.Duration]string{
		20 * time.Second:               "under a minute left",
		70 * time.Second:               "about 1 min left",
		7*time.Minute + 20*time.Second: "about 7 min left",
		2 * time.Hour:                  "about 2 hr left",
		time.Hour + 25*time.Minute:     "about 1 hr 25 min left",
	}
	for d, want := range cases {
		if got := humanETA(d); got != want {
			t.Errorf("humanETA(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestETAEstimatorIgnoresPausedTime(t *testing.T) {
	start := time.Now().Add(-10 * time.Minute)
	e := etaEstimator{start: start, last: start, paused: 5 * time.Minute}
	got := e.text(0.5, false)
	if got != "about 5 min left" {
		t.Errorf("eta = %q, want about 5 minutes left", got)
	}
	if got := e.text(0.5, true); got != "paused" {
		t.Errorf("stalled eta = %q", got)
	}
	fresh := etaEstimator{start: time.Now(), last: time.Now()}
	if got := fresh.text(0.5, false); got != "estimating time…" {
		t.Errorf("early eta = %q", got)
	}
}

func TestTruncateVisibleKeepsColorCodes(t *testing.T) {
	colored := ansiGreen + "████████" + ansiReset + "░░░░"
	got := truncateVisible(colored, 5)
	if strings.Count(got, "█") != 5 || strings.Contains(got, "░") {
		t.Errorf("truncate = %q", got)
	}
	if !strings.HasSuffix(got, ansiReset) {
		t.Errorf("truncated colored text should end with a reset: %q", got)
	}
	if got := truncateVisible("hello", 10); got != "hello" {
		t.Errorf("short text changed: %q", got)
	}
}

func TestPhoneWatchStallsOnlyWhileWaiting(t *testing.T) {
	w := newPhoneWatch()
	w.lastActivity = time.Now().Add(-time.Minute)
	if w.stalled() {
		t.Fatal("idle watch with nothing outstanding should not be stalled")
	}
	done := w.begin()
	w.busySince = time.Now().Add(-30 * time.Second)
	if !w.stalled() {
		t.Fatal("request outstanding with a silent phone should be stalled")
	}
	w.touch()
	if w.stalled() {
		t.Fatal("a reply from the phone should clear the stall")
	}
	done()
	sent := time.Now()
	if w.repliedSince(sent) {
		t.Fatal("no reply has come since sending")
	}
	time.Sleep(20 * time.Millisecond)
	w.touch()
	if !w.repliedSince(sent) {
		t.Fatal("reply should be seen")
	}
}

func TestPhoneWatchWaitReturnsOnActivity(t *testing.T) {
	w := newPhoneWatch()
	since := time.Now()
	go func() {
		time.Sleep(50 * time.Millisecond)
		w.touch()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if !w.waitForActivity(ctx, since) {
		t.Fatal("expected activity to end the wait")
	}
}

func TestRenderDeletedStubsAndSenderlessEvents(t *testing.T) {
	group := "120363000000000000@g.us"
	msgs := []storedMessage{
		{ChatJID: "905551112233@s.whatsapp.net", ID: "D1", SenderJID: "905551112233@s.whatsapp.net", Timestamp: at(0),
			StubType: int32(waWeb.WebMessageInfo_REVOKE)},
		{ChatJID: "905551112233@s.whatsapp.net", ID: "D2", FromMe: true, Timestamp: at(1),
			StubType: int32(waWeb.WebMessageInfo_REVOKE)},
	}
	got := renderChat("Ayşe Yılmaz", msgs, fakeNamer{}, "/media", testZone)
	for _, want := range []string{
		"[05/09/2024, 11:00:50] Ayşe Yılmaz: \u200eThis message was deleted.\r\n",
		"[05/09/2024, 11:01:50] Northwind: \u200eYou deleted this message.\r\n",
	} {
		if !strings.Contains(got.Text, want) {
			t.Errorf("missing %q in\n%s", want, got.Text)
		}
	}

	groupMsgs := []storedMessage{
		{ChatJID: group, ID: "G1", IsGroup: true, Timestamp: at(0),
			StubType: int32(waWeb.WebMessageInfo_GROUP_CREATE), StubParams: []string{"Clinic team"}},
		{ChatJID: group, ID: "G2", IsGroup: true, Timestamp: at(1),
			StubType: int32(waWeb.WebMessageInfo_GROUP_PARTICIPANT_ADD), StubParams: []string{"905551112233@s.whatsapp.net"}},
		{ChatJID: group, ID: "G3", IsGroup: true, Timestamp: at(2),
			StubType: int32(waWeb.WebMessageInfo_GROUP_PARTICIPANT_CHANGE_NUMBER), StubParams: []string{"447700900123@s.whatsapp.net"}},
		{ChatJID: group, ID: "G4", IsGroup: true, Timestamp: at(3),
			StubType: int32(waWeb.WebMessageInfo_GROUP_PARTICIPANT_LEAVE)},
	}
	got = renderChat("Clinic team", groupMsgs, fakeNamer{}, "/media", testZone)
	for _, want := range []string{
		"Clinic team: \u200eGroup “Clinic team” was created\r\n",
		"Clinic team: \u200eAyşe Yılmaz joined\r\n",
		"Clinic team: \u200e+447700900123 changed their phone number to a new number\r\n",
	} {
		if !strings.Contains(got.Text, want) {
			t.Errorf("missing %q in\n%s", want, got.Text)
		}
	}
	if strings.Contains(got.Text, " left") {
		t.Errorf("a leave event with nobody named should be skipped:\n%s", got.Text)
	}
}

func TestRenderSenderlessGroupMessagesUseProfileName(t *testing.T) {
	group := "120363000000000000@g.us"
	msgs := []storedMessage{
		{ChatJID: group, ID: "N1", IsGroup: true, Timestamp: at(0), PushName: "Kemal",
			Message: &waE2E.Message{Conversation: proto.String("Hello everyone")}},
		{ChatJID: group, ID: "N2", IsGroup: true, Timestamp: at(1),
			Message: &waE2E.Message{Conversation: proto.String("No name on this one")}},
	}
	got := renderChat("Founders", msgs, fakeNamer{}, "/media", testZone)
	for _, want := range []string{
		"[05/09/2024, 11:00:50] Kemal: Hello everyone\r\n",
		"[05/09/2024, 11:01:50] Founders: No name on this one\r\n",
	} {
		if !strings.Contains(got.Text, want) {
			t.Errorf("missing %q in\n%s", want, got.Text)
		}
	}
}
