package crap

import (
	"testing"

	"github.com/Hedronite/go-tops/internal/complexity"
)

func fnAt(file, name string, line int) *fn {
	return &fn{Func: complexity.Func{File: file, Name: name, Span: complexity.Span{Start: complexity.Pos{Line: line, Col: 1}}}}
}

// The two parses of a file must agree. A function in a file the typed load
// compiled that the typed load does not list is an error: counting it
// as "not measured" would hide a function the run did measure.
func TestSplitRejectsParsesThatDisagree(t *testing.T) {
	typed := []complexity.Func{{File: "a.go", Name: "A", Span: complexity.Span{Start: complexity.Pos{Line: 3, Col: 1}}}}
	if _, err := split([]*fn{fnAt("a.go", "A", 3), fnAt("a.go", "B", 9)}, typed); err == nil {
		t.Error("a function missing from a compiled file was accepted")
	}
	p, err := split([]*fn{fnAt("a.go", "A", 3), fnAt("b.go", "B", 1)}, typed)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.measured) != 1 || len(p.notMeasured) != 1 || p.notMeasured[0].Name != "B" {
		t.Errorf("split: %d measured, %d not measured; want A measured and B not", len(p.measured), len(p.notMeasured))
	}
}
