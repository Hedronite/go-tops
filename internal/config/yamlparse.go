package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"

	"go.yaml.in/yaml/v3"
)

// maxBytes bounds every file check reads: a config, a ledger or a lock is a few
// kilobytes, and a larger one is a mistake or an attack.
const maxBytes = 1 << 20

// parseYAML decodes one YAML document into the generic value the schema
// validator takes (maps, slices, strings, json.Number, bool, nil). It is
// stricter than YAML: no anchors, aliases or merge keys, no key that is not a
// string, no repeated key, no second document, and a date written without
// quotes is an error, because §6.8 wants a quoted "YYYY-MM-DD".
func parseYAML(data []byte) (any, []Issue) {
	if len(data) > maxBytes {
		return nil, []Issue{{Rule: "yaml", Msg: fmt.Sprintf("larger than %d bytes", maxBytes)}}
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var root yaml.Node
	if err := dec.Decode(&root); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, []Issue{{Rule: "yaml", Msg: "the file is empty"}}
		}
		return nil, []Issue{{Rule: "yaml", Msg: "invalid YAML: " + err.Error()}}
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, []Issue{{Rule: "yaml", Msg: "more than one YAML document, or text after the first"}}
	}
	w := &walker{}
	v := w.value(&root, "")
	return v, w.issues
}

type walker struct {
	issues []Issue
}

func (w *walker) bad(ptr, format string, a ...any) {
	w.issues = append(w.issues, Issue{Rule: "yaml", Pointer: ptr, Msg: fmt.Sprintf(format, a...)})
}

func (w *walker) value(n *yaml.Node, ptr string) any {
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil
		}
		n = n.Content[0]
	}
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		w.bad(ptr, "anchors and aliases are not allowed")
		return nil
	}
	switch n.Kind {
	case yaml.MappingNode:
		return w.mapping(n, ptr)
	case yaml.SequenceNode:
		return w.sequence(n, ptr)
	case yaml.ScalarNode:
		return w.scalar(n, ptr)
	default:
		w.bad(ptr, "unsupported YAML node at line %d", n.Line)
		return nil
	}
}

func (w *walker) mapping(n *yaml.Node, ptr string) any {
	m := make(map[string]any, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind == yaml.ScalarNode && k.ShortTag() == "!!merge" {
			w.bad(ptr, "merge keys (<<) are not allowed")
			continue
		}
		if k.Kind != yaml.ScalarNode || k.ShortTag() != "!!str" {
			w.bad(ptr, "the mapping key at line %d is not a string", k.Line)
			continue
		}
		child := ptr + "/" + escape(k.Value)
		if _, dup := m[k.Value]; dup {
			w.bad(child, "key %q appears twice", k.Value)
			continue
		}
		m[k.Value] = w.value(v, child)
	}
	return m
}

func (w *walker) sequence(n *yaml.Node, ptr string) any {
	s := make([]any, len(n.Content))
	for i, c := range n.Content {
		s[i] = w.value(c, ptr+"/"+strconv.Itoa(i))
	}
	return s
}

func (w *walker) scalar(n *yaml.Node, ptr string) any {
	switch tag := n.ShortTag(); tag {
	case "!!str":
		return n.Value
	case "!!null":
		return nil
	case "!!bool":
		var b bool
		if err := n.Decode(&b); err != nil {
			w.bad(ptr, "not a boolean: %q", n.Value)
		}
		return b
	case "!!int":
		return w.integer(n, ptr)
	case "!!float":
		return w.float(n, ptr)
	case "!!timestamp":
		w.bad(ptr, "unquoted date or time %q: write a date as a quoted string, \"YYYY-MM-DD\"", n.Value)
		return n.Value
	default:
		w.bad(ptr, "unsupported YAML type %s", tag)
		return nil
	}
}

func (w *walker) integer(n *yaml.Node, ptr string) any {
	var i int64
	if err := n.Decode(&i); err != nil {
		w.bad(ptr, "integer %q is out of range", n.Value)
		return nil
	}
	return json.Number(strconv.FormatInt(i, 10))
}

func (w *walker) float(n *yaml.Node, ptr string) any {
	var f float64
	if err := n.Decode(&f); err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		w.bad(ptr, "number %q is not a finite number", n.Value)
		return nil
	}
	return json.Number(strconv.FormatFloat(f, 'g', -1, 64))
}
