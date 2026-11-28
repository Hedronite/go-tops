package synctest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestRunIgnoresTimeDotAfterSelector(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x_test.go", `package p
import "time"
func TestX(t *testing.T) {
	ch := time.After(1)
	_ = ch
}`, 0)
	if err != nil {
		t.Fatal(err)
	}
	var reports int
	pass := &analysis.Pass{
		Analyzer: Analyzer, Fset: fset, Files: []*ast.File{f},
		Report: func(analysis.Diagnostic) { reports++ },
	}
	if _, err := run(pass); err != nil {
		t.Fatal(err)
	}
	if reports != 0 {
		t.Fatalf("reports=%d", reports)
	}
}
