package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

const (
	historyBatchSize      = 50
	backfillWorkers       = 4
	backfillReplyTimeout  = 30 * time.Second
	historyIdleAfterDone  = 20 * time.Second
	historyIdleUnknown    = 90 * time.Second
	historyNoStartStall   = 40 * time.Second
	historyGiveUpAfter    = 15 * time.Minute
	reconnectSettleWindow = 20 * time.Second
)

var (
	errLoggedOut         = errors.New("this computer was unlinked from the phone")
	errNoBackfillAnswer  = errors.New("the phone did not answer the request for older messages")
	errNoHistoryReceived = errors.New("no chat history arrived from the phone")
)

type progressReport struct {
	fraction float64
	known    bool
	stalled  bool
	status   string
}

type syncProgress struct {
	seen        bool
	bootstrap   uint32
	recent      uint32
	full        uint32
	fullSeen    bool
	lastHistory time.Time
	received    int
}

func (s syncProgress) best() uint32 {
	if s.fullSeen {
		return s.full
	}
	return max(s.bootstrap, s.recent)
}

func (s syncProgress) status() string {
	if !s.seen {
		return "Waiting for your phone to start sending history"
	}
	return fmt.Sprintf("Receiving history from your phone · %s messages so far", formatCount(s.received))
}

func (s syncProgress) fraction() (float64, bool) {
	if s.fullSeen {
		return 0.3 + 0.7*float64(s.full)/100, true
	}
	if p := max(s.bootstrap, s.recent); p > 0 {
		return 0.3 * float64(p) / 100, true
	}
	return 0, false
}

type collector struct {
	cli     *whatsmeow.Client
	arc     *archive
	log     waLog.Logger
	retries *mediaRetries
	phone   *phoneWatch

	connected     chan struct{}
	loggedOut     chan struct{}
	loggedOutOnce sync.Once

	fetchedOlder atomic.Int64

	mu           sync.Mutex
	sync         syncProgress
	offlineDone  bool
	businessName string
	waiters      map[string]chan int
}

func newCollector(cli *whatsmeow.Client, arc *archive, log waLog.Logger, retries *mediaRetries, phone *phoneWatch) *collector {
	return &collector{
		cli:       cli,
		arc:       arc,
		log:       log,
		retries:   retries,
		phone:     phone,
		connected: make(chan struct{}, 1),
		loggedOut: make(chan struct{}),
		waiters:   map[string]chan int{},
	}
}

func isExportableChat(jid types.JID) bool {
	switch jid.Server {
	case types.DefaultUserServer, types.HiddenUserServer, types.GroupServer:
		return jid.User != ""
	}
	return false
}

func (c *collector) handle(rawEvent any) {
	switch evt := rawEvent.(type) {
	case *events.HistorySync:
		c.phone.touch()
		c.onHistorySync(evt)
	case *events.Message:
		c.onLiveMessage(evt)
	case *events.MediaRetry:
		c.phone.touch()
		c.retries.deliver(evt)
	case *events.Connected:
		select {
		case c.connected <- struct{}{}:
		default:
		}
	case *events.OfflineSyncCompleted:
		c.mu.Lock()
		c.offlineDone = true
		c.mu.Unlock()
	case *events.PairSuccess:
		c.mu.Lock()
		c.businessName = evt.BusinessName
		c.mu.Unlock()
	case *events.LoggedOut:
		c.loggedOutOnce.Do(func() { close(c.loggedOut) })
	}
}

func (c *collector) onHistorySync(evt *events.HistorySync) {
	ctx := context.Background()
	data := evt.Data
	syncType := data.GetSyncType()
	totalAdded := 0
	for _, conv := range data.GetConversations() {
		chatJID, err := types.ParseJID(conv.GetID())
		if err != nil || !isExportableChat(chatJID) {
			continue
		}
		record := chatRecord{
			JID:          chatJID.String(),
			Name:         firstNonEmpty(conv.GetName(), conv.GetDisplayName()),
			PN:           conv.GetPnJID(),
			LID:          conv.GetLidJID(),
			Archived:     conv.GetArchived(),
			EndOfHistory: conv.GetEndOfHistoryTransfer(),
		}
		if err := c.arc.upsertChat(ctx, record); err != nil {
			c.log.Errorf("Failed to save chat %s: %v", record.JID, err)
		}
		batch := make([]storedMessage, 0, len(conv.GetMessages()))
		for _, item := range conv.GetMessages() {
			if msg, ok := c.fromWebMessage(chatJID, item.GetMessage()); ok {
				batch = append(batch, msg)
			}
		}
		added, err := c.arc.insertMessages(ctx, batch)
		if err != nil {
			c.log.Errorf("Failed to save %d messages for %s: %v", len(batch), record.JID, err)
		}
		totalAdded += added
		if syncType == waHistorySync.HistorySync_ON_DEMAND {
			c.notifyWaiters(added, record.JID, record.PN, record.LID)
		}
	}
	progress := data.GetProgress()
	c.mu.Lock()
	c.sync.seen = true
	c.sync.lastHistory = time.Now()
	if syncType != waHistorySync.HistorySync_ON_DEMAND {
		c.sync.received += totalAdded
	}
	switch syncType {
	case waHistorySync.HistorySync_INITIAL_BOOTSTRAP:
		c.sync.bootstrap = max(c.sync.bootstrap, progress)
	case waHistorySync.HistorySync_RECENT:
		c.sync.recent = max(c.sync.recent, progress)
	case waHistorySync.HistorySync_FULL:
		c.sync.fullSeen = true
		c.sync.full = max(c.sync.full, progress)
	}
	c.mu.Unlock()
	c.log.Infof("History sync %s: %d conversations, %d new messages, progress %d", syncType, len(data.GetConversations()), totalAdded, progress)
}

func (c *collector) fromWebMessage(chat types.JID, web *waWeb.WebMessageInfo) (storedMessage, bool) {
	if web == nil || (web.GetMessage() == nil && web.GetMessageStubType() == 0) {
		return storedMessage{}, false
	}
	evt, err := c.cli.ParseWebMessage(chat, web)
	if err != nil {
		c.log.Debugf("Keeping history message %s in %s without a known sender: %v", web.GetKey().GetID(), chat, err)
		evt = &events.Message{
			RawMessage:   web.GetMessage(),
			SourceWebMsg: web,
			Info: types.MessageInfo{
				MessageSource: types.MessageSource{
					Chat:     chat,
					IsFromMe: web.GetKey().GetFromMe(),
					IsGroup:  chat.Server == types.GroupServer,
				},
				ID:        web.GetKey().GetID(),
				PushName:  web.GetPushName(),
				Timestamp: time.Unix(int64(web.GetMessageTimestamp()), 0),
			},
		}
	}
	evt.UnwrapRaw()
	msg := storedFromEvent(evt)
	msg.StubType = int32(web.GetMessageStubType())
	msg.StubParams = web.GetMessageStubParameters()
	if msg.ID == "" || (msg.Message == nil && msg.StubType == 0) {
		return storedMessage{}, false
	}
	return msg, true
}

func (c *collector) onLiveMessage(evt *events.Message) {
	if !isExportableChat(evt.Info.Chat) {
		return
	}
	ctx := context.Background()
	msg := storedFromEvent(evt)
	if msg.Message == nil {
		return
	}
	if err := c.arc.ensureChat(ctx, msg.ChatJID); err != nil {
		c.log.Errorf("Failed to save chat %s: %v", msg.ChatJID, err)
	}
	if _, err := c.arc.insertMessages(ctx, []storedMessage{msg}); err != nil {
		c.log.Errorf("Failed to save live message %s: %v", msg.ID, err)
	}
}

func storedFromEvent(evt *events.Message) storedMessage {
	info := evt.Info
	msg := storedMessage{
		ChatJID:      info.Chat.ToNonAD().String(),
		ID:           info.ID,
		SenderJID:    info.Sender.ToNonAD().String(),
		SenderAltJID: info.SenderAlt.ToNonAD().String(),
		FromMe:       info.IsFromMe,
		IsGroup:      info.IsGroup,
		Timestamp:    info.Timestamp,
		PushName:     info.PushName,
		Message:      evt.Message,
	}
	if _, ok := findMedia(evt.Message); ok {
		msg.MediaState = mediaPending
	}
	return msg
}

func (c *collector) registerWaiter(chatJID string) chan int {
	ch := make(chan int, 1)
	c.mu.Lock()
	c.waiters[chatJID] = ch
	c.mu.Unlock()
	return ch
}

func (c *collector) removeWaiter(chatJID string) {
	c.mu.Lock()
	delete(c.waiters, chatJID)
	c.mu.Unlock()
}

func (c *collector) notifyWaiters(added int, jids ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, jid := range jids {
		if ch, ok := c.waiters[jid]; ok && jid != "" {
			select {
			case ch <- added:
			default:
			}
		}
	}
}

func (c *collector) syncState() (syncProgress, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sync, c.offlineDone
}

func (c *collector) linkedBusinessName() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.businessName
}

func (c *collector) waitForHistory(ctx context.Context, freshLink bool, report func(progressReport)) error {
	start := time.Now()
	ticker := time.NewTicker(progressTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.loggedOut:
			return errLoggedOut
		case <-ticker.C:
		}
		state, offlineDone := c.syncState()
		idle := time.Since(state.lastHistory)
		best := state.best()
		fraction, known := state.fraction()
		stalled := (freshLink && !state.seen && time.Since(start) > historyNoStartStall) ||
			(state.seen && best > 0 && best < 100 && idle > stallThreshold)
		report(progressReport{fraction: fraction, known: known, stalled: stalled, status: state.status()})

		switch {
		case state.seen && best >= 100 && idle > historyIdleAfterDone:
			return nil
		case state.seen && best == 0 && idle > historyIdleUnknown:
			return nil
		case state.seen && idle > historyGiveUpAfter:
			return nil
		case !state.seen && !freshLink && offlineDone && time.Since(start) > reconnectSettleWindow:
			return nil
		case !state.seen && time.Since(start) > historyGiveUpAfter:
			if messages, _ := c.arc.counts(ctx); messages > 0 {
				return nil
			}
			return errNoHistoryReceived
		}
	}
}

func (c *collector) backfill(ctx context.Context, only map[string]bool, report func(progressReport)) (complete bool, err error) {
	chats, err := c.arc.chatsNeedingBackfill(ctx)
	if err != nil {
		return false, err
	}
	chats = keepSelected(chats, only, func(jid string) string { return jid })
	if len(chats) == 0 {
		return true, nil
	}
	var checked, unfinished, started atomic.Int64
	c.fetchedOlder.Store(0)
	jobs := make(chan string)
	var wg sync.WaitGroup
	for range backfillWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for chat := range jobs {
				started.Add(1)
				if !c.backfillChat(ctx, chat) {
					unfinished.Add(1)
				}
				checked.Add(1)
			}
		}()
	}
	stopReports := every(ctx, progressTick, func() {
		report(progressReport{
			fraction: float64(checked.Load()) / float64(len(chats)),
			known:    true,
			stalled:  c.phone.stalled(),
			status:   backfillStatus(int(started.Load()), len(chats), int(c.fetchedOlder.Load())),
		})
	})
feed:
	for _, chat := range chats {
		select {
		case jobs <- chat:
		case <-ctx.Done():
			break feed
		case <-c.loggedOut:
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	stopReports()
	select {
	case <-c.loggedOut:
		return false, errLoggedOut
	default:
	}
	return unfinished.Load() == 0, ctx.Err()
}

func (c *collector) backfillChat(ctx context.Context, chatJID string) bool {
	for ctx.Err() == nil {
		oldest, err := c.arc.oldestMessage(ctx, chatJID)
		if err != nil || oldest == nil {
			return err == nil
		}
		sentAt := time.Now()
		added, err := c.requestOlder(ctx, oldest)
		switch {
		case err == nil && added == 0:
			c.markBackfillDone(ctx, chatJID)
			return true
		case err == nil:
			c.fetchedOlder.Add(int64(added))
			continue
		case errors.Is(err, errNoBackfillAnswer) && c.phone.repliedSince(sentAt):
			c.log.Infof("No older messages offered for %s", chatJID)
			c.markBackfillDone(ctx, chatJID)
			return true
		case errors.Is(err, errNoBackfillAnswer):
			if !c.phone.waitForActivity(ctx, sentAt) {
				c.log.Warnf("Gave up waiting for the phone while fetching older messages for %s", chatJID)
				return false
			}
		default:
			c.log.Warnf("Stopped fetching older messages for %s: %v", chatJID, err)
			return false
		}
	}
	return false
}

func backfillStatus(started, total, fetched int) string {
	status := fmt.Sprintf("Checking older messages: chat %s of %s", formatCount(min(max(started, 1), total)), formatCount(total))
	if fetched > 0 {
		status += fmt.Sprintf(" · %s older messages found", formatCount(fetched))
	}
	return status
}

func (c *collector) markBackfillDone(ctx context.Context, chatJID string) {
	if err := c.arc.markBackfillDone(ctx, chatJID); err != nil {
		c.log.Errorf("Failed to mark %s as complete: %v", chatJID, err)
	}
}

func (c *collector) requestOlder(ctx context.Context, oldest *types.MessageInfo) (int, error) {
	chatJID := oldest.Chat.String()
	reply := c.registerWaiter(chatJID)
	defer c.removeWaiter(chatJID)
	done := c.phone.begin()
	defer done()
	if _, err := c.cli.SendPeerMessage(ctx, c.cli.BuildHistorySyncRequest(oldest, historyBatchSize)); err != nil {
		return 0, err
	}
	select {
	case added := <-reply:
		return added, nil
	case <-time.After(backfillReplyTimeout):
		return 0, errNoBackfillAnswer
	case <-c.loggedOut:
		return 0, errLoggedOut
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func every(ctx context.Context, interval time.Duration, fn func()) func() {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				fn()
			case <-done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return func() {
		close(done)
		wg.Wait()
		fn()
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
