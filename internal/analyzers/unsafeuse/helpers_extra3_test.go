package unsafeuse

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestRunIgnoresUnsafeImportAlias(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", `package p
import u "unsafe"
func f() { _ = u.Sizeof(0) }`, 0)
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
		t.Fatalf("reports=%d want package diag", reports)
	}
}

func TestFileDeclaresUnsafeFromPrePackageComment(t *testing.T) {
	fset := token.NewFileSet()
	const src = `//tops:unsafe
package p
`
	f, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if !fileDeclaresUnsafe(f) {
		t.Fatal("file comment before package")
	}
	_ = ast.IsGenerated(f)
}

func TestFileDeclaresUnsafePrePackageComment(t *testing.T) {
	fset := token.NewFileSet()
	const src = `// tops:unsafe kernel
package p
`
	f, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if !fileDeclaresUnsafe(f) {
		t.Fatal("pre-package comment should declare unsafe")
	}
}
