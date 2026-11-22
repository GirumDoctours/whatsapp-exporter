package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
)

const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiGreen  = "\033[38;5;41m"
	ansiTeal   = "\033[38;5;30m"
	ansiYellow = "\033[38;5;214m"
	ansiRed    = "\033[38;5;203m"
)

type terminal struct {
	mu          sync.Mutex
	in          *bufio.Reader
	out         io.Writer
	interactive bool
	color       bool
	liveLines   int
}

type menuOption struct {
	label string
	hint  string
}

type keyPress int

const (
	keyNone keyPress = iota
	keyUp
	keyDown
	keyChoose
	keyInterrupt
	keyEOF
)

func newTerminal() *terminal {
	enableVirtualTerminal()
	interactive := term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
	return &terminal{
		in:          bufio.NewReader(os.Stdin),
		out:         os.Stdout,
		interactive: interactive,
		color:       interactive && os.Getenv("NO_COLOR") == "",
	}
}

func (t *terminal) paint(style, text string) string {
	if !t.color || text == "" {
		return text
	}
	return style + text + ansiReset
}

func (t *terminal) width() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w >= 20 {
		return w
	}
	return 80
}

func (t *terminal) control(sequence string) {
	if !t.interactive {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	fmt.Fprint(t.out, sequence)
}

func (t *terminal) setTitle(title string) { t.control("\033]0;" + title + "\007") }
func (t *terminal) hideCursor()           { t.control("\033[?25l") }
func (t *terminal) showCursor()           { t.control("\033[?25h") }

func (t *terminal) ensureHeight(rows, cols int) {
	if !t.interactive {
		return
	}
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || (h >= rows && w >= cols) {
		return
	}
	t.control(fmt.Sprintf("\033[8;%d;%dt", max(h, rows), max(w, cols)))
}

func (t *terminal) clearScreen() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.liveLines = 0
	if t.interactive {
		fmt.Fprint(t.out, "\033[H\033[2J\033[3J")
	}
}

func (t *terminal) clearLiveLocked() {
	if t.liveLines == 0 {
		return
	}
	fmt.Fprint(t.out, "\r")
	for i := 1; i < t.liveLines; i++ {
		fmt.Fprint(t.out, "\033[1A")
	}
	fmt.Fprint(t.out, "\033[J")
	t.liveLines = 0
}

func (t *terminal) say(format string, args ...any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.clearLiveLocked()
	fmt.Fprintf(t.out, format+"\r\n", args...)
}

func (t *terminal) live(lines ...string) {
	if !t.interactive {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.clearLiveLocked()
	limit := t.width() - 2
	for i, line := range lines {
		if i > 0 {
			fmt.Fprint(t.out, "\r\n")
		}
		fmt.Fprint(t.out, truncateVisible(line, limit))
	}
	t.liveLines = len(lines)
}

func (t *terminal) draw(paint func(w io.Writer)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.clearLiveLocked()
	paint(t.out)
}

func truncateVisible(s string, limit int) string {
	var b strings.Builder
	visible := 0
	truncated := false
	for i := 0; i < len(s); {
		if s[i] == '\033' {
			end := i + 1
			for end < len(s) && !(s[end] >= '@' && s[end] <= '~' && end > i+1) {
				end++
			}
			if end < len(s) {
				end++
			}
			b.WriteString(s[i:end])
			i = end
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if visible >= limit {
			truncated = true
			i += size
			continue
		}
		b.WriteRune(r)
		visible++
		i += size
	}
	if truncated && strings.Contains(s, "\033") {
		b.WriteString(ansiReset)
	}
	return b.String()
}

func (t *terminal) ask(prompt string) string {
	t.mu.Lock()
	t.clearLiveLocked()
	fmt.Fprint(t.out, prompt)
	t.mu.Unlock()
	t.showCursor()
	line, _ := t.in.ReadString('\n')
	return strings.TrimSpace(line)
}

func (t *terminal) waitForEnter(message string) {
	t.ask("  " + t.paint(ansiDim, message) + " ")
}

func (t *terminal) countdown(seconds int, message func(left int) string) {
	if !t.interactive {
		return
	}
	pressed := make(chan struct{})
	go func() {
		_, _ = t.in.ReadString('\n')
		close(pressed)
	}()
	t.hideCursor()
	defer t.showCursor()
	for left := seconds; left > 0; left-- {
		t.live("  " + t.paint(ansiDim, message(left)))
		select {
		case <-pressed:
			t.live()
			return
		case <-time.After(time.Second):
		}
	}
	t.live()
}

func (t *terminal) readKey() keyPress {
	b, err := t.in.ReadByte()
	if err != nil {
		return keyEOF
	}
	switch b {
	case 3:
		return keyInterrupt
	case '\r', '\n', ' ':
		return keyChoose
	case 'k', 'K', 'w', 'W':
		return keyUp
	case 'j', 'J', 's', 'S':
		return keyDown
	case 27:
		next, err := t.in.ReadByte()
		if err != nil || (next != '[' && next != 'O') {
			return keyNone
		}
		code, err := t.in.ReadByte()
		if err != nil {
			return keyNone
		}
		switch code {
		case 'A':
			return keyUp
		case 'B':
			return keyDown
		}
	}
	return keyNone
}

func (t *terminal) choose(question string, options []menuOption) int {
	if !t.interactive {
		return t.chooseByNumber(question, options)
	}
	fd := int(os.Stdin.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return t.chooseByNumber(question, options)
	}
	t.hideCursor()
	restore := func() {
		_ = term.Restore(fd, state)
		t.showCursor()
	}

	selected := 0
	render := func() []string {
		lines := []string{"", "  " + t.paint(ansiBold, question), ""}
		for i, option := range options {
			if i == selected {
				line := "  " + t.paint(ansiGreen+ansiBold, "❯ "+option.label)
				if option.hint != "" {
					line += "  " + t.paint(ansiDim, option.hint)
				}
				lines = append(lines, line)
			} else {
				lines = append(lines, "    "+option.label)
			}
		}
		return append(lines, "", "  "+t.paint(ansiDim, "Use ↑ ↓ to move, then press Enter or Space to choose"))
	}
	for {
		t.live(render()...)
		switch t.readKey() {
		case keyUp:
			selected = (selected - 1 + len(options)) % len(options)
		case keyDown:
			selected = (selected + 1) % len(options)
		case keyChoose, keyEOF:
			lines := []string{"", "  " + t.paint(ansiBold, question), "  " + t.paint(ansiGreen, "✔ "+options[selected].label)}
			t.live(lines...)
			t.mu.Lock()
			t.liveLines = 0
			fmt.Fprint(t.out, "\r\n")
			t.mu.Unlock()
			restore()
			return selected
		case keyInterrupt:
			t.live()
			restore()
			t.say("")
			t.say("  Cancelled.")
			os.Exit(130)
		}
	}
}

func (t *terminal) chooseByNumber(question string, options []menuOption) int {
	t.say("")
	t.say("%s", question)
	for i, option := range options {
		t.say("  %d) %s", i+1, option.label)
	}
	for {
		answer := t.ask(fmt.Sprintf("Type a number (1-%d) and press Enter [1]: ", len(options)))
		if answer == "" {
			return 0
		}
		n, err := strconv.Atoi(answer)
		if err == nil && n >= 1 && n <= len(options) {
			return n - 1
		}
		t.say("Please type a number between 1 and %d.", len(options))
	}
}

var logoLines = []string{
	"     ▄▄▄██████▄▄▄     ",
	"   ▄██▀▀      ▀▀██▄   ",
	"  ██▀     ██     ▀██  ",
	"  ██      ██      ██  ",
	"  ██   ▀▄▄██▄▄▀   ██  ",
	"  ██▄     ▀▀     ▄██  ",
	"  ▄██▄▄        ▄▄██▀  ",
	" ▀▀▀  ▀▀██████▀▀      ",
}

func (t *terminal) banner() {
	text := []string{
		"",
		"",
		t.paint(ansiGreen+ansiBold, "WhatsApp Exporter"),
		"",
		"Every chat, message and photo,",
		"saved to one ZIP on this computer.",
		"",
		t.paint(ansiDim, "Nothing is uploaded anywhere."),
	}
	t.draw(func(w io.Writer) {
		fmt.Fprint(w, "\r\n")
		for i, line := range logoLines {
			fmt.Fprint(w, "  "+t.paint(ansiGreen, line)+"   "+text[i]+"\r\n")
		}
		fmt.Fprint(w, "\r\n")
	})
}

func (t *terminal) box(rows [][2]string) {
	labelWidth, valueWidth := 0, 0
	for _, row := range rows {
		labelWidth = max(labelWidth, utf8.RuneCountInString(row[0]))
		valueWidth = max(valueWidth, utf8.RuneCountInString(row[1]))
	}
	inner := labelWidth + valueWidth + 7
	border := t.paint(ansiTeal, "  ╭"+strings.Repeat("─", inner)+"╮")
	bottom := t.paint(ansiTeal, "  ╰"+strings.Repeat("─", inner)+"╯")
	side := t.paint(ansiTeal, "│")
	t.say("%s", border)
	for _, row := range rows {
		label := row[0] + strings.Repeat(" ", labelWidth-utf8.RuneCountInString(row[0]))
		value := row[1] + strings.Repeat(" ", valueWidth-utf8.RuneCountInString(row[1]))
		t.say("  %s  %s   %s  %s", side, t.paint(ansiDim, label), t.paint(ansiBold, value), side)
	}
	t.say("%s", bottom)
}

func formatCount(n int) string {
	if n < 0 {
		return "-" + formatCount(-n)
	}
	digits := strconv.Itoa(n)
	var b strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(d)
	}
	return b.String()
}
