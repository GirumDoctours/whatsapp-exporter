package main

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sampleExport(t *testing.T, redact bool) finishedExport {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data", finishedPrefix+"export-1")
	folder := filepath.Join(dir, "Lena Hartwell")
	must(t, os.MkdirAll(folder, 0o700))
	must(t, os.WriteFile(filepath.Join(folder, "_chat.txt"), []byte(sampleChat), 0o600))
	index := "\xef\xbb\xbfFolder,Chat,Type,Phone number or group ID,WhatsApp profile name,Archived,Messages,First message,Last message,Attachments saved,Attachments missing\r\nLena Hartwell,Lena Hartwell,Individual,+447700900999,Lena,No,4,x,y,0,0\r\n"
	must(t, os.WriteFile(filepath.Join(dir, indexFileName), []byte(index), 0o600))
	must(t, os.WriteFile(filepath.Join(dir, reportFileName), []byte("WhatsApp export for +15550001111\r\n"), 0o600))
	state := exportState{Phone: "15550001111", Redact: redact, Created: time.Now(), FolderDone: true}
	saveState(dir, state)
	must(t, sampleBook().save(filepath.Join(dir, peopleFileName)))
	return finishedExport{dir: dir, state: state}
}

func zipNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	must(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func quietTerminal() (*terminal, *bytes.Buffer) {
	var screen bytes.Buffer
	return &terminal{in: bufio.NewReader(strings.NewReader("")), out: &screen}, &screen
}

func TestRedactedExportKeepsOnlyTheRedactedZip(t *testing.T) {
	enableDebugLog()
	f := sampleExport(t, true)
	out := t.TempDir()
	term, screen := quietTerminal()
	e, err := finishFromFolder(context.Background(), term, f, out, "", false, nil, false)
	must(t, err)
	names := zipNames(t, out)
	if len(names) != 1 || !strings.HasSuffix(names[0], "-redacted.zip") || e.savedZip != filepath.Join(out, names[0]) {
		t.Fatalf("saved %v, zip %q", names, e.savedZip)
	}
	files := readZipFiles(t, e.savedZip)
	for name, data := range files {
		for _, secret := range []string{"Lena", "Hartwell", "X1234567", "15550001111"} {
			if strings.Contains(name, secret) || strings.Contains(data, secret) {
				t.Errorf("%s contains %q", name, secret)
			}
		}
	}
	if _, ok := files[debugFileName]; ok {
		t.Error("the debug log must not be kept with redaction")
	}
	if _, err := os.Stat(f.dir); !os.IsNotExist(err) {
		t.Error("the original files should be gone")
	}
	shown := screen.String()
	if strings.Count(shown, "Your export is ready") != 1 || strings.Contains(shown, "Redacted copy") || strings.Contains(shown, "High confidence") {
		t.Errorf("unexpected screen:\n%s", shown)
	}
	if strings.Index(shown, "Your export is ready") < strings.Index(shown, "Saving the redacted ZIP") {
		t.Error("the export is only ready once redaction is done")
	}
	if !strings.Contains(shown, "Original messages and files deleted") || !strings.Contains(shown, names[0]) {
		t.Errorf("the screen should confirm the deletion and name the ZIP:\n%s", shown)
	}
}

func TestPlainExportShowsReadyOnce(t *testing.T) {
	f := sampleExport(t, false)
	out := t.TempDir()
	term, screen := quietTerminal()
	_, err := finishFromFolder(context.Background(), term, f, out, "", false, nil, false)
	must(t, err)
	if names := zipNames(t, out); len(names) != 1 || strings.Contains(names[0], "redacted") {
		t.Fatalf("saved %v", names)
	}
	if strings.Count(screen.String(), "Your export is ready") != 1 || strings.Contains(screen.String(), "personal details") {
		t.Errorf("unexpected screen:\n%s", screen.String())
	}
}

func TestStoppedRedactionSavesNothingAndCanResume(t *testing.T) {
	f := sampleExport(t, true)
	out := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	term, screen := quietTerminal()
	if _, err := finishFromFolder(ctx, term, f, out, "", false, nil, false); err == nil {
		t.Fatal("a stopped redaction should report an error")
	}
	if names := zipNames(t, out); len(names) != 0 || strings.Contains(screen.String(), "Your export is ready") {
		t.Fatalf("a stopped redaction saved %v", names)
	}
	_, err := finishFromFolder(context.Background(), term, f, out, "", false, nil, false)
	must(t, err)
	if names := zipNames(t, out); len(names) != 1 {
		t.Fatalf("after resuming, saved %v", names)
	}
}

func TestMissingChatStopsTheRedactedZip(t *testing.T) {
	f := sampleExport(t, true)
	run := newRedaction(context.Background(), nil, sampleBook(), f.dir, f.state.Phone)
	path := filepath.Join(t.TempDir(), "redacted.zip")
	if _, err := run.writeRedactedZip(path, nil); err == nil {
		t.Fatal("a chat that wasn't redacted must not be dropped silently")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("no partial ZIP should be left")
	}
}
