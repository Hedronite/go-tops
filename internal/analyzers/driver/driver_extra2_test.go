package driver

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunOnlyIllTypedModule(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/onlybroken\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "broken")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "broken.go"), []byte("package broken\nfunc x( {\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}
