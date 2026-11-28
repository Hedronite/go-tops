package lawpass

import (
	"os"
	"path/filepath"
	"testing"
)

func TestModuleRelModuleLocalFile(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "a.go")
	if err := os.WriteFile(want, []byte("package a"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ModuleRel(dir, LawsConfigRel, "a.go")
	if got != "a.go" {
		t.Fatalf("got %q", got)
	}
}

func TestModuleRelUnderGoTopsConfig(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".go-tops")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(cfgDir, "x.go")
	if err := os.WriteFile(target, []byte("package x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ModuleRel(dir, LawsConfigRel, "x.go")
	if got != ".go-tops/x.go" {
		t.Fatalf("got %q", got)
	}
}

func TestModuleRelAbsoluteOutsideModule(t *testing.T) {
	dir := t.TempDir()
	got := ModuleRel(dir, "", "/tmp/outside/some.go")
	if got != "/tmp/outside/some.go" {
		t.Fatalf("got %q", got)
	}
}

func TestModuleRelAbsoluteInsideModule(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(dir, "internal", "foo.go")
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("package internal"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ModuleRel(dir, "", abs)
	if got != "internal/foo.go" {
		t.Fatalf("got %q", got)
	}
}