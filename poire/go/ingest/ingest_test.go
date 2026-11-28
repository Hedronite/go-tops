package ingest

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadStaysInsideItsDirectory(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ok.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}

	got, err := Read(dir, "ok.txt")
	if err != nil || string(got) != "hello" {
		t.Fatalf("Read(ok.txt) = %q, %v", got, err)
	}
	rel, err := filepath.Rel(dir, secret)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{rel, "link", "/etc/hosts", "missing"} {
		if data, err := Read(dir, name); err == nil {
			t.Errorf("Read(%q) = %q, want an error", name, data)
		}
	}
	if _, err := Read(filepath.Join(dir, "no-such-dir"), "ok.txt"); err == nil {
		t.Error("Read in a missing directory succeeded")
	}
}

func TestReadIsBounded(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "big"), bytes.Repeat([]byte{'x'}, MaxFile+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "edge"), bytes.Repeat([]byte{'x'}, MaxFile), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(dir, "big"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Read(big) error = %v, want ErrTooLarge", err)
	}
	if got, err := Read(dir, "edge"); err != nil || len(got) != MaxFile {
		t.Fatalf("Read(edge) = %d bytes, %v; want %d, nil", len(got), err, MaxFile)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(dir, "sub"); err == nil {
		t.Fatal("reading a directory succeeded")
	}
}
