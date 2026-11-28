// Package telemetry formats timed events as log lines.
//
// Go-POIRE conditions cured here: GO-26 padded layout, GO-27 quadratic
// accumulation, GO-29 heap escape on a hot path, GO-30 boxing in a hot loop.
package telemetry

import (
	"encoding"
	"net/netip"
	"strconv"
	"time"
)

// Event is one timed request. Its fields are ordered largest first, so the
// struct carries no padding: 64 bytes, where the same fields in arrival order
// take 72.
type Event struct {
	At      time.Time
	Source  netip.Addr
	Latency time.Duration
	Status  uint16
	Retried bool
}

var _ encoding.TextAppender = Event{}

// AppendLine appends e to dst as one log line and returns the extended slice.
//
// The caller owns dst. A caller that reuses one buffer formats any number of
// events without allocating: no fmt, no interface boxing, no intermediate
// strings.
func (e Event) AppendLine(dst []byte) []byte {
	dst = e.At.UTC().AppendFormat(dst, time.RFC3339Nano)
	dst = append(dst, " src="...)
	dst = e.Source.AppendTo(dst)
	dst = append(dst, " status="...)
	dst = strconv.AppendUint(dst, uint64(e.Status), 10)
	dst = append(dst, " latency_ns="...)
	dst = strconv.AppendInt(dst, e.Latency.Nanoseconds(), 10)
	if e.Retried {
		dst = append(dst, " retried"...)
	}
	return append(dst, '\n')
}

// AppendText implements [encoding.TextAppender]. It is [Event.AppendLine],
// and its error is always nil.
func (e Event) AppendText(dst []byte) ([]byte, error) {
	return e.AppendLine(dst), nil
}

// Lines formats events into one string, one line each. It grows one buffer
// and converts it once, so the cost is linear in the number of events.
func Lines(events []Event) string {
	buf := make([]byte, 0, 96*len(events))
	for _, e := range events {
		buf = e.AppendLine(buf)
	}
	return string(buf)
}
