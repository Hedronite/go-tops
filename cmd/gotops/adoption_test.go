//go:build adoption

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

// The adoption path of GO_TOPS.md §6.7 ("First adoption of a legacy repo"),
// end to end with the real binary: init into a legacy module that has functions
// over the CRAP cap, baseline --init, commit, check, and the ratchet judges the
// first ledger. Then a change that raises a function fails, --accept-regression
// records it and the gate passes, and a change that lowers a function passes and a
// refresh drops its entry. It runs twice: a module at the root of the repository
// and a module in a subdirectory of it. The drop-in's own judging step is run
// against each report.
//
// It needs the network (init tidies tools/go.mod, the hygiene layer reads the
// vulnerability database) and the pinned golangci-lint on PATH, so it carries
// the build tag and runs by hand:
//
//	go test -tags adoption -run TestAdoption -timeout 20m ./cmd/gotops
//
// What "the gate passes under ratchet" means at M2 (ruling C9): the report has
// ratchet.outcome pass and every tier 0 to 1 layer (hygiene, tests, coverage,
// CRAP) pass, and the only failing item is the class's required laws, reported as
// required and not evaluated until M5. A class with no required laws exits 0.
// The test asserts that from report.json, the pattern of the kit's own assertion
// step, because the exit code is 1 on those laws until then.

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

// adLayerObject is a layer of the report as a whole, for a failure message.
func adLayerObject(t *testing.T, rep map[string]any, id string) map[string]any {
	t.Helper()
	for _, l := range array(t, rep["layers"]) {
		if m := object(t, l); m["id"] == id {
			return m
		}
	}
	return nil
}

// adAssertPasses is ruling C9: ratchet pass, hygiene, tests, coverage and CRAP
// pass, and nothing fails but the required laws, none of them evaluated; a class
// with no required laws exits 0.
func adAssertPasses(t *testing.T, code int, rep map[string]any, when string) {
	t.Helper()
	if got := adRatchet(t, rep); got != "pass" {
		t.Errorf("%s: the ratchet outcome is %s, want pass", when, got)
	}
	for _, l := range []string{"hygiene", "tests", "coverage", "crap"} {
		if got := adLayer(t, rep, l); got != "pass" {
			t.Errorf("%s: layer %s is %s, want pass: %v", when, l, got, adLayerObject(t, rep, l))
		}
	}
	lw := object(t, rep["laws"])
	required, notEvaluated := lw["required"], lw["required_not_evaluated"]
	failed := failed(t, rep)
	if required == float64(0) {
		if code != 0 || len(failed) != 0 {
			t.Errorf("%s: a class with no required laws exits 0 with nothing failed: exit %d, failed %v", when, code, failed)
		}
		return
	}
	if notEvaluated != required {
		t.Errorf("%s: %v required laws, %v not evaluated: until M5 every required law is reported not evaluated", when, required, notEvaluated)
	}
	if len(failed) != 1 || failed[0] != "laws" || code != 1 {
		t.Errorf("%s: exit %d, failed %v: the only failing item must be the required laws", when, code, failed)
	}
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

// adJudge runs the drop-in's judging step (the one the rendered workflow carries)
// against the report the gate just wrote, as the runner would on one leg, with the
// gate's exit code. It returns the step's error, or nil when it passes. Without jq
// the check is skipped: the runners have it.
func adJudge(t *testing.T, dir, leg string, code int) error {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Log("jq is not installed: the drop-in's judging step is not run")
		return nil
	}
	var wf struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(readFile(t, dir, ".github/workflows/go-tops.yml")), &wf); err != nil {
		t.Fatal(err)
	}
	for _, s := range wf.Jobs["gate"].Steps {
		if !strings.Contains(s.Name, "judge the gate") {
			continue
		}
		cmd := exec.CommandContext(t.Context(), "bash", "-e", "-c", s.Run)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GATE_EXIT="+strconv.Itoa(code), "LEG="+leg)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%w: %s", err, out)
		}
		return nil
	}
	t.Fatal("the drop-in has no step that judges the gate")
	return nil
}

// adAssertJudged is the C9 assertion made by the drop-in itself: its judging
// step passes on both gate legs for the report just written.
func adAssertJudged(t *testing.T, dir string, code int, when string) {
	t.Helper()
	for _, leg := range []string{"ubuntu", "macos"} {
		if err := adJudge(t, dir, leg, code); err != nil {
			t.Errorf("%s: the drop-in's judging step fails on the %s leg: %v", when, leg, err)
		}
	}
}

// adExplain accepts every unexplained survivor of the last report, as an owner of
// a legacy repository would, and commits the ledger. The fixture's tests are weak
// on purpose, so the lines a change touches hold survivors (§6.4): the mutation
// layer must fail on them first.
func adExplain(t *testing.T, dir string, rep map[string]any) {
	t.Helper()
	if got := adLayer(t, rep, "mutation"); got != "fail" {
		t.Fatalf("a change the tests do not pin down must fail the mutation layer, got %s: %v", got, adLayerObject(t, rep, "mutation"))
	}
	expires := time.Now().UTC().AddDate(0, 0, 30).Format(time.DateOnly)
	explained := map[string]bool{}
	for _, r := range array(t, object(t, adLayerObject(t, rep, "mutation")["details"])["survivors"]) {
		row := object(t, r)
		key, ok := row["key"].(string)
		if !ok {
			t.Fatalf("a survivor row without a key: %v", row)
		}
		// Two rows share a key when their lines read the same.
		if row["explained"] != nil || explained[key] {
			continue
		}
		explained[key] = true
		code, stdout, stderr := gotopsEnv(t, dir, nil, "survivors", "explain", key,
			"--reason", "legacy test gap accepted", "--owner", "legacy-owners", "--expires", expires)
		if code != 0 {
			t.Fatalf("survivors explain %s: exit %d\n%s\n%s", key, code, stdout, stderr)
		}
	}
	adGit(t, dir, "add", ".go-tops/survivors.yaml")
	adGit(t, dir, "commit", "-q", "-m", "explain survivors")
}

func TestAdoptionOfALegacyRepo(t *testing.T) {
	if _, err := exec.LookPath("golangci-lint"); err != nil {
		t.Skip("the pinned golangci-lint is not on PATH")
	}
	for _, c := range []struct{ name, sub string }{{"module at the root", ""}, {"module in mod", "mod"}} {
		t.Run(c.name, func(t *testing.T) { adoptLegacy(t, c.sub) })
	}
}

// adoptLegacy is the adoption path for a repository whose module is at its root
// (sub is "") or in a subdirectory of it. Git runs at the repository root and
// gotops in the module.
func adoptLegacy(t *testing.T, sub string) {
	t.Helper()
	repo := t.TempDir()
	dir := filepath.Join(repo, sub)
	for name, content := range legacy(heavyFunc("")) {
		writeFile(t, dir, name, content)
	}
	cgo := []string{"CGO_ENABLED=1"}
	adGit(t, repo, "init", "-q", "-b", "main")
	adGit(t, repo, "add", "-A")
	adGit(t, repo, "commit", "-q", "-m", "legacy")
	adGit(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD") // the diff base: no baseline yet
	adGit(t, repo, "checkout", "-q", "-b", "adopt")
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
	// are in the ledger, the function under the cap is not, and the command says
	// to commit the file.
	code, stdout, stderr = gotopsEnv(t, dir, cgo, "baseline", "--init")
	if code != 0 || !strings.Contains(stdout, ".go-tops/baseline.json written") || !strings.Contains(stdout, "Commit it with go-tops.yaml") {
		t.Fatalf("baseline --init: exit %d\n%s\n%s", code, stdout, stderr)
	}
	fns := adFunctions(t, dir)
	if !hasKeySuffix(fns, ".Heavy") || !hasKeySuffix(fns, ".Mid") || hasKeySuffix(fns, ".Simple") || len(fns) != 2 {
		t.Fatalf("the ledger holds %v, want Heavy and Mid only", fns)
	}

	// 3. commit, check, and the first ledger passes the ratchet with no source edit.
	adGit(t, repo, "add", "-A")
	adGit(t, repo, "commit", "-q", "-m", "adopt Go-TOPS")
	if code, _, stderr := gotopsEnv(t, dir, nil, "check"); code != 0 {
		t.Fatalf("check: exit %d\n%s", code, stderr)
	}
	code, rep := adGate(t, dir)
	adAssertPasses(t, code, rep, "the first ledger, with no source edit")
	adAssertJudged(t, dir, code, "the first ledger")

	// 4. The adoption merges; a change that raises Heavy fails, and the drop-in's
	// ledger leg fails on it.
	adGit(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	adGit(t, repo, "checkout", "-q", "-b", "raise")
	writeFile(t, dir, "legacy.go", legacy(heavyFunc("\tif a > 100 {\n\t\tn++\n\t}\n"))["legacy.go"])
	adGit(t, repo, "commit", "-q", "-a", "-m", "raise Heavy")
	code, rep = adGate(t, dir)
	if adRatchet(t, rep) != "fail" || !strings.Contains(strings.Join(failed(t, rep), ","), "ratchet") {
		t.Fatalf("raising a function past its entry must fail the ratchet: %v", rep["summary"])
	}
	if err := adJudge(t, dir, "ubuntu", code); err == nil {
		if _, lookErr := exec.LookPath("jq"); lookErr == nil {
			t.Error("the drop-in's judging step passes on a raised function")
		}
	}

	// 4b. --accept-regression records the raise, and the gate then passes.
	code, stdout, stderr = gotopsEnv(t, dir, cgo, "baseline", "--accept-regression", "--reason", "Heavy gained a branch for a new limit", "--owner", "legacy-owners")
	if code != 0 || !strings.Contains(stdout, "1 regression(s) recorded by this write under owner legacy-owners") {
		t.Fatalf("baseline --accept-regression: exit %d\n%s\n%s", code, stdout, stderr)
	}
	adGit(t, repo, "commit", "-q", "-a", "-m", "accept the raise of Heavy")
	_, rep = adGate(t, dir)
	adExplain(t, dir, rep)
	code, rep = adGate(t, dir)
	adAssertPasses(t, code, rep, "after the raise is recorded")
	adAssertJudged(t, dir, code, "after the raise is recorded")

	// 5. A change that lowers Heavy passes, and a refresh drops its entry.
	adGit(t, repo, "checkout", "-q", "main")
	adGit(t, repo, "checkout", "-q", "-b", "lower", "adopt")
	writeFile(t, dir, "legacy.go", legacy(loopedHeavy)["legacy.go"])
	adGit(t, repo, "commit", "-q", "-a", "-m", "lower Heavy")
	_, rep = adGate(t, dir)
	adExplain(t, dir, rep)
	code, rep = adGate(t, dir)
	adAssertPasses(t, code, rep, "after lowering Heavy")
	adAssertJudged(t, dir, code, "after lowering Heavy")
	if code, stdout, stderr := gotopsEnv(t, dir, cgo, "baseline"); code != 0 {
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
