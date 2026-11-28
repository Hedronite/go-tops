package ctxroot

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestRelFilePaths(t *testing.T) {
	fset := token.NewFileSet()
	name := "/tmp/proj/testdata/src/apkg/x.go"
	f, err := parser.ParseFile(fset, name, "package apkg\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	pass := &analysis.Pass{Fset: fset, Files: []*ast.File{f}}
	if got := relFile(pass, f); got != "apkg/x.go" {
		t.Fatalf("testdata rel=%q", got)
	}

	dir := t.TempDir()
	ModuleDir = dir
	t.Cleanup(func() { ModuleDir = "" })
	full := filepath.Join(dir, "internal", "work", "ctx.go")
	f2, err := parser.ParseFile(fset, full, "package work\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	want := "internal/work/ctx.go"
	if got := relFile(pass, f2); got != want {
		t.Fatalf("module rel=%q want %q", got, want)
	}

	f3, err := parser.ParseFile(fset, "orphan.go", "package orphan\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := relFile(pass, f3); got != "orphan.go" {
		t.Fatalf("base=%q", got)
	}
}

func TestRunReportsDetachedContext(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "work/ctx.go", `package work
import "context"
func f() { context.Background() }`, 0)
	if err != nil {
		t.Fatal(err)
	}
	AllowPaths = []string{"cmd/**/main.go"}
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

func TestRunAllowsAllowedPath(t *testing.T) {
	fset := token.NewFileSet()
	name := "/tmp/proj/testdata/src/cmd/app/main.go"
	f, err := parser.ParseFile(fset, name, `package main
import "context"
func main() { context.Background() }`, 0)
	if err != nil {
		t.Fatal(err)
	}
	AllowPaths = DefaultAllowPaths
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
