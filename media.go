package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waMmsRetry"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

const (
	mediaWorkers        = 6
	phoneRetrySlots     = 3
	phoneRetryTimeout   = 60 * time.Second
	singleDownloadLimit = 15 * time.Minute
	reasonNotOnPhone    = "no longer on the phone"
	reasonNotDownloaded = "not downloaded yet"
)

var (
	errPhoneTimeout  = errors.New("the phone did not re-send the file in time")
	errPhoneNotFound = errors.New("the phone no longer has this file")
)

type mediaRetries struct {
	mu      sync.Mutex
	waiting map[string]chan *events.MediaRetry
}

func newMediaRetries() *mediaRetries {
	return &mediaRetries{waiting: map[string]chan *events.MediaRetry{}}
}

func (r *mediaRetries) register(messageID string) chan *events.MediaRetry {
	ch := make(chan *events.MediaRetry, 1)
	r.mu.Lock()
	r.waiting[messageID] = ch
	r.mu.Unlock()
	return ch
}

func (r *mediaRetries) remove(messageID string) {
	r.mu.Lock()
	delete(r.waiting, messageID)
	r.mu.Unlock()
}

func (r *mediaRetries) deliver(evt *events.MediaRetry) {
	r.mu.Lock()
	ch, ok := r.waiting[evt.MessageID]
	r.mu.Unlock()
	if ok {
		select {
		case ch <- evt:
		default:
		}
	}
}

type mediaDownloader struct {
	cli        *whatsmeow.Client
	arc        *archive
	dir        string
	retries    *mediaRetries
	phone      *phoneWatch
	log        waLog.Logger
	phoneSlots chan struct{}
	loggedOut  <-chan struct{}
}

type mediaOutcome struct {
	state   int
	file    string
	errText string
}

func newMediaDownloader(cli *whatsmeow.Client, arc *archive, dir string, retries *mediaRetries, phone *phoneWatch, log waLog.Logger, loggedOut <-chan struct{}) *mediaDownloader {
	return &mediaDownloader{
		cli:        cli,
		arc:        arc,
		dir:        dir,
		retries:    retries,
		phone:      phone,
		log:        log,
		phoneSlots: make(chan struct{}, phoneRetrySlots),
		loggedOut:  loggedOut,
	}
}

func (d *mediaDownloader) pending(ctx context.Context, only map[string]bool) ([]storedMessage, error) {
	queue, err := d.arc.mediaQueue(ctx)
	if err != nil {
		return nil, err
	}
	return keepSelected(queue, only, func(m storedMessage) string { return m.ChatJID }), nil
}

func (d *mediaDownloader) run(ctx context.Context, queue []storedMessage, report func(progressReport), onDone func(storedMessage)) error {
	if len(queue) == 0 {
		return nil
	}
	var processed atomic.Int64
	jobs := make(chan storedMessage)
	var wg sync.WaitGroup
	for range mediaWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for msg := range jobs {
				outcome := d.fetch(ctx, msg)
				if ctx.Err() != nil {
					return
				}
				if err := d.arc.setMedia(ctx, msg.ChatJID, msg.ID, outcome.state, outcome.file, outcome.errText); err != nil {
					d.log.Errorf("Failed to record media result for %s: %v", msg.ID, err)
				}
				processed.Add(1)
				if onDone != nil {
					onDone(msg)
				}
			}
		}()
	}
	stopReports := every(ctx, progressTick, func() {
		report(progressReport{
			fraction: float64(processed.Load()) / float64(len(queue)),
			known:    true,
			stalled:  d.phone.stalled(),
		})
	})
feed:
	for _, msg := range queue {
		select {
		case jobs <- msg:
		case <-ctx.Done():
			break feed
		case <-d.loggedOut:
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	stopReports()
	select {
	case <-d.loggedOut:
		return errLoggedOut
	default:
	}
	return ctx.Err()
}

func (d *mediaDownloader) fetch(ctx context.Context, msg storedMessage) mediaOutcome {
	media, ok := findMedia(msg.Message)
	if !ok {
		return mediaOutcome{state: mediaNone}
	}
	name := localMediaName(msg, media.ext)
	path := filepath.Join(d.dir, name)
	if _, err := os.Stat(path); err == nil {
		return mediaOutcome{state: mediaDone, file: name}
	}
	err := d.download(ctx, media, path)
	if err == nil {
		return mediaOutcome{state: mediaDone, file: name}
	}
	if !needsPhone(err) {
		d.log.Warnf("Media download failed for %s: %v", msg.ID, err)
		return mediaOutcome{state: mediaFailed, errText: err.Error()}
	}
	err = d.askPhone(ctx, msg, media)
	if err == nil {
		err = d.download(ctx, media, path)
		if err == nil {
			return mediaOutcome{state: mediaDone, file: name}
		}
	}
	d.log.Infof("Phone re-upload failed for %s: %v", msg.ID, err)
	if errors.Is(err, whatsmeow.ErrMediaNotAvailableOnPhone) || errors.Is(err, errPhoneNotFound) {
		return mediaOutcome{state: mediaUnavailable, errText: reasonNotOnPhone}
	}
	return mediaOutcome{state: mediaFailed, errText: err.Error()}
}

func needsPhone(err error) bool {
	return errors.Is(err, whatsmeow.ErrNoURLPresent) ||
		errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith403) ||
		errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith404) ||
		errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith410)
}

func (d *mediaDownloader) download(ctx context.Context, media mediaPart, path string) error {
	ctx, cancel := context.WithTimeout(ctx, singleDownloadLimit)
	defer cancel()
	tmp := path + ".part"
	file, err := os.Create(tmp)
	if err != nil {
		return err
	}
	err = d.cli.DownloadToFile(ctx, media.part, file)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func (d *mediaDownloader) askPhone(ctx context.Context, msg storedMessage, media mediaPart) error {
	select {
	case d.phoneSlots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-d.phoneSlots }()
	for {
		sentAt := time.Now()
		err := d.requestFromPhone(ctx, msg, media)
		if !errors.Is(err, errPhoneTimeout) || d.phone.repliedSince(sentAt) {
			return err
		}
		if !d.phone.waitForActivity(ctx, sentAt) {
			return err
		}
	}
}

func (d *mediaDownloader) requestFromPhone(ctx context.Context, msg storedMessage, media mediaPart) error {
	reply := d.retries.register(msg.ID)
	defer d.retries.remove(msg.ID)
	done := d.phone.begin()
	defer done()
	info := msg.messageInfo()
	mediaKey := media.part.GetMediaKey()
	if err := d.cli.SendMediaRetryReceipt(ctx, &info, mediaKey); err != nil {
		return err
	}
	select {
	case evt := <-reply:
		notification, err := whatsmeow.DecryptMediaRetryNotification(evt, mediaKey)
		if err != nil {
			return err
		}
		switch notification.GetResult() {
		case waMmsRetry.MediaRetryNotification_SUCCESS:
		case waMmsRetry.MediaRetryNotification_NOT_FOUND:
			return errPhoneNotFound
		default:
			return errors.New("the phone could not re-send the file (" + notification.GetResult().String() + ")")
		}
		if notification.GetDirectPath() == "" || !setDirectPath(media.part, notification.GetDirectPath()) {
			return errors.New("the phone's reply did not include a usable file location")
		}
		return nil
	case <-time.After(phoneRetryTimeout):
		return errPhoneTimeout
	case <-d.loggedOut:
		return errLoggedOut
	case <-ctx.Done():
		return ctx.Err()
	}
}

func localMediaName(msg storedMessage, ext string) string {
	sum := sha1.Sum([]byte(msg.ChatJID + "|" + msg.ID))
	return hex.EncodeToString(sum[:10]) + "." + ext
}
