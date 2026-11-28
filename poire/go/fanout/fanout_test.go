package fanout

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

const strandedDemo = "POIRE_STRANDED_DEMO"

func TestMain(m *testing.M) {
	code := 0
	if os.Getenv(strandedDemo) == "1" {
		strandOne()
	} else {
		code = m.Run()
	}
	// Writing the profile is what runs the leak detector. Count alone reports
	// the last run, and before any write that is zero (go1.27.3), so a check
	// that only calls Count passes whatever leaked.
	leaks := pprof.Lookup("goroutineleak")
	var stacks strings.Builder
	for range 1000 {
		if err := leaks.WriteTo(&stacks, 1); err != nil {
			fmt.Fprintln(os.Stderr, "leak check:", err)
			os.Exit(1)
		}
		if leaks.Count() > 0 || code != 0 || os.Getenv(strandedDemo) != "1" {
			break
		}
		// In the demo the stranded sender may not have blocked yet.
		stacks.Reset()
		runtime.Gosched()
	}
	if leaks.Count() > 0 {
		fmt.Fprintf(os.Stderr, "goroutine leak: %d goroutine(s) blocked forever\n%s", leaks.Count(), stacks.String())
		os.Exit(1)
	}
	os.Exit(code)
}

// strandedFirst is GO-02's counter-example: an unbuffered result channel. When
// the caller's context ends first, the sender has no receiver and blocks forever.
func strandedFirst(ctx context.Context, probe func() int) (int, error) {
	ch := make(chan int)
	go func() { ch <- probe() }()
	select {
	case v := <-ch:
		return v, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func strandOne() {
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := strandedFirst(ctx, func() int { <-release; return 1 }); !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "stranded demo: want context.Canceled, got", err)
		os.Exit(2)
	}
	close(release)
}

// The leak check above is a live detector: run in a child process, the
// stranded sender fails it.
func TestTheLeakCheckCatchesAStrandedSender(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), strandedDemo+"=1")
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("child exit = %v, want exit status 1\n%s", err, out)
	}
	if !strings.Contains(string(out), "goroutine leak: 1 goroutine(s)") || !strings.Contains(string(out), "strandedFirst") {
		t.Fatalf("child output does not name the stranded sender:\n%s", out)
	}
}

func TestFirstReturnsTheFastestAndStopsTheRest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var cancelled atomic.Int32
		probe := func(ctx context.Context, ep string) (string, error) {
			if ep == "fast" {
				time.Sleep(10 * time.Millisecond)
				return "ok", nil
			}
			select {
			case <-time.After(time.Hour):
				return "late", nil
			case <-ctx.Done():
				cancelled.Add(1)
				return "", ctx.Err()
			}
		}
		start := time.Now()
		got, err := First(t.Context(), []string{"slow-1", "fast", "slow-2"}, probe)
		if err != nil || got != "ok" {
			t.Fatalf("First = %q, %v; want ok, nil", got, err)
		}
		if elapsed := time.Since(start); elapsed != 10*time.Millisecond {
			t.Errorf("First took %v of bubble time, want 10ms", elapsed)
		}
		if n := cancelled.Load(); n != 2 {
			t.Errorf("%d slow probes saw cancellation, want 2", n)
		}
	})
}

func TestFirstJoinsEveryError(t *testing.T) {
	errA, errB := errors.New("a down"), errors.New("b down")
	probe := func(_ context.Context, ep string) (int, error) {
		if ep == "a" {
			return 0, errA
		}
		return 0, errB
	}
	_, err := First(t.Context(), []string{"a", "b"}, probe)
	if !errors.Is(err, errA) || !errors.Is(err, errB) {
		t.Fatalf("First error = %v, want both causes", err)
	}
	if _, err := First(t.Context(), nil, probe); !errors.Is(err, ErrNoEndpoints) {
		t.Fatalf("First(nil) error = %v, want ErrNoEndpoints", err)
	}
}

func TestFirstStopsWhenTheCallerDoes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		probe := func(ctx context.Context, _ string) (int, error) {
			<-ctx.Done()
			return 0, ctx.Err()
		}
		if _, err := First(ctx, []string{"x"}, probe); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("First error = %v, want DeadlineExceeded", err)
		}
	})
}

func TestEachNeverRunsMoreThanTheLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var running, peak atomic.Int32
		fn := func(_ context.Context, n int) error {
			now := running.Add(1)
			for {
				old := peak.Load()
				if now <= old || peak.CompareAndSwap(old, now) {
					break
				}
			}
			time.Sleep(time.Second)
			running.Add(-1)
			if n == 7 {
				return fmt.Errorf("item %d failed", n)
			}
			return nil
		}
		start := time.Now()
		err := Each(t.Context(), make([]int, 10), 3, func(ctx context.Context, _ int) error { return fn(ctx, 0) })
		if err != nil {
			t.Fatalf("Each = %v, want nil", err)
		}
		if p := peak.Load(); p != 3 {
			t.Errorf("peak concurrency %d, want 3", p)
		}
		if elapsed := time.Since(start); elapsed != 4*time.Second {
			t.Errorf("10 one-second items at 3 wide took %v, want 4s", elapsed)
		}
		items := []int{1, 7}
		if err := Each(t.Context(), items, 0, fn); err == nil || err.Error() != "item 7 failed" {
			t.Errorf("Each error = %v, want the failing item's error", err)
		}
	})
}

func TestEachStartsNothingAfterCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		var started atomic.Int32
		fn := func(context.Context, int) error {
			started.Add(1)
			cancel()
			return nil
		}
		err := Each(ctx, []int{1, 2, 3}, 1, fn)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Each error = %v, want context.Canceled", err)
		}
		if n := started.Load(); n != 1 {
			t.Errorf("%d items started, want 1", n)
		}
	})
}
