package synctest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestIsTestFile(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x_test.go", "package p\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !IsTestFile(fset, f.Pos()) {
		t.Fatal("want test file")
	}
	f2, err := parser.ParseFile(fset, "x.go", "package p\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	if IsTestFile(fset, f2.Pos()) {
		t.Fatal("want non-test file")
	}
}

func TestRunReportsSleep(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x_test.go", `package p
import "time"
func TestX(t *testing.T) { time.Sleep(1) }`, 0)
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

func TestRunIgnoresProdFile(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", `package p
import "time"
func f() { time.Sleep(1) }`, 0)
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
		t.Fatalf("reports=%d want 0", reports)
	}
}
