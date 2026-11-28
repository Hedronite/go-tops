package unsafeuse

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestRunIgnoresNonUnsafeSelector(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", `package p
var x int
func f() { _ = x }`, 0)
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

func TestFileDeclaresUnsafeIgnoresPostPackageComment(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", `package p
//tops:unsafe
var x int
`, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if fileDeclaresUnsafe(f) {
		t.Fatal("comment after package should not declare unsafe")
	}
}

func TestHasSafetyCommentWrongLine(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", `//tops:unsafe
package p
import "unsafe"
// SAFETY: too far
func f() {
	_ = unsafe.Sizeof(0)
}`, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	pass := &analysis.Pass{Fset: fset, Files: []*ast.File{f}}
	var pos token.Pos
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "unsafe" {
				pos = sel.Pos()
			}
		}
		return true
	})
	if hasSafetyComment(pass, pos) {
		t.Fatal("SAFETY not on prior line")
	}
}
