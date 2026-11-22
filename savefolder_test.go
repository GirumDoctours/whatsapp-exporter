package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCanWriteTo(t *testing.T) {
	dir := t.TempDir()
	if !canWriteTo(dir) {
		t.Fatal("a fresh temp folder should be writable")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("the write check left %d files behind", len(entries))
	}
	if canWriteTo(filepath.Join(dir, "missing")) {
		t.Fatal("a missing folder isn't writable")
	}
	if runtime.GOOS != "windows" {
		locked := filepath.Join(dir, "locked")
		must(t, os.Mkdir(locked, 0o500))
		defer os.Chmod(locked, 0o700)
		if canWriteTo(locked) {
			t.Fatal("a read-only folder isn't writable")
		}
	}
}

func TestMoveIntoKeepsExistingFiles(t *testing.T) {
	work, desktop := t.TempDir(), t.TempDir()
	must(t, os.WriteFile(filepath.Join(desktop, "15551234567-11-26.zip"), []byte("older"), 0o600))
	zip := filepath.Join(work, "15551234567-11-26.zip")
	must(t, os.WriteFile(zip, []byte("newer"), 0o600))

	moved, err := moveInto(zip, desktop)
	must(t, err)

	if moved != filepath.Join(desktop, "15551234567-11-26-2.zip") {
		t.Fatalf("moved to %s, want a -2 name next to the older ZIP", moved)
	}
	if got, _ := os.ReadFile(moved); string(got) != "newer" {
		t.Fatalf("moved file holds %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(desktop, "15551234567-11-26.zip")); string(got) != "older" {
		t.Fatal("the older ZIP on the Desktop was changed")
	}
	if _, err := os.Stat(zip); !os.IsNotExist(err) {
		t.Fatal("the ZIP should no longer be in the working folder")
	}
}

func TestMoveIntoSameFolderIsANoOp(t *testing.T) {
	dir := t.TempDir()
	zip := filepath.Join(dir, "a.zip")
	must(t, os.WriteFile(zip, []byte("x"), 0o600))
	moved, err := moveInto(zip, dir+string(filepath.Separator))
	must(t, err)
	if moved != zip {
		t.Fatalf("got %s, want %s", moved, zip)
	}
}

func TestCopyFileWritesCompleteFile(t *testing.T) {
	dir := t.TempDir()
	from, to := filepath.Join(dir, "from.zip"), filepath.Join(dir, "to.zip")
	must(t, os.WriteFile(from, []byte("payload"), 0o600))
	must(t, copyFile(from, to))
	if got, _ := os.ReadFile(to); string(got) != "payload" {
		t.Fatalf("copy holds %q", got)
	}
	if _, err := os.Stat(to + ".partial"); !os.IsNotExist(err) {
		t.Fatal("the .partial file should be gone")
	}
}
