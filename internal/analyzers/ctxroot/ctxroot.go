// Package ctxroot implements G-17: context.Background/TODO only on allow_paths.
package ctxroot

import (
	"go/ast"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/Hedronite/go-tops/internal/analyzers/pathglob"
)

const diag = "context.Background or context.TODO outside allow_paths (G-17)"

// DefaultAllowPaths are the G-17 allow_paths from laws/G-17.yaml.
var DefaultAllowPaths = []string{
	"cmd/**/main.go",
	"internal/app/wire*.go",
	"**/*_test.go",
}

// Analyzer flags detached contexts outside allowed files.
var Analyzer = &analysis.Analyzer{
	Name: "ctxroot",
	Doc:  "flags context.Background/TODO outside allow_paths (G-17)",
	Run:  run,
}

// AllowPaths is set by the driver before analysis.
var AllowPaths = DefaultAllowPaths

// ModuleDir is the module root for relativizing paths.
var ModuleDir string

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		rel := relFile(pass, f)
		if pathglob.Match(AllowPaths, rel) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok || id.Name != "context" {
				return true
			}
			switch sel.Sel.Name {
			case "Background", "TODO":
				pass.Reportf(call.Pos(), diag)
			}
			return true
		})
	}
	return nil, nil
}

func relFile(pass *analysis.Pass, f *ast.File) string {
	name := pass.Fset.File(f.Pos()).Name()
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/testdata/src/"); i >= 0 {
		return name[i+len("/testdata/src/"):]
	}
	if ModuleDir != "" {
		if rel, err := filepath.Rel(ModuleDir, name); err == nil {
			return strings.ReplaceAll(rel, "\\", "/")
		}
	}
	return filepath.Base(name)
}
