package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

func plainResolver() *nameResolver {
	return &nameResolver{
		ctx:       context.Background(),
		contacts:  map[types.JID]types.ContactInfo{},
		ownName:   "Northwind",
		ownUsers:  map[string]bool{},
		pushNames: map[string]string{},
	}
}

func TestLatestChatsPicksMostRecentAndMergesAliases(t *testing.T) {
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	var chats []chatRecord
	for i := 0; i < 15; i++ {
		chats = append(chats, chatRecord{
			JID:         fmt.Sprintf("90555000%04d@s.whatsapp.net", i),
			LastMessage: base.Add(time.Duration(i) * time.Hour),
		})
	}
	chats = append(chats,
		chatRecord{JID: "447700900123@s.whatsapp.net", LastMessage: base.Add(100 * time.Hour)},
		chatRecord{JID: "123456789012345@lid", PN: "447700900123@s.whatsapp.net", LastMessage: base.Add(99 * time.Hour)},
		chatRecord{JID: "status@broadcast", LastMessage: base.Add(200 * time.Hour)},
	)

	ownChat := "999999999999999@lid"
	onlyOldVisible := "905559990000@s.whatsapp.net"
	chats = append(chats,
		chatRecord{JID: ownChat, LastMessage: base.Add(300 * time.Hour)},
		chatRecord{JID: onlyOldVisible, LastMessage: base.Add(150 * time.Hour)},
	)
	visible := map[string]time.Time{ownChat: {}, onlyOldVisible: base.Add(-time.Hour)}
	var looked []string
	lastVisible := func(group *chatGroup) (time.Time, error) {
		looked = append(looked, group.raw...)
		if last, ok := visible[group.raw[0]]; ok {
			return last, nil
		}
		return group.last, nil
	}

	selected, err := latestChats(chats, plainResolver(), 10, lastVisible)
	must(t, err)

	if !selected["447700900123@s.whatsapp.net"] || !selected["123456789012345@lid"] {
		t.Fatalf("newest chat and its LID alias should both be selected: %v", selected)
	}
	for _, jid := range []string{"status@broadcast", ownChat, onlyOldVisible} {
		if selected[jid] {
			t.Fatalf("%s should not be selected: %v", jid, selected)
		}
	}
	if len(selected) != 11 {
		t.Fatalf("expected 10 chats (11 JIDs with the alias), got %d: %v", len(selected), selected)
	}
	if !selected[chats[6].JID] || selected[chats[5].JID] {
		t.Fatalf("expected the 9 newest numbered chats down to %s: %v", chats[6].JID, selected)
	}
	if slices.Contains(looked, chats[5].JID) {
		t.Fatalf("ranking should stop once no older chat can beat the 10th: looked at %v", looked)
	}
}

func TestLatestChatsSkipsChatsWithOnlyHiddenMessages(t *testing.T) {
	ctx := context.Background()
	arc, err := openArchive(filepath.Join(t.TempDir(), "archive.db"))
	must(t, err)
	defer arc.close()

	ownChat := "999999999999999@lid"
	hidden := &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type: waE2E.ProtocolMessage_PEER_DATA_OPERATION_REQUEST_RESPONSE_MESSAGE.Enum(),
	}}
	text := &waE2E.Message{Conversation: proto.String("hello")}
	stored := []storedMessage{
		{ChatJID: "905551112233@s.whatsapp.net", ID: "A", SenderJID: "905551112233@s.whatsapp.net", Timestamp: at(1), Message: text},
		{ChatJID: "447700900123@s.whatsapp.net", ID: "B", SenderJID: "447700900123@s.whatsapp.net", Timestamp: at(2), Message: text},
		{ChatJID: ownChat, ID: "C", SenderJID: ownChat, FromMe: true, Timestamp: at(3), Message: hidden},
	}
	_, err = arc.insertMessages(ctx, stored)
	must(t, err)
	chats, err := arc.listChats(ctx)
	must(t, err)

	names := plainResolver()
	selected, err := latestChats(chats, names, 2, lastVisibleMessage(ctx, arc, names))
	must(t, err)

	want := map[string]bool{"905551112233@s.whatsapp.net": true, "447700900123@s.whatsapp.net": true}
	if len(selected) != len(want) || !selected["905551112233@s.whatsapp.net"] || !selected["447700900123@s.whatsapp.net"] {
		t.Fatalf("selected %v, want %v", selected, want)
	}
}

func TestKeepSelectedWithoutFilterKeepsEverything(t *testing.T) {
	items := []string{"a", "b"}
	if got := keepSelected(items, nil, func(s string) string { return s }); len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	if got := keepSelected(items, map[string]bool{"b": true}, func(s string) string { return s }); len(got) != 1 || got[0] != "b" {
		t.Fatalf("got %v", got)
	}
}

func TestKeepSelectedGroupsMatchesAnyAlias(t *testing.T) {
	groups := []*chatGroup{
		{key: "one", raw: []string{"123456789012345@lid", "447700900123@s.whatsapp.net"}},
		{key: "two", raw: []string{"905551112233@s.whatsapp.net"}},
	}
	got := keepSelectedGroups(groups, map[string]bool{"447700900123@s.whatsapp.net": true})
	if len(got) != 1 || got[0].key != "one" {
		t.Fatalf("got %v", got)
	}
	if got := keepSelectedGroups(groups, nil); len(got) != 2 {
		t.Fatalf("no filter should keep every group, got %d", len(got))
	}
}

func TestWriteExportOnlyIncludesSelectedChats(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	arc, err := openArchive(filepath.Join(dir, "archive.db"))
	must(t, err)
	defer arc.close()
	mediaDir := filepath.Join(dir, "media")
	must(t, os.MkdirAll(mediaDir, 0o700))

	for i, jid := range []string{"905551112233@s.whatsapp.net", "447700900123@s.whatsapp.net", "120363000000000000@g.us"} {
		name := ""
		if strings.HasSuffix(jid, "@g.us") {
			name = "Group"
		}
		must(t, arc.upsertChat(ctx, chatRecord{JID: jid, Name: name}))
		_, err := arc.insertMessages(ctx, []storedMessage{{
			ChatJID: jid, ID: "M" + string(rune('0'+i)), SenderJID: "905551112233@s.whatsapp.net",
			IsGroup: strings.HasSuffix(jid, "@g.us"), Timestamp: at(i),
			Message: &waE2E.Message{Conversation: proto.String("hello")},
		}})
		must(t, err)
	}

	only := map[string]bool{"447700900123@s.whatsapp.net": true, "120363000000000000@g.us": true}
	out := filepath.Join(dir, zipFileName("905000000000", at(0), true))
	summary, err := writeExport(ctx, arc, plainResolver(), mediaDir, out, "905000000000", testZone, only, func(float64) {})
	must(t, err)
	if summary.Chats != 2 {
		t.Fatalf("exported %d chats, want 2", summary.Chats)
	}
	var paths []string
	for name := range readZip(t, out) {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	want := "+447700900123/_chat.txt\nGP-Group/_chat.txt\nindex.csv\nreport.txt"
	if strings.Join(paths, "\n") != want {
		t.Fatalf("zip entries:\n%s\nwant:\n%s", strings.Join(paths, "\n"), want)
	}
	if !strings.HasSuffix(out, "905000000000-09-24-test.zip") {
		t.Fatalf("test zip name = %s", filepath.Base(out))
	}
}
