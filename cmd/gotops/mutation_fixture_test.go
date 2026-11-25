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

func prepareMutationFixture(t *testing.T, repoRoot, caseName string) string {
	t.Helper()
	dir := t.TempDir()
	fixDir := filepath.Join(repoRoot, "testdata", "mutation")
	for _, pair := range [][2]string{
		{filepath.Join(fixDir, "go.mod"), filepath.Join(dir, "go.mod")},
		{filepath.Join(fixDir, "cases", caseName+"_test.go"), filepath.Join(dir, "boundary_test.go")},
		{filepath.Join(repoRoot, "tools", "go.mod"), filepath.Join(dir, "tools", "go.mod")},
		{filepath.Join(repoRoot, "tools", "go.sum"), filepath.Join(dir, "tools", "go.sum")},
	} {
		if err := os.MkdirAll(filepath.Dir(pair[1]), 0o755); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(pair[0])
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(pair[1], data, 0o644); err != nil {
			t.Fatal(err)
		}
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

	prod, err := os.ReadFile(filepath.Join(fixDir, "boundary.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "boundary.go"), prod, 0o644); err != nil {
		t.Fatal(err)
	}
	runGitFixture(t, dir, "commit", "-am", "plant boundary")
	return dir
}

func TestMutationFixtureWeakStrongAndUncovered(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	bin := buildGotopsFixtureBin(t, repoRoot)

	t.Run("weak", func(t *testing.T) {
		dir := prepareMutationFixture(t, repoRoot, "weak")
		cmd := exec.Command(bin, "gate", "--only", "mutation", "--base", "main")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("weak fixture passed unexpectedly:\n%s", string(out))
		}
		rep, rerr := report.Load(filepath.Join(dir, report.DefaultPath))
		if rerr != nil {
			t.Fatalf("report.Load: %v\n%s", rerr, string(out))
		}
		if len(rep.Layers) != 1 || rep.Layers[0].Outcome != "fail" {
			t.Fatalf("unexpected layer outcome: %+v", rep.Layers)
		}
		raw, merr := json.Marshal(rep.Layers[0].Details)
		if merr != nil {
			t.Fatal(merr)
		}
		if !strings.Contains(string(raw), `"CONDITIONALS_BOUNDARY"`) || !strings.Contains(string(raw), `"LIVED"`) {
			t.Fatalf("weak details missing LIVED CONDITIONALS_BOUNDARY: %s", string(raw))
		}
	})

	t.Run("none_uncovered", func(t *testing.T) {
		dir := prepareMutationFixture(t, repoRoot, "none")
		cmd := exec.Command(bin, "gate", "--only", "mutation", "--base", "main")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("uncovered fixture passed unexpectedly:\n%s", string(out))
		}
		rep, rerr := report.Load(filepath.Join(dir, report.DefaultPath))
		if rerr != nil {
			t.Fatalf("report.Load: %v\n%s", rerr, string(out))
		}
		if len(rep.Layers) != 1 || rep.Layers[0].Outcome != "fail" {
			t.Fatalf("unexpected layer outcome: %+v", rep.Layers)
		}
		raw, merr := json.Marshal(rep.Layers[0].Details)
		if merr != nil {
			t.Fatal(merr)
		}
		if !strings.Contains(string(raw), `"NOT COVERED"`) {
			t.Fatalf("uncovered details missing NOT COVERED: %s", string(raw))
		}
	})

	t.Run("strong", func(t *testing.T) {
		dir := prepareMutationFixture(t, repoRoot, "strong")
		cmd := exec.Command(bin, "gate", "--only", "mutation", "--base", "main")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("strong fixture failed: %v\n%s", err, string(out))
		}
		rep, rerr := report.Load(filepath.Join(dir, report.DefaultPath))
		if rerr != nil {
			t.Fatalf("report.Load: %v", rerr)
		}
		if len(rep.Layers) != 1 || rep.Layers[0].Outcome != "pass" {
			t.Fatalf("unexpected layer outcome: %+v", rep.Layers)
		}
	})
}
__MUT_FIXTURE_TEST__'
cat <<'__G4_PATCH__' | ssh tower 'python3 - <<'"'"'PY'"'"'
from pathlib import Path

root = Path.home() / "tmp/gt-m4-backend"

# 1. G4b: go-tops.yaml enable mutation
gp = root / "go-tops.yaml"
gs = gp.read_text()
old_mut = """  mutation:
    enabled: false
    reason: the mutation layer lands at M4
    expires: "2027-03-01\""""
new_mut = """  mutation:
    enabled: true"""
assert old_mut in gs, "go-tops.yaml mutation block not found"
gp.write_text(gs.replace(old_mut, new_mut, 1))

# 2. G4a & G4b: .github/workflows/ci.yml
cp = root / ".github/workflows/ci.yml"
cs = cp.read_text()

old_assert = """      #   - hygiene, tests, coverage and crap are `pass`;
      #   - mutation is `skip` with an `expires` that has not passed;"""
new_assert = """      #   - hygiene, tests, coverage and crap are `pass`;
      #   - mutation is `pass` or `vacuous` (never `skip`);"""

old_jq = """              ( "mutation" as $id
                | layer($id) as $l
                | select($l == null or $l.outcome != "skip" or (($l.expires // "") <= $today))
                | "\\($id): want skip with an expires after \\($today), got \\($l.outcome // "absent") expires \\($l.expires // "none")" ),"""
new_jq = """              ( "mutation" as $id
                | layer($id) as $l
                | select($l == null or ($l.outcome != "pass" and $l.outcome != "vacuous"))
                | "\\($id): outcome \\($l.outcome // "absent"), want pass or vacuous" ),"""

old_ci = """  ci:
    name: ci
    # Runs even when a dependency failed, was skipped or was cancelled, and
    # fails unless every one of them succeeded: a skipped job must not pass. It
    # runs on the events the gate runs on; a scheduled or dispatched run has no
    # gate to wait for.
    if: ${{ always() && (github.event_name == '\''pull_request'\'' || github.event_name == '\''push'\'') }}
    needs: [gate, skills, bootstrap]"""

new_ci = """  mutation:
    name: mutation (${{ matrix.name }})
    if: ${{ github.event_name == '\''pull_request'\'' || github.event_name == '\''push'\'' }}
    strategy:
      fail-fast: false
      matrix:
        include:
          - name: ubuntu
            os: ubuntu-latest
          - name: macos
            os: macos-latest
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          fetch-depth: 0
          persist-credentials: false
      - uses: actions/setup-go@d8cbb8045d7f43706b9177cceb36c1f91a6644d5 # v7.1.0
        with:
          go-version: ${{ env.CURRENT_GO }}
          cache-dependency-path: |
            go.sum
            tools/go.sum
      - name: mutation fixture tests
        env:
          CGO_ENABLED: "1"
          GOTOOLCHAIN: local
        shell: bash
        run: |
          set -euo pipefail
          log="${RUNNER_TEMP}/mutation.log"
          go test -v -tags mutation -run TestMutationFixtureWeakStrongAndUncovered -timeout 15m -count=1 ./cmd/gotops 2>&1 | tee "${log}"
          if ! grep -q -- "--- PASS: TestMutationFixtureWeakStrongAndUncovered " "${log}"; then
            echo "::error::no '\''--- PASS'\'' line for TestMutationFixtureWeakStrongAndUncovered: the test did not run, or it was skipped"
            exit 1
          fi

  ci:
    name: ci
    # Runs even when a dependency failed, was skipped or was cancelled, and
    # fails unless every one of them succeeded: a skipped job must not pass. It
    # runs on the events the gate runs on; a scheduled or dispatched run has no
    # gate to wait for.
    if: ${{ always() && (github.event_name == '\''pull_request'\'' || github.event_name == '\''push'\'') }}
    needs: [gate, skills, bootstrap, mutation]"""

assert old_assert in cs and old_jq in cs and old_ci in cs, "ci.yml anchors missing"
cs = cs.replace(old_assert, new_assert, 1).replace(old_jq, new_jq, 1).replace(old_ci, new_ci, 1)
# Remove now-unused --arg today if unused? Wait: check if $today is used elsewhere in jq!
cp.write_text(cs)
PY
cd ~/tmp/gt-m4-backend
git config user.name "VirtualMachinist"
git config user.email "TheVirtualMachinist@protonmail.com"
git add testdata/mutation/ cmd/gotops/mutation_fixture_test.go go-tops.yaml .github/workflows/ci.yml
git commit -m "Draft G4: add mutation fixture tests, mutation CI job, and enable kit mutation gate"
git show --stat HEAD && git branch --list && git status -s'