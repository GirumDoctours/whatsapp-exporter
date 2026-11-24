package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

var (
	deepgramListenURL   = "https://api.deepgram.com/v1/listen?model=nova-3&punctuate=true&numerals=true&detect_language=true"
	deepgramProjectsURL = "https://api.deepgram.com/v1/projects"
)

const (
	deepgramMaxFileBytes   = 100 << 20
	transcribeMaxAttempts  = 6
	transcriptSuffix       = ".transcript.txt"
	transcriptPrefix       = "\u200eTranscript: "
	transcribeStartPar     = 8
	transcribeMaxPar       = 32
	transcribeTimeout      = 2 * time.Minute
	deepgramPricePerMinute = 0.0043
)

type transcriber struct {
	key     string
	client  *http.Client
	limit   *adaptiveLimit
	done    atomic.Int64
	failed  atomic.Int64
	seconds atomic.Int64
}

func newTranscriber(key string) *transcriber {
	return &transcriber{
		key:    strings.TrimSpace(key),
		client: &http.Client{Timeout: transcribeTimeout},
		limit:  newAdaptiveLimit(transcribeStartPar, transcribeMaxPar),
	}
}

func (tr *transcriber) check(ctx context.Context) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, deepgramProjectsURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Token "+tr.key)
	resp, err := tr.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("deepgram answered %s", resp.Status)
	}
	return resp.StatusCode, nil
}

func transcriptPath(audioPath string) string {
	return audioPath + transcriptSuffix
}

func readTranscript(audioPath string) (string, bool) {
	data, err := os.ReadFile(transcriptPath(audioPath))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(data)), true
}

func (tr *transcriber) transcribeFile(ctx context.Context, audioPath string, seconds uint32) {
	if _, ok := readTranscript(audioPath); ok {
		return
	}
	info, err := os.Stat(audioPath)
	if err != nil || info.Size() == 0 || info.Size() > deepgramMaxFileBytes {
		logTranscribe.Warnf("Skipping %s: not a usable audio file (%v)", filepath.Base(audioPath), err)
		tr.failed.Add(1)
		return
	}
	text, err := tr.request(ctx, audioPath)
	if err != nil {
		logTranscribe.Warnf("Couldn't transcribe %s: %v", filepath.Base(audioPath), err)
		tr.failed.Add(1)
		return
	}
	if err := os.WriteFile(transcriptPath(audioPath), []byte(text), 0o600); err != nil {
		logTranscribe.Errorf("Couldn't save the transcript of %s: %v", filepath.Base(audioPath), err)
		tr.failed.Add(1)
		return
	}
	tr.done.Add(1)
	tr.seconds.Add(int64(max(seconds, 1)))
	logTranscribe.Debugf("Transcribed %s (%ds, %d characters)", filepath.Base(audioPath), seconds, len(text))
}

func (tr *transcriber) request(ctx context.Context, audioPath string) (string, error) {
	audio, err := os.ReadFile(audioPath)
	if err != nil {
		return "", err
	}
	var lastErr error
	for attempt := 1; attempt <= transcribeMaxAttempts; attempt++ {
		release, err := tr.limit.acquire(ctx)
		if err != nil {
			return "", err
		}
		text, status, retryAfter, err := tr.post(ctx, audio)
		throttled := status == http.StatusTooManyRequests
		release(throttled)
		if err == nil {
			return text, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if status != 0 && status != http.StatusTooManyRequests && status < 500 {
			return "", err
		}
		delay := retryAfter
		if delay == 0 {
			delay = time.Duration(1<<min(attempt, 5)) * 500 * time.Millisecond
		}
		logTranscribe.Warnf("Transcription retry %d in %s (status %d): %v", attempt, delay, status, err)
		if err := sleepCtx(ctx, min(delay, aiMaxBackoff)); err != nil {
			return "", err
		}
	}
	return "", lastErr
}

func (tr *transcriber) post(ctx context.Context, audio []byte) (string, int, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deepgramListenURL, bytes.NewReader(audio))
	if err != nil {
		return "", 0, 0, err
	}
	req.Header.Set("Authorization", "Token "+tr.key)
	req.Header.Set("Content-Type", "audio/ogg")
	resp, err := tr.client.Do(req)
	if err != nil {
		return "", 0, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		retry := time.Duration(0)
		if s, err := strconv.Atoi(resp.Header.Get("retry-after")); err == nil && s > 0 {
			retry = time.Duration(s) * time.Second
		}
		return "", resp.StatusCode, retry, fmt.Errorf("deepgram answered %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	var reply struct {
		Results struct {
			Channels []struct {
				Alternatives []struct {
					Transcript string `json:"transcript"`
				} `json:"alternatives"`
			} `json:"channels"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &reply); err != nil {
		return "", resp.StatusCode, 0, errors.New("deepgram sent an unreadable reply")
	}
	if len(reply.Results.Channels) == 0 || len(reply.Results.Channels[0].Alternatives) == 0 {
		return "", resp.StatusCode, 0, nil
	}
	return strings.TrimSpace(reply.Results.Channels[0].Alternatives[0].Transcript), resp.StatusCode, 0, nil
}

func (tr *transcriber) summary() string {
	minutes := float64(tr.seconds.Load()) / 60
	return fmt.Sprintf("%d voice notes transcribed, %d failed, about %.1f min of audio, about $%.3f", tr.done.Load(), tr.failed.Load(), minutes, minutes*deepgramPricePerMinute)
}
