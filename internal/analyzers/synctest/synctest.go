// Package synctest implements G-08: no time.Sleep in *_test.go files.
package synctest

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const diag = "time.Sleep in tests; use synctest or synchronization (G-08)"

// Analyzer flags time.Sleep in test files.
var Analyzer = &analysis.Analyzer{
	Name: "synctest",
	Doc:  "flags time.Sleep in Go tests (G-08)",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		if !strings.HasSuffix(pass.Fset.File(f.Pos()).Name(), "_test.go") {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Sleep" {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok || id.Name != "time" {
				return true
			}
			pass.Reportf(call.Pos(), diag)
			return true
		})
	}
	return nil, nil
}

// IsTestFile reports whether the file set position is in a test file.
func IsTestFile(fset *token.FileSet, pos token.Pos) bool {
	return strings.HasSuffix(fset.File(pos).Name(), "_test.go")
}
