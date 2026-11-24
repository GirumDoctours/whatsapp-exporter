package main

import "strings"

type refineInput struct {
	Folder     string            `json:"folder"`
	Title      string            `json:"title"`
	IsGroup    bool              `json:"is_group"`
	Text       string            `json:"text"`
	Slots      []slot            `json:"slots"`
	SenderTags map[string]string `json:"sender_tags"`
	TitleText  string            `json:"title_text"`
	Roles      map[string]string `json:"roles"`
	doc        *chatDoc
}

func (in refineInput) parsed() chatDoc {
	if in.doc != nil {
		return *in.doc
	}
	return parseChat(in.Text, nil)
}

const finalSource = "final:"

type refineStep struct {
	name string
	run  func(refineInput) []slot
}

var refineSteps = []refineStep{
	{"mentions", mentionConvo},
	{"cues", cueCues},
	{"layout", layoutLayout},
	{"dates", dateTemporal},
	{"structure", structureStructMine},
	{"spans", spanSpanRepair},
}

func refineSlots(in refineInput) []slot {
	doc := in.parsed()
	in.doc = &doc
	filter := newRefineFilter(in)
	original := slotsByLine(in.Slots)
	for _, step := range refineSteps {
		var kept []slot
		for _, s := range step.run(in) {
			if !slotOverlaps(s, original[s.Line]) {
				if !filter.keep(s) {
					continue
				}
				if !strings.HasPrefix(s.Source, finalSource) {
					s.Source = finalSource + step.name
					s.Category = strings.ToUpper(firstNonEmpty(s.Category, "NAME"))
				}
			}
			kept = append(kept, s)
		}
		sortSlots(kept)
		in.Slots = kept
	}
	return restoreUncovered(original, in.Slots)
}

func restoreUncovered(original map[int][]slot, refined []slot) []slot {
	byLine := slotsByLine(refined)
	broken := map[int]bool{}
	for line, before := range original {
		if _, missing := uncovered(before, byLine[line]); missing {
			broken[line] = true
		}
	}
	if len(broken) == 0 {
		return refined
	}
	var out []slot
	for _, s := range refined {
		if !broken[s.Line] {
			out = append(out, s)
		}
	}
	for line := range broken {
		out = append(out, original[line]...)
	}
	return out
}

func slotsByLine(slots []slot) map[int][]slot {
	byLine := map[int][]slot{}
	for _, s := range slots {
		byLine[s.Line] = append(byLine[s.Line], s)
	}
	return byLine
}

func slotOverlaps(s slot, others []slot) bool {
	for _, o := range others {
		if s.Start < o.End && o.Start < s.End {
			return true
		}
	}
	return false
}

func uncovered(before, after []slot) (slot, bool) {
	byLine := slotsByLine(after)
	for _, s := range before {
		at := s.Start
		for moved := true; moved && at < s.End; {
			moved = false
			for _, o := range byLine[s.Line] {
				if o.Start <= at && o.End > at {
					at = o.End
					moved = true
				}
			}
		}
		if at < s.End {
			return s, true
		}
	}
	return slot{}, false
}
