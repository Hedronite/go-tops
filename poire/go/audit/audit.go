// Package audit records events in the background, after the request that
// caused them has been answered.
//
// Go-POIRE conditions cured here: GO-01 orphan goroutine (the Recorder owns
// every write and Close waits for them), GO-03 severed context (a write keeps
// the request's values, drops its cancellation, and is bounded on its own).
package audit

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Event is one audited action.
type Event struct {
	Actor  string
	Action string
}

// Sink stores one event. It must return when ctx ends.
type Sink func(ctx context.Context, e Event) error

// Recorder owns the goroutines that write events.
type Recorder struct {
	sink    Sink
	timeout time.Duration
	wg      sync.WaitGroup
	mu      sync.Mutex // guards errs
	errs    []error
}

// New returns a Recorder that gives each write at most timeout.
func New(sink Sink, timeout time.Duration) *Recorder {
	return &Recorder{sink: sink, timeout: timeout}
}

// Record writes e in the background and returns at once.
//
// The write keeps ctx's values, such as a trace id, but not its cancellation,
// so answering the request does not abort it; the Recorder's timeout bounds it
// instead. Record must not be called after [Recorder.Close].
func (r *Recorder) Record(ctx context.Context, e Event) {
	detached := context.WithoutCancel(ctx)
	r.wg.Go(func() {
		wctx, cancel := context.WithTimeout(detached, r.timeout)
		defer cancel()
		if err := r.sink(wctx, e); err != nil {
			r.mu.Lock()
			r.errs = append(r.errs, fmt.Errorf("audit %s %s: %w", e.Actor, e.Action, err))
			r.mu.Unlock()
		}
	})
}

// Close waits for every write started so far and returns their errors, joined.
func (r *Recorder) Close() error {
	r.wg.Wait()
	r.mu.Lock()
	defer r.mu.Unlock()
	return errors.Join(r.errs...)
}
