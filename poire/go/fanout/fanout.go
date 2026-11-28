// Package fanout runs work concurrently, with an owner and an end for every
// goroutine it starts.
//
// Go-POIRE conditions cured here: GO-01 orphan goroutine, GO-02 stranded
// sender, GO-23 unbounded fan-out. The tests show GO-12's cure: time and
// concurrency checked inside synctest bubbles, never by sleeping.
package fanout

import (
	"context"
	"errors"
	"sync"
)

// ErrNoEndpoints is returned by [First] when it is given nothing to query.
var ErrNoEndpoints = errors.New("fanout: no endpoints")

// First queries every endpoint at once and returns the first success, or every
// error joined if none succeeds.
//
// Every goroutine First starts has finished before it returns. The result
// channel has room for every answer, so no sender is ever stranded, and the
// losers see their context cancelled. A probe that ignores its context delays
// First's return by however long it runs.
func First[T any](ctx context.Context, endpoints []string, probe func(context.Context, string) (T, error)) (T, error) {
	var zero T
	if len(endpoints) == 0 {
		return zero, ErrNoEndpoints
	}
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	defer func() {
		cancel()
		wg.Wait()
	}()

	type outcome struct {
		val T
		err error
	}
	results := make(chan outcome, len(endpoints))
	for _, ep := range endpoints {
		wg.Go(func() {
			val, err := probe(ctx, ep)
			results <- outcome{val: val, err: err}
		})
	}

	errs := make([]error, 0, len(endpoints))
	for range endpoints {
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case r := <-results:
			if r.err == nil {
				return r.val, nil
			}
			errs = append(errs, r.err)
		}
	}
	return zero, errors.Join(errs...)
}

// Each calls fn for every item, with at most limit calls running at once, and
// returns their errors joined. A limit below 1 is treated as 1.
//
// When ctx ends, Each starts no further calls, waits for the running ones, and
// adds ctx's error to the result.
func Each[T any](ctx context.Context, items []T, limit int, fn func(context.Context, T) error) error {
	slots := make(chan struct{}, max(limit, 1))
	errs := make([]error, len(items))
	var wg sync.WaitGroup
	for i, item := range items {
		select {
		case <-ctx.Done():
			wg.Wait()
			return errors.Join(append(errs, ctx.Err())...)
		case slots <- struct{}{}:
		}
		wg.Go(func() {
			defer func() { <-slots }()
			errs[i] = fn(ctx, item)
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}
