package hygiene

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/gate"
)

// The kit's lint pass may read its own configuration, apart from the adopter's
// .golangci.yml (gates.hygiene.golangci_lint.config, for example under .go-tops/).
// golangci-lint resolves every relative path against the directory of the config it
// reads unless the config says otherwise (run.relative-path-mode), the output path
// and the file names of its findings included. The layer must name the result file
// absolutely and read finding names as module-relative paths however they come.

const underGoTops = ".go-tops/golangci.yml"

func (f *fake) write(rel, data string) {
	f.t.Helper()
	p := filepath.Join(f.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func TestTheLintResultPathIsAbsoluteWhateverTheConfigIs(t *testing.T) {
	for _, cfg := range []string{"", underGoTops} {
		f := newFake(t)
		runLayer(t, f, gate.Config{Hygiene: gate.HygieneConfig{LintConfig: cfg}})
		want := "--output.json.path=" + filepath.Join(f.dir, ".go-tops", "lint-style.json")
		found := false
		for _, argv := range f.calls {
			if call := strings.Join(argv, " "); strings.HasPrefix(call, "golangci-lint run") {
				found = strings.Contains(call, want)
			}
		}
		if !found {
			t.Errorf("config %q: the lint run does not save to the absolute path %s: %v", cfg, want, f.calls)
		}
	}
}

// A finding the linter names relative to the config's directory is read as the
// module-relative file it is, so the message, the ledger and the leg's file sets
// agree on one name.
func TestFindingNamesAreModuleRelativeForAConfigUnderGoTops(t *testing.T) {
	f := newFake(t)
	f.write("a/a.go", "package a\n")
	f.lintDoc = `{"Issues":[{"FromLinter":"errcheck","Text":"unchecked","Pos":{"Filename":"../a/a.go","Line":3}}],"Report":{}}`
	res := runLayer(t, f, gate.Config{Hygiene: gate.HygieneConfig{LintConfig: underGoTops}})
	msg := onlyFails(t, res, "golangci-lint run")
	if !strings.Contains(msg, "first: a/a.go:3 unchecked") {
		t.Errorf("message %q should name the file module-relative", msg)
	}
}

func TestModuleRelNamesAFileHoweverTheLinterNamedIt(t *testing.T) {
	dir := t.TempDir()
	for _, rel := range []string{"a/a.go", "b.go"} {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("package a\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct{ name, cfg, in, want string }{
		{"already module-relative", ".golangci.yml", "a/a.go", "a/a.go"},
		{"relative to a config under .go-tops", underGoTops, "../a/a.go", "a/a.go"},
		{"module-relative with a config under .go-tops", underGoTops, "b.go", "b.go"},
		{"absolute", underGoTops, filepath.Join(dir, "a", "a.go"), "a/a.go"},
		{"a name no file has", underGoTops, "../nowhere.go", "../nowhere.go"},
		{"a dot slash", ".golangci.yml", "./a/a.go", "a/a.go"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := ModuleRel(dir, c.cfg, c.in); got != c.want {
				t.Errorf("ModuleRel(%q, %q) = %q, want %q", c.cfg, c.in, got, c.want)
			}
		})
	}
}

// The real linter, a config under .go-tops/ and a planted exclusion. In gomod mode
// the linter names files from the module, so an anchored exclusion applies and the
// names need no help. In the default mode it names them from the config's directory
// ("../a.go"), and the layer must still save the result where it reads it and name the
// file module-relative.
func TestRealLinterWithAConfigUnderGoTops(t *testing.T) {
	if _, err := exec.LookPath("golangci-lint"); err != nil {
		t.Skip("the pinned golangci-lint is not on PATH")
	}
	for _, mode := range []string{"gomod", ""} {
		t.Run("relative-path-mode "+mode, func(t *testing.T) {
			f := newFake(t)
			f.write("go.mod", "module example.com/m\n\ngo 1.26\n")
			f.write("a.go", "package m\n\nimport \"os\"\n\nfunc A() { _ = os.Remove(\"x\") }\n")
			f.write("zz.go", "package m\n\nimport \"os\"\n\nfunc Z() { _ = os.Remove(\"x\") }\n")
			cfg := "version: \"2\"\nrun:\n"
			if mode != "" {
				cfg += "  relative-path-mode: " + mode + "\n"
			}
			// Paths are regular expressions matched against the name the linter uses: an
			// anchored one needs the module-relative names of gomod mode.
			excluded := `'^zz\.go$'`
			if mode == "" {
				excluded = `'zz\.go$'`
			}
			cfg += "linters:\n  default: standard\n  settings:\n    errcheck:\n      check-blank: true\n  exclusions:\n    paths: [" + excluded + "]\n"
			f.write(underGoTops, cfg)
			f.override["golangci-lint run"] = func(argv []string) (gate.Output, error) {
				cmd := exec.CommandContext(context.Background(), "golangci-lint", argv[1:]...)
				cmd.Dir = f.dir
				out, err := cmd.CombinedOutput()
				var exit *exec.ExitError
				if err != nil && !errors.As(err, &exit) {
					t.Fatal(err) // the linter exits 1 for findings, which is not a failure here
				}
				return gate.Output{Stdout: out}, nil
			}
			res := runLayer(t, f, gate.Config{Hygiene: gate.HygieneConfig{LintConfig: underGoTops}})
			if _, err := os.Stat(filepath.Join(f.dir, ".go-tops", "lint-style.json")); err != nil {
				t.Fatalf("the result is not at the module's .go-tops/lint-style.json: %v", err)
			}
			msg := onlyFails(t, res, "golangci-lint run")
			if !strings.Contains(msg, "1 finding(s)") || !strings.Contains(msg, "first: a.go:5") {
				t.Errorf("message %q: want exactly the finding in a.go, with zz.go excluded, named module-relative", msg)
			}
		})
	}
}
