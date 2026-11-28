package driver

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunSkipsIllTypedSource(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/broken\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	good := "package main\n\nimport \"context\"\n\nfunc main() { _ = context.Background() }\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "broken")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	bad := "package broken\n\nfunc broken( {\n"
	if err := os.WriteFile(filepath.Join(sub, "broken.go"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ByLaw["G-17"] < 1 {
		t.Fatalf("G-17=%d want diagnostics from typed package only", res.ByLaw["G-17"])
	}
}
