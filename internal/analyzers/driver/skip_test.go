package driver

import (
	"go/ast"
	"go/types"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestSkipPackage(t *testing.T) {
	if !skipPackage(&packages.Package{IllTyped: true}) {
		t.Fatal("ill-typed")
	}
	if !skipPackage(&packages.Package{Syntax: nil, Types: types.NewPackage("p", "p")}) {
		t.Fatal("empty syntax")
	}
	if !skipPackage(&packages.Package{Syntax: []*ast.File{{}}, Types: nil}) {
		t.Fatal("nil types")
	}
	ok := &packages.Package{Syntax: []*ast.File{{}}, Types: types.NewPackage("p", "p")}
	if skipPackage(ok) {
		t.Fatal("typed package should run")
	}
}
