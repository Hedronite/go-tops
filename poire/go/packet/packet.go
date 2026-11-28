// Package packet reads fields out of wire frames without holding on to the frame.
//
// Go-POIRE conditions cured here: GO-24 slice aliasing, GO-25 pinned buffer.
package packet

import (
	"bytes"
	"encoding/binary"
	"errors"
	"slices"
)

// ErrShort is returned for a frame too short for what it claims to hold.
var ErrShort = errors.New("packet: frame too short")

// ID is a frame's 16-byte identifier. As an array it is a value: keeping one
// keeps 16 bytes, never the frame it came from.
type ID [16]byte

// Header reads a frame: a 16-byte ID, a big-endian uint16 payload length, then
// the payload.
//
// The payload is a copy. Callers keep payloads in caches long after the frame,
// which is often a slice of a large read buffer; a sub-slice would keep that
// whole buffer alive.
func Header(frame []byte) (ID, []byte, error) {
	if len(frame) < 18 {
		return ID{}, nil, ErrShort
	}
	id := ID(frame[:16])
	n := int(binary.BigEndian.Uint16(frame[16:18]))
	if len(frame) < 18+n {
		return ID{}, nil, ErrShort
	}
	return id, bytes.Clone(frame[18 : 18+n]), nil
}

// Tags is a list of tags that grows as tags are added.
type Tags struct {
	list []string
}

// Add appends a tag.
func (t *Tags) Add(tag string) {
	t.list = append(t.list, tag)
}

// List returns the tags. Its capacity is clipped to its length, so a caller
// that appends to it gets a fresh array instead of writing into the one the
// Tags value, and every other caller, still shares.
func (t *Tags) List() []string {
	return slices.Clip(t.list)
}
