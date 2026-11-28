package errmatch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestIsErrorStringRejectsWrongSelector(t *testing.T) {
	call := &ast.CallExpr{
		Fun: &ast.SelectorExpr{X: &ast.Ident{Name: "err"}, Sel: &ast.Ident{Name: "String"}},
	}
	if isErrorString(call) {
		t.Fatal("String() is not Error()")
	}
}

func TestReportSwitchIgnoresExprTag(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", `package p
func f(x int) {
	switch x {}
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

func TestIsContainsSecondArgError(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", `package p
import "strings"
func f(a, b error) {
	_ = strings.Contains("x", b.Error())
}`, 0)
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
		t.Fatal("want match on second arg")
	}
}
