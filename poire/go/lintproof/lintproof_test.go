package lintproof

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const counterDir = "testdata/counter"

type finding struct {
	FromLinter string
	Text       string
	Pos        struct {
		Filename string
		Line     int
	}
}

// id names a finding as a marker does: the linter, and for govet the analyzer.
func (f finding) id() string {
	if f.FromLinter == "govet" {
		analyzer, _, _ := strings.Cut(f.Text, ":")
		return "govet/" + analyzer
	}
	return f.FromLinter
}

func TestEveryCounterExampleTripsItsLinter(t *testing.T) {
	lint, err := exec.LookPath("golangci-lint")
	if err != nil {
		t.Fatalf("golangci-lint is not on PATH, so the detectors cannot be proven: %v", err)
	}
	report := filepath.Join(t.TempDir(), "lint.json")
	cmd := exec.CommandContext(t.Context(), lint, "run", "--output.json.path="+report, "./...")
	cmd.Dir = counterDir
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
		t.Fatalf("golangci-lint did not run: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(report)
	if err != nil {
		t.Fatalf("no lint report: %v\n%s", err, out)
	}
	var parsed struct{ Issues []finding }
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("lint report does not parse: %v", err)
	}

	got := map[string][]string{}
	for _, f := range parsed.Issues {
		at := fmt.Sprintf("%s:%d", filepath.Base(f.Pos.Filename), f.Pos.Line)
		got[at] = append(got[at], f.id())
	}
	want := markers(t)
	if len(want) == 0 {
		t.Fatal("no want markers found")
	}
	for at, ids := range want {
		for _, id := range ids {
			if !slices.Contains(got[at], id) {
				t.Errorf("%s: %s did not fire; it reported %v", at, id, got[at])
			}
		}
	}
	for at, ids := range got {
		for _, id := range ids {
			if !slices.Contains(want[at], id) {
				t.Errorf("%s: unmarked finding from %s", at, id)
			}
		}
	}
}

// markers reads every `// want a b` comment in the counter module.
func markers(t *testing.T) map[string][]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(counterDir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{}
	for _, name := range files {
		f, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		lines := bufio.NewScanner(f)
		for n := 1; lines.Scan(); n++ {
			_, ids, ok := strings.Cut(lines.Text(), "// want ")
			if ok {
				want[fmt.Sprintf("%s:%d", filepath.Base(name), n)] = strings.Fields(ids)
			}
		}
		if err := errors.Join(lines.Err(), f.Close()); err != nil {
			t.Fatal(err)
		}
	}
	return want
}
