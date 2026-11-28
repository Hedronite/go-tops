// Package errmatch implements the G-11 err.Error() string-matching half.
package errmatch

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
)

const diag = "do not match on err.Error(); use errors.Is or errors.As (G-11)"

var stringsMatchers = map[string]bool{
	"Contains":  true,
	"HasPrefix": true,
	"HasSuffix": true,
	"EqualFold": true,
}

// Analyzer flags string comparisons and switches on err.Error().
var Analyzer = &analysis.Analyzer{
	Name: "errmatch",
	Doc:  "flags err.Error() string matching (G-11)",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			inspectFunc(pass, fd.Body)
		}
	}
	return nil, nil
}

func inspectFunc(pass *analysis.Pass, body *ast.BlockStmt) {
	if body == nil {
		return
	}
	saved := map[string]struct{}{}
	walk := func(n ast.Node) bool {
		if n == nil {
			return false
		}
		switch n := n.(type) {
		case *ast.AssignStmt:
			recordSavedErrorString(n, saved)
		case *ast.BinaryExpr:
			reportBinaryErrmatch(pass, n, saved)
		case *ast.CallExpr:
			if isStringsErrmatch(n, saved) {
				pass.Reportf(n.Pos(), diag)
			}
		case *ast.SwitchStmt:
			reportSwitchErrmatch(pass, n, saved)
		}
		return true
	}
	ast.Inspect(body, walk)
}

func recordSavedErrorString(stmt *ast.AssignStmt, saved map[string]struct{}) {
	if len(stmt.Lhs) != 1 || len(stmt.Rhs) != 1 {
		return
	}
	id, ok := stmt.Lhs[0].(*ast.Ident)
	if !ok || !isErrorString(stmt.Rhs[0]) {
		return
	}
	saved[id.Name] = struct{}{}
}

func reportBinaryErrmatch(pass *analysis.Pass, n *ast.BinaryExpr, saved map[string]struct{}) {
	op := n.Op.String()
	if op != "==" && op != "!=" {
		return
	}
	if matchesErrString(n.X, saved) || matchesErrString(n.Y, saved) {
		pass.Reportf(n.Pos(), diag)
	}
}

func reportSwitchErrmatch(pass *analysis.Pass, n *ast.SwitchStmt, saved map[string]struct{}) {
	if n.Tag != nil && matchesErrString(n.Tag, saved) {
		pass.Reportf(n.Pos(), diag)
	}
}

func matchesErrString(e ast.Expr, saved map[string]struct{}) bool {
	if isErrorString(e) {
		return true
	}
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	_, ok = saved[id.Name]
	return ok
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

// isContainsErrorString reports strings.Contains(..., err.Error()) style calls.
func isContainsErrorString(call *ast.CallExpr) bool {
	return isStringsErrmatch(call, nil)
}

func isStringsErrmatch(call *ast.CallExpr, saved map[string]struct{}) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !stringsMatchers[sel.Sel.Name] {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "strings" {
		return false
	}
	if len(call.Args) < 2 {
		return false
	}
	return matchesErrString(call.Args[0], saved) || matchesErrString(call.Args[1], saved)
}
