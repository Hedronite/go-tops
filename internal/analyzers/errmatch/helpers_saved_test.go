package errmatch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestSavedErrStringTriggersBinaryMatch(t *testing.T) {
	const src = `package p
func f(err error) {
	msg := err.Error()
	_ = msg == "x"
}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, 0)
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
		t.Fatalf("reports=%d want 1 for saved err.Error match", reports)
	}
}

func TestRecordSavedRejectsMultiAssign(t *testing.T) {
	saved := map[string]struct{}{}
	stmt := &ast.AssignStmt{Lhs: []ast.Expr{&ast.Ident{}, &ast.Ident{}}, Rhs: []ast.Expr{&ast.CallExpr{}}}
	recordSavedErrorString(stmt, saved)
	if len(saved) != 0 {
		t.Fatal("want no save on multi lhs")
	}
}
