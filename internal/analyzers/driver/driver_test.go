package driver

import (
	"context"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestRunOnKitModule(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ByLaw == nil {
		t.Fatal("ByLaw nil")
	}
}

func TestShouldSkipPkg(t *testing.T) {
	skip := &packages.Package{CompiledGoFiles: []string{
		"/tmp/x/internal/analyzers/foo/testdata/src/tp/tp.go",
	}}
	if !shouldSkipPkg(skip) {
		t.Fatal("want skip for internal/analyzers testdata")
	}
	keep := &packages.Package{CompiledGoFiles: []string{"/tmp/x/cmd/foo/main.go"}}
	if shouldSkipPkg(keep) {
		t.Fatal("want keep for normal package")
	}
	other := &packages.Package{CompiledGoFiles: []string{"/tmp/x/foo/testdata/bar.go"}}
	if shouldSkipPkg(other) {
		t.Fatal("want keep for non-analyzer testdata")
	}
}
