// Package errmatch implements the G-11 err.Error() string-matching half.
package errmatch

import (
	"go/ast"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const diag = "do not match on err.Error(); use errors.Is or errors.As (G-11)"

// Analyzer flags string comparisons and switches on err.Error().
var Analyzer = &analysis.Analyzer{
	Name: "errmatch",
	Doc:  "flags err.Error() string matching (G-11)",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.File(f.Pos()).Name(), "_test.go") {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.BinaryExpr:
				if n.Op.String() == "==" || n.Op.String() == "!=" {
					if isErrorString(n.X) || isErrorString(n.Y) {
						pass.Reportf(n.Pos(), diag)
					}
				}
			case *ast.CallExpr:
				if isContainsErrorString(n) {
					pass.Reportf(n.Pos(), diag)
				}
			case *ast.SwitchStmt:
				if tag, ok := n.Tag.(*ast.CallExpr); ok && isErrorString(tag) {
					pass.Reportf(n.Pos(), diag)
				}
			}
			return true
		})
	}
	return nil, nil
}

func isErrorString(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Error" {
		return false
	}
	_, ok = sel.X.(*ast.Ident)
	return ok
}

func isContainsErrorString(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Contains" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "strings" {
		return false
	}
	if len(call.Args) < 2 {
		return false
	}
	return isErrorString(call.Args[0]) || isErrorString(call.Args[1])
}
