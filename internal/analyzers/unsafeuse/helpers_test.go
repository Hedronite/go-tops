package unsafeuse

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestPackageDeclaredUnsafe(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", `//tops:unsafe
package p
`, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	pass := &analysis.Pass{Files: []*ast.File{f}}
	if !packageDeclaredUnsafe(pass) {
		t.Fatal("want declared")
	}
	f2, err := parser.ParseFile(fset, "y.go", "package p\n", parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	pass2 := &analysis.Pass{Files: []*ast.File{f2}}
	if packageDeclaredUnsafe(pass2) {
		t.Fatal("want not declared")
	}
}

func TestRunRequiresSafetyComment(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", `//tops:unsafe
package p
import "unsafe"
func f() { _ = unsafe.Sizeof(0) }`, parser.ParseComments)
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
		t.Fatalf("reports=%d want 1 for missing SAFETY", reports)
	}
}

func TestRunAllowsWithSafety(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", `//tops:unsafe
package p
import "unsafe"
func f() {
	// SAFETY: sized type
	_ = unsafe.Sizeof(0)
}`, parser.ParseComments)
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

func TestRunRejectsOutsideUnsafePackage(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", `package p
import "unsafe"
func f() { _ = unsafe.Sizeof(0) }`, 0)
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
