package unsafeuse

import (
	"go/parser"
	"go/token"
	"testing"
)

func TestFileDeclaresUnsafeIgnoresPackageLineComment(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", "package p //tops:unsafe\n", parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if fileDeclaresUnsafe(f) {
		t.Fatal("end-of-line package comment must not declare unsafe")
	}
}

func TestFileDeclaresUnsafeBlockBeforePackage(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", "/*tops:unsafe*/ package p\n", parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if !fileDeclaresUnsafe(f) {
		t.Fatal("block comment before package should declare unsafe")
	}
}

func TestIsPrePackageCommentGroupEndEqualsPackage(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", "/*tops:unsafe*/package p\n", parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if isPrePackageCommentGroup(f.Comments[0], f.Package) {
		t.Fatal("comment ending at package pos is not pre-package")
	}
	if fileDeclaresUnsafe(f) {
		t.Fatal("must not declare unsafe")
	}
}

func TestIsPrePackageCommentGroup(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", "/*x*/ package p\n", parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if !isPrePackageCommentGroup(f.Comments[0], f.Package) {
		t.Fatal("want pre-package")
	}
	f2, err := parser.ParseFile(fset, "x.go", "package p /*x*/\n", parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if isPrePackageCommentGroup(f2.Comments[0], f2.Package) {
		t.Fatal("post-package comment")
	}
}
