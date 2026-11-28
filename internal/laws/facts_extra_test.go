package laws

import (
	"path/filepath"
	"testing"
)

func TestCheckGoModCGONocgoToolchain(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/t\n\ngo 1.26\n\ntoolchain go1.26.0+abi0.nocgo\n")
	out := map[string]int{"G-26": 0}
	if err := checkG26(dir, "cgo-bridge", out); err != nil {
		t.Fatal(err)
	}
	if out["G-26"] != 1 {
		t.Fatalf("G-26=%d want 1 for nocgo toolchain", out["G-26"])
	}
}

func TestCheckFactsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/t\n\ngo 1.26\n")
	copyLock(t, dir)
	out, err := CheckFacts(dir, "binary-cli", func() (string, error) { return "go1.27.4", nil })
	if err != nil {
		t.Fatal(err)
	}
	if out["G-26"] != 0 || out["G-33"] != 0 {
		t.Fatalf("out=%v", out)
	}
}

func TestCheckG26SkipsVendorTree(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/t\n\ngo 1.26\n")
	vendor := filepath.Join(dir, "vendor", "dep", "cgo.go")
	writeFile(t, vendor, "package dep\nimport \"C\"\n")
	out := map[string]int{"G-26": 0}
	if err := checkG26(dir, "library-core", out); err != nil {
		t.Fatal(err)
	}
	if out["G-26"] != 0 {
		t.Fatalf("G-26=%d want 0 skipping vendor", out["G-26"])
	}
}

func TestCheckG26SkipsTestdata(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/t\n\ngo 1.26\n")
	p := filepath.Join(dir, "testdata", "fixture", "cgo.go")
	writeFile(t, p, "package fixture\nimport \"C\"\n")
	out := map[string]int{"G-26": 0}
	if err := checkG26(dir, "library-core", out); err != nil {
		t.Fatal(err)
	}
	if out["G-26"] != 0 {
		t.Fatalf("G-26=%d want 0 in testdata", out["G-26"])
	}
}

func TestDefaultGoEnvLive(t *testing.T) {
	if _, err := defaultGoEnv(); err != nil {
		t.Skip(err)
	}
}

func TestModGoVersionMissingGo(t *testing.T) {
	v, err := ModGoVersion([]byte("module m\n"))
	if err != nil || v != "" {
		t.Fatalf("v=%q err=%v", v, err)
	}
}

func TestLintGoModBadSyntax(t *testing.T) {
	if err := LintGoMod([]byte("not a mod")); err == nil {
		t.Fatal("want parse error")
	}
}

func TestReadGoMod(t *testing.T) {
	dir := t.TempDir()
	body := "module m\n\ngo 1.26\n"
	writeFile(t, filepath.Join(dir, "go.mod"), body)
	got, err := ReadGoMod(dir)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("got %q", got)
	}
}

func TestFileHasCImportUnparseable(t *testing.T) {
	if !FileHasCImport([]byte("package p\nimport \"C\"\n")) {
		t.Fatal("fallback")
	}
}

func TestCheckG33LockMissing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/t\n\ngo 1.26\n")
	out := map[string]int{"G-33": 0}
	if err := checkG33(dir, func() (string, error) { return "go1.27.4", nil }, out); err != nil {
		t.Fatal(err)
	}
	if out["G-33"] != 1 {
		t.Fatalf("G-33=%d want 1 without lock", out["G-33"])
	}
}

func TestNeedsGoModCGOCheck(t *testing.T) {
	if !needsGoModCGOCheck(false, "cgo-bridge") {
		t.Fatal("bridge always checks go.mod")
	}
	if needsGoModCGOCheck(false, "library-core") {
		t.Fatal("no prod cgo")
	}
}

func TestIsG26ProdGo(t *testing.T) {
	if isG26ProdGo("x_test.go") {
		t.Fatal("skip tests")
	}
	if isG26ProdGo(filepath.Join("pkg", "testdata", "x.go")) {
		t.Fatal("skip testdata")
	}
	if !isG26ProdGo("main.go") {
		t.Fatal("want prod go")
	}
}
