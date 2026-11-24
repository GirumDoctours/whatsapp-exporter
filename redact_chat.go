package main

import (
	"regexp"
	"sort"
	"strings"
)

var chatStampPattern = regexp.MustCompile(`^\[\d{2}/\d{2}/\d{4}, \d{2}:\d{2}:\d{2}\] `)

type chatLine struct {
	Stamp     string
	Sender    string
	HasSender bool
	Body      string
}

type chatDoc struct {
	Lines    []chatLine
	Newline  string
	Trailing bool
	Preamble string
}

func parseChat(text string, knownSenders []string) chatDoc {
	doc := chatDoc{Newline: "\n"}
	if strings.Contains(text, "\r\n") {
		doc.Newline = "\r\n"
	}
	if strings.HasSuffix(text, doc.Newline) {
		doc.Trailing = true
		text = strings.TrimSuffix(text, doc.Newline)
	}
	senders := append([]string(nil), knownSenders...)
	sort.Slice(senders, func(i, j int) bool { return len(senders[i]) > len(senders[j]) })
	var preamble []string
	for _, physical := range strings.Split(text, doc.Newline) {
		stamp := chatStampPattern.FindString(physical)
		if stamp == "" {
			if len(doc.Lines) == 0 {
				preamble = append(preamble, physical)
				continue
			}
			last := &doc.Lines[len(doc.Lines)-1]
			last.Body += "\n" + physical
			continue
		}
		doc.Lines = append(doc.Lines, splitSender(stamp, physical[len(stamp):], senders))
	}
	doc.Preamble = strings.Join(preamble, doc.Newline)
	return doc
}

func splitSender(stamp, rest string, senders []string) chatLine {
	for _, s := range senders {
		if s != "" && strings.HasPrefix(rest, s+": ") {
			return chatLine{Stamp: stamp, Sender: s, HasSender: true, Body: rest[len(s)+2:]}
		}
	}
	if cut := strings.Index(rest, ": "); cut >= 0 {
		return chatLine{Stamp: stamp, Sender: rest[:cut], HasSender: true, Body: rest[cut+2:]}
	}
	return chatLine{Stamp: stamp, Body: rest}
}

func (l chatLine) text() string {
	if !l.HasSender {
		return l.Stamp + l.Body
	}
	return l.Stamp + l.Sender + ": " + l.Body
}

func (d chatDoc) render() string {
	var parts []string
	if d.Preamble != "" || (len(d.Lines) == 0 && d.Trailing) {
		parts = append(parts, d.Preamble)
	}
	for _, l := range d.Lines {
		parts = append(parts, strings.ReplaceAll(l.text(), "\n", d.Newline))
	}
	out := strings.Join(parts, d.Newline)
	if d.Trailing {
		out += d.Newline
	}
	return out
}

type lineRange struct {
	start, end int
}

func chunkRanges(lines []chatLine, maxLines, maxChars int) []lineRange {
	var out []lineRange
	start, chars := 0, 0
	for i, l := range lines {
		size := len(l.Sender) + len(l.Body) + 8
		if i > start && (i-start >= maxLines || chars+size > maxChars) {
			out = append(out, lineRange{start, i})
			start, chars = i, 0
		}
		chars += size
	}
	if start < len(lines) {
		out = append(out, lineRange{start, len(lines)})
	}
	return out
}

func (d chatDoc) senders() []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range d.Lines {
		if l.HasSender && !seen[l.Sender] {
			seen[l.Sender] = true
			out = append(out, l.Sender)
		}
	}
	return out
}
