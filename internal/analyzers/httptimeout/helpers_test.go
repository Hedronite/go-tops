package httptimeout

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestTypeNameAndFields(t *testing.T) {
	id := typeName(&ast.Ident{Name: "Client"})
	if id != "Client" {
		t.Fatalf("Client=%q", id)
	}
	sel := typeName(&ast.SelectorExpr{X: &ast.Ident{Name: "http"}, Sel: &ast.Ident{Name: "Server"}})
	if sel != "http.Server" {
		t.Fatalf("sel=%q", sel)
	}
	lit := &ast.CompositeLit{
		Type: &ast.Ident{Name: "Client"},
		Elts: []ast.Expr{&ast.KeyValueExpr{Key: &ast.Ident{Name: "Timeout"}, Value: &ast.Ident{Name: "d"}}},
	}
	if !hasField(lit, "Timeout") {
		t.Fatal("want Timeout field")
	}
	if hasAnyField(lit, "ReadTimeout", "WriteTimeout") {
		t.Fatal("want no server fields")
	}
	srv := &ast.CompositeLit{
		Type: &ast.Ident{Name: "Server"},
		Elts: []ast.Expr{&ast.KeyValueExpr{Key: &ast.Ident{Name: "ReadTimeout"}, Value: &ast.Ident{Name: "d"}}},
	}
	if !hasAnyField(srv, "ReadHeaderTimeout", "ReadTimeout", "WriteTimeout") {
		t.Fatal("want ReadTimeout")
	}
}

func TestRunReportsDefaultClient(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", `package p
import "net/http"
func f() { _ = http.DefaultClient }`, 0)
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

func TestRunSkipsTestFile(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x_test.go", `package p
import "net/http"
func TestX(t *testing.T) { _ = http.DefaultClient }`, 0)
	if err != nil {
		t.Fatal(err)
	}
	pass := &analysis.Pass{Analyzer: Analyzer, Fset: fset, Files: []*ast.File{f}}
	if _, err := run(pass); err != nil {
		t.Fatal(err)
	}
}

func TestRunClientWithTimeoutClean(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", `package p
import "net/http"
import "time"
func f() { _ = http.Client{Timeout: time.Second} }`, 0)
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
