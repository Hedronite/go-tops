package initcmd

import (
	"os"
	"path/filepath"
	"testing"
)

// Goldens per class: the files init writes for a fixed module, owner, kit
// version and date. Review a golden change like a protocol change; refresh
// with UPDATE_GOLDEN=1 go test ./internal/initcmd.
func TestGoldenPerClass(t *testing.T) {
	for _, class := range launchClasses {
		t.Run(class, func(t *testing.T) {
			dir := newModule(t, "github.com/acme/demo", "1.26")
			if code, _, errs := run(opts(dir, class)); code != ExitOK {
				t.Fatal(errs)
			}
			golden := filepath.Join("testdata", "golden", class)
			for _, rel := range listing(t, dir) {
				if rel == "go.mod" {
					continue // the fixture module, not init's output
				}
				got := read(t, dir, rel)
				path := filepath.Join(golden, filepath.FromSlash(rel))
				if os.Getenv("UPDATE_GOLDEN") != "" {
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
						t.Fatal(err)
					}
					continue
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("no golden for %s (UPDATE_GOLDEN=1 writes it): %v", rel, err)
				}
				if got != string(want) {
					t.Errorf("%s differs from its golden %s", rel, path)
				}
			}
		})
	}
}
