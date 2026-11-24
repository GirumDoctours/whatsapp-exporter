package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestRefineManyNames(t *testing.T) {
	if os.Getenv("EXPORTER_REFINE_SPEED") == "" {
		t.Skip("set EXPORTER_REFINE_SPEED=1 to time the final local step on a chat full of different names")
	}
	syllables := strings.Fields("ka ri mo le sa ti nu vo be da fe go hi ju pe")
	word := func(n int) string {
		w := ""
		for range 4 {
			w += syllables[n%len(syllables)]
			n /= len(syllables)
		}
		return strings.ToUpper(w[:1]) + w[1:]
	}
	var b strings.Builder
	var slots []slot
	const lines = 20000
	for i := range lines {
		name := word(i % 3000)
		b.WriteString("[02/01/2026, 15:04:05] Person One: Thanks " + name + ", see you later with the paperwork\n")
		if i < 3000 {
			slots = append(slots, slot{Line: i, Start: 7, End: 7 + len(name), Tag: "{{PERSON_FIRST_NAME}}", Value: name, Category: "AI", Source: "ai", Confidence: 0.9})
		}
	}
	fc := refineInput{Title: "Person One", Text: b.String(), Slots: slots, SenderTags: map[string]string{"Person One": "{{CONTACT_FULL_NAME}}"}}
	for _, step := range refineSteps {
		at := time.Now()
		fc.Slots = step.run(fc)
		t.Logf("%-10s %s", step.name, time.Since(at).Round(time.Millisecond))
	}
}
