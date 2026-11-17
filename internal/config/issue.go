// Package config loads and checks go-tops.yaml (GO_TOPS.md §9, §12.1): the
// document is parsed from YAML, validated against the embedded JSON Schema, and
// then held to the semantic rules the schema cannot state. Every finding is an
// Issue that names its file, a rule and the JSON pointer of the offending value.
package config

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// Issue is one finding. Pointer is an RFC 6901 JSON pointer into File; the
// empty pointer is the whole document.
type Issue struct {
	File    string
	Rule    string
	Pointer string
	Msg     string
}

// String renders the issue as one line: file, pointer, message and rule.
func (i Issue) String() string {
	at := i.Pointer
	if at == "" {
		at = "(document)"
	}
	return fmt.Sprintf("%s: %s: %s (%s)", i.File, at, i.Msg, i.Rule)
}

func sortIssues(issues []Issue) []Issue {
	slices.SortFunc(issues, func(a, b Issue) int {
		return cmp.Or(
			cmp.Compare(a.File, b.File),
			cmp.Compare(a.Pointer, b.Pointer),
			cmp.Compare(a.Rule, b.Rule),
			cmp.Compare(a.Msg, b.Msg),
		)
	})
	return slices.Compact(issues)
}

// asMap and asList narrow a parsed value; anything else is nil, which reads as
// empty.
func asMap(v any) map[string]any {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return m
}

func asList(v any) []any {
	l, ok := v.([]any)
	if !ok {
		return nil
	}
	return l
}

// escape encodes one key as an RFC 6901 reference token.
func escape(key string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(key)
}
