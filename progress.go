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
	progressTick       = 100 * time.Millisecond
	stallThreshold     = 20 * time.Second
	phonePatienceLimit = 15 * time.Minute
	labelWidth         = 22
)

const (
	stallHeadline = "  Paused: waiting for your phone"
	stallAdvice   = "   Open WhatsApp on your phone and keep the screen unlocked to continue."
	hintWidth     = 22
)

type progressView struct {
	t         *terminal
	extra     func() string
	label     string
	started   time.Time
	frame     int
	eta       etaEstimator
	smooth    smoother
	heartbeat heartbeat
	now       func() time.Time
}

func newProgressView(t *terminal, label string) *progressView {
	now := time.Now()
	return &progressView{
		t:         t,
		label:     label,
		started:   now,
		eta:       etaEstimator{start: now, last: now},
		smooth:    newSmoother(now),
		heartbeat: heartbeat{lastChange: now},
		now:       time.Now,
	}
}

func (p *progressView) update(r progressReport) {
	p.frame++
	now := p.now()
	target := math.Max(0, math.Min(1, r.fraction))
	shown := p.smooth.advance(target, r.stalled, now)
	p.heartbeat.observe(target, r.status, now)
	p.eta.observe(r.stalled)

	width := p.t.width()
	barWidth := max(10, min(40, width-labelWidth-hintWidth-17))
	label := p.label + strings.Repeat(" ", max(1, labelWidth-len([]rune(p.label))))
	var bar, percent, hint string
	if r.known {
		bar = p.bar(shown, barWidth)
		percent = fmt.Sprintf("%3d%%", int(shown*100))
		hint = p.eta.text(p.smooth.target, r.stalled)
	} else {
		bar = p.bounce(barWidth)
		percent = "    "
		hint = "starting…"
		if r.stalled {
			hint = "paused"
		}
	}
	dot := p.dot(p.heartbeat.state(now, r.known, r.stalled))
	lines := []string{"  " + p.t.paint(ansiBold, label) + bar + "  " + percent + "  " + dot + "  " + p.t.paint(ansiDim, hint)}
	indent := "  " + strings.Repeat(" ", labelWidth)
	if r.status != "" {
		lines = append(lines, indent+p.t.paint(ansiDim, r.status))
	}
	if p.extra != nil {
		if status := p.extra(); status != "" {
			lines = append(lines, indent+p.t.paint(ansiDim, status))
		}
	}
	if r.stalled {
		lines = append(lines, "", "  "+p.t.paint(ansiYellow+ansiBold, glyphs.warn+stallHeadline), "  "+p.t.paint(ansiYellow, stallAdvice))
	}
	p.t.live(lines...)
}

func (p *progressView) bar(fraction float64, width int) string {
	cells := fraction * float64(width)
	filled := int(cells)
	partial := ""
	if steps := len(glyphs.barEighths); steps > 0 && filled < width {
		partial = glyphs.barEighths[int((cells-float64(filled))*float64(steps))]
	} else if steps == 0 {
		filled = int(math.Round(cells))
	}
	empty := width - filled
	if partial != "" {
		empty--
	}
	return p.t.paint(ansiGreen, strings.Repeat("█", filled)+partial) + p.t.paint(ansiDim, strings.Repeat("░", empty))
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

func (p *progressView) dot(state pulse) string {
	switch state {
	case pulseOn:
		return p.t.paint(ansiGreen+ansiBold, glyphs.heartbeat)
	case pulseQuiet:
		return p.t.paint(ansiGreen+ansiDim, glyphs.heartbeat)
	case pulseStalled:
		return p.t.paint(ansiYellow, glyphs.heartbeat)
	}
	return " "
}

func (p *progressView) finish(note string) {
	elapsed := "done in " + humanDuration(time.Since(p.started))
	line := "  " + p.t.paint(ansiGreen, glyphs.check+" ") + p.t.paint(ansiBold, p.label) + "  " + p.t.paint(ansiDim, firstNonEmpty(note, elapsed))
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

const (
	easeTime      = 350 * time.Millisecond
	minCreepTime  = 4 * time.Second
	creepShare    = 0.8
	maxCreepStep  = 0.08
	firstStepHint = 0.03
)

type smoother struct {
	shown        float64
	target       float64
	step         float64
	interval     time.Duration
	lastFrame    time.Time
	lastTargetAt time.Time
}

func newSmoother(now time.Time) smoother {
	return smoother{step: firstStepHint, interval: minCreepTime, lastFrame: now, lastTargetAt: now}
}

func (s *smoother) advance(target float64, hold bool, now time.Time) float64 {
	if target > s.target {
		s.learn(target-s.target, now.Sub(s.lastTargetAt))
		s.target = target
		s.lastTargetAt = now
	}
	dt := now.Sub(s.lastFrame).Seconds()
	s.lastFrame = now
	if dt <= 0 {
		return s.shown
	}
	if s.shown < s.target {
		s.shown += (s.target - s.shown) * (1 - math.Exp(-dt/easeTime.Seconds()))
		if s.target-s.shown < 0.0005 {
			s.shown = s.target
		}
		return s.shown
	}
	if ceiling := s.ceiling(); !hold && s.shown < ceiling {
		s.shown += (ceiling - s.shown) * (1 - math.Exp(-dt/s.interval.Seconds()))
	}
	return s.shown
}

func (s *smoother) learn(step float64, gap time.Duration) {
	s.step = s.step*0.7 + math.Min(step, maxCreepStep)*0.3
	s.interval = max(minCreepTime, time.Duration(float64(s.interval)*0.7+float64(gap)*0.3))
}

func (s *smoother) ceiling() float64 {
	if s.target >= 1 {
		return 1
	}
	return math.Min(s.target+s.step*creepShare, 0.995)
}

type pulse int

const (
	pulseOff pulse = iota
	pulseOn
	pulseQuiet
	pulseStalled
)

const (
	blinkPeriod = 600 * time.Millisecond
	quietAfter  = 5 * time.Second
)

type heartbeat struct {
	target     float64
	status     string
	lastChange time.Time
}

func (h *heartbeat) observe(target float64, status string, now time.Time) {
	if target != h.target || status != h.status {
		h.target = target
		h.status = status
		h.lastChange = now
	}
}

func (h *heartbeat) state(now time.Time, known, stalled bool) pulse {
	switch {
	case stalled:
		return pulseStalled
	case known && now.Sub(h.lastChange) > quietAfter:
		return pulseQuiet
	case (now.UnixMilli()/blinkPeriod.Milliseconds())%2 == 0:
		return pulseOn
	}
	return pulseOff
}
