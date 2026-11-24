package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const (
	goldPath       = ".eval/gold.json"
	goldModel      = "claude-opus-5-5"
	goldPerChat    = 30
	goldRunsDir    = ".eval/runs"
	bootstrapDraws = 5000
)

type goldSpan struct {
	Text string `json:"text"`
	Kind string `json:"kind"`
}

type goldLine struct {
	Chat  string     `json:"chat"`
	Line  int        `json:"line"`
	Spans []goldSpan `json:"spans"`
}

type goldSet struct {
	Lines []goldLine `json:"lines"`
}

const goldSystem = `You build an answer key for a PII redactor of WhatsApp chats. For every numbered line, list every personal or identifying detail exactly as written, character for character: people's names, nicknames and initials (a full name as one item), phone numbers, emails, social handles, private links, home or street addresses, postcodes, cities or towns where a person lives, ages, birthdays, ID, passport, booking or invoice numbers, bank or card details, names of clinics, hospitals, companies, shops, groups or events tied to the people, and dates of personal events such as surgeries or arrivals.

Do not list: ordinary words, job titles, greetings or religious phrases, the fixed wording of WhatsApp notices, public brands or famous places mentioned generically, prices, timestamps, or medical details without an identifier. Sender names before the colon are part of the line; include them too.

Lines marked CONTEXT are only for understanding; label only the lines marked LABEL. Return an entry for every LABEL line, with an empty list when it has nothing personal.`

func goldSchema() map[string]any {
	return objectSchema(map[string]any{
		"lines": arrayOf(objectSchema(map[string]any{
			"line": schemaInteger,
			"spans": arrayOf(objectSchema(map[string]any{
				"text": schemaString,
				"kind": map[string]any{"type": "string", "enum": []any{"name", "organisation", "phone", "email", "handle", "link", "address", "city", "age", "date", "id", "bank", "other"}},
			})),
		})),
	})
}

func sampleStart(n int, seed uint64) int {
	if n <= goldPerChat {
		return 0
	}
	return rand.New(rand.NewPCG(seed, 7)).IntN(n - goldPerChat)
}

func TestBuildGold(t *testing.T) {
	loadDotEnv(t)
	if os.Getenv("EXPORTER_BUILD_GOLD") != "1" {
		t.Skip("set EXPORTER_BUILD_GOLD=1 to label the answer key with Opus")
	}
	opus := newModelClient(os.Getenv("ANTHROPIC_API_KEY"), goldModel)
	cache := newAICache(".eval/cache")
	chats := loadEvalSet(t, ".eval/train")
	var gold goldSet
	var mu sync.Mutex
	var wg sync.WaitGroup
	for ci, c := range chats {
		wg.Add(1)
		go func() {
			defer wg.Done()
			doc := parseChat(c.input.Text, nil)
			start := sampleStart(len(doc.Lines), uint64(ci+1))
			end := min(start+goldPerChat, len(doc.Lines))
			var b strings.Builder
			for i := max(0, start-8); i < min(len(doc.Lines), end+3); i++ {
				mark := "CONTEXT"
				if i >= start && i < end {
					mark = "LABEL"
				}
				fmt.Fprintf(&b, "#%d %s %s\n", i+1, mark, doc.Lines[i].text())
			}
			var reply struct {
				Lines []struct {
					Line  int        `json:"line"`
					Spans []goldSpan `json:"spans"`
				} `json:"lines"`
			}
			err := cachedGenerate(context.Background(), opus, cache, cacheKey("gold-v1", goldSystem, b.String()), aiRequest{System: goldSystem, Prompt: b.String(), Schema: goldSchema(), MaxTokens: 16000, Effort: "high"}, &reply)
			if err != nil {
				t.Errorf("labelling failed: %v", err)
				return
			}
			got := map[int][]goldSpan{}
			for _, l := range reply.Lines {
				got[l.Line-1] = l.Spans
			}
			mu.Lock()
			defer mu.Unlock()
			for i := start; i < end; i++ {
				var kept []goldSpan
				for _, s := range got[i] {
					if strings.TrimSpace(s.Text) != "" && strings.Contains(doc.Lines[i].text(), s.Text) {
						kept = append(kept, s)
					}
				}
				gold.Lines = append(gold.Lines, goldLine{Chat: c.input.Folder, Line: i, Spans: kept})
			}
		}()
	}
	wg.Wait()
	sort.Slice(gold.Lines, func(i, j int) bool {
		if gold.Lines[i].Chat != gold.Lines[j].Chat {
			return gold.Lines[i].Chat < gold.Lines[j].Chat
		}
		return gold.Lines[i].Line < gold.Lines[j].Line
	})
	data, _ := json.MarshalIndent(gold, "", " ")
	if err := os.WriteFile(goldPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	spans := 0
	kinds := map[string]int{}
	for _, l := range gold.Lines {
		spans += len(l.Spans)
		for _, s := range l.Spans {
			kinds[s.Kind]++
		}
	}
	t.Logf("answer key: %d lines, %d details, by kind %v", len(gold.Lines), spans, kinds)
}

type chunkScore struct {
	Chat     string `json:"chat"`
	Gold     int    `json:"gold"`
	Caught   int    `json:"caught"`
	Extra    int    `json:"extra"`
	LeakFree bool   `json:"leak_free"`
}

type runScore struct {
	Label     string         `json:"label"`
	Recall    float64        `json:"recall"`
	Precision float64        `json:"precision"`
	LeakFree  float64        `json:"leak_free_chunks"`
	ByKind    map[string]int `json:"missed_by_kind"`
	Chunks    []chunkScore   `json:"chunks"`
}

func spanCovered(redactedLine, text, kind string) bool {
	if strings.Contains(redactedLine, text) {
		return false
	}
	switch kind {
	case "phone", "email", "link", "handle", "id", "bank", "date", "age":
		return true
	}
	visible := map[string]bool{}
	for _, tok := range wordToken.FindAllString(redactedLine, -1) {
		visible[tok] = true
	}
	for _, tok := range wordToken.FindAllString(text, -1) {
		if len([]rune(tok)) >= 3 && !isCommonWord(tok) && visible[tok] {
			return false
		}
	}
	return true
}

func scoreRun(label string, gold goldSet, outputs map[string]chatOutput) runScore {
	rs := runScore{Label: label, ByKind: map[string]int{}}
	byChat := map[string][]goldLine{}
	for _, l := range gold.Lines {
		byChat[l.Chat] = append(byChat[l.Chat], l)
	}
	totalGold, caught, slots, goodSlots := 0, 0, 0, 0
	for _, chat := range sortedKeys(byChat) {
		out := outputs[chat]
		cs := chunkScore{Chat: chat, LeakFree: true}
		for _, gl := range byChat[chat] {
			if gl.Line >= len(out.Lines) {
				continue
			}
			red := out.Lines[gl.Line].text()
			for _, s := range gl.Spans {
				if len([]rune(strings.TrimSpace(s.Text))) < 2 {
					continue
				}
				cs.Gold++
				if spanCovered(red, s.Text, s.Kind) {
					cs.Caught++
				} else {
					cs.LeakFree = false
					rs.ByKind[s.Kind]++
				}
			}
			for _, sl := range out.Slots {
				if sl.Line != gl.Line {
					continue
				}
				slots++
				matched := false
				for _, s := range gl.Spans {
					a, b := strings.ToLower(sl.Value), strings.ToLower(s.Text)
					if strings.Contains(a, b) || strings.Contains(b, a) {
						matched = true
						break
					}
				}
				if matched {
					goodSlots++
				} else {
					cs.Extra++
				}
			}
		}
		totalGold += cs.Gold
		caught += cs.Caught
		rs.Chunks = append(rs.Chunks, cs)
	}
	rs.Recall = float64(caught) / float64(max(totalGold, 1))
	rs.Precision = float64(goodSlots) / float64(max(slots, 1))
	free := 0
	for _, c := range rs.Chunks {
		if c.LeakFree {
			free++
		}
	}
	rs.LeakFree = float64(free) / float64(max(len(rs.Chunks), 1))
	return rs
}

func pairedBootstrap(a, b []float64) (mean, lo, hi float64) {
	n := len(a)
	diffs := make([]float64, n)
	for i := range a {
		diffs[i] = b[i] - a[i]
		mean += diffs[i]
	}
	mean /= float64(max(n, 1))
	r := rand.New(rand.NewPCG(1, 2))
	means := make([]float64, bootstrapDraws)
	for k := range means {
		s := 0.0
		for range n {
			s += diffs[r.IntN(n)]
		}
		means[k] = s / float64(n)
	}
	sort.Float64s(means)
	return mean, means[int(0.025*bootstrapDraws)], means[int(0.975*bootstrapDraws)]
}

func TestScoreGold(t *testing.T) {
	loadDotEnv(t)
	label := os.Getenv("EXPORTER_SCORE")
	if label == "" {
		t.Skip("set EXPORTER_SCORE=<label> to score the pipeline against the answer key")
	}
	data, err := os.ReadFile(goldPath)
	if err != nil {
		t.Fatal("build the answer key first with EXPORTER_BUILD_GOLD=1")
	}
	var gold goldSet
	must(t, json.Unmarshal(data, &gold))
	runs, _ := strconv.Atoi(firstNonEmpty(os.Getenv("EXPORTER_SCORE_RUNS"), "3"))
	chats := loadEvalSet(t, ".eval/train")
	book := ownerBookFor(chats)
	haiku := newHaikuClient(os.Getenv("ANTHROPIC_API_KEY"))
	var ai aiClient = haiku
	offline := &offlineAI{}
	cached := label
	if from := os.Getenv("EXPORTER_SCORE_OFFLINE"); from != "" {
		ai = offline
		cached = from
	}
	_ = os.MkdirAll(goldRunsDir, 0o700)
	var all []runScore
	for run := 1; run <= runs; run++ {
		cache := newAICache(filepath.Join(".eval", "score-cache", cached, strconv.Itoa(run)))
		r := newRedactor(ai, cache, dictionary, book)
		r.skipRefine = os.Getenv("EXPORTER_SCORE_PLAIN") != ""
		outputs := map[string]chatOutput{}
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, c := range chats {
			wg.Add(1)
			go func() {
				defer wg.Done()
				out := r.redactChat(context.Background(), c.input)
				mu.Lock()
				outputs[c.input.Folder] = out
				mu.Unlock()
			}()
		}
		wg.Wait()
		rs := scoreRun(fmt.Sprintf("%s#%d", label, run), gold, outputs)
		all = append(all, rs)
		t.Logf("%s run %d: details caught %.1f%%, precision %.1f%%, leak-free chunks %.0f%%, missed %v", label, run, 100*rs.Recall, 100*rs.Precision, 100*rs.LeakFree, rs.ByKind)
	}
	out, _ := json.MarshalIndent(all, "", " ")
	must(t, os.WriteFile(filepath.Join(goldRunsDir, label+".json"), out, 0o600))
	t.Logf("usage: %s, offline cache misses: %d", haiku.usage.summary(), offline.misses.Load())
	if base := os.Getenv("EXPORTER_COMPARE"); base != "" {
		compareRuns(t, base, label)
	}
}

func TestCompareGold(t *testing.T) {
	base, next := os.Getenv("EXPORTER_BASE"), os.Getenv("EXPORTER_NEXT")
	if base == "" || next == "" {
		t.Skip("set EXPORTER_BASE and EXPORTER_NEXT to compare saved runs")
	}
	compareRuns(t, base, next)
}

func chunkMisses(runs []runScore) map[string]float64 {
	misses := map[string]float64{}
	for _, r := range runs {
		for _, c := range r.Chunks {
			misses[c.Chat] += float64(c.Gold-c.Caught) / float64(len(runs))
		}
	}
	return misses
}

func chunkExtras(runs []runScore) map[string]float64 {
	extra := map[string]float64{}
	for _, r := range runs {
		for _, c := range r.Chunks {
			extra[c.Chat] += float64(c.Extra) / float64(len(runs))
		}
	}
	return extra
}

func compareRuns(t *testing.T, baseLabel, newLabel string) {
	load := func(label string) []runScore {
		data, err := os.ReadFile(filepath.Join(goldRunsDir, label+".json"))
		if err != nil {
			t.Fatalf("no saved runs for %s", label)
		}
		var rs []runScore
		must(t, json.Unmarshal(data, &rs))
		return rs
	}
	base, next := load(baseLabel), load(newLabel)
	for _, metric := range []struct {
		name string
		f    func([]runScore) map[string]float64
	}{{"missed details per chat", chunkMisses}, {"extra tags per chat", chunkExtras}} {
		a, b := metric.f(base), metric.f(next)
		var xs, ys []float64
		for _, chat := range sortedKeys(a) {
			xs = append(xs, a[chat])
			ys = append(ys, b[chat])
		}
		mean, lo, hi := pairedBootstrap(xs, ys)
		verdict := "no clear difference"
		if hi < 0 {
			verdict = "clearly better"
		} else if lo > 0 {
			verdict = "clearly worse"
		}
		t.Logf("%s: change %+.2f (95%% range %+.2f to %+.2f) -> %s", metric.name, mean, lo, hi, verdict)
	}
	avg := func(rs []runScore, f func(runScore) float64) float64 {
		s := 0.0
		for _, r := range rs {
			s += f(r)
		}
		return s / math.Max(1, float64(len(rs)))
	}
	t.Logf("details caught: %.1f%% -> %.1f%%; precision %.1f%% -> %.1f%%; leak-free chunks %.0f%% -> %.0f%%",
		100*avg(base, func(r runScore) float64 { return r.Recall }), 100*avg(next, func(r runScore) float64 { return r.Recall }),
		100*avg(base, func(r runScore) float64 { return r.Precision }), 100*avg(next, func(r runScore) float64 { return r.Precision }),
		100*avg(base, func(r runScore) float64 { return r.LeakFree }), 100*avg(next, func(r runScore) float64 { return r.LeakFree }))
}
