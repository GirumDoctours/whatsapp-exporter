package main

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

func TestWriteExportBuildsFolderPerChat(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	mediaDir := filepath.Join(dir, "media")
	if err := os.MkdirAll(mediaDir, 0o700); err != nil {
		t.Fatal(err)
	}
	arc, err := openArchive(filepath.Join(dir, "archive.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer arc.close()

	customerPN := "905551112233@s.whatsapp.net"
	customerLID := "123456789012345@lid"
	group := "120363000000000000@g.us"
	must(t, arc.upsertChat(ctx, chatRecord{JID: customerPN}))
	must(t, arc.upsertChat(ctx, chatRecord{JID: customerLID, PN: customerPN}))
	must(t, arc.upsertChat(ctx, chatRecord{JID: group, Name: "Patients: Sept", Archived: true}))
	must(t, arc.upsertChat(ctx, chatRecord{JID: "status@broadcast"}))

	image := storedMessage{
		ChatJID: customerLID, ID: "IMG1", SenderJID: customerLID, Timestamp: at(3), PushName: "Ayşe",
		MediaState: mediaPending,
		Message:    &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/jpeg")}},
	}
	msgs := []storedMessage{
		{ChatJID: customerPN, ID: "A1", SenderJID: customerPN, Timestamp: at(1), PushName: "Ayşe",
			Message: &waE2E.Message{Conversation: proto.String("Hi from my phone number chat")}},
		{ChatJID: customerLID, ID: "A2", SenderJID: customerLID, Timestamp: at(2), PushName: "Ayşe",
			Message: &waE2E.Message{Conversation: proto.String("Hi from the LID chat")}},
		image,
		{ChatJID: group, ID: "G1", SenderJID: "447700900123@s.whatsapp.net", IsGroup: true, Timestamp: at(5),
			Message: &waE2E.Message{Conversation: proto.String("Group hello")}},
		{ChatJID: group, ID: "G2", FromMe: true, IsGroup: true, Timestamp: at(6),
			Message:    &waE2E.Message{AudioMessage: &waE2E.AudioMessage{Mimetype: proto.String("audio/ogg; codecs=opus")}},
			MediaState: mediaPending},
		{ChatJID: "status@broadcast", ID: "S1", SenderJID: customerPN, Timestamp: at(7),
			Message: &waE2E.Message{Conversation: proto.String("status update")}},
	}
	added, err := arc.insertMessages(ctx, msgs)
	must(t, err)
	if added != len(msgs) {
		t.Fatalf("inserted %d, want %d", added, len(msgs))
	}
	again, err := arc.insertMessages(ctx, msgs[:2])
	must(t, err)
	if again != 0 {
		t.Fatalf("duplicate insert added %d rows, want 0", again)
	}

	queue, err := arc.mediaQueue(ctx)
	must(t, err)
	if len(queue) != 2 {
		t.Fatalf("media queue = %d, want 2", len(queue))
	}
	imageName := localMediaName(image, "jpg")
	must(t, os.WriteFile(filepath.Join(mediaDir, imageName), []byte("fake-jpeg"), 0o600))
	must(t, arc.setMedia(ctx, customerLID, "IMG1", mediaDone, imageName, ""))
	must(t, arc.setMedia(ctx, group, "G2", mediaUnavailable, "", reasonNotOnPhone))

	oldest, err := arc.oldestMessage(ctx, customerLID)
	must(t, err)
	if oldest == nil || oldest.ID != "A2" {
		t.Fatalf("oldest message = %+v, want A2", oldest)
	}

	names := &nameResolver{
		ctx:       ctx,
		contacts:  map[types.JID]types.ContactInfo{},
		ownName:   "Northwind",
		ownUsers:  map[string]bool{"905000000000": true},
		pushNames: map[string]string{},
	}
	outPath := filepath.Join(dir, zipFileName("905000000000", at(0), false))
	var progress []float64
	summary, err := writeExport(ctx, arc, names, mediaDir, outPath, "905000000000", testZone, nil, func(f float64) { progress = append(progress, f) })
	must(t, err)
	if len(progress) == 0 || progress[len(progress)-1] != 1 {
		t.Errorf("progress did not reach 100%%: %v", progress)
	}
	for i := 1; i < len(progress); i++ {
		if progress[i] < progress[i-1] {
			t.Errorf("progress went backwards: %v", progress)
		}
	}

	if summary.Chats != 2 || summary.Individuals != 1 || summary.Groups != 1 || summary.Archived != 1 {
		t.Errorf("summary = %+v", summary)
	}
	if summary.Messages != 5 || summary.Attachments != 1 || summary.Missing != 1 {
		t.Errorf("summary counts = %+v", summary)
	}

	files := readZip(t, outPath)
	var paths []string
	for name := range files {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	wantPaths := []string{
		"+905551112233/00000001.jpg",
		"+905551112233/_chat.txt",
		"GP-Patients_ Sept/_chat.txt",
		"index.csv",
		"report.txt",
	}
	if strings.Join(paths, "\n") != strings.Join(wantPaths, "\n") {
		t.Fatalf("zip entries:\n%s\nwant:\n%s", strings.Join(paths, "\n"), strings.Join(wantPaths, "\n"))
	}

	customerChat := files["+905551112233/_chat.txt"]
	for _, want := range []string{
		"[05/09/2024, 11:01:50] +905551112233: Hi from my phone number chat\r\n",
		"[05/09/2024, 11:02:50] +905551112233: Hi from the LID chat\r\n",
		"<attached: 00000001.jpg>",
	} {
		if !strings.Contains(customerChat, want) {
			t.Errorf("customer chat missing %q\n%s", want, customerChat)
		}
	}
	if files["+905551112233/00000001.jpg"] != "fake-jpeg" {
		t.Errorf("attachment bytes not copied")
	}
	groupChat := files["GP-Patients_ Sept/_chat.txt"]
	if !strings.Contains(groupChat, "Northwind: \u200eaudio omitted") {
		t.Errorf("group chat missing omitted audio line\n%s", groupChat)
	}
	index := files[indexFileName]
	if !strings.HasPrefix(index, "\xef\xbb\xbfFolder,Chat,Type") || !strings.Contains(index, "+905551112233,Ayşe,No,3,") {
		t.Errorf("unexpected index:\n%s", index)
	}
	report := files[reportFileName]
	if !strings.Contains(report, "GP-Patients_ Sept | 05/09/2024 11:06:50 | audio | no longer on the phone") {
		t.Errorf("report missing audio line:\n%s", report)
	}
	if _, err := os.Stat(outPath + ".partial"); !os.IsNotExist(err) {
		t.Errorf("partial file left behind")
	}
}

func TestZipFileName(t *testing.T) {
	when := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	if got := zipFileName("15551234567", when, false); got != "15551234567-11-26.zip" {
		t.Fatalf("zipFileName = %q", got)
	}
}

func TestFolderNames(t *testing.T) {
	used := map[string]int{}
	group := &chatGroup{isGroup: true}
	person := &chatGroup{}
	if got := uniqueFolder(folderName(group, "Clinic: Team"), used); got != "GP-Clinic_ Team" {
		t.Errorf("group folder = %q", got)
	}
	if got := uniqueFolder(folderName(person, "Ayşe Yılmaz"), used); got != "Ayşe Yılmaz" {
		t.Errorf("person folder = %q", got)
	}
	if got := uniqueFolder(folderName(person, "ayşe yılmaz"), used); got != "ayşe yılmaz-2" {
		t.Errorf("duplicate folder = %q", got)
	}
}

func TestAvailablePathAddsSuffix(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "export.zip")
	if got := availablePath(first); got != first {
		t.Fatalf("got %q", got)
	}
	must(t, os.WriteFile(first, nil, 0o600))
	if got := availablePath(first); got != filepath.Join(dir, "export-2.zip") {
		t.Fatalf("got %q", got)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func readZip(t *testing.T, path string) map[string]string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	must(t, err)
	defer zr.Close()
	files := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		must(t, err)
		data, err := io.ReadAll(rc)
		rc.Close()
		must(t, err)
		files[f.Name] = string(data)
	}
	return files
}
