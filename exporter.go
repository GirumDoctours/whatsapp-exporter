package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"google.golang.org/protobuf/proto"
)

const (
	linkedDeviceName   = "WhatsApp Exporter"
	connectTimeout     = 2 * time.Minute
	qrFullExtraRows    = 9
	qrCompactExtraRows = 1
	qrSizeCheckEvery   = 400 * time.Millisecond
)

var (
	errLinkCancelled = errors.New("linking was cancelled")
	errWrongNumber   = errors.New("a different number was linked")
)

type exporter struct {
	dir            string
	log            *logger
	logFile        io.Closer
	arc            *archive
	container      *sqlstore.Container
	cli            *whatsmeow.Client
	col            *collector
	retries        *mediaRetries
	phone          *phoneWatch
	notes          []string
	testMode       bool
	historyReady   bool
	onlyChats      map[string]bool
	savedZip       string
	saveDir        string
	desktopBlocked bool
	closed         bool
	redact         bool
	messagesOnly   bool
	ai             *haikuClient
	transcriber    *transcriber
	people         *peopleBook
	redaction      *redactionRun
}

func configureDeviceProps() {
	store.SetOSInfo(linkedDeviceName, [3]uint32{1, 0, 0})
	store.DeviceProps.PlatformType = waCompanionReg.DeviceProps_DESKTOP.Enum()
	store.DeviceProps.RequireFullSync = proto.Bool(true)
	history := store.DeviceProps.HistorySyncConfig
	history.FullSyncDaysLimit = proto.Uint32(36500)
	history.FullSyncSizeMbLimit = proto.Uint32(102400)
	history.StorageQuotaMb = proto.Uint32(102400)
	history.OnDemandReady = proto.Bool(true)
}

func openExporter(dir string) (*exporter, error) {
	configureDeviceProps()
	if err := os.MkdirAll(filepath.Join(dir, "media"), 0o700); err != nil {
		return nil, err
	}
	logFile, err := attachLogFile(filepath.Join(dir, "exporter.log"))
	if err != nil {
		return nil, err
	}
	log := appLog
	e := &exporter{dir: dir, log: log, logFile: logFile}
	log.Infof("Opening working folder %s", dir)
	if e.arc, err = openArchive(filepath.Join(dir, "archive.db")); err != nil {
		e.close()
		return nil, err
	}
	ctx := context.Background()
	if e.container, err = sqlstore.New(ctx, "sqlite", sqliteDSN(filepath.Join(dir, "session.db")), log.Sub("WA/Store")); err != nil {
		e.close()
		return nil, err
	}
	device, err := e.container.GetFirstDevice(ctx)
	if err != nil {
		e.close()
		return nil, err
	}
	e.cli = whatsmeow.NewClient(device, log.Sub("WA/Client"))
	e.cli.EnableAutoReconnect = true
	e.retries = newMediaRetries()
	e.phone = newPhoneWatch()
	e.col = newCollector(e.cli, e.arc, log.Sub("Collector"), e.retries, e.phone)
	e.cli.AddEventHandler(e.col.handle)
	return e, nil
}

func (e *exporter) close() {
	if e.closed {
		return
	}
	e.closed = true
	if e.cli != nil {
		e.cli.Disconnect()
	}
	if e.container != nil {
		_ = e.container.Close()
	}
	if e.arc != nil {
		_ = e.arc.close()
	}
	if e.logFile != nil {
		_ = e.logFile.Close()
	}
}

func (e *exporter) linked() bool {
	return e.cli != nil && e.cli.Store.ID != nil
}

func (e *exporter) run(ctx context.Context, t *terminal, outDir string) error {
	started := time.Now()
	if err := e.connect(ctx, t); err != nil {
		return err
	}
	phone := e.cli.Store.ID.User
	if known := e.arc.meta(ctx, "phone"); known != "" && known != phone {
		return fmt.Errorf("%w: this export belongs to +%s but +%s was linked", errWrongNumber, known, phone)
	}
	_ = e.arc.setMeta(ctx, "phone", phone)
	_ = e.arc.setMeta(ctx, "last_run", time.Now().Format(time.RFC3339))
	e.log.Infof("Linked to +%s (quick test %v, messages only %v, redact %v, AI %v)", phone, e.testMode, e.messagesOnly, e.redact, e.ai != nil)
	if name := e.col.linkedBusinessName(); name != "" {
		_ = e.arc.setMeta(ctx, "business_name", name)
	}

	t.clearScreen()
	t.banner()
	t.say("  %s Linked to %s", t.paint(ansiGreen, glyphs.check), t.paint(ansiBold, "+"+phone))
	t.say("  %s", t.paint(ansiDim, "Keep WhatsApp open on your phone, with the screen unlocked, until this finishes."))
	if e.testMode {
		t.say("  %s", t.paint(ansiYellow, fmt.Sprintf("Quick test: only your %d most recent chats will be exported.", testChatLimit)))
	}
	t.say("")

	if err := e.exportChats(ctx, t, outDir, phone, started); err != nil || !e.testMode {
		return err
	}
	if !e.wantsFullRun(ctx, t) {
		e.cleanUp(ctx, t)
		return nil
	}
	e.switchToFullRun(ctx)
	t.say("")
	t.say("  %s", t.paint(ansiBold, "Full run"))
	t.say("  %s", t.paint(ansiDim, "Keep WhatsApp open on your phone, with the screen unlocked, until this finishes."))
	t.say("")
	return e.exportChats(ctx, t, outDir, phone, time.Now())
}

func (e *exporter) wantsFullRun(ctx context.Context, t *terminal) bool {
	yes := t.confirm("The quick test is done. Do you want to do a full run now?",
		"Yes, export everything", "same choices as the test · no need to link again",
		"No, exit", "unlinks from your phone and deletes the working files")
	yes = yes && ctx.Err() == nil
	e.log.Infof("Full run after the quick test: %v", yes)
	return yes
}

func (e *exporter) switchToFullRun(ctx context.Context) {
	e.testMode = false
	e.onlyChats = nil
	e.notes = nil
	e.historyReady = true
	if e.ai != nil {
		e.ai = e.ai.withFreshUsage()
	}
	_ = e.arc.setMeta(ctx, quickTestMeta, strconv.FormatBool(false))
	_ = e.arc.setMeta(ctx, "last_run", time.Now().Format(time.RFC3339))
}

func (e *exporter) stillWorking() bool {
	return e.arc != nil && !e.closed
}

func (e *exporter) exportChats(ctx context.Context, t *terminal, outDir, phone string, started time.Time) error {
	t.hideCursor()
	defer t.showCursor()

	done := e.log.timed("Downloading messages")
	if err := e.downloadMessages(ctx, t); err != nil {
		e.log.Errorf("Downloading messages stopped: %v", err)
		return err
	}
	messages, chats := e.arc.counts(ctx)
	done("%d messages in %d chats", messages, chats)
	plan, err := e.prepareFolder(ctx, phone)
	if err != nil {
		e.log.Errorf("Preparing the export folder failed: %v", err)
		return fmt.Errorf("could not save the export: %w", err)
	}
	if e.messagesOnly {
		e.log.Infof("Messages only: skipping media downloads")
	} else {
		done := e.log.timed("Downloading media")
		if err := e.downloadMediaInto(ctx, t, plan); err != nil {
			e.log.Errorf("Downloading media stopped: %v", err)
			return err
		}
		done("")
	}
	return e.completeExport(ctx, t, plan, outDir, phone, started)
}

func (e *exporter) moveToSaveFolder(zipPath string) string {
	if e.saveDir == "" {
		if e.desktopBlocked {
			e.notes = append(e.notes, "Your Desktop couldn't be used, so the ZIP was saved in the folder above instead.")
		}
		return zipPath
	}
	moved, err := moveInto(zipPath, e.saveDir)
	if err == nil {
		logPipeline.Infof("Moved %s to %s", filepath.Base(zipPath), e.saveDir)
	} else {
		logPipeline.Warnf("Moving the ZIP to %s failed: %v", e.saveDir, err)
		e.notes = append(e.notes, "The ZIP couldn't be moved to your Desktop, so it was saved in the folder above instead.")
	}
	return moved
}

func zipFileName(phone string, now time.Time, test bool) string {
	name := phone + "-" + now.Format("01-06")
	if test {
		name += "-test"
	}
	return name + ".zip"
}

func (e *exporter) downloadMessages(ctx context.Context, t *terminal) error {
	view := newProgressView(t, "Downloading messages")
	freshLink := e.arc.meta(ctx, "initial_sync_done") != "1"
	syncShare := 0.1
	if freshLink {
		syncShare = 0.8
	}
	if !e.historyReady {
		err := e.col.waitForHistory(ctx, freshLink, func(r progressReport) {
			view.update(r.fraction*syncShare, r.known, r.stalled)
		})
		if err != nil {
			view.abandon()
			return err
		}
	}
	_ = e.arc.setMeta(ctx, "initial_sync_done", "1")

	e.whileShowing(ctx, view, syncShare, func() { e.refreshChatDetails(ctx) })
	if e.testMode {
		if err := e.selectTestChats(ctx); err != nil {
			view.abandon()
			return err
		}
	}

	complete, err := e.col.backfill(ctx, e.onlyChats, func(r progressReport) {
		view.update(syncShare+(1-syncShare)*r.fraction, true, r.stalled)
	})
	if err != nil {
		view.abandon()
		return err
	}
	if !complete {
		e.notes = append(e.notes, "Some older messages couldn't be fetched because the phone stopped answering. Run the exporter again to pick them up.")
	}
	view.finish("")
	return nil
}

func (e *exporter) whileShowing(ctx context.Context, view *progressView, fraction float64, work func()) {
	stop := every(ctx, progressTick, func() { view.update(fraction, true, false) })
	work()
	stop()
}

func (e *exporter) ownName(ctx context.Context, phone string) string {
	return firstNonEmpty(e.cli.Store.PushName, e.arc.meta(ctx, "business_name"), "+"+phone)
}

func (e *exporter) showSummary(t *terminal, s exportSummary, zipPath string, elapsed time.Duration) {
	size := int64(0)
	if info, err := os.Stat(zipPath); err == nil {
		size = info.Size()
	}
	t.say("")
	t.say("  %s", t.paint(ansiGreen+ansiBold, "Your export is ready"))
	t.say("")
	rows := [][2]string{
		{"Chats", fmt.Sprintf("%s  (%s people · %s groups)", formatCount(s.Chats), formatCount(s.Individuals), formatCount(s.Groups))},
		{"Messages", formatCount(s.Messages)},
		{"Photos & files", formatCount(s.Attachments) + " saved"},
	}
	if s.Missing > 0 {
		rows = append(rows, [2]string{"Not available", formatCount(s.Missing) + " (listed in " + reportFileName + ")"})
	}
	rows = append(rows,
		[2]string{"ZIP size", humanBytes(size)},
		[2]string{"Time taken", humanDuration(elapsed)},
	)
	t.box(rows)
	t.say("")
	t.say("  %s  %s", t.paint(ansiDim, "Saved in"), filepath.Dir(zipPath))
	t.say("  %s      %s", t.paint(ansiDim, "File"), t.paint(ansiBold, filepath.Base(zipPath)))
	for _, note := range e.notes {
		t.say("")
		t.say("  %s", t.paint(ansiYellow, glyphs.warn+"  "+note))
	}
}

func humanBytes(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d bytes", n)
	}
	value := float64(n)
	for _, suffix := range []string{"KB", "MB", "GB", "TB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f PB", value/unit)
}

func (e *exporter) refreshChatDetails(ctx context.Context) {
	for _, name := range appstate.AllPatchNames {
		if err := e.cli.FetchAppState(ctx, name, false, true); err != nil {
			e.log.Warnf("Could not refresh %s: %v", name, err)
		}
	}
	groups, err := e.cli.GetJoinedGroups(ctx)
	if err != nil {
		e.log.Warnf("Could not load group names: %v", err)
		return
	}
	for _, group := range groups {
		if group.Name != "" {
			_ = e.arc.setChatName(ctx, group.JID.String(), group.Name)
		}
	}
}

func (e *exporter) cleanUp(ctx context.Context, t *terminal) {
	logoutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancel()
	logoutErr := e.cli.Logout(logoutCtx)
	if logoutErr != nil {
		e.log.Warnf("Logout failed: %v", logoutErr)
	}
	e.close()
	removeErr := os.RemoveAll(e.dir)
	if removeErr != nil {
		e.log.Warnf("Couldn't delete the working folder %s: %v", e.dir, removeErr)
	} else {
		e.log.Infof("Unlinked (error: %v) and deleted the working folder", logoutErr)
	}
	_ = os.Remove(filepath.Dir(e.dir))

	t.say("")
	if logoutErr != nil {
		t.say("  %s", t.paint(ansiYellow, glyphs.warn+"  Couldn't unlink automatically."))
		t.say("     On your phone, open Linked devices and remove \"%s\".", linkedDeviceName)
	} else {
		t.say("  %s Unlinked from your phone", t.paint(ansiGreen, glyphs.check))
	}
	if removeErr != nil {
		t.say("  %s", t.paint(ansiYellow, glyphs.warn+"  Couldn't delete the working files in "+e.dir))
	} else {
		t.say("  %s Working files deleted", t.paint(ansiGreen, glyphs.check))
	}
}

func (e *exporter) connect(ctx context.Context, t *terminal) error {
	label := "Reconnecting to WhatsApp"
	if e.cli.Store.ID == nil {
		if err := e.link(ctx, t); err != nil {
			return err
		}
		label = "Connecting to WhatsApp"
	} else if err := e.cli.ConnectContext(ctx); err != nil {
		return err
	}
	view := newProgressView(t, label)
	t.hideCursor()
	defer t.showCursor()
	deadline := time.After(connectTimeout)
	ticker := time.NewTicker(progressTick)
	defer ticker.Stop()
	for {
		select {
		case <-e.col.connected:
			view.abandon()
			return nil
		case <-e.col.loggedOut:
			view.abandon()
			return errLoggedOut
		case <-deadline:
			view.abandon()
			return errors.New("couldn't connect to WhatsApp. Check this computer's internet connection")
		case <-ctx.Done():
			view.abandon()
			return ctx.Err()
		case <-ticker.C:
			view.update(0, false, false)
		}
	}
}

func (e *exporter) link(ctx context.Context, t *terminal) error {
	method := t.choose("How would you like to link your phone?", []menuOption{
		{label: "Scan a QR code", hint: "quickest"},
		{label: "Use a pairing code instead", hint: "type an 8-character code on your phone"},
	})
	phone := ""
	if method == 1 {
		phone = askPhoneNumber(t)
	}
	for {
		err := e.pairOnce(ctx, t, phone)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		e.cli.Disconnect()
		t.say("")
		t.say("  %s", t.paint(ansiYellow, glyphs.warn+"  Linking didn't finish: "+err.Error()))
		again := t.choose("Try again?", []menuOption{{label: "Yes, try again"}, {label: "No, quit"}})
		if again == 1 {
			return errLinkCancelled
		}
	}
}

func (e *exporter) pairOnce(ctx context.Context, t *terminal, phone string) error {
	qrChan, err := e.cli.GetQRChannel(ctx)
	if err != nil {
		return err
	}
	if err := e.cli.ConnectContext(ctx); err != nil {
		return err
	}
	var qr *qrScreen
	if phone == "" {
		qr = startQRScreen(t)
		defer qr.stop()
	}
	codeRequested := false
	for item := range qrChan {
		switch item.Event {
		case whatsmeow.QRChannelEventCode:
			if qr != nil {
				qr.show(item.Code)
				continue
			}
			if codeRequested {
				continue
			}
			codeRequested = true
			code, err := e.cli.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, "Chrome ("+hostOSName()+")")
			if err != nil {
				return err
			}
			showPairingCode(t, code)
		case whatsmeow.QRChannelSuccess.Event:
			if qr != nil {
				qr.stop()
			}
			t.clearScreen()
			t.banner()
			t.say("  %s Phone linked", t.paint(ansiGreen, glyphs.check))
			return nil
		case whatsmeow.QRChannelTimeout.Event:
			return errors.New("the code expired before it was used")
		case whatsmeow.QRChannelEventError:
			return item.Error
		default:
			return fmt.Errorf("WhatsApp refused the link (%s)", item.Event)
		}
	}
	return errors.New("the connection closed while linking")
}

func hostOSName() string {
	switch runtime.GOOS {
	case "darwin":
		return "Mac OS"
	case "windows":
		return "Windows"
	}
	return "Linux"
}

func askPhoneNumber(t *terminal) string {
	t.say("")
	t.say("  %s", t.paint(ansiBold, "What's the phone number on that WhatsApp account?"))
	t.say("  %s", t.paint(ansiDim, "Include the country code, for example +44 7700 900123"))
	for {
		answer := t.ask("  " + t.paint(ansiGreen, glyphs.pointer+" "))
		var digits strings.Builder
		for _, r := range answer {
			if r >= '0' && r <= '9' {
				digits.WriteRune(r)
			}
		}
		number := strings.TrimPrefix(digits.String(), "00")
		if len(number) >= 8 && len(number) <= 15 {
			return number
		}
		t.say("  %s", t.paint(ansiYellow, "That doesn't look like a full number. Include the country code."))
	}
}

func screenHeader(t *terminal, w io.Writer, subtitle string) {
	fmt.Fprint(w, "\r\n  "+t.paint(ansiGreen+ansiBold, "WhatsApp Exporter")+t.paint(ansiDim, "  ·  "+subtitle)+"\r\n\r\n")
}

type qrScreen struct {
	t        *terminal
	mu       sync.Mutex
	code     string
	cols     int
	rows     int
	done     chan struct{}
	stopOnce sync.Once
}

func startQRScreen(t *terminal) *qrScreen {
	s := &qrScreen{t: t, done: make(chan struct{})}
	go s.followWindowSize()
	return s
}

func (s *qrScreen) show(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.code = code
	s.render(true)
}

func (s *qrScreen) stop() {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.code = ""
		close(s.done)
	})
}

func (s *qrScreen) followWindowSize() {
	ticker := time.NewTicker(qrSizeCheckEvery)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.mu.Lock()
			if cols, rows, _ := s.t.size(); s.code != "" && (cols != s.cols || rows != s.rows) {
				s.render(false)
			}
			s.mu.Unlock()
		}
	}
}

func (s *qrScreen) render(grow bool) {
	t := s.t
	art, artRows, artCols := qrArt(s.code)
	if grow {
		t.ensureSize(artRows+qrFullExtraRows, max(artCols, 80))
	}
	cols, rows, known := t.size()
	s.cols, s.rows = cols, rows
	layout := pickQRLayout(cols, rows, known, artRows, artCols)
	t.clearScreen()
	t.draw(func(w io.Writer) {
		switch layout {
		case qrFull:
			screenHeader(t, w, "Link your phone")
			fmt.Fprint(w, "  1. Open WhatsApp on your phone\r\n")
			fmt.Fprint(w, "  2. Tap "+t.paint(ansiBold, "Settings")+" (iPhone) or "+t.paint(ansiBold, glyphs.androidMenu)+" (Android), then "+t.paint(ansiBold, "Linked devices")+"\r\n")
			fmt.Fprint(w, "  3. Tap "+t.paint(ansiBold, "Link a device")+" and point your phone at this code\r\n\r\n")
			fmt.Fprint(w, art)
			fmt.Fprint(w, "\r\n  "+t.paint(ansiDim, "Waiting for you to scan… The code refreshes by itself."))
		case qrCompact:
			fmt.Fprint(w, art)
			fmt.Fprint(w, "  "+t.paint(ansiDim, "In WhatsApp, open Linked devices → Link a device and scan this code."))
		default:
			screenHeader(t, w, "Link your phone")
			fmt.Fprint(w, "  "+t.paint(ansiBold, "Make this window bigger to show the QR code.")+"\r\n\r\n")
			fmt.Fprint(w, "  Maximize the window, or press "+zoomOutKeys+" a few times\r\n")
			fmt.Fprint(w, "  to make the text smaller. The code appears as soon as it fits.")
		}
	})
}

type qrLayout int

const (
	qrFull qrLayout = iota
	qrCompact
	qrTooSmall
)

func pickQRLayout(cols, rows int, known bool, artRows, artCols int) qrLayout {
	switch {
	case !known:
		return qrFull
	case cols < artCols:
		return qrTooSmall
	case rows >= artRows+qrFullExtraRows:
		return qrFull
	case rows >= artRows+qrCompactExtraRows:
		return qrCompact
	}
	return qrTooSmall
}

func qrArt(code string) (art string, rows, cols int) {
	var b strings.Builder
	qrterminal.GenerateWithConfig(code, qrterminal.Config{
		Level:          qrterminal.L,
		Writer:         &indentWriter{w: &b, indent: "  "},
		HalfBlocks:     true,
		BlackChar:      qrterminal.BLACK_BLACK,
		WhiteBlackChar: qrterminal.WHITE_BLACK,
		WhiteChar:      qrterminal.WHITE_WHITE,
		BlackWhiteChar: qrterminal.BLACK_WHITE,
		QuietZone:      2,
	})
	art = b.String()
	lines := strings.Split(strings.TrimRight(art, "\r\n"), "\r\n")
	for _, line := range lines {
		cols = max(cols, utf8.RuneCountInString(line))
	}
	return art, len(lines), cols
}

func showPairingCode(t *terminal, code string) {
	spaced := strings.Join(strings.Split(code, ""), " ")
	t.clearScreen()
	t.draw(func(w io.Writer) {
		screenHeader(t, w, "Link your phone")
		fmt.Fprint(w, "  1. Open WhatsApp on your phone\r\n")
		fmt.Fprint(w, "  2. Go to "+t.paint(ansiBold, "Linked devices")+" and tap "+t.paint(ansiBold, "Link a device")+"\r\n")
		fmt.Fprint(w, "  3. Tap "+t.paint(ansiBold, "Link with phone number instead")+" and type this code:\r\n\r\n")
		fmt.Fprint(w, "        "+t.paint(ansiGreen+ansiBold, spaced)+"\r\n\r\n")
		fmt.Fprint(w, "  "+t.paint(ansiDim, "Waiting for your phone…")+"\r\n")
	})
}

type indentWriter struct {
	w       io.Writer
	indent  string
	midLine bool
}

func (iw *indentWriter) Write(p []byte) (int, error) {
	var b strings.Builder
	for _, c := range string(p) {
		if !iw.midLine {
			b.WriteString(iw.indent)
			iw.midLine = true
		}
		if c == '\n' {
			b.WriteString("\r\n")
			iw.midLine = false
			continue
		}
		b.WriteRune(c)
	}
	if _, err := io.WriteString(iw.w, b.String()); err != nil {
		return 0, err
	}
	return len(p), nil
}
