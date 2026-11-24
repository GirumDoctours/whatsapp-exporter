package main

import (
	"context"
	"sync"
	"sync/atomic"
)

const redactBatchSize = 10

type batchDispatcher struct {
	ctx     context.Context
	size    int
	work    func(context.Context, chatInput) chatOutput
	onDone  func(chatOutput)
	mu      sync.Mutex
	pending []chatInput
	seen    map[string]bool
	wg      sync.WaitGroup
	queued  atomic.Int64
	done    atomic.Int64
	batches atomic.Int64
}

func newBatchDispatcher(ctx context.Context, size int, work func(context.Context, chatInput) chatOutput, onDone func(chatOutput)) *batchDispatcher {
	return &batchDispatcher{ctx: ctx, size: max(1, size), work: work, onDone: onDone, seen: map[string]bool{}}
}

func (d *batchDispatcher) add(in chatInput) {
	d.mu.Lock()
	if in.Folder != "" && d.seen[in.Folder] {
		d.mu.Unlock()
		logRedact.Debugf("Chat %q is already queued; not redacting it twice", in.Folder)
		return
	}
	d.seen[in.Folder] = true
	d.pending = append(d.pending, in)
	d.queued.Add(1)
	var batch []chatInput
	if len(d.pending) >= d.size {
		batch, d.pending = d.pending, nil
	}
	d.mu.Unlock()
	d.launch(batch)
}

func (d *batchDispatcher) launch(batch []chatInput) {
	if len(batch) == 0 {
		return
	}
	n := d.batches.Add(1)
	logRedact.Infof("Starting redaction batch %d with %d chats (%d queued so far)", n, len(batch), d.queued.Load())
	for _, in := range batch {
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			out := d.work(d.ctx, in)
			if d.onDone != nil {
				d.onDone(out)
			}
			d.done.Add(1)
		}()
	}
}

func (d *batchDispatcher) flush() {
	d.mu.Lock()
	batch := d.pending
	d.pending = nil
	d.mu.Unlock()
	d.launch(batch)
}

func (d *batchDispatcher) wait() {
	d.flush()
	d.wg.Wait()
}

func (d *batchDispatcher) progress() (done, queued int64) {
	return d.done.Load(), d.queued.Load()
}
