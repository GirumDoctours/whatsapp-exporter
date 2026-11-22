package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
	"time"
)

const (
	dataFolderName    = "WhatsApp Exporter Data"
	closeDelaySeconds = 15
	quickTestMeta     = "quick_test"
)

type workspace struct {
	dir       string
	phone     string
	lastRun   time.Time
	quickTest bool
}

func (w workspace) kind() string {
	if w.quickTest {
		return "the quick test"
	}
	return "the full export"
}

func main() {
	baseDir := flag.String("dir", "", "folder for the export ZIP and working data (defaults to the folder the exporter is in)")
	flag.Parse()
	base := *baseDir
	if base == "" {
		base = appDir()
	}
	t := newTerminal()
	t.setTitle("WhatsApp Exporter")
	code, zipPath := run(t, base, *baseDir == "")
	t.showCursor()
	t.say("")
	if code != 0 {
		t.waitForEnter("Press Enter to close this window.")
		os.Exit(code)
	}
	t.countdown(closeDelaySeconds, func(left int) string {
		return fmt.Sprintf("Showing your ZIP in %s in %d seconds. Press Enter to go now.", fileManagerName, left)
	})
	revealFile(zipPath)
	closeTerminalWindow()
	os.Exit(0)
}

func appDir() string {
	exe, err := os.Executable()
	if err != nil {
		wd, _ := os.Getwd()
		return wd
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}

func run(t *terminal, base string, useDesktop bool) (int, string) {
	t.clearScreen()
	t.banner()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	ws, err := chooseWorkspace(t, filepath.Join(base, dataFolderName))
	if err != nil {
		t.say("  %s", t.paint(ansiRed, "Couldn't prepare the working folder: "+err.Error()))
		if errors.Is(err, fs.ErrPermission) {
			for _, line := range folderPermissionHelp {
				t.say("  %s", line)
			}
		}
		return 1, ""
	}
	dir := ws.dir
	exp, err := openExporter(dir)
	if err != nil {
		t.say("  %s", t.paint(ansiRed, "Couldn't start: "+err.Error()))
		return 1, ""
	}
	defer exp.close()
	exp.testMode = ws.quickTest
	_ = exp.arc.setMeta(ctx, quickTestMeta, strconv.FormatBool(ws.quickTest))
	if useDesktop {
		saveDir, ok := desktopSaveFolder(t)
		exp.saveDir, exp.desktopBlocked = saveDir, !ok
	}

	err = exp.run(ctx, t, base)
	switch {
	case err == nil:
		return 0, exp.savedZip
	case errors.Is(err, context.Canceled):
		t.say("")
		if exp.linked() {
			t.say("  %s", t.paint(ansiYellow, "Stopped. Your progress is saved."))
			t.say("  Open the exporter again and choose to continue where you left off.")
		} else {
			t.say("  Stopped before a phone was linked.")
		}
		return 130, ""
	case errors.Is(err, errLoggedOut):
		t.say("")
		t.say("  %s", t.paint(ansiYellow, "This computer was unlinked from your phone, so the export stopped."))
		t.say("  Open the exporter again and link your phone to continue where you left off.")
		return 1, ""
	case errors.Is(err, errLinkCancelled):
		t.say("  Linking cancelled.")
		return 1, ""
	default:
		t.say("")
		t.say("  %s", t.paint(ansiRed, "The export stopped: "+err.Error()))
		t.say("  Your progress is saved. Open the exporter again to continue.")
		t.say("  %s", t.paint(ansiDim, "Details: "+filepath.Join(dir, "exporter.log")))
		return 1, ""
	}
}

func chooseWorkspace(t *terminal, root string) (workspace, error) {
	found := findWorkspaces(root)
	if len(found) > 0 {
		options := make([]menuOption, 0, len(found)+1)
		for _, w := range found {
			options = append(options, menuOption{
				label: "Continue " + w.kind() + " for +" + w.phone,
				hint:  "last run " + w.lastRun.Local().Format("02/01/2006 15:04"),
			})
		}
		options = append(options, menuOption{label: "Start a new export", hint: "link a different phone"})
		pick := t.choose("You have an unfinished export on this computer.", options)
		if pick < len(found) {
			return found[pick], nil
		}
	}
	quickTest := t.choose("What would you like to do?", []menuOption{
		{label: "Run a quick test", hint: "your 10 latest chats · about 3 min"},
		{label: "Export everything", hint: "every chat, photo and file · about 25 min"},
	}) == 0
	dir := filepath.Join(root, "export-"+time.Now().Format("20060102-150405"))
	return workspace{dir: dir, quickTest: quickTest}, os.MkdirAll(dir, 0o700)
}

func findWorkspaces(root string) []workspace {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var found []workspace
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		archivePath := filepath.Join(dir, "archive.db")
		if _, err := os.Stat(archivePath); err != nil {
			continue
		}
		arc, err := openArchive(archivePath)
		if err != nil {
			continue
		}
		ctx := context.Background()
		phone := arc.meta(ctx, "phone")
		lastRun, _ := time.Parse(time.RFC3339, arc.meta(ctx, "last_run"))
		quickTest, _ := strconv.ParseBool(arc.meta(ctx, quickTestMeta))
		_ = arc.close()
		if phone == "" {
			_ = os.RemoveAll(dir)
			continue
		}
		found = append(found, workspace{dir: dir, phone: phone, lastRun: lastRun, quickTest: quickTest})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].lastRun.After(found[j].lastRun) })
	return found
}
