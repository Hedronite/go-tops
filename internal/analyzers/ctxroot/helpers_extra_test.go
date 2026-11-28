package ctxroot

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestRunReportsTODO(t *testing.T) {
	fset := token.NewFileSet()
	name := "/tmp/proj/testdata/src/work/todo.go"
	f, err := parser.ParseFile(fset, name, `package work
import "context"
func f() { context.TODO() }`, 0)
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
	if reports != 1 {
		t.Fatalf("reports=%d want 1", reports)
	}
}

func TestRelFileTestdataAtRoot(t *testing.T) {
	fset := token.NewFileSet()
	name := "/testdata/src/x/y.go"
	f, err := parser.ParseFile(fset, name, "package y\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	pass := &analysis.Pass{Fset: fset, Files: []*ast.File{f}}
	if got := relFile(pass, f); got != "x/y.go" {
		t.Fatalf("got %q", got)
	}
}

func TestRunIgnoresNonContextCall(t *testing.T) {
	fset := token.NewFileSet()
	name := "/tmp/proj/testdata/src/work/other.go"
	f, err := parser.ParseFile(fset, name, `package work
import "fmt"
func f() { fmt.Println("x") }`, 0)
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
