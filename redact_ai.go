package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

const (
	haikuModel         = "claude-haiku-5-5"
	aiStartConcurrency = 16
	aiMaxConcurrency   = 48
	aiMaxAttempts      = 8
	aiRequestTimeout   = 4 * time.Minute
	aiMaxBackoff       = 60 * time.Second
	haikuInputPrice    = 0.10
	haikuOutputPrice   = 0.50
	haikuCacheRead     = 0.01
	haikuCacheWrite    = 0.125
)

var (
	errAIRefused   = errors.New("the AI declined this chunk")
	errAITruncated = errors.New("the AI reply was cut off")
	errAIBadReply  = errors.New("the AI reply wasn't valid JSON")
)

type aiRequest struct {
	System    string
	Prompt    string
	Schema    map[string]any
	MaxTokens int64
	Effort    anthropic.OutputConfigEffort
}

type aiClient interface {
	generate(ctx context.Context, req aiRequest, out any) error
}

type aiUsage struct {
	requests   atomic.Int64
	input      atomic.Int64
	output     atomic.Int64
	cacheRead  atomic.Int64
	cacheWrite atomic.Int64
	throttled  atomic.Int64
	failed     atomic.Int64
}

func (u *aiUsage) dollars() float64 {
	return (float64(u.input.Load())*haikuInputPrice +
		float64(u.output.Load())*haikuOutputPrice +
		float64(u.cacheRead.Load())*haikuCacheRead +
		float64(u.cacheWrite.Load())*haikuCacheWrite) / 1e6
}

func (u *aiUsage) summary() string {
	return fmt.Sprintf("%d requests, %d input + %d cached + %d output tokens, about $%.2f, %d throttled, %d failed",
		u.requests.Load(), u.input.Load(), u.cacheRead.Load()+u.cacheWrite.Load(), u.output.Load(), u.dollars(), u.throttled.Load(), u.failed.Load())
}

type haikuClient struct {
	model  string
	client anthropic.Client
	limit  *adaptiveLimit
	usage  *aiUsage
}

func newHaikuClient(apiKey string) *haikuClient {
	return newModelClient(apiKey, haikuModel)
}

func newModelClient(apiKey, model string) *haikuClient {
	return &haikuClient{
		model: model,
		client: anthropic.NewClient(
			option.WithAPIKey(apiKey),
			option.WithMaxRetries(0),
			option.WithRequestTimeout(aiRequestTimeout),
		),
		limit: newAdaptiveLimit(aiStartConcurrency, aiMaxConcurrency),
		usage: &aiUsage{},
	}
}

func (h *haikuClient) check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, err := h.client.Models.Get(ctx, h.model, anthropic.ModelGetParams{})
	if err != nil {
		status, _ := apiStatus(err)
		logAI.Warnf("API key check failed (status %d): %v", status, err)
	} else {
		logAI.Infof("API key check passed for %s", h.model)
	}
	return err
}

func (h *haikuClient) generate(ctx context.Context, req aiRequest, out any) error {
	effort := req.Effort
	if effort == "" {
		effort = anthropic.OutputConfigEffortLow
	}
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(h.model),
		MaxTokens: req.MaxTokens,
		System: []anthropic.TextBlockParam{{
			Text:         req.System,
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(req.Prompt)),
		},
		OutputConfig: anthropic.OutputConfigParam{
			Effort: effort,
			Format: anthropic.JSONOutputFormatParam{Schema: req.Schema},
		},
	}
	var lastErr error
	for attempt := 1; attempt <= aiMaxAttempts; attempt++ {
		release, err := h.limit.acquire(ctx)
		if err != nil {
			return err
		}
		h.usage.requests.Add(1)
		started := time.Now()
		msg, err := h.client.Messages.New(ctx, params)
		throttled := isThrottle(err)
		release(throttled)
		if err == nil {
			h.usage.input.Add(msg.Usage.InputTokens)
			h.usage.output.Add(msg.Usage.OutputTokens)
			h.usage.cacheRead.Add(msg.Usage.CacheReadInputTokens)
			h.usage.cacheWrite.Add(msg.Usage.CacheCreationInputTokens)
			logAI.Debugf("Reply in %s (attempt %d, effort %s): %d in, %d cached, %d out, stop %s", time.Since(started).Round(time.Millisecond), attempt, effort, msg.Usage.InputTokens, msg.Usage.CacheReadInputTokens, msg.Usage.OutputTokens, msg.StopReason)
			if err := decodeReply(msg, out); err != nil {
				logAI.Warnf("Unusable reply (stop %s): %v", msg.StopReason, err)
				return err
			}
			return nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return ctx.Err()
		}
		status, _ := apiStatus(err)
		if !isRetryable(err) {
			h.usage.failed.Add(1)
			logAI.Errorf("Request failed with status %d, not retrying: %v", status, err)
			return err
		}
		delay := retryDelay(err, attempt)
		if throttled {
			h.usage.throttled.Add(1)
			logAI.Warnf("Rate limited (status %d); concurrency now %d, retrying in %s", status, h.limit.current(), delay.Round(time.Millisecond))
		} else {
			logAI.Warnf("Request failed (status %d, attempt %d), retrying in %s: %v", status, attempt, delay.Round(time.Millisecond), err)
		}
		if err := sleepCtx(ctx, delay); err != nil {
			return err
		}
	}
	h.usage.failed.Add(1)
	logAI.Errorf("Giving up after %d attempts: %v", aiMaxAttempts, lastErr)
	return lastErr
}

func decodeReply(msg *anthropic.Message, out any) error {
	switch msg.StopReason {
	case anthropic.StopReasonRefusal:
		return errAIRefused
	case anthropic.StopReasonMaxTokens:
		return errAITruncated
	}
	var text strings.Builder
	for _, block := range msg.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(t.Text)
		}
	}
	if err := json.Unmarshal([]byte(text.String()), out); err != nil {
		return fmt.Errorf("%w: %v", errAIBadReply, err)
	}
	return nil
}

func apiStatus(err error) (int, *anthropic.Error) {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode, apiErr
	}
	return 0, nil
}

func isThrottle(err error) bool {
	status, _ := apiStatus(err)
	return status == 429 || status == 529
}

func isRetryable(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	status, _ := apiStatus(err)
	switch {
	case status == 408 || status == 409 || status == 429 || status >= 500:
		return true
	case status != 0:
		return false
	}
	var netErr net.Error
	return errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "connection")
}

func retryDelay(err error, attempt int) time.Duration {
	if _, apiErr := apiStatus(err); apiErr != nil && apiErr.Response != nil {
		if s, convErr := strconv.Atoi(apiErr.Response.Header.Get("retry-after")); convErr == nil && s > 0 {
			return min(time.Duration(s)*time.Second, aiMaxBackoff)
		}
	}
	base := time.Duration(1<<min(attempt, 6)) * 500 * time.Millisecond
	jitter := time.Duration(rand.Int64N(int64(base / 2)))
	return min(base+jitter, aiMaxBackoff)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type adaptiveLimit struct {
	mu        sync.Mutex
	limit     int
	max       int
	inFlight  int
	successes int
	wake      chan struct{}
}

func newAdaptiveLimit(start, max int) *adaptiveLimit {
	return &adaptiveLimit{limit: start, max: max, wake: make(chan struct{})}
}

func (l *adaptiveLimit) acquire(ctx context.Context) (func(throttled bool), error) {
	for {
		l.mu.Lock()
		if l.inFlight < l.limit {
			l.inFlight++
			l.mu.Unlock()
			return l.release, nil
		}
		wake := l.wake
		l.mu.Unlock()
		select {
		case <-wake:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (l *adaptiveLimit) release(throttled bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.inFlight--
	if throttled {
		l.limit = max(1, l.limit/2)
		l.successes = 0
	} else {
		l.successes++
		if l.successes >= l.limit && l.limit < l.max {
			l.limit++
			l.successes = 0
		}
	}
	close(l.wake)
	l.wake = make(chan struct{})
}

func (l *adaptiveLimit) current() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.limit
}

type aiCache struct {
	dir string
}

func newAICache(dir string) *aiCache {
	if dir == "" {
		return nil
	}
	_ = os.MkdirAll(dir, 0o700)
	return &aiCache{dir: dir}
}

func cacheKey(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (c *aiCache) get(key string, out any) bool {
	if c == nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(c.dir, key+".json"))
	if err != nil {
		return false
	}
	return json.Unmarshal(data, out) == nil
}

func (c *aiCache) put(key string, value any) {
	if c == nil {
		return
	}
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	tmp := filepath.Join(c.dir, key+".tmp")
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, filepath.Join(c.dir, key+".json"))
	}
}

func cachedGenerate(ctx context.Context, ai aiClient, cache *aiCache, key string, req aiRequest, out any) error {
	if cache.get(key, out) {
		logAI.Debugf("Cache hit %s", key[:12])
		return nil
	}
	if err := ai.generate(ctx, req, out); err != nil {
		return err
	}
	cache.put(key, out)
	return nil
}

func objectSchema(properties map[string]any) map[string]any {
	required := make([]string, 0, len(properties))
	for name := range properties {
		required = append(required, name)
	}
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func arrayOf(item map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": item}
}

var (
	schemaString  = map[string]any{"type": "string"}
	schemaInteger = map[string]any{"type": "integer"}
	schemaNumber  = map[string]any{"type": "number"}
	schemaBool    = map[string]any{"type": "boolean"}
)
