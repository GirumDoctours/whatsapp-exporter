package main

import _ "embed"

var (
	//go:embed prompts/cast.txt
	castSystemPrompt string
	//go:embed prompts/pass1.txt
	pass1SystemPrompt string
	//go:embed prompts/pass2.txt
	pass2SystemPrompt string
	//go:embed prompts/roles.txt
	roleSystemPrompt string
	//go:embed prompts/confirm.txt
	confirmSystemPrompt string
)

type confirmDecision struct {
	ID         string  `json:"id"`
	Same       bool    `json:"same"`
	Confidence float64 `json:"confidence"`
}

type confirmReply struct {
	Decisions []confirmDecision `json:"decisions"`
}

func confirmSchema() map[string]any {
	return objectSchema(map[string]any{
		"decisions": arrayOf(objectSchema(map[string]any{
			"id":         schemaString,
			"same":       schemaBool,
			"confidence": schemaNumber,
		})),
	})
}

type roleAssignment struct {
	ID         string  `json:"id"`
	Role       string  `json:"role"`
	SameAs     string  `json:"same_as"`
	Confidence float64 `json:"confidence"`
}

type roleReply struct {
	People []roleAssignment `json:"people"`
}

func roleSchema() map[string]any {
	return objectSchema(map[string]any{
		"people": arrayOf(objectSchema(map[string]any{
			"id":         schemaString,
			"role":       schemaString,
			"same_as":    schemaString,
			"confidence": schemaNumber,
		})),
	})
}

const promptVersion = "v5"

type castRole struct {
	Ref        string  `json:"ref"`
	Role       string  `json:"role"`
	Confidence float64 `json:"confidence"`
}

type castReply struct {
	Roles []castRole `json:"roles"`
}

type maskedTag struct {
	ID         string  `json:"id"`
	Tag        string  `json:"tag"`
	Confidence float64 `json:"confidence"`
}

type candidateTag struct {
	ID         string  `json:"id"`
	Keep       bool    `json:"keep"`
	Tag        string  `json:"tag"`
	Confidence float64 `json:"confidence"`
}

type foundTag struct {
	Line       int     `json:"line"`
	Text       string  `json:"text"`
	Tag        string  `json:"tag"`
	Confidence float64 `json:"confidence"`
}

type pass1Reply struct {
	Masked     []maskedTag    `json:"masked"`
	Candidates []candidateTag `json:"candidates"`
	Found      []foundTag     `json:"found"`
}

type slotVerdict struct {
	Slot       string  `json:"slot"`
	Correct    bool    `json:"correct"`
	BetterTag  string  `json:"better_tag"`
	IsPII      bool    `json:"is_pii"`
	Confidence float64 `json:"confidence"`
}

type pass2Reply struct {
	Verdicts []slotVerdict `json:"verdicts"`
	Leaks    []foundTag    `json:"leaks"`
}

func castSchema() map[string]any {
	return objectSchema(map[string]any{
		"roles": arrayOf(objectSchema(map[string]any{
			"ref":        schemaString,
			"role":       schemaString,
			"confidence": schemaNumber,
		})),
	})
}

func foundSchema() map[string]any {
	return objectSchema(map[string]any{
		"line":       schemaInteger,
		"text":       schemaString,
		"tag":        schemaString,
		"confidence": schemaNumber,
	})
}

func pass1Schema() map[string]any {
	return objectSchema(map[string]any{
		"masked": arrayOf(objectSchema(map[string]any{
			"id":         schemaString,
			"tag":        schemaString,
			"confidence": schemaNumber,
		})),
		"candidates": arrayOf(objectSchema(map[string]any{
			"id":         schemaString,
			"keep":       schemaBool,
			"tag":        schemaString,
			"confidence": schemaNumber,
		})),
		"found": arrayOf(foundSchema()),
	})
}

func pass2Schema() map[string]any {
	return objectSchema(map[string]any{
		"verdicts": arrayOf(objectSchema(map[string]any{
			"slot":       schemaString,
			"correct":    schemaBool,
			"better_tag": schemaString,
			"is_pii":     schemaBool,
			"confidence": schemaNumber,
		})),
		"leaks": arrayOf(foundSchema()),
	})
}
