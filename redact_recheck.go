package main

import (
	"context"
	_ "embed"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

//go:embed prompts/recheck.txt
var recheckSystemPrompt string

const (
	recheckBelow  = 0.75
	recheckBatch  = 80
	recheckTokens = 8000
)

type recheckDecision struct {
	ID         string  `json:"id"`
	Personal   bool    `json:"personal"`
	Confidence float64 `json:"confidence"`
}

type recheckReply struct {
	Decisions []recheckDecision `json:"decisions"`
}

func recheckSchema() map[string]any {
	return objectSchema(map[string]any{
		"decisions": arrayOf(objectSchema(map[string]any{
			"id":         schemaString,
			"personal":   schemaBool,
			"confidence": schemaNumber,
		})),
	})
}

func addedByFinalStep(s slot) bool {
	return strings.HasPrefix(s.Source, finalSource) && s.Confidence < recheckBelow
}

func (j *chatJob) recheckPrompt(batch []int) string {
	var b strings.Builder
	b.WriteString(j.chatKind() + "\nITEMS\n")
	for _, idx := range batch {
		s := j.slots[idx]
		l := j.doc.Lines[s.Line]
		marked := j.previewLine(s.Line, l.Body[:s.Start]) + "«" + s.Value + "»" + j.previewLineFrom(s.Line, l.Body, s.End)
		fmt.Fprintf(&b, "R%d: tagged %s. Line #%d %s: %s\n", idx, s.Tag, s.Line+1, j.senderDisplay(l), around200(marked))
	}
	return b.String()
}

func (j *chatJob) notPersonal(ctx context.Context, prompt string) []int {
	var reply recheckReply
	if err := j.generate(ctx, "recheck", recheckSystemPrompt, prompt, recheckSchema(), recheckTokens, &reply); err != nil {
		logRedact.Warnf("Chat %q: second-opinion check failed, keeping every item: %v", j.in.Folder, err)
		return nil
	}
	var rejected []int
	for _, d := range reply.Decisions {
		if idx, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(d.ID), "R")); err == nil && !d.Personal {
			rejected = append(rejected, idx)
		}
	}
	return rejected
}

func (j *chatJob) recheck(ctx context.Context, pick func(slot) bool) {
	if !j.r.aiAvailable() {
		return
	}
	var targets []int
	for i, s := range j.slots {
		if s.Line >= 0 && pick(s) {
			targets = append(targets, i)
		}
	}
	if len(targets) == 0 {
		return
	}
	asked := map[int]bool{}
	var prompts []string
	for start := 0; start < len(targets); start += recheckBatch {
		batch := targets[start:min(start+recheckBatch, len(targets))]
		for _, idx := range batch {
			asked[idx] = true
		}
		prompts = append(prompts, j.recheckPrompt(batch))
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	drop := map[int]bool{}
	for _, prompt := range prompts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rejected := j.notPersonal(ctx, prompt)
			mu.Lock()
			defer mu.Unlock()
			for _, idx := range rejected {
				if asked[idx] {
					drop[idx] = true
				}
			}
		}()
	}
	wg.Wait()
	if len(drop) == 0 {
		return
	}
	var kept []slot
	for i, s := range j.slots {
		if !drop[i] {
			kept = append(kept, s)
		}
	}
	j.slots = kept
	logRedact.Debugf("Chat %q: second opinion removed %d of %d less certain items", j.in.Folder, len(drop), len(targets))
}
