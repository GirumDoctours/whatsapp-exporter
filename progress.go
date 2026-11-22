package main

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

const (
	progressTick       = 200 * time.Millisecond
	stallThreshold     = 20 * time.Second
	phonePatienceLimit = 15 * time.Minute
	labelWidth         = 22
)

const (
	stallHeadline = "⚠  Paused: waiting for your phone"
	stallAdvice   = "   Open WhatsApp on your phone and keep the screen unlocked to continue."
	hintWidth     = 22
)

type progressView struct {
	t       *terminal
	label   string
	started time.Time
	shown   float64
	frame   int
	eta     etaEstimator
}

func newProgressView(t *terminal, label string) *progressView {
	now := time.Now()
	return &progressView{t: t, label: label, started: now, eta: etaEstimator{start: now, last: now}}
}

func (p *progressView) update(fraction float64, known, stalled bool) {
	p.frame++
	fraction = math.Max(0, math.Min(1, fraction))
	if fraction > p.shown {
		p.shown = fraction
	}
	p.eta.observe(stalled)

	width := p.t.width()
	barWidth := max(10, min(40, width-labelWidth-hintWidth-14))
	label := p.label + strings.Repeat(" ", max(1, labelWidth-len([]rune(p.label))))
	var bar, percent, hint string
	if known {
		bar = p.bar(barWidth)
		percent = fmt.Sprintf("%3d%%", int(p.shown*100))
		hint = p.eta.text(p.shown, stalled)
	} else {
		bar = p.bounce(barWidth)
		percent = "    "
		hint = "starting…"
		if stalled {
			hint = "paused"
		}
	}
	line := "  " + p.t.paint(ansiBold, label) + bar + "  " + percent + "  " + p.t.paint(ansiDim, hint)
	if stalled {
		p.t.live(line, "", "  "+p.t.paint(ansiYellow+ansiBold, stallHeadline), "  "+p.t.paint(ansiYellow, stallAdvice))
		return
	}
	p.t.live(line)
}

func (p *progressView) bar(width int) string {
	filled := int(math.Round(p.shown * float64(width)))
	return p.t.paint(ansiGreen, strings.Repeat("█", filled)) + p.t.paint(ansiDim, strings.Repeat("░", width-filled))
}

func (p *progressView) bounce(width int) string {
	const block = 6
	span := width - block
	pos := p.frame % (2 * span)
	if pos > span {
		pos = 2*span - pos
	}
	return p.t.paint(ansiDim, strings.Repeat("░", pos)) +
		p.t.paint(ansiGreen, strings.Repeat("█", block)) +
		p.t.paint(ansiDim, strings.Repeat("░", span-pos))
}

func (p *progressView) finish(note string) {
	elapsed := "done in " + humanDuration(time.Since(p.started))
	line := "  " + p.t.paint(ansiGreen, "✔ ") + p.t.paint(ansiBold, p.label) + "  " + p.t.paint(ansiDim, firstNonEmpty(note, elapsed))
	p.t.live()
	p.t.say("%s", line)
}

func (p *progressView) abandon() {
	p.t.live()
}

type etaEstimator struct {
	start    time.Time
	last     time.Time
	paused   time.Duration
	smoothed float64
}

func (e *etaEstimator) observe(stalled bool) {
	now := time.Now()
	if stalled {
		e.paused += now.Sub(e.last)
	}
	e.last = now
}

func (e *etaEstimator) text(fraction float64, stalled bool) string {
	if stalled {
		return "paused"
	}
	if fraction >= 1 {
		return "finishing…"
	}
	active := time.Since(e.start) - e.paused
	if fraction < 0.02 || active < 5*time.Second {
		return "estimating time…"
	}
	raw := active.Seconds() * (1 - fraction) / fraction
	if e.smoothed == 0 {
		e.smoothed = raw
	} else {
		e.smoothed = e.smoothed*0.9 + raw*0.1
	}
	return humanETA(time.Duration(e.smoothed * float64(time.Second)))
}

func humanETA(d time.Duration) string {
	switch {
	case d < 45*time.Second:
		return "under a minute left"
	case d < 90*time.Second:
		return "about 1 min left"
	case d < time.Hour:
		return fmt.Sprintf("about %d min left", int(math.Round(d.Minutes())))
	}
	hours := int(d.Hours())
	minutes := int(math.Round(d.Minutes())) - hours*60
	if minutes == 0 {
		return fmt.Sprintf("about %d hr left", hours)
	}
	return fmt.Sprintf("about %d hr %d min left", hours, minutes)
}

func humanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d sec", max(1, int(d.Seconds())))
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(math.Round(d.Minutes())))
	}
	hours := int(d.Hours())
	return fmt.Sprintf("%d hr %d min", hours, int(d.Minutes())-hours*60)
}

type phoneWatch struct {
	mu           sync.Mutex
	lastActivity time.Time
	outstanding  int
	busySince    time.Time
}

func newPhoneWatch() *phoneWatch {
	return &phoneWatch{lastActivity: time.Now()}
}

func (w *phoneWatch) touch() {
	w.mu.Lock()
	w.lastActivity = time.Now()
	w.mu.Unlock()
}

func (w *phoneWatch) begin() func() {
	w.mu.Lock()
	if w.outstanding == 0 {
		w.busySince = time.Now()
	}
	w.outstanding++
	w.mu.Unlock()
	return func() {
		w.mu.Lock()
		w.outstanding--
		w.mu.Unlock()
	}
}

func (w *phoneWatch) quietSince() time.Time {
	if w.busySince.After(w.lastActivity) {
		return w.busySince
	}
	return w.lastActivity
}

func (w *phoneWatch) stalled() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.outstanding > 0 && time.Since(w.quietSince()) > stallThreshold
}

func (w *phoneWatch) repliedSince(t time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastActivity.After(t)
}

func (w *phoneWatch) waitForActivity(ctx context.Context, since time.Time) bool {
	done := w.begin()
	defer done()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		w.mu.Lock()
		active := w.lastActivity.After(since)
		silentFor := time.Since(w.quietSince())
		w.mu.Unlock()
		if active {
			return true
		}
		if silentFor > phonePatienceLimit {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}
