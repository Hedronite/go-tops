// Package sqlparam implements G-38: no string-built SQL for Query/Exec.
package sqlparam

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
)

const diag = "SQL built from string concat or Sprintf (G-38); use placeholders"

// Analyzer flags non-constant first arguments to database/sql Exec/Query calls.
var Analyzer = &analysis.Analyzer{
	Name: "sqlparam",
	Doc:  "flags string-built SQL (G-38)",
	Run:  run,
}

var sqlMethods = map[string]bool{
	"Query": true, "QueryContext": true, "Exec": true, "ExecContext": true,
}

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !sqlMethods[sel.Sel.Name] {
				return true
			}
			if dynamicSQL(call.Args[0]) {
				pass.Reportf(call.Pos(), diag)
			}
			return true
		})
	}
	return nil, nil
}

func dynamicSQL(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.BasicLit:
		return false
	case *ast.BinaryExpr:
		if e.Op.String() == "+" {
			return true
		}
	case *ast.CallExpr:
		if fmtCall(e) {
			return true
		}
	}
	return false
}

func fmtCall(c *ast.CallExpr) bool {
	switch fun := c.Fun.(type) {
	case *ast.SelectorExpr:
		if id, ok := fun.X.(*ast.Ident); ok && id.Name == "fmt" && fun.Sel.Name == "Sprintf" {
			return true
		}
	case *ast.Ident:
		if fun.Name == "Sprintf" {
			return true
		}
	}
	return false
}
