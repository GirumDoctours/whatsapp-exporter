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
	"runtime"
	"sort"
	"strconv"
	"syscall"
	"time"
)

const (
	dataFolderName    = "WhatsApp Exporter Data"
	closeDelaySeconds = 15
	quickTestMeta     = "quick_test"
	redactMeta        = "redact"
	mediaMeta         = "messages_only"
)

type workspace struct {
	dir          string
	phone        string
	lastRun      time.Time
	quickTest    bool
	redact       bool
	messagesOnly bool
}

type selection struct {
	ws       workspace
	finished *finishedExport
}

func (w workspace) kind() string {
	kind := "the full export"
	if w.quickTest {
		kind = "the quick test"
	}
	if w.messagesOnly {
		kind += " (messages only)"
	}
	return kind
}

func main() {
	baseDir := flag.String("dir", "", "folder for the export ZIP and working data (defaults to the folder the exporter is in)")
	redactFolder := flag.String("redact-folder", "", "redact an existing export folder instead of exporting (for testing)")
	debug := flag.Bool("debug", false, "save a detailed log as "+debugFileName+" inside the ZIP (never with redaction, unless the run fails)")
	flag.Parse()
	if *debug {
		enableDebugLog()
	}
	logMain.Infof("WhatsApp Exporter starting on %s/%s", runtime.GOOS, runtime.GOARCH)
	base := *baseDir
	if base == "" {
		base = appDir()
	}
	t := newTerminal()
	t.setTitle("WhatsApp Exporter")
	var code int
	var zipPath string
	if *redactFolder != "" {
		code, zipPath = runRedactFolder(t, *redactFolder, base)
		t.showCursor()
		t.say("")
		t.say("  %s", zipPath)
		os.Exit(code)
	} else {
		code, zipPath = run(t, base, *baseDir == "")
	}
	t.showCursor()
	t.say("")
	logMain.Infof("Finished with exit code %d", code)
	if code != 0 && debugLogEnabled() {
		path := filepath.Join(base, debugFileName)
		if err := os.WriteFile(path, debugLogContents(), 0o600); err == nil {
			t.say("  %s", t.paint(ansiDim, "Debug log saved to "+path))
		}
	}
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

	sel, err := chooseWorkspace(t, filepath.Join(base, dataFolderName))
	if err != nil {
		t.say("  %s", t.paint(ansiRed, "Couldn't prepare the working folder: "+err.Error()))
		if errors.Is(err, fs.ErrPermission) {
			for _, line := range folderPermissionHelp {
				t.say("  %s", line)
			}
		}
		return 1, ""
	}
	if sel.finished != nil {
		logMain.Infof("Finishing a saved export for +%s", sel.finished.state.Phone)
		return finishSaved(ctx, t, *sel.finished, base, useDesktop)
	}
	ws := sel.ws
	logMain.Infof("Workspace %s: quick test %v, messages only %v, redact %v", ws.dir, ws.quickTest, ws.messagesOnly, ws.redact)
	dir := ws.dir
	var ai *haikuClient
	if ws.redact {
		ai = setupAI(ctx, t)
	}
	exp, err := openExporter(dir)
	if err != nil {
		t.say("  %s", t.paint(ansiRed, "Couldn't start: "+err.Error()))
		return 1, ""
	}
	defer exp.close()
	exp.testMode = ws.quickTest
	exp.redact = ws.redact
	exp.messagesOnly = ws.messagesOnly
	exp.ai = ai
	if !ws.messagesOnly {
		exp.transcriber = setupTranscription(ctx, t)
	}
	_ = exp.arc.setMeta(ctx, quickTestMeta, strconv.FormatBool(ws.quickTest))
	_ = exp.arc.setMeta(ctx, redactMeta, strconv.FormatBool(ws.redact))
	_ = exp.arc.setMeta(ctx, mediaMeta, strconv.FormatBool(ws.messagesOnly))
	if useDesktop {
		saveDir, ok := desktopSaveFolder(t)
		exp.saveDir, exp.desktopBlocked = saveDir, !ok
		logMain.Infof("Save folder %q, Desktop usable %v", saveDir, ok)
	}

	err = exp.run(ctx, t, base)
	if err != nil {
		logMain.Errorf("Export stopped: %v", err)
	}
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

func chooseWorkspace(t *terminal, root string) (selection, error) {
	found := findWorkspaces(root)
	finished := findFinished(root)
	if len(found)+len(finished) > 0 {
		options := make([]menuOption, 0, len(found)+len(finished)+1)
		for _, f := range finished {
			options = append(options, menuOption{
				label: "Finish saving the export for +" + f.state.Phone,
				hint:  "your phone is no longer needed",
			})
		}
		for _, w := range found {
			options = append(options, menuOption{
				label: "Continue " + w.kind() + " for +" + w.phone,
				hint:  "last run " + w.lastRun.Local().Format("02/01/2006 15:04"),
			})
		}
		options = append(options, menuOption{label: "Start a new export", hint: "link a different phone"})
		pick := t.choose("You have an unfinished export on this computer.", options)
		if pick < len(finished) {
			return selection{finished: &finished[pick]}, nil
		}
		if pick-len(finished) < len(found) {
			return selection{ws: found[pick-len(finished)]}, nil
		}
	}
	quickTest := t.choose("What would you like to do?", []menuOption{
		{label: "Run a quick test", hint: "your 10 latest chats · about 3 min"},
		{label: "Export everything", hint: "every chat, photo and file · about 25 min"},
	}) == 0
	messagesOnly := t.choose("What should it download?", []menuOption{
		{label: "Messages only", hint: "just the text · much faster"},
		{label: "Messages and media", hint: "photos, videos, voice notes and files"},
	}) == 0
	redact := askRedact(t)
	dir := filepath.Join(root, "export-"+time.Now().Format("20060102-150405"))
	return selection{ws: workspace{dir: dir, quickTest: quickTest, redact: redact, messagesOnly: messagesOnly}}, os.MkdirAll(dir, 0o700)
}

func finishSaved(ctx context.Context, t *terminal, f finishedExport, base string, useDesktop bool) (int, string) {
	var ai *haikuClient
	if f.state.Redact {
		ai = setupAI(ctx, t)
	}
	saveDir, blocked := "", false
	if useDesktop {
		dir, ok := desktopSaveFolder(t)
		saveDir, blocked = dir, !ok
	}
	exp, err := finishFromFolder(ctx, t, f, base, saveDir, blocked, ai, false)
	if err != nil {
		t.say("")
		t.say("  %s", t.paint(ansiRed, "Couldn't finish saving the export: "+err.Error()))
		t.say("  Open the exporter again to try once more.")
		return 1, ""
	}
	return 0, exp.savedZip
}

func runRedactFolder(t *terminal, folder, base string) (int, string) {
	t.clearScreen()
	t.banner()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	state, ok := loadState(folder)
	if !ok {
		state = exportState{Created: time.Now()}
	}
	state.Redact, state.FolderDone = true, true
	exp, err := finishFromFolder(ctx, t, finishedExport{dir: folder, state: state}, base, "", false, setupAI(ctx, t), true)
	if err != nil {
		t.say("  %s", t.paint(ansiRed, "Redaction failed: "+err.Error()))
		return 1, ""
	}
	return 0, exp.savedZip
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
		redact, _ := strconv.ParseBool(arc.meta(ctx, redactMeta))
		messagesOnly, _ := strconv.ParseBool(arc.meta(ctx, mediaMeta))
		_ = arc.close()
		if phone == "" {
			_ = os.RemoveAll(dir)
			continue
		}
		found = append(found, workspace{dir: dir, phone: phone, lastRun: lastRun, quickTest: quickTest, redact: redact, messagesOnly: messagesOnly})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].lastRun.After(found[j].lastRun) })
	return found
}
