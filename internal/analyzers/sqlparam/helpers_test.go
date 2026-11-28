package sqlparam

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestFmtCallAndDynamicSQL(t *testing.T) {
	call := &ast.CallExpr{Fun: &ast.SelectorExpr{X: &ast.Ident{Name: "fmt"}, Sel: &ast.Ident{Name: "Sprintf"}}}
	if !fmtCall(call) {
		t.Fatal("fmt.Sprintf")
	}
	if !isFmtSprintf(&ast.Ident{Name: "Sprintf"}) {
		t.Fatal("bare Sprintf")
	}
	if isFmtSprintf(&ast.Ident{Name: "printf"}) {
		t.Fatal("wrong ident")
	}
	if dynamicSQL(&ast.BasicLit{Value: `"select"`}) {
		t.Fatal("literal sql is static")
	}
	plus := &ast.BinaryExpr{Op: token.ADD, X: &ast.Ident{Name: "a"}, Y: &ast.Ident{Name: "b"}}
	if !dynamicSQL(plus) {
		t.Fatal("concat is dynamic")
	}
}

func TestRunReportsSprintfSQL(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", `package p
import "database/sql"
import "fmt"
func f(db *sql.DB) { db.Query(fmt.Sprintf("x%d", 1)) }`, 0)
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

func TestRunAllowsConstSQL(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", `package p
import "database/sql"
func f(db *sql.DB) { db.Query("select 1") }`, 0)
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
