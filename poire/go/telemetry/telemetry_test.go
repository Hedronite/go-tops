package telemetry

import (
	"fmt"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"
)

var sample = Event{
	At:      time.Date(2026, 11, 27, 12, 0, 0, 5, time.UTC),
	Source:  netip.MustParseAddr("10.0.0.7"),
	Latency: 1500 * time.Microsecond,
	Status:  200,
	Retried: true,
}

func TestAppendTextWritesOneLine(t *testing.T) {
	got, err := sample.AppendText([]byte("> "))
	want := "> 2026-11-27T12:00:00.000000005Z src=10.0.0.7 status=200 latency_ns=1500000 retried\n"
	if err != nil || string(got) != want {
		t.Fatalf("AppendText = %q, %v\nwant %q", got, err, want)
	}
	plain := sample
	plain.Retried = false
	if got := Lines([]Event{plain, sample}); strings.Count(got, "\n") != 2 || strings.Count(got, "retried") != 1 {
		t.Fatalf("Lines = %q", got)
	}
}

// arrival is Event's fields in the order an author might first write them.
type arrival struct {
	Retried bool
	At      time.Time
	Status  uint16
	Latency time.Duration
	Source  netip.Addr
}

func TestEventCarriesNoPadding(t *testing.T) {
	if got := reflect.TypeFor[Event]().Size(); got != 64 {
		t.Errorf("Event is %d bytes, want 64", got)
	}
	if got := reflect.TypeFor[arrival]().Size(); got != 72 {
		t.Errorf("the arrival-order struct is %d bytes, want 72", got)
	}
}

// The measurements behind GO-29, GO-30 and GO-27, counted exactly.
func TestAllocationCounts(t *testing.T) {
	buf := make([]byte, 0, 256)
	reused := testing.AllocsPerRun(100, func() {
		buf = sample.AppendLine(buf[:0])
	})
	if reused != 0 {
		t.Errorf("AppendText into a reused buffer: %v allocs, want 0", reused)
	}

	boxed := testing.AllocsPerRun(100, func() {
		_ = fmt.Sprintf("%s src=%s status=%d latency_ns=%d", sample.At.Format(time.RFC3339Nano), sample.Source, sample.Status, sample.Latency.Nanoseconds())
	})
	if boxed < 2 {
		t.Errorf("the fmt version: %v allocs, expected several", boxed)
	}

	events := make([]Event, 200)
	for i := range events {
		events[i] = sample
	}
	linear := testing.AllocsPerRun(10, func() { _ = Lines(events) })
	quadratic := testing.AllocsPerRun(10, func() {
		var s string
		for _, e := range events {
			s += string(e.AppendLine(nil)) // copies everything so far, every time
		}
		_ = s
	})
	if linear > 2 || quadratic < float64(len(events)) {
		t.Errorf("Lines: %v allocs (want at most 2); += version: %v (expected at least %d)", linear, quadratic, len(events))
	}
	t.Logf("allocs: reused buffer %v, fmt %v, Lines %v, += %v", reused, boxed, linear, quadratic)
}

func BenchmarkAppendText(b *testing.B) {
	buf := make([]byte, 0, 256)
	b.ReportAllocs()
	for b.Loop() {
		buf = sample.AppendLine(buf[:0])
	}
}
