//go:build adoption

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The adoption path of GO_TOPS.md §6.7 ("First adoption of a legacy repo"),
// end to end with the real binary: init into a legacy module that has functions
// over the CRAP cap, baseline --init, commit, check, and the ratchet judges the
// first ledger. Then a change that raises a function fails, and one that lowers
// it passes and a refresh drops its entry.
//
// It needs the network (init tidies tools/go.mod, the hygiene layer reads the
// vulnerability database) and the pinned golangci-lint on PATH, so it carries
// the build tag and runs on tower:
//
//	go test -tags adoption -run TestAdoption -timeout 20m ./cmd/gotops
//
// The full gate cannot exit 0 before M5, because the required laws are not
// evaluated until then, so the test reads the report: the ratchet outcome, and
// the layers that feed the ledger.

// branches writes n ifs on a with distinct conditions (a > 0, a > 1, ...), each
// adding one to n: complexity grows by n, and a call with a small a runs only
// the first few bodies.
func branches(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString("\tif a > " + strconv.Itoa(i) + " {\n\t\tn++\n\t}\n")
	}
	return b.String()
}

// legacy is a module with two functions over the cap of 10: Heavy has
// complexity 12 and every branch tested, Mid has complexity 9 and one branch
// tested (about 61% covered: 13.8). Simple is under it.
func legacy(heavy string) map[string]string {
	return map[string]string{
		"go.mod": "module github.com/acme/legacy\n\ngo 1.26\n",
		"legacy.go": "// Package legacy is a fixture: complexity the ledger carries as debt.\npackage legacy\n\n" +
			"// Heavy has complexity 12 and is fully tested.\n" + heavy + "\n" +
			"// Mid has complexity 9 and one branch tested.\nfunc Mid(a int) int {\n\tn := 0\n" +
			branches(8) + "\treturn n\n}\n\n" +
			"// Simple is under the cap.\nfunc Simple(x int) int { return x + 1 }\n",
		"legacy_test.go": "package legacy\n\nimport \"testing\"\n\n" +
			"func TestHeavy(t *testing.T) {\n\tif Heavy(20) != 11 {\n\t\tt.Fatal(\"Heavy\")\n\t}\n}\n\n" +
			"func TestMid(t *testing.T) {\n\tif Mid(1) != 1 {\n\t\tt.Fatal(\"Mid\")\n\t}\n}\n\n" +
			"func TestSimple(t *testing.T) {\n\tif Simple(1) != 2 {\n\t\tt.Fatal(\"Simple\")\n\t}\n}\n",
	}
}

func heavyFunc(extra string) string {
	return "func Heavy(a int) int {\n\tn := 0\n" + branches(11) + extra + "\treturn n\n}\n"
}

// loopedHeavy is Heavy with the same behaviour and complexity 3.
const loopedHeavy = "func Heavy(a int) int {\n\tn := 0\n\tfor i := range 11 {\n\t\tif a > i {\n\t\t\tn++\n\t\t}\n\t}\n\treturn n\n}\n"

func adRun(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return string(out)
}

func adGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return adRun(t, dir, "git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
}

// adGate runs the gate and returns its exit code and report.
func adGate(t *testing.T, dir string) (int, map[string]any) {
	t.Helper()
	code, stdout, stderr := gotopsEnv(t, dir, []string{"CGO_ENABLED=1"}, "gate", "--base", "origin/main")
	if code == 2 || code == 3 || code == 4 {
		t.Fatalf("the gate did not run: exit %d\n%s\n%s", code, stdout, stderr)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".go-tops", "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rep map[string]any
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatal(err)
	}
	return code, rep
}

func adRatchet(t *testing.T, rep map[string]any) string {
	t.Helper()
	r := object(t, rep["ratchet"])
	out, ok := r["outcome"].(string)
	if !ok {
		t.Fatalf("ratchet outcome is %T", r["outcome"])
	}
	return out
}

func adLayer(t *testing.T, rep map[string]any, id string) string {
	t.Helper()
	for _, l := range array(t, rep["layers"]) {
		if m := object(t, l); m["id"] == id {
			if s, ok := m["outcome"].(string); ok {
				return s
			}
		}
	}
	t.Fatalf("no %s layer in the report", id)
	return ""
}

func adFunctions(t *testing.T, dir string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, ".go-tops", "baseline.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	return object(t, object(t, doc["ledger"])["functions"])
}

func hasKeySuffix(m map[string]any, suffix string) bool {
	for k := range m {
		if strings.HasSuffix(k, suffix) {
			return true
		}
	}
	return false
}

func TestAdoptionOfALegacyRepo(t *testing.T) {
	if _, err := exec.LookPath("golangci-lint"); err != nil {
		t.Skip("the pinned golangci-lint is not on PATH")
	}
	dir := t.TempDir()
	for name, content := range legacy(heavyFunc("")) {
		writeFile(t, dir, name, content)
	}
	adGit(t, dir, "init", "-q", "-b", "main")
	adGit(t, dir, "add", "-A")
	adGit(t, dir, "commit", "-q", "-m", "legacy")
	adGit(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD") // the diff base: no baseline yet
	adGit(t, dir, "checkout", "-q", "-b", "adopt")
	writeFile(t, dir, ".gitignore", ".go-tops/bin/\n.go-tops/report.json\n.go-tops/cover.out\n.go-tops/lint-style.json\n")

	// 1. init: CRAP on, the next step printed.
	code, stdout, stderr := gotopsEnv(t, dir, nil, "init", "--class", "binary-cli")
	if code != 0 || !strings.Contains(stdout, "next: ") || !strings.Contains(stdout, "gotops baseline --init") {
		t.Fatalf("init: exit %d\n%s\n%s", code, stdout, stderr)
	}
	if cfg := readFile(t, dir, "go-tops.yaml"); !strings.Contains(cfg, "crap:\n    enabled: true") {
		t.Fatalf("init left CRAP off:\n%s", cfg)
	}

	// 2. baseline --init measures and judges nothing: the two legacy functions
	// are in the ledger, the function under the cap is not.
	if code, stdout, stderr := gotopsEnv(t, dir, []string{"CGO_ENABLED=1"}, "baseline", "--init"); code != 0 {
		t.Fatalf("baseline --init: exit %d\n%s\n%s", code, stdout, stderr)
	}
	fns := adFunctions(t, dir)
	if !hasKeySuffix(fns, ".Heavy") || !hasKeySuffix(fns, ".Mid") || hasKeySuffix(fns, ".Simple") || len(fns) != 2 {
		t.Fatalf("the ledger holds %v, want Heavy and Mid only", fns)
	}

	// 3. commit, check, and the first ledger passes the ratchet with no source edit.
	adGit(t, dir, "add", "-A")
	adGit(t, dir, "commit", "-q", "-m", "adopt Go-TOPS")
	if code, _, stderr := gotopsEnv(t, dir, nil, "check"); code != 0 {
		t.Fatalf("check: exit %d\n%s", code, stderr)
	}
	_, rep := adGate(t, dir)
	if got := adRatchet(t, rep); got != "pass" {
		t.Fatalf("the first ledger must pass the ratchet, got %s", got)
	}
	for _, l := range []string{"hygiene", "tests", "coverage"} {
		if got := adLayer(t, rep, l); got != "pass" {
			t.Errorf("%s: %s", l, got)
		}
	}

	// 4. The adoption merges; a change that raises Heavy fails.
	adGit(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
	adGit(t, dir, "checkout", "-q", "-b", "raise")
	writeFile(t, dir, "legacy.go", legacy(heavyFunc("\tif a > 100 {\n\t\tn++\n\t}\n"))["legacy.go"])
	adGit(t, dir, "commit", "-q", "-a", "-m", "raise Heavy")
	if _, rep := adGate(t, dir); adRatchet(t, rep) != "fail" || !strings.Contains(strings.Join(failed(t, rep), ","), "crap") {
		t.Fatalf("raising a function past its entry must fail the ratchet and CRAP: %v", rep["summary"])
	}

	// 5. A change that lowers Heavy passes, and a refresh drops its entry.
	adGit(t, dir, "checkout", "-q", "main")
	adGit(t, dir, "checkout", "-q", "-b", "lower", "adopt")
	writeFile(t, dir, "legacy.go", legacy(loopedHeavy)["legacy.go"])
	adGit(t, dir, "commit", "-q", "-a", "-m", "lower Heavy")
	_, rep = adGate(t, dir)
	if got := adRatchet(t, rep); got != "pass" || strings.Contains(strings.Join(failed(t, rep), ","), "crap") {
		t.Fatalf("lowering a function must pass: ratchet %s, summary %v", got, rep["summary"])
	}
	if code, stdout, stderr := gotopsEnv(t, dir, []string{"CGO_ENABLED=1"}, "baseline"); code != 0 {
		t.Fatalf("baseline refresh: exit %d\n%s\n%s", code, stdout, stderr)
	}
	if fns := adFunctions(t, dir); hasKeySuffix(fns, ".Heavy") || !hasKeySuffix(fns, ".Mid") {
		t.Fatalf("the refresh must drop Heavy and keep Mid: %v", fns)
	}
}

func failed(t *testing.T, rep map[string]any) []string {
	t.Helper()
	var out []string
	for _, v := range array(t, object(t, rep["summary"])["failed"]) {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func readFile(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
