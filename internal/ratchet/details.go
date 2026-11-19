package ratchet

import (
	"encoding/json"

	"github.com/Hedronite/go-tops/internal/report"
)

// A layer's details reach the ratchet as the layer built them, with ints and
// []any, or as a stored report gives them back, with float64 and []any: and a
// layer is free to build a typed slice of rows. Reading a value by a type
// assertion on one of those forms reads the other as nothing, so every reader works
// on layers that have been through the JSON round trip, which makes every number a
// float64, every list a []any and every row a map[string]any.

// plain returns the layers with their details as a JSON round trip makes them. A
// layer whose details cannot be marshalled keeps them: the readers then find
// nothing in them and the layer is left as it ran.
func plain(layers []report.Layer) []report.Layer {
	out := make([]report.Layer, len(layers))
	copy(out, layers)
	for i := range out {
		if d, ok := roundTrip(out[i].Details); ok {
			out[i].Details = d
		}
	}
	return out
}

func roundTrip(details map[string]any) (map[string]any, bool) {
	if details == nil {
		return nil, true
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return nil, false
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, false
	}
	return out, true
}

// number is v as an int, or 0 when it is not a number.
func number(v any) int {
	f, ok := v.(float64)
	if !ok {
		return 0
	}
	return int(f)
}

// text is v as a string, or "" when it is not one.
func text(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// listOf is v as a list of objects; anything else in it is skipped.
func listOf(v any) []map[string]any {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []map[string]any
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
