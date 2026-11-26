package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const (
	sweepMaxTokens = 6000
	sweepMaxWords  = 40
	sweepLooks     = 2
	sweepMinConf   = 0.3
)

var visibleWord = regexp.MustCompile(`[\p{L}][\p{L}\p{N}'’&.-]*[\p{L}\p{N}]`)

func (j *chatJob) sweep(ctx context.Context) {
	ranges := chunkRanges(j.doc.Lines, chunkLines, chunkChars)
	if len(ranges) == 0 && j.in.IsGroup {
		ranges = []lineRange{{0, 0}}
	}
	sortSlots(j.slots)
	var wg sync.WaitGroup
	for look := range sweepLooks {
		for _, rg := range ranges {
			wg.Add(1)
			go func() {
				defer wg.Done()
				j.sweepRange(ctx, rg, look)
			}()
		}
	}
	wg.Wait()
}

func (j *chatJob) sweepRange(ctx context.Context, rg lineRange, look int) {
	if errors.Is(j.sweepChunk(ctx, rg, look, rg.end-rg.start >= 2*minSplitLines), errAITruncated) {
		mid := (rg.start + rg.end) / 2
		j.sweepRange(ctx, lineRange{rg.start, mid}, look)
		j.sweepRange(ctx, lineRange{mid, rg.end}, look)
	}
}

func (j *chatJob) sweepChunk(ctx context.Context, rg lineRange, look int, canSplit bool) error {
	j.mu.Lock()
	prompt := fmt.Sprintf("LOOK %d\n", look+1) + j.redactedChunk(rg)
	prompt += wordsToCheck(prompt)
	j.mu.Unlock()
	var reply sweepReply
	err := j.generate(ctx, "sweep", sweepSystemPrompt+j.r.dict.promptText(), prompt, sweepSchema(), sweepMaxTokens, &reply)
	if errors.Is(err, errAITruncated) && canSplit {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.stats.AIChunks++
	if err != nil {
		j.stats.AIFailedChunks++
		logRedact.Warnf("Chat %q lines %d-%d: final sweep failed: %v", j.in.Folder, rg.start+1, rg.end, err)
		return err
	}
	for _, leak := range reply.Leaks {
		leak.Confidence = clamp(leak.Confidence * 0.9)
		if j.addFound(leak, rg, "ai-sweep", sweepMinConf) {
			j.stats.LeaksCaught++
		}
	}
	return nil
}

func (j *chatJob) redactedChunk(rg lineRange) string {
	var b strings.Builder
	b.WriteString(j.chatKind() + "\n" + j.castTable() + "\nREDACTED CHUNK\n")
	if rg.start == 0 && j.in.IsGroup {
		fmt.Fprintf(&b, "#0 (group name): %s\n", j.previewLine(-1, j.titleLabel))
	}
	for i := rg.start; i < rg.end; i++ {
		l := j.doc.Lines[i]
		sender := "(system)"
		if l.HasSender {
			sender = firstNonEmpty(j.senderTagFor(l.Sender), j.senderDisplay(l))
			if j.in.IsGroup && l.Sender == j.titleLabel {
				sender = "(group)"
			}
		}
		fmt.Fprintf(&b, "#%d %s: %s\n", i+1, sender, j.previewLine(i, l.Body))
	}
	return b.String()
}

func wordsToCheck(chunk string) string {
	_, body, found := strings.Cut(chunk, "\nREDACTED CHUNK\n")
	if !found {
		return ""
	}
	counts := map[string]int{}
	for _, line := range strings.Split(markerPattern.ReplaceAllString(body, " "), "\n") {
		if _, text, ok := strings.Cut(line, ": "); ok {
			line = text
		}
		for _, w := range visibleWord.FindAllString(line, -1) {
			if candidateWord(w) {
				counts[w]++
			}
		}
	}
	words := make([]string, 0, len(counts))
	for w := range counts {
		words = append(words, w)
	}
	sort.Slice(words, func(a, b int) bool {
		if counts[words[a]] != counts[words[b]] {
			return counts[words[a]] > counts[words[b]]
		}
		return words[a] < words[b]
	})
	if len(words) == 0 {
		return ""
	}
	words = words[:min(len(words), sweepMaxWords)]
	return "\nCAPITALISED WORDS STILL VISIBLE (judge each one; report it on every line where it is a person's name, an organisation tied to the people or a home city, and ignore the rest)\n" + strings.Join(words, ", ") + "\n"
}

func candidateWord(w string) bool {
	first, _ := utf8.DecodeRuneInString(w)
	if !unicode.IsUpper(first) || utf8.RuneCountInString(w) < 3 || isCommonWord(w) {
		return false
	}
	return strings.ToUpper(w) != w || utf8.RuneCountInString(w) > 4
}
