package ctxroot

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestRunIgnoresContextImportAlias(t *testing.T) {
	fset := token.NewFileSet()
	name := "/tmp/proj/testdata/src/work/alias.go"
	f, err := parser.ParseFile(fset, name, `package work
import ctx "context"
func f() { ctx.Background() }`, 0)
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
		t.Fatalf("reports=%d want 0 for non-context ident", reports)
	}
}

func TestRunIgnoresNonSelectorCall(t *testing.T) {
	fset := token.NewFileSet()
	name := "/tmp/proj/testdata/src/work/call.go"
	f, err := parser.ParseFile(fset, name, `package work
func f() { f() }`, 0)
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
