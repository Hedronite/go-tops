package packet

import (
	"bytes"
	"errors"
	"testing"
)

func frame(id byte, payload string) []byte {
	out := bytes.Repeat([]byte{id}, 16)
	out = append(out, byte(len(payload)>>8), byte(len(payload)))
	return append(out, payload...)
}

func TestHeaderCopiesWhatItReturns(t *testing.T) {
	buf := frame(7, "hello")
	buf = append(buf, make([]byte, 4096)...) // the rest of a large read buffer
	id, payload, err := Header(buf)
	if err != nil || id != (ID{7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7}) || string(payload) != "hello" {
		t.Fatalf("Header = %v, %q, %v", id, payload, err)
	}
	if cap(payload) > 16 {
		t.Fatalf("payload capacity %d: it still reaches into the read buffer", cap(payload))
	}
	buf[18] = 'J'
	if string(payload) != "hello" {
		t.Fatal("the payload changed when the buffer did")
	}
}

func TestHeaderRefusesShortFrames(t *testing.T) {
	for _, f := range [][]byte{nil, make([]byte, 17), frame(1, "abc")[:20]} {
		if _, _, err := Header(f); !errors.Is(err, ErrShort) {
			t.Errorf("Header(%d bytes) error = %v, want ErrShort", len(f), err)
		}
	}
	if _, p, err := Header(frame(1, "")); err != nil || len(p) != 0 {
		t.Errorf("an empty payload = %q, %v", p, err)
	}
}

// GO-24's harm, and its cure: two callers append to the same list.
func TestListedTagsDoNotShareSpareCapacity(t *testing.T) {
	var tags Tags
	for _, tag := range []string{"a", "b", "c"} {
		tags.Add(tag) // capacity is now 4: one spare slot
	}

	unclipped := tags.list
	first := append(unclipped, "x")
	_ = append(unclipped, "y") // a second caller, writing into the same spare slot
	if first[3] != "y" {
		t.Fatalf("expected the shared slot to be overwritten, got %q", first[3])
	}

	first = append(tags.List(), "x")
	second := append(tags.List(), "y")
	if first[3] != "x" || second[3] != "y" {
		t.Fatalf("clipped lists collided: %q, %q", first[3], second[3])
	}
}
