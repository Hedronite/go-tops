package laws

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckFactsMissingDir(t *testing.T) {
	_, err := CheckFacts(context.Background(), filepath.Join(os.TempDir(), "no-such-gt-m5-dir"), "binary-cli", nil)
	if err == nil {
		t.Fatal("want walk error")
	}
}

func TestCheckFactsPropagatesG33(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module m\n\ngo 1.26\n")
	copyLock(t, dir)
	out, err := CheckFacts(context.Background(), dir, "binary-cli", func() (string, error) { return "go1.26.0", nil })
	if err != nil {
		t.Fatal(err)
	}
	if out["G-33"] != 1 {
		t.Fatalf("G-33=%d", out["G-33"])
	}
}

func TestFileHasCImportNegative(t *testing.T) {
	if FileHasCImport([]byte("package p\n")) {
		t.Fatal("no import C")
	}
}
