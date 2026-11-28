package audit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime/pprof"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func TestMain(m *testing.M) {
	code := m.Run()
	// Writing the profile is what runs the leak detector; Count alone reads
	// the last run, which before a write is zero (go1.27.3).
	leaks := pprof.Lookup("goroutineleak")
	var stacks strings.Builder
	if err := leaks.WriteTo(&stacks, 1); err != nil {
		fmt.Fprintln(os.Stderr, "leak check:", err)
		os.Exit(1)
	}
	if leaks.Count() > 0 {
		fmt.Fprintf(os.Stderr, "goroutine leak: %d goroutine(s) blocked forever\n%s", leaks.Count(), stacks.String())
		os.Exit(1)
	}
	os.Exit(code)
}

type traceKey struct{}

func TestAWriteOutlivesItsRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var mu sync.Mutex
		var got []string
		sink := func(ctx context.Context, e Event) error {
			time.Sleep(time.Second) // a slow store
			if err := ctx.Err(); err != nil {
				return err
			}
			mu.Lock()
			defer mu.Unlock()
			got = append(got, fmt.Sprintf("%s %s %v", e.Actor, e.Action, ctx.Value(traceKey{})))
			return nil
		}
		rec := New(sink, time.Minute)
		req, answered := context.WithCancel(context.WithValue(t.Context(), traceKey{}, "trace-7"))
		rec.Record(req, Event{Actor: "ana", Action: "refund"})
		answered() // the response went out before the write ran
		if err := rec.Close(); err != nil {
			t.Fatalf("Close = %v, want nil", err)
		}
		if len(got) != 1 || got[0] != "ana refund trace-7" {
			t.Fatalf("stored %q, want the event with its trace id", got)
		}
	})
}

func TestAHungWriteIsBoundedAndReported(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sink := func(ctx context.Context, _ Event) error {
			<-ctx.Done()
			return ctx.Err()
		}
		rec := New(sink, 2*time.Second)
		start := time.Now()
		rec.Record(t.Context(), Event{Actor: "bo", Action: "login"})
		err := rec.Close()
		if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "audit bo login") {
			t.Fatalf("Close = %v, want the write's DeadlineExceeded", err)
		}
		if elapsed := time.Since(start); elapsed != 2*time.Second {
			t.Fatalf("the hung write took %v, want the 2s bound", elapsed)
		}
	})
}
