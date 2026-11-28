package errmatch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
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
