package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
)

var wordToken = regexp.MustCompile(`[\p{L}\p{N}'’]+`)

type evalChat struct {
	input chatInput
	lines int
}

func loadEvalSet(t *testing.T, dir string) []evalChat {
	index := readIndex(dir)
	var out []evalChat
	for _, folder := range chatFolders(dir, index) {
		data, err := os.ReadFile(filepath.Join(dir, folder, "_chat.txt"))
		if err != nil {
			t.Fatal(err)
		}
		doc := parseChat(string(data), nil)
		title, isGroup := "", strings.HasPrefix(folder, groupFolderPrefix)
		if row, ok := index.byDir[folder]; ok && len(row) > 2 {
			title, isGroup = row[1], row[2] == "Group"
		} else if len(doc.Lines) > 0 {
			title = doc.Lines[0].Sender
			isGroup = len(doc.senders()) > 2
		}
		out = append(out, evalChat{input: chatInput{Folder: folder, Title: title, IsGroup: isGroup, Text: string(data)}, lines: len(doc.Lines)})
	}
	return out
}

func ownerBookFor(chats []evalChat) *peopleBook {
	counts := map[string]int{}
	for _, c := range chats {
		for _, s := range parseChat(c.input.Text, nil).senders() {
			if s != c.input.Title {
				counts[s]++
			}
		}
	}
	owner, best := "", 0
	for label, n := range counts {
		if n > best || (n == best && label < owner) {
			owner, best = label, n
		}
	}
	b := newPeopleBook()
	b.OwnerKey = "owner"
	b.addLabel("owner", owner)
	b.mu.Lock()
	b.Persons["owner"].Own = true
	b.mu.Unlock()
	return b
}

type offlineAI struct {
	misses atomic.Int64
}

func (o *offlineAI) generate(ctx context.Context, req aiRequest, out any) error {
	o.misses.Add(1)
	return errors.New("offline: not in cache")
}
