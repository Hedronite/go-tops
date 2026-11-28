package unsafeuse

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestCommentGroupDeclaresUnsafe(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", `//tops:unsafe
package p
`, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if !commentGroupDeclaresUnsafe(f.Comments[0]) {
		t.Fatal("want tops:unsafe in comment group")
	}
}

func TestHasSafetyCommentAbsent(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", `//tops:unsafe
package p
import "unsafe"
func f() { _ = unsafe.Alignof(0) }`, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	pass := &analysis.Pass{Fset: fset, Files: []*ast.File{f}}
	var pos token.Pos
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			pos = sel.Pos()
		}
		return true
	})
	if hasSafetyComment(pass, pos) {
		t.Fatal("want no SAFETY comment")
	}
}

func TestFileDeclaresUnsafeFalse(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", "package p\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	if fileDeclaresUnsafe(f) {
		t.Fatal("plain package")
	}
}
