// Package unsafeuse implements G-25: unsafe only in //tops:unsafe packages with SAFETY comments.
package unsafeuse

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const (
	diagPkg    = "unsafe use outside a //tops:unsafe package (G-25)"
	diagSafety = "unsafe use requires a // SAFETY: comment on the prior line (G-25)"
)

// Analyzer confines unsafe and requires SAFETY comments.
var Analyzer = &analysis.Analyzer{
	Name: "unsafeuse",
	Doc:  "flags unsafe outside declared packages (G-25)",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	declared := packageDeclaredUnsafe(pass)
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			reportUnsafeNode(pass, n, declared)
			return true
		})
	}
	return nil, nil
}

func reportUnsafeNode(pass *analysis.Pass, n ast.Node, declared bool) {
	sel, ok := n.(*ast.SelectorExpr)
	if !ok {
		return
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok || id.Name != "unsafe" {
		return
	}
	if !declared {
		pass.Reportf(n.Pos(), diagPkg)
		return
	}
	if !hasSafetyComment(pass, n.Pos()) {
		pass.Reportf(n.Pos(), diagSafety)
	}
}

func packageDeclaredUnsafe(pass *analysis.Pass) bool {
	for _, f := range pass.Files {
		if fileDeclaresUnsafe(f) {
			return true
		}
	}
	return false
}

func fileDeclaresUnsafe(f *ast.File) bool {
	if docDeclaresUnsafe(f.Doc) {
		return true
	}
	for _, cg := range f.Comments {
		if !isPrePackageCommentGroup(cg, f.Package) {
			continue
		}
		if commentGroupDeclaresUnsafe(cg) {
			return true
		}
	}
	return false
}

func docDeclaresUnsafe(doc *ast.CommentGroup) bool {
	if doc == nil {
		return false
	}
	for _, c := range doc.List {
		if strings.Contains(c.Text, "tops:unsafe") {
			return true
		}
	}
	return false
}

func isPrePackageCommentGroup(cg *ast.CommentGroup, pkgPos token.Pos) bool {
	return cg.End() < pkgPos
}

func commentGroupDeclaresUnsafe(cg *ast.CommentGroup) bool {
	for _, c := range cg.List {
		if strings.Contains(c.Text, "tops:unsafe") {
			return true
		}
	}
	return false
}

func hasSafetyComment(pass *analysis.Pass, pos token.Pos) bool {
	line := pass.Fset.File(pos).Line(pos)
	for _, f := range pass.Files {
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				if pass.Fset.File(c.Slash).Line(c.Slash) == line-1 && strings.Contains(c.Text, "SAFETY:") {
					return true
				}
			}
		}
	}
	return false
}
