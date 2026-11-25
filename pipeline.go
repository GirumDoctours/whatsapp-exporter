package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nyaruka/phonenumbers"
)

const (
	finishedPrefix = "finished-"
	stateFileName  = ".state.json"
)

type exportState struct {
	Phone      string    `json:"phone"`
	QuickTest  bool      `json:"quick_test"`
	Redact     bool      `json:"redact"`
	NoMedia    bool      `json:"messages_only"`
	Created    time.Time `json:"created"`
	FolderDone bool      `json:"folder_done"`
}

func saveState(dir string, state exportState) {
	if data, err := json.Marshal(state); err == nil {
		_ = os.WriteFile(filepath.Join(dir, stateFileName), data, 0o600)
	}
}

func loadState(dir string) (exportState, bool) {
	var state exportState
	data, err := os.ReadFile(filepath.Join(dir, stateFileName))
	if err != nil || json.Unmarshal(data, &state) != nil {
		return state, false
	}
	return state, true
}

func finishedDirFor(workDir string) string {
	return filepath.Join(filepath.Dir(workDir), finishedPrefix+filepath.Base(workDir))
}

func countryCode(phone string) string {
	num, err := phonenumbers.Parse("+"+phone, "")
	if err != nil {
		return ""
	}
	return strconv.Itoa(int(num.GetCountryCode()))
}

func redactedZipName(now time.Time, test bool) string {
	name := "WhatsApp-" + now.Format("01-06")
	if test {
		name += "-test"
	}
	return name + "-redacted.zip"
}

const messagesOnlySuffix = "-messages-only"

func withNameSuffix(name, suffix string) string {
	if suffix == "" {
		return name
	}
	ext := filepath.Ext(name)
	return strings.TrimSuffix(name, ext) + suffix + ext
}

func newOwnerBook(phone, ownName string) *peopleBook {
	book := newPeopleBook()
	book.OwnerKey = phone
	book.OwnerCode = countryCode(phone)
	book.addLabel(phone, ownName)
	book.addPhone(phone, phone)
	book.mu.Lock()
	book.Persons[phone].Own = true
	book.mu.Unlock()
	return book
}

func newRedaction(ctx context.Context, ai *haikuClient, book *peopleBook, dir, phone string) *redactionRun {
	var client aiClient
	if ai != nil {
		client = ai
	}
	r := newRedactor(client, newAICache(filepath.Join(dir, redactionCacheDir)), dictionary, book)
	run := newRedactionRun(ctx, r, dir, phone)
	if ai != nil {
		run.usage = ai.usage.summary
	}
	run.media = newMediaChecker(r, filepath.Join(dir, redactedMediaDir))
	run.media.replaceAudio = true
	return run
}

func (e *exporter) prepareFolder(ctx context.Context, phone string) (*exportPlan, error) {
	names := newNameResolver(ctx, e.cli, e.ownName(ctx, phone))
	dir := finishedDirFor(e.dir)
	plan, err := newExportPlan(ctx, e.arc, names, filepath.Join(e.dir, "media"), dir, phone, time.Local, e.onlyChats)
	if err != nil {
		return nil, err
	}
	plan.skipMedia = e.messagesOnly
	saveState(dir, exportState{Phone: phone, QuickTest: e.testMode, Redact: e.redact, NoMedia: e.messagesOnly, Created: time.Now()})
	logPipeline.Infof("Export folder %s: %d chats to write", dir, len(plan.groups))
	if !e.redact {
		return plan, nil
	}
	book := newOwnerBook(phone, names.ownName)
	book.learnContacts(names)
	plan.namer = recordingNamer{inner: names, book: book}
	e.people = book
	e.redaction = newRedaction(ctx, e.ai, book, dir, phone)
	e.redaction.media.replaceAudio = true
	e.redaction.media.transcribed = e.transcriber != nil
	plan.onChat = e.redaction.dispatcher.add
	plan.onFile = func(_, path string) { e.redaction.media.submit(path) }
	logPipeline.Infof("Redaction on: %d known people, AI %v", len(book.Persons), e.ai != nil)
	return plan, nil
}

func (e *exporter) transcribe(ctx context.Context, m storedMessage) {
	if e.transcriber == nil {
		return
	}
	media, ok := findMedia(m.Message)
	if !ok || media.kind != kindAudio {
		return
	}
	path := filepath.Join(e.dir, "media", localMediaName(m, media.ext))
	if _, err := os.Stat(path); err != nil {
		return
	}
	e.transcriber.transcribeFile(ctx, path, m.Message.GetAudioMessage().GetSeconds())
}

func (e *exporter) redactionStatus() string {
	if e.redaction == nil {
		return ""
	}
	done, queued := e.redaction.dispatcher.progress()
	status := fmt.Sprintf("Checking personal details: %s of %s chats done", formatCount(int(done)), formatCount(int(queued)))
	if e.transcriber != nil {
		status += fmt.Sprintf(" · %s voice notes transcribed", formatCount(int(e.transcriber.done.Load())))
	}
	if mDone, mQueued := e.redaction.media.progress(); mQueued > 0 {
		status += fmt.Sprintf(" · %s of %s photos and files", formatCount(int(mDone)), formatCount(int(mQueued)))
	}
	return status
}

type chatWriter struct {
	ch   chan *chatGroup
	done chan struct{}
	mu   sync.Mutex
	err  error
}

func newChatWriter(ctx context.Context, plan *exportPlan) *chatWriter {
	w := &chatWriter{ch: make(chan *chatGroup, len(plan.groups)+1), done: make(chan struct{})}
	go func() {
		defer close(w.done)
		for g := range w.ch {
			if err := plan.writeChat(ctx, g); err != nil {
				logExport.Errorf("Writing chat %s failed: %v", g.key, err)
				w.mu.Lock()
				if w.err == nil {
					w.err = err
				}
				w.mu.Unlock()
			}
		}
	}()
	return w
}

func (w *chatWriter) enqueue(g *chatGroup) {
	w.ch <- g
}

func (w *chatWriter) close() error {
	close(w.ch)
	<-w.done
	return w.err
}

func (e *exporter) downloadMediaInto(ctx context.Context, t *terminal, plan *exportPlan) error {
	view := newProgressView(t, "Downloading media")
	view.extra = e.redactionStatus
	downloader := newMediaDownloader(e.cli, e.arc, filepath.Join(e.dir, "media"), e.retries, e.phone, e.log.Sub("Media"), e.col.loggedOut)
	queue, err := downloader.pending(ctx, e.onlyChats)
	if err != nil {
		return err
	}
	order := map[string]int{}
	for i, g := range plan.groups {
		order[g.key] = i
	}
	position := func(m storedMessage) int {
		if g := plan.groupOf(m.ChatJID); g != nil {
			return order[g.key]
		}
		return len(order)
	}
	sort.SliceStable(queue, func(a, b int) bool { return position(queue[a]) < position(queue[b]) })
	remaining := map[string]int{}
	for _, m := range queue {
		if g := plan.groupOf(m.ChatJID); g != nil {
			remaining[g.key]++
		}
	}
	writer := newChatWriter(ctx, plan)
	ready := 0
	for _, g := range plan.groups {
		if remaining[g.key] == 0 {
			writer.enqueue(g)
			ready++
		}
	}
	logPipeline.Infof("Media queue: %d files across %d chats; %d chats have nothing to download and are written now", len(queue), len(remaining), ready)
	var mu sync.Mutex
	onDone := func(m storedMessage) {
		e.transcribe(ctx, m)
		g := plan.groupOf(m.ChatJID)
		if g == nil {
			return
		}
		mu.Lock()
		remaining[g.key]--
		left := remaining[g.key]
		mu.Unlock()
		if left == 0 {
			logPipeline.Debugf("All media for chat %s is in; writing it", g.key)
			writer.enqueue(g)
		}
	}
	if len(queue) == 0 {
		writeErr := writer.close()
		view.finish("nothing new to download")
		return writeErr
	}
	err = downloader.run(ctx, queue, func(r progressReport) {
		view.update(r.fraction, r.known, r.stalled)
	}, onDone)
	writeErr := writer.close()
	if err != nil {
		view.abandon()
		return err
	}
	if writeErr != nil {
		view.abandon()
		return writeErr
	}
	view.finish("")
	return nil
}

type outputJob struct {
	dir          string
	outDir       string
	phone        string
	test         bool
	messagesOnly bool
	redaction    *redactionRun
}

func waitWithProgress(t *terminal, label string, work func() error, fraction func() (float64, bool), extra func() string) error {
	view := newProgressView(t, label)
	view.extra = extra
	done := make(chan error, 1)
	go func() { done <- work() }()
	ticker := time.NewTicker(progressTick)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			if err != nil {
				view.abandon()
				return err
			}
			view.finish("")
			return nil
		case <-ticker.C:
			f, known := fraction()
			view.update(f, known, false)
		}
	}
}

func (r *redactionRun) fraction() (float64, bool) {
	done, queued := r.dispatcher.progress()
	mDone, mQueued := int64(0), int64(0)
	if r.media != nil {
		mDone, mQueued = r.media.progress()
	}
	total := queued*4 + mQueued
	if total == 0 {
		return 0, false
	}
	return float64(done*4+mDone) / float64(total), true
}

func writeOutputs(ctx context.Context, t *terminal, job outputJob) (string, error) {
	if job.redaction != nil {
		return writeRedactedOutput(ctx, t, job)
	}
	path := job.zipPath(zipFileName(job.phone, time.Now(), job.test))
	entries, err := folderEntries(job.dir)
	if err != nil {
		return "", err
	}
	logZip.Infof("ZIP %s: %d files", filepath.Base(path), len(entries))
	var zipped float64
	var zipMu sync.Mutex
	err = waitWithProgress(t, "Creating the ZIP", func() error {
		return writeZip(path, entries, addDebugLog, func(f float64) {
			zipMu.Lock()
			zipped = f
			zipMu.Unlock()
		})
	}, func() (float64, bool) {
		zipMu.Lock()
		defer zipMu.Unlock()
		return zipped, true
	}, nil)
	return path, err
}

func writeRedactedOutput(ctx context.Context, t *terminal, job outputJob) (string, error) {
	run := job.redaction
	err := waitWithProgress(t, "Checking personal details", func() error {
		run.finish(ctx)
		return ctx.Err()
	}, run.fraction, nil)
	if err != nil {
		return "", err
	}
	path := job.zipPath(redactedZipName(time.Now(), job.test))
	var totals redactionTotals
	err = waitWithProgress(t, "Saving the redacted ZIP", func() error {
		var werr error
		totals, werr = run.writeRedactedZip(path, nil)
		return werr
	}, func() (float64, bool) { return 0, false }, nil)
	if err != nil {
		return "", fmt.Errorf("could not save the redacted ZIP: %w", err)
	}
	logRedact.Infof("Redacted ZIP %s: %d chats, %d slots, %d flagged, %d rebuilt, %d whole-body, media %+v", filepath.Base(path), totals.Chats, totals.Slots, totals.Flagged, totals.Rebuilt, totals.WholeBody, totals.Media)
	return path, nil
}

func (job outputJob) zipPath(name string) string {
	if job.messagesOnly {
		name = withNameSuffix(name, messagesOnlySuffix)
	}
	return availablePath(filepath.Join(job.outDir, name))
}

func (e *exporter) completeExport(ctx context.Context, t *terminal, plan *exportPlan, outDir, phone string, started time.Time) error {
	var summary exportSummary
	err := waitWithProgress(t, "Saving your export", func() error {
		if err := plan.writeRemaining(ctx, nil); err != nil {
			return err
		}
		var err error
		summary, err = plan.finish()
		return err
	}, func() (float64, bool) { return 0, false }, e.redactionStatus)
	if err != nil {
		return fmt.Errorf("could not save the export: %w", err)
	}
	saveState(plan.dir, exportState{Phone: phone, QuickTest: e.testMode, Redact: e.redact, NoMedia: e.messagesOnly, Created: time.Now(), FolderDone: true})
	logPipeline.Infof("Export folder complete: %+v", summary)
	if e.people != nil {
		_ = e.people.save(filepath.Join(plan.dir, peopleFileName))
	}
	if e.transcriber != nil {
		logTranscribe.Infof("Transcription finished: %s", e.transcriber.summary())
	}
	if !e.testMode {
		e.cleanUp(ctx, t)
		t.say("")
	}
	zipPath, err := writeOutputs(ctx, t, outputJob{dir: plan.dir, outDir: outDir, phone: phone, test: e.testMode, messagesOnly: e.messagesOnly, redaction: e.redaction})
	if err != nil {
		logPipeline.Errorf("Saving the ZIP failed: %v", err)
		return fmt.Errorf("could not save the export: %w", err)
	}
	e.publish(zipPath)
	e.removeExportFolder(t, plan.dir)
	e.showSummary(t, summary, e.savedZip, time.Since(started))
	return nil
}

var previousTestZip = regexp.MustCompile(`^(?:\d+-\d{2}-\d{2}-test(?:-messages-only)?(?:-\d+)?|WhatsApp-\d{2}-\d{2}-test-redacted(?:-messages-only)?(?:-\d+)?)\.zip$`)

func deletePreviousTests(keep map[string]bool, dirs ...string) int {
	removed := 0
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			if entry.IsDir() || !previousTestZip.MatchString(entry.Name()) || keep[path] {
				continue
			}
			if err := os.Remove(path); err != nil {
				logPipeline.Warnf("Couldn't delete the old test ZIP %s: %v", path, err)
				continue
			}
			logPipeline.Infof("Deleted the old test ZIP %s", path)
			removed++
		}
	}
	return removed
}

func (e *exporter) publish(zipPath string) {
	if e.testMode {
		deletePreviousTests(map[string]bool{zipPath: true}, e.saveDir, filepath.Dir(zipPath))
	}
	e.savedZip = e.moveToSaveFolder(zipPath)
	if e.redaction != nil {
		if reason := e.redaction.r.aiStopReason(); reason != "" {
			e.notes = append(e.notes, "The AI check stopped early, so the rest used local checks only, which miss more: "+reason)
		}
	}
}

func addDebugLog(zw *zip.Writer) error {
	if !debugLogEnabled() {
		return nil
	}
	logZip.Infof("Adding %s to the ZIP", debugFileName)
	return addBytes(zw, debugFileName, debugLogContents(), time.Now(), true)
}

func (e *exporter) removeExportFolder(t *terminal, dir string) {
	err := os.RemoveAll(dir)
	_ = os.Remove(filepath.Dir(dir))
	if err != nil {
		logPipeline.Warnf("Couldn't delete the export folder %s: %v", dir, err)
		if e.redact {
			e.notes = append(e.notes, "Some original files couldn't be deleted. Delete this folder yourself: "+dir)
		}
		return
	}
	if e.redact && !e.stillWorking() {
		t.say("  %s Original messages and files deleted", t.paint(ansiGreen, glyphs.check))
	}
}

type finishedExport struct {
	dir   string
	state exportState
}

func findFinished(root string) []finishedExport {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var found []finishedExport
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), finishedPrefix) {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		state, ok := loadState(dir)
		if !ok || !state.FolderDone {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, strings.TrimPrefix(entry.Name(), finishedPrefix), "archive.db")); err == nil {
			continue
		}
		found = append(found, finishedExport{dir: dir, state: state})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].state.Created.After(found[j].state.Created) })
	return found
}

func summaryFromIndex(dir string) exportSummary {
	var s exportSummary
	index := readIndex(dir)
	for _, row := range index.rows {
		if len(row) < 11 {
			continue
		}
		s.Chats++
		if row[2] == "Group" {
			s.Groups++
		} else {
			s.Individuals++
		}
		if row[5] == "Yes" {
			s.Archived++
		}
		n, _ := strconv.Atoi(strings.ReplaceAll(row[6], ",", ""))
		s.Messages += n
		a, _ := strconv.Atoi(row[9])
		s.Attachments += a
		m, _ := strconv.Atoi(row[10])
		s.Missing += m
	}
	return s
}

func finishFromFolder(ctx context.Context, t *terminal, f finishedExport, outDir, saveDir string, desktopBlocked bool, ai *haikuClient, keepFolder bool) (*exporter, error) {
	started := time.Now()
	e := &exporter{saveDir: saveDir, desktopBlocked: desktopBlocked, testMode: f.state.QuickTest, redact: f.state.Redact, messagesOnly: f.state.NoMedia, ai: ai}
	logPipeline.Infof("Finishing a saved export folder %s (redact %v, AI %v)", f.dir, f.state.Redact, ai != nil)
	if f.state.Redact {
		book, err := loadPeopleBook(filepath.Join(f.dir, peopleFileName))
		if err != nil {
			book = newOwnerBook(f.state.Phone, "")
		}
		e.people = book
		e.redaction = newRedaction(ctx, ai, book, f.dir, f.state.Phone)
		for _, folder := range chatFolders(f.dir, readIndex(f.dir)) {
			files, _ := os.ReadDir(filepath.Join(f.dir, folder))
			for _, file := range files {
				if !file.IsDir() {
					e.redaction.media.submit(filepath.Join(f.dir, folder, file.Name()))
				}
			}
		}
	}
	summary := summaryFromIndex(f.dir)
	zipPath, err := writeOutputs(ctx, t, outputJob{dir: f.dir, outDir: outDir, phone: f.state.Phone, test: f.state.QuickTest, messagesOnly: f.state.NoMedia, redaction: e.redaction})
	if err != nil {
		return e, err
	}
	e.publish(zipPath)
	if !keepFolder {
		e.removeExportFolder(t, f.dir)
	}
	e.showSummary(t, summary, e.savedZip, time.Since(started))
	return e, nil
}
