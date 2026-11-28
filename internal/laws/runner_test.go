package laws

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunnerMergesPositiveFactsIntoByLawFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/facts\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "cgo.go"), "package facts\nimport \"C\"\n")
	data, err := os.ReadFile(filepath.Join("..", "..", "go-tops.lock"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go-tops.lock"), string(data))
	rr, err := Run(context.Background(), dir, "library-core", func() (string, error) { return "go1.27.4", nil })
	if err != nil {
		t.Fatal(err)
	}
	if n := rr.ByLaw["G-26"]; n < 1 {
		t.Fatalf("G-26=%d want fact hit", n)
	}
	if rr.ByLawFile["G-26"]["(module)"] < 1 {
		t.Fatalf("ByLawFile=%+v", rr.ByLawFile)
	}
}

func TestRunnerFactsOnlyModuleByLawFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "empty.go"), "package clean\n")
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/clean\n\ngo 1.26\n")
	ctx := context.Background()
	rr, err := Run(ctx, dir, "library-core", func() (string, error) { return "go1.27.4", nil })
	if err != nil {
		t.Fatal(err)
	}
	if rr.ByLawFile == nil {
		t.Fatal("want ByLawFile map")
	}
}

func TestRunnerFactsOnlyModule(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "empty.go"), "package clean\n")
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/clean\n\ngo 1.26\n")
	data, err := os.ReadFile(filepath.Join("..", "..", "go-tops.lock"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "go-tops.lock"), string(data))
	ctx := context.Background()
	if _, err := Run(ctx, dir, "library-core", func() (string, error) { return "go1.27.4", nil }); err != nil {
		t.Fatalf("Run: %v", err)
	}
}
