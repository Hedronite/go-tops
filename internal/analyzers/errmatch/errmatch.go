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
		if isTestGoFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			reportErrmatchNode(pass, n)
			return true
		})
	}
	return nil, nil
}

func isTestGoFile(pass *analysis.Pass, f *ast.File) bool {
	return strings.HasSuffix(pass.Fset.File(f.Pos()).Name(), "_test.go")
}

func reportErrmatchNode(pass *analysis.Pass, n ast.Node) {
	switch n := n.(type) {
	case *ast.BinaryExpr:
		reportBinaryErrmatch(pass, n)
	case *ast.CallExpr:
		if isContainsErrorString(n) {
			pass.Reportf(n.Pos(), diag)
		}
	case *ast.SwitchStmt:
		reportSwitchErrmatch(pass, n)
	}
}

func reportBinaryErrmatch(pass *analysis.Pass, n *ast.BinaryExpr) {
	op := n.Op.String()
	if op != "==" && op != "!=" {
		return
	}
	if isErrorString(n.X) || isErrorString(n.Y) {
		pass.Reportf(n.Pos(), diag)
	}
}

func reportSwitchErrmatch(pass *analysis.Pass, n *ast.SwitchStmt) {
	tag, ok := n.Tag.(*ast.CallExpr)
	if !ok || !isErrorString(tag) {
		return
	}
	pass.Reportf(n.Pos(), diag)
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
