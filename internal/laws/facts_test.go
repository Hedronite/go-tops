package laws

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckG33Floor(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/t\n\ngo 1.25\n")
	copyLock(t, dir)
	out := map[string]int{"G-33": 0}
	if err := checkG33(dir, func() (string, error) { return "go1.27.4", nil }, out); err != nil {
		t.Fatal(err)
	}
	if out["G-33"] != 1 {
		t.Fatalf("G-33=%d want 1 for low go directive", out["G-33"])
	}
}

func copyLock(t *testing.T, dir string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "go-tops.lock"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go-tops.lock"), string(data))
}

func TestCheckG26NonBridge(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/t\n\ngo 1.26\n")
	p := filepath.Join(dir, "cgo.go")
	writeFile(t, p, "package p\nimport \"C\"\n")
	out := map[string]int{"G-26": 0}
	if err := checkG26(dir, "library-core", out); err != nil {
		t.Fatal(err)
	}
	if out["G-26"] != 1 {
		t.Fatalf("G-26=%d want 1", out["G-26"])
	}
}

func TestCheckG26TestExempt(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/t\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "cgo_test.go"), "package p\nimport \"C\"\n")
	out := map[string]int{"G-26": 0}
	if err := checkG26(dir, "library-core", out); err != nil {
		t.Fatal(err)
	}
	if out["G-26"] != 0 {
		t.Fatalf("G-26=%d want 0 for test-only cgo", out["G-26"])
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckG33MismatchGoEnv(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/t\n\ngo 1.26\n")
	copyLock(t, dir)
	out := map[string]int{"G-33": 0}
	if err := checkG33(dir, func() (string, error) { return "go1.26.0", nil }, out); err != nil {
		t.Fatal(err)
	}
	if out["G-33"] != 1 {
		t.Fatalf("G-33=%d want 1 for GOVERSION mismatch", out["G-33"])
	}
}

func TestCheckG33MissingGoDirective(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/t\n")
	copyLock(t, dir)
	out := map[string]int{"G-33": 0}
	if err := checkG33(dir, func() (string, error) { return "go1.27.4", nil }, out); err != nil {
		t.Fatal(err)
	}
	if out["G-33"] != 1 {
		t.Fatalf("G-33=%d want 1", out["G-33"])
	}
}

func TestCheckG33OK(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/t\n\ngo 1.26\n")
	copyLock(t, dir)
	out := map[string]int{"G-33": 0}
	if err := checkG33(dir, func() (string, error) { return "go1.27.4", nil }, out); err != nil {
		t.Fatal(err)
	}
	if out["G-33"] != 0 {
		t.Fatalf("G-33=%d want 0", out["G-33"])
	}
}

func TestCheckG26CgoBridgeAllows(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/t\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "cgo.go"), "package p\nimport \"C\"\n")
	out := map[string]int{"G-26": 0}
	if err := checkG26(dir, "cgo-bridge", out); err != nil {
		t.Fatal(err)
	}
	if out["G-26"] != 0 {
		t.Fatalf("G-26=%d want 0 on cgo-bridge", out["G-26"])
	}
}

func TestModGoVersionAndCompare(t *testing.T) {
	v, err := ModGoVersion([]byte("module m\n\ngo 1.26\n"))
	if err != nil || v != "1.26" {
		t.Fatalf("ModGoVersion: %q %v", v, err)
	}
	if CompareGoVersions("1.25", "1.26") >= 0 {
		t.Fatal("compare order")
	}
}

func TestFileHasCImport(t *testing.T) {
	if !FileHasCImport([]byte("package p\nimport \"C\"\n")) {
		t.Fatal("want c import")
	}
	if FileHasCImport([]byte("package p\n")) {
		t.Fatal("want no c import")
	}
}

func TestParseFileForTest(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.go")
	writeFile(t, p, "package p\nimport \"C\"\n")
	ok, err := ParseFileForTest(p)
	if err != nil || !ok {
		t.Fatalf("ParseFileForTest: ok=%v err=%v", ok, err)
	}
}
