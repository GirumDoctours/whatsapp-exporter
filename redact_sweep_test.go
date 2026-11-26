package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

type scriptedReplies struct {
	mu      sync.Mutex
	prompts []string
	reply   func(req aiRequest) (any, error)
}

func (s *scriptedReplies) generate(_ context.Context, req aiRequest, out any) error {
	s.mu.Lock()
	s.prompts = append(s.prompts, req.Prompt)
	s.mu.Unlock()
	reply, err := s.reply(req)
	if err != nil {
		return err
	}
	data, err := json.Marshal(reply)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

const sweepChat = "[01/02/2026, 10:00:00] Lena Hartwell: I work at Brightwell Studio with Corin\r\n" +
	"[01/02/2026, 10:01:00] Halcyon Team: Great, see you Monday\r\n"

func slotAt(j *chatJob, line int, value, tag, source string) slot {
	start := strings.Index(j.doc.Lines[line].Body, value)
	return slot{Line: line, Start: start, End: start + len(value), Tag: tag, Value: value, Source: source, Confidence: 0.6}
}

func newSweepJob(ai aiClient) *chatJob {
	r := newRedactor(ai, nil, dictionary, sampleBook())
	j := &chatJob{r: r, in: chatInput{Folder: "Lena Hartwell", Title: "Lena Hartwell", Text: sweepChat}, roles: map[string]string{}}
	j.doc = parseChat(sweepChat, []string{"Lena Hartwell", "Halcyon Team"})
	j.indexPeople()
	j.fillMissingRoles()
	return j
}

func TestSweepAddsWhatTheOtherPassesMissed(t *testing.T) {
	ai := &scriptedReplies{reply: func(req aiRequest) (any, error) {
		return sweepReply{Leaks: []foundTag{
			{Line: 1, Text: "Brightwell Studio", Tag: "COMPANY_NAME", Confidence: 0.9},
			{Line: 1, Text: "Corin", Tag: "PERSON_FIRST_NAME", Confidence: 0.3},
		}}, nil
	}}
	j := newSweepJob(ai)
	j.sweep(context.Background())
	added := map[string]bool{}
	for _, s := range j.slots {
		added[s.Value] = s.Source == "ai-sweep"
	}
	if !added["Brightwell Studio"] {
		t.Errorf("a confident find should be hidden: %+v", j.slots)
	}
	if added["Corin"] {
		t.Error("a find below the sweep's bar once scaled should be left out")
	}
	if len(ai.prompts) != 2*len(chunkRanges(j.doc.Lines, chunkLines, chunkChars)) {
		t.Errorf("each chunk should get two looks, made %d calls", len(ai.prompts))
	}
	if !strings.HasPrefix(ai.prompts[0], "LOOK ") || ai.prompts[0] == ai.prompts[1] {
		t.Error("the two looks must differ so the second isn't served from the cache")
	}
}

func TestSweepFailureKeepsEverything(t *testing.T) {
	ai := &scriptedReplies{reply: func(req aiRequest) (any, error) { return nil, errors.New("offline") }}
	j := newSweepJob(ai)
	j.slots = []slot{slotAt(j, 0, "Brightwell Studio", "{{COMPANY_NAME}}", "ai")}
	j.sweep(context.Background())
	if len(j.slots) != 1 || j.stats.AIFailedChunks == 0 {
		t.Errorf("slots %+v, stats %+v", j.slots, j.stats)
	}
}

func TestWordsToCheck(t *testing.T) {
	chunk := "kind\ncast\nREDACTED CHUNK\n#1 {{PATIENT_FULL_NAME}}: I work at Brightwell with Corin and The team\n#2 (system): UNICEF and NASA said Monday OK"
	got := wordsToCheck(chunk)
	for _, want := range []string{"Brightwell", "Corin", "UNICEF"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing from %q", want, got)
		}
	}
	for _, unwanted := range []string{"PATIENT", "The", "Monday", "OK", "work", "NASA"} {
		if strings.Contains(strings.SplitN(got, "\n", 3)[2], unwanted) {
			t.Errorf("%q shouldn't be listed: %q", unwanted, got)
		}
	}
	if wordsToCheck("kind\ncast\nREDACTED CHUNK\n#1 x: all lowercase here") != "" {
		t.Error("nothing capitalised means no list")
	}
}

func TestSecondOpinionRemovesOnlyWhatItRejects(t *testing.T) {
	ai := &scriptedReplies{reply: func(req aiRequest) (any, error) {
		return recheckReply{Decisions: []recheckDecision{
			{ID: "R0", Personal: false, Confidence: 0.9},
			{ID: "R1", Personal: true, Confidence: 0.9},
			{ID: "R2", Personal: false, Confidence: 0.9},
		}}, nil
	}}
	j := newSweepJob(ai)
	j.slots = []slot{
		slotAt(j, 0, "Brightwell", "{{COMPANY_NAME}}", finalSource+"structure"),
		slotAt(j, 0, "Corin", "{{PERSON_FIRST_NAME}}", finalSource+"cues"),
		slotAt(j, 1, "Monday", "{{PERSON_FIRST_NAME}}", "ai"),
	}
	j.recheck(context.Background(), addedByFinalStep)
	var kept []string
	for _, s := range j.slots {
		kept = append(kept, s.Value)
	}
	if strings.Join(kept, ",") != "Corin,Monday" {
		t.Errorf("kept %v", kept)
	}
	if len(ai.prompts) != 1 || strings.Contains(ai.prompts[0], "«Monday»") {
		t.Errorf("only items the final step added should be asked about: %q", ai.prompts)
	}
}

func TestSecondOpinionKeepsEverythingWhenItFails(t *testing.T) {
	ai := &scriptedReplies{reply: func(req aiRequest) (any, error) { return nil, errors.New("offline") }}
	j := newSweepJob(ai)
	j.slots = []slot{slotAt(j, 0, "Brightwell", "{{COMPANY_NAME}}", finalSource+"structure")}
	j.recheck(context.Background(), addedByFinalStep)
	if len(j.slots) != 1 {
		t.Errorf("a failed check must keep the item: %+v", j.slots)
	}
	if addedByFinalStep(slot{Source: finalSource + "x", Confidence: recheckBelow}) || addedByFinalStep(slot{Source: "ai", Confidence: 0.1}) {
		t.Error("only less certain final-step additions are rechecked")
	}
}
