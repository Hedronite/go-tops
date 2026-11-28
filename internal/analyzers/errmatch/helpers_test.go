package errmatch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestIsErrorStringBranches(t *testing.T) {
	if isErrorString(&ast.Ident{Name: "x"}) {
		t.Fatal("ident")
	}
	fset := token.NewFileSet()
	const src = `package p
func f(err error) bool {
	return err.Error() == "x"
}`
	f, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && isErrorString(call) {
			found = true
		}
		return true
	})
	if !found {
		t.Fatal("want err.Error call")
	}
}

func TestContainsErrorString(t *testing.T) {
	fset := token.NewFileSet()
	const src = `package p
import "strings"
func f(err error) bool {
	return strings.Contains(err.Error(), "x")
}`
	f, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && isContainsErrorString(call) {
			found = true
		}
		return true
	})
	if !found {
		t.Fatal("want strings.Contains err.Error")
	}
}

func TestRunReportsInTestGoFiles(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x_test.go", `package p
func TestX(t *testing.T) {
	var err error
	_ = err.Error() == "x"
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
	if reports != 1 {
		t.Fatalf("reports=%d want 1 in _test.go", reports)
	}
}

func TestRunReportsSwitchOnError(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", `package p
func f(err error) {
	switch err.Error() {}
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
	if reports != 1 {
		t.Fatalf("reports=%d want 1", reports)
	}
}

func TestRunReportsNotEqual(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", `package p
func f(err error) {
	_ = err.Error() != "x"
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
	if reports != 1 {
		t.Fatalf("reports=%d want 1", reports)
	}
}

func TestIsContainsRejectsShortArgList(t *testing.T) {
	call := &ast.CallExpr{
		Fun: &ast.SelectorExpr{
			X:   &ast.Ident{Name: "strings"},
			Sel: &ast.Ident{Name: "Contains"},
		},
		Args: []ast.Expr{&ast.Ident{Name: "only"}},
	}
	if isContainsErrorString(call) {
		t.Fatal("want false for len(args)<2")
	}
}

func TestIsContainsRejectsNonStringsPkg(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", `package p
import "bytes"
func f(err error) {
	_ = bytes.Contains([]byte("x"), []byte("y"))
}`, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && isContainsErrorString(call) {
			t.Fatal("bytes.Contains should not match")
		}
		return true
	})
}
