//go:build mutation

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/report"
)

func runGitFixture(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(out))
	}
}

func buildGotopsFixtureBin(t *testing.T, repoRoot string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "gotops")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/gotops")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build gotops: %v\n%s", err, string(out))
	}
	return bin
}

func copyFileFixture(t *testing.T, src, dst string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func prepareMutationFixture(t *testing.T, repoRoot, caseName string) string {
	t.Helper()
	dir := t.TempDir()
	fixDir := filepath.Join(repoRoot, "testdata", "mutation")
	for _, pair := range [][2]string{
		{filepath.Join(fixDir, "go.mod"), filepath.Join(dir, "go.mod")},
		{filepath.Join(fixDir, "cases", caseName+"_test.go"), filepath.Join(dir, "boundary_test.go")},
		{filepath.Join(repoRoot, "go-tops.lock"), filepath.Join(dir, "go-tops.lock")},
		{filepath.Join(repoRoot, "tools", "go.mod"), filepath.Join(dir, "tools", "go.mod")},
		{filepath.Join(repoRoot, "tools", "go.sum"), filepath.Join(dir, "tools", "go.sum")},
	} {
		copyFileFixture(t, pair[0], pair[1])
	}
	cfgYAML := `protocol: "0.4.5"
version: "0.1.0"
module: example.com/mutationfixture
class: library-core
enforce: hard

toolchain:
  go_floor: "1.26"
  go_current: "1.27"

gates:
  mutation:
    enabled: true
`
	if err := os.WriteFile(filepath.Join(dir, "go-tops.yaml"), []byte(cfgYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "boundary.go"), []byte("package mutation\n\n// Below reports whether v is strictly less than limit.\nfunc Below(v, limit int) bool {\n\treturn v == limit\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitFixture(t, dir, "init", "-b", "main")
	runGitFixture(t, dir, "config", "user.name", "Test Author")
	runGitFixture(t, dir, "config", "user.email", "author@example.com")
	runGitFixture(t, dir, "add", ".")
	runGitFixture(t, dir, "commit", "-m", "base")
	runGitFixture(t, dir, "checkout", "-b", "feat")

	copyFileFixture(t, filepath.Join(fixDir, "boundary.go"), filepath.Join(dir, "boundary.go"))
	runGitFixture(t, dir, "commit", "-am", "plant boundary")
	return dir
}

func runFixtureGate(t *testing.T, bin, dir string) (string, report.Layer, string, error) {
	t.Helper()
	cmd := exec.Command(bin, "gate", "--only", "mutation", "--base", "main")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	rep, rerr := report.Load(filepath.Join(dir, report.DefaultPath))
	if rerr != nil {
		t.Fatalf("report.Load: %v\n%s", rerr, string(out))
	}
	for _, layer := range rep.Layers {
		if layer.ID != "mutation" {
			continue
		}
		raw, merr := json.Marshal(layer.Details)
		if merr != nil {
			t.Fatal(merr)
		}
		return string(out), layer, string(raw), err
	}
	t.Fatalf("mutation layer missing in %+v", rep.Layers)
	return "", report.Layer{}, "", err
}

func TestMutationFixtureWeakStrongAndUncovered(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	bin := buildGotopsFixtureBin(t, repoRoot)

	t.Run("weak", func(t *testing.T) {
		dir := prepareMutationFixture(t, repoRoot, "weak")
		out, layer, raw, err := runFixtureGate(t, bin, dir)
		if err == nil || layer.Outcome != "fail" {
			t.Fatalf("weak fixture: err=%v, layer=%+v\n%s", err, layer, out)
		}
		for _, want := range []string{"CONDITIONALS_BOUNDARY", "LIVED", "boundary.go:Below:CONDITIONALS_BOUNDARY:"} {
			if !strings.Contains(raw, want) || !strings.Contains(out, want) {
				t.Errorf("weak fixture missing %q\nout:\n%s\ndetails:\n%s", want, out, raw)
			}
		}
	})

	t.Run("none_uncovered", func(t *testing.T) {
		dir := prepareMutationFixture(t, repoRoot, "none")
		out, layer, raw, err := runFixtureGate(t, bin, dir)
		if err == nil || layer.Outcome != "fail" {
			t.Fatalf("uncovered fixture: err=%v, layer=%+v\n%s", err, layer, out)
		}
		for _, want := range []string{"NOT COVERED", "boundary.go:Below:"} {
			if !strings.Contains(raw, want) || !strings.Contains(out, want) {
				t.Errorf("uncovered fixture missing %q\nout:\n%s\ndetails:\n%s", want, out, raw)
			}
		}
	})

	t.Run("strong", func(t *testing.T) {
		dir := prepareMutationFixture(t, repoRoot, "strong")
		out, layer, raw, err := runFixtureGate(t, bin, dir)
		if err != nil || layer.Outcome != "pass" {
			t.Fatalf("strong fixture: err=%v, layer=%+v\n%s", err, layer, out)
		}
		if !strings.Contains(raw, `"survived_unexplained":0`) || !strings.Contains(raw, `"killed":2`) {
			t.Fatalf("strong fixture did not kill mutants cleanly: %s", raw)
		}
	})
}
