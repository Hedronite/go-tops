// Package httptimeout implements G-19: HTTP client/server timeouts in production code.
package httptimeout

import (
	"go/ast"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const (
	diagDefault = "http.DefaultClient has no timeout (G-19)"
	diagClient  = "http.Client without Timeout (G-19)"
	diagServer  = "http.Server without timeout fields (G-19)"
)

// Analyzer flags unbounded HTTP clients and servers in non-test code.
var Analyzer = &analysis.Analyzer{
	Name: "httptimeout",
	Doc:  "flags missing HTTP timeouts (G-19)",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.File(f.Pos()).Name(), "_test.go") {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			reportHTTPNode(pass, n)
			return true
		})
	}
	return nil, nil
}

func reportHTTPNode(pass *analysis.Pass, n ast.Node) {
	switch n := n.(type) {
	case *ast.SelectorExpr:
		if id, ok := n.X.(*ast.Ident); ok && id.Name == "http" && n.Sel.Name == "DefaultClient" {
			pass.Reportf(n.Pos(), diagDefault)
		}
	case *ast.CompositeLit:
		reportCompositeHTTP(pass, n)
	}
}

func reportCompositeHTTP(pass *analysis.Pass, n *ast.CompositeLit) {
	typ := typeName(n.Type)
	switch typ {
	case "http.Client", "Client":
		if !hasField(n, "Timeout") {
			pass.Reportf(n.Pos(), diagClient)
		}
	case "http.Server", "Server":
		if !hasAnyField(n, "ReadHeaderTimeout", "ReadTimeout", "WriteTimeout") {
			pass.Reportf(n.Pos(), diagServer)
		}
	}
}

func typeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.SelectorExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name + "." + t.Sel.Name
		}
		return t.Sel.Name
	case *ast.Ident:
		return t.Name
	default:
		return ""
	}
}

func hasField(lit *ast.CompositeLit, name string) bool {
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if keyName(kv.Key) == name {
			return true
		}
	}
	return false
}

func hasAnyField(lit *ast.CompositeLit, names ...string) bool {
	for _, n := range names {
		if hasField(lit, n) {
			return true
		}
	}
	return false
}

func keyName(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}
