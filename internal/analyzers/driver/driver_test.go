package driver

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestRunOnTempModule(t *testing.T) {
	dir := t.TempDir()
	mod := filepath.Join(dir, "go.mod")
	const modBody = "module example.com/driverprobe\n\ngo 1.26\n"
	if err := os.WriteFile(mod, []byte(modBody), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "main.go")
	const srcBody = "package main\n\nimport \"context\"\n\nfunc main() { _ = context.Background() }\n"
	if err := os.WriteFile(src, []byte(srcBody), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ByLaw == nil {
		t.Fatal("ByLaw nil")
	}
	if res.ByLaw["G-17"] < 1 {
		t.Fatalf("G-17=%d want at least one ctx diagnostic", res.ByLaw["G-17"])
	}
	if res.ByLawFile == nil || res.ByLawFile["G-17"]["main.go"] < 1 {
		t.Fatalf("ByLawFile=%+v want main.go entry", res.ByLawFile)
	}
}

func TestModuleRelAbsoluteAndEmpty(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(dir, "pkg", "a.go")
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	got := moduleRel(dir, abs)
	if got != "pkg/a.go" {
		t.Fatalf("got %q", got)
	}
	if moduleRel(dir, "") != "(no file)" {
		t.Fatal("want placeholder for empty name")
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
