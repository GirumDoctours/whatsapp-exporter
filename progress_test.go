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

func TestSmootherEasesInsteadOfJumping(t *testing.T) {
	start := time.Now()
	s := newSmoother(start)
	now := start.Add(100 * time.Millisecond)
	first := s.advance(0.4, false, now)
	if first <= 0 || first >= 0.4 {
		t.Fatalf("first frame after a jump to 40%% shows %.3f, want partway there", first)
	}
	previous := first
	for range 30 {
		now = now.Add(100 * time.Millisecond)
		got := s.advance(0.4, false, now)
		if got < previous {
			t.Fatalf("bar went backward: %.4f after %.4f", got, previous)
		}
		previous = got
	}
	if previous < 0.4 {
		t.Fatalf("after 3 seconds the bar shows %.4f, want it to reach 40%%", previous)
	}
}

func TestSmootherCreepsButStaysBelowNextCheckpoint(t *testing.T) {
	start := time.Now()
	s := newSmoother(start)
	now := start
	for _, target := range []float64{0.1, 0.2, 0.3} {
		now = now.Add(5 * time.Second)
		s.advance(target, false, now)
	}
	ceiling := s.ceiling()
	if ceiling <= 0.3 || ceiling >= 0.3+maxCreepStep {
		t.Fatalf("ceiling %.3f should sit between the target and one step past it", ceiling)
	}
	var previous float64
	for range 600 {
		now = now.Add(100 * time.Millisecond)
		got := s.advance(0.3, false, now)
		if got < previous {
			t.Fatalf("bar went backward while creeping: %.4f after %.4f", got, previous)
		}
		if got > ceiling {
			t.Fatalf("creep passed the ceiling: %.4f > %.4f", got, ceiling)
		}
		previous = got
	}
	if previous <= 0.3 {
		t.Fatalf("bar stayed frozen at %.4f while waiting", previous)
	}
	if got := s.advance(0.25, false, now.Add(time.Second)); got < previous {
		t.Fatalf("a smaller report pulled the bar back to %.4f", got)
	}
}

func TestSmootherHoldsStillWhilePaused(t *testing.T) {
	start := time.Now()
	s := newSmoother(start)
	now := start
	for range 40 {
		now = now.Add(100 * time.Millisecond)
		s.advance(0.3, false, now)
	}
	held := s.advance(0.3, true, now.Add(100*time.Millisecond))
	for range 100 {
		now = now.Add(100 * time.Millisecond)
		if got := s.advance(0.3, true, now); got != held {
			t.Fatalf("bar moved from %.4f to %.4f while paused", held, got)
		}
	}
}

func TestSmootherReachesTheEnd(t *testing.T) {
	start := time.Now()
	s := newSmoother(start)
	now := start
	var got float64
	for range 40 {
		now = now.Add(100 * time.Millisecond)
		got = s.advance(1, false, now)
	}
	if got != 1 {
		t.Fatalf("finished work shows %.4f, want exactly 1", got)
	}
}

func TestHeartbeatBlinksOnlyWhileWorkMoves(t *testing.T) {
	start := time.UnixMilli(0)
	h := heartbeat{lastChange: start}
	h.observe(0.1, "chat 1 of 5", start)
	on := h.state(start, true, false)
	off := h.state(start.Add(blinkPeriod), true, false)
	if on != pulseOn || off != pulseOff {
		t.Fatalf("active work should blink, got %v then %v", on, off)
	}
	later := start.Add(quietAfter + time.Second)
	if got := h.state(later, true, false); got != pulseQuiet {
		t.Fatalf("no change for a while should dim the dot, got %v", got)
	}
	h.observe(0.1, "chat 2 of 5", later)
	if got := h.state(later, true, false); got != pulseOn {
		t.Fatalf("new status text should restart the blink, got %v", got)
	}
	if got := h.state(later, true, true); got != pulseStalled {
		t.Fatalf("a stalled phone should show the stalled dot, got %v", got)
	}
}

func TestBarKeepsItsWidth(t *testing.T) {
	original := glyphs
	defer func() { glyphs = original }()
	plain := original
	plain.barEighths = nil
	for name, set := range map[string]glyphSet{"unicode": original, "plain console": plain} {
		glyphs = set
		view := &progressView{t: &terminal{}}
		for _, fraction := range []float64{0, 0.013, 0.37, 0.5, 0.999, 1} {
			bar := view.bar(fraction, 40)
			if got := len([]rune(bar)); got != 40 {
				t.Errorf("%s bar at %.3f is %d cells wide, want 40: %q", name, fraction, got, bar)
			}
		}
	}
}
