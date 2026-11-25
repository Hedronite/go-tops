package mutation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/report"
)

var (
	buildOnce   sync.Once
	gremlins    string
	gremlinsDir string
	buildErr    error
)

func TestMain(m *testing.M) {
	buildOnce.Do(buildGremlinsOnce)
	code := m.Run()
	if gremlinsDir != "" {
		if err := os.RemoveAll(gremlinsDir); err != nil {
			code = 1
		}
	}
	os.Exit(code)
}

func buildGremlinsOnce() {
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		buildErr = err
		return
	}
	tmpDir, err := os.MkdirTemp("", "gotops-test-gremlins-*")
	if err != nil {
		buildErr = err
		return
	}
	gremlinsDir = tmpDir
	bin := filepath.Join(tmpDir, "gremlins")
	cmd := exec.Command("go", "build", "-modfile="+filepath.Join(repoRoot, "tools", "go.mod"),
		"-o", bin, "github.com/go-gremlins/gremlins/cmd/gremlins")
	out, err := cmd.CombinedOutput()
	if err != nil {
		buildErr = errors.New(string(out) + ": " + err.Error())
		return
	}
	gremlins = bin
}

func gremlinsBinary(t *testing.T) string {
	t.Helper()
	if buildErr != nil {
		t.Fatalf("build gremlins binary: %v", buildErr)
	}
	return gremlins
}

type staticResolver struct {
	bin string
	err error
}

func (r staticResolver) Resolve(_ context.Context, name string) ([]string, error) {
	if r.err != nil {
		return nil, r.err
	}
	if name == "gremlins" && r.bin != "" {
		return []string{r.bin}, nil
	}
	return nil, gate.ErrToolMissing
}

func gitExec(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func setupGitModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	gitExec(t, dir, "init", "-b", "main")
	gitExec(t, dir, "config", "user.name", "Test Author")
	gitExec(t, dir, "config", "user.email", "author@example.com")
	writeFiles(t, dir, files)
	gitExec(t, dir, "add", ".")
	gitExec(t, dir, "commit", "-m", "base")
	gitExec(t, dir, "checkout", "-b", "feat")
	return dir
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func testEnv(t *testing.T, dir, bin string) *gate.Env {
	t.Helper()
	return &gate.Env{
		Dir:   dir,
		Base:  "main",
		Exec:  gate.OS{},
		Tools: staticResolver{bin: bin},
		Config: gate.Config{
			Base:     "main",
			Mutation: gate.MutationConfig{Engine: "gremlins", UnexplainedMax: 0},
		},
	}
}

func TestLayerPreflightAndMissingTool(t *testing.T) {
	l := Layer{}
	if l.ID() != "mutation" || l.Tier() != 1 || len(l.Needs()) != 1 || l.Needs()[0] != "tests" {
		t.Fatalf("unexpected layer metadata: %s %d %v", l.ID(), l.Tier(), l.Needs())
	}
	lockErr := &gate.LockError{Err: errors.New("lock mismatch")}
	if err := l.Preflight(context.Background(), &gate.Env{Tools: staticResolver{err: lockErr}}); !errors.Is(err, lockErr) {
		t.Fatalf("Preflight error = %v, want LockError", err)
	}

	dir := setupGitModule(t, map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.26\n",
		"m.go":   "package m\nfunc F(a int) bool { return a > 0 }\n",
	})
	writeFiles(t, dir, map[string]string{"m.go": "package m\nfunc F(a int) bool { return a < 1 }\n"})
	env := testEnv(t, dir, "")
	res := l.Run(context.Background(), env)
	if res.Outcome != gate.ToolMissing {
		t.Fatalf("Outcome = %s, want tool-missing (%s)", res.Outcome, res.Reason)
	}
}

func TestLayerRealEngineWeakVsStrongAndEnvScrub(t *testing.T) {
	bin := gremlinsBinary(t)
	dir := setupGitModule(t, map[string]string{
		"go.mod":       "module example.com/calc\n\ngo 1.26\n",
		"calc.go":      "package calc\nfunc Ok(a int) bool { return a != 0 }\n",
		"calc_test.go": "package calc\nimport \"testing\"\nfunc TestOk(t *testing.T) { if !Ok(1) { t.Fail() } }\n",
	})
	writeFiles(t, dir, map[string]string{
		".gremlins.yaml": "mutants:\n  conditionals-boundary:\n    enabled: false\nunleash:\n  workers: 99\n",
		"calc.go":        "package calc\nfunc Lt(a, b int) bool { return a < b }\n",
		"calc_test.go":   "package calc\nimport \"testing\"\nfunc TestLt(t *testing.T) {\n\tif !Lt(1, 2) || Lt(3, 2) { t.Fatal(\"bad\") }\n}\n",
	})
	t.Setenv("GREMLINS_MUTANTS_CONDITIONALS_BOUNDARY_ENABLED", "false")
	t.Setenv("GREMLINS_UNLEASH_EXCLUDE_FILES", ".*")

	env := testEnv(t, dir, bin)
	resFail := Layer{}.Run(context.Background(), env)
	if resFail.Outcome != gate.Fail {
		t.Fatalf("expected fail on weak boundary test, got %s: %s", resFail.Outcome, resFail.Reason)
	}
	raw, err := json.Marshal(resFail.Details)
	if err != nil {
		t.Fatal(err)
	}
	var details map[string]any
	if err := json.Unmarshal(raw, &details); err != nil {
		t.Fatal(err)
	}
	surv, ok := details["survived_unexplained"].(float64)
	if !ok || surv < 1 || details["base"] != "main" || details["base_sha"] == "" {
		t.Fatalf("unexpected mutationDetails: %s", string(raw))
	}
	rows, ok := details["survivors"].([]any)
	if !ok {
		t.Fatalf("unexpected survivors type: %T", details["survivors"])
	}
	foundBoundary := false
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("unexpected row type: %T", r)
		}
		if _, has := m["explained"]; !has || m["explained"] != nil {
			t.Fatalf("expected explicit explained:null, got %+v", m)
		}
		if m["mutator"] == "CONDITIONALS_BOUNDARY" && m["function"] == "Lt" && m["status"] == "LIVED" {
			foundBoundary = true
		}
	}
	if !foundBoundary {
		t.Fatalf("expected CONDITIONALS_BOUNDARY LIVED in Lt, got %s", string(raw))
	}

	// Expired exemption does not cover the survivor (P11).
	envExpired := testEnv(t, dir, bin)
	envExpired.Config.Exemptions = []report.Exemption{
		{Path: "calc.go", Symbol: "Lt", Layer: "mutation", Reason: "old", Owner: "@a", Expires: "2020-01-01"},
	}
	if resExp := (Layer{}).Run(context.Background(), envExpired); resExp.Outcome != gate.Fail {
		t.Fatalf("expected expired exemption not to excuse survivor, got %s", resExp.Outcome)
	}

	// Strengthen test -> pass.
	writeFiles(t, dir, map[string]string{
		"calc_test.go": "package calc\nimport \"testing\"\nfunc TestLt(t *testing.T) {\n\tif !Lt(1, 2) || Lt(2, 2) || Lt(3, 2) { t.Fatal(\"bad\") }\n}\n",
	})
	resPass := Layer{}.Run(context.Background(), env)
	if resPass.Outcome != gate.Pass {
		t.Fatalf("expected pass on strong test, got %s: %s", resPass.Outcome, resPass.Reason)
	}
}

func TestLayerRealEnginePackageMainMultiEditSubdirAndOutOfBuild(t *testing.T) {
	bin := gremlinsBinary(t)

	// 1. package main (E18/Q1 -> NeedsIntegration=true) + E1 multi-edit hunk in one file.
	dirMain := setupGitModule(t, map[string]string{
		"go.mod": "module example.com/cli\n\ngo 1.26\n",
		"cmd/tool/main.go": `package main

func first(a, b int) bool {
	return a == b
}

func keepOne() int { return 1 }

func second(a, b int) bool {
	return a != b
}
`,
		"cmd/tool/main_test.go": `package main
import "testing"
func TestAll(t *testing.T) {
	if !first(1, 2) || first(3, 2) { t.Fatal("first") }
	if !second(1, 2) || second(3, 2) { t.Fatal("second") }
}
`,
	})
	writeFiles(t, dirMain, map[string]string{
		"cmd/tool/main.go": `package main

func first(a, b int) bool {
	return a < b
}

func keepOne() int { return 1 }

func second(a, b int) bool {
	return a < b
}
`,
		"cmd/tool/main_test.go": `package main
import "testing"
func TestAll(t *testing.T) {
	if !first(1, 2) || first(2, 2) || first(3, 2) { t.Fatal("first") }
	if !second(1, 2) || second(3, 2) { t.Fatal("second") }
}
`,
	})
	resMainFail := Layer{}.Run(context.Background(), testEnv(t, dirMain, bin))
	if resMainFail.Outcome != gate.Fail {
		t.Fatalf("package main + multi-edit hunk expected fail on second LIVED, got %s: %s", resMainFail.Outcome, resMainFail.Reason)
	}

	writeFiles(t, dirMain, map[string]string{
		"cmd/tool/main_test.go": `package main
import "testing"
func TestAll(t *testing.T) {
	if !first(1, 2) || first(2, 2) || first(3, 2) { t.Fatal("first") }
	if !second(1, 2) || second(2, 2) || second(3, 2) { t.Fatal("second") }
}
`,
	})
	resMainPass := Layer{}.Run(context.Background(), testEnv(t, dirMain, bin))
	if resMainPass.Outcome != gate.Pass {
		t.Fatalf("package main strong test expected pass, got %s: %s", resMainPass.Outcome, resMainPass.Reason)
	}

	// 2. Subdirectory module (E2) + mixed out-of-build _linux.go (E6).
	dirSub := setupGitModule(t, map[string]string{
		"README.md":            "repo root\n",
		"sub/mod/go.mod":       "module example.com/sub\n\ngo 1.26\n",
		"sub/mod/calc.go":      "package mod\nfunc Lt(a, b int) bool { return a == b }\n",
		"sub/mod/calc_test.go": "package mod\nimport \"testing\"\nfunc TestLt(t *testing.T) {}\n",
	})
	writeFiles(t, dirSub, map[string]string{
		"sub/mod/calc.go":       "package mod\nfunc Lt(a, b int) bool { return a < b }\n",
		"sub/mod/calc_test.go":  "package mod\nimport \"testing\"\nfunc TestLt(t *testing.T) { if !Lt(1, 2) || Lt(2, 2) || Lt(3, 2) { t.Fatal(\"bad\") } }\n",
		"sub/mod/plat_linux.go": "package mod\nfunc Lin(a, b int) bool { return a < b }\n",
	})
	resSub := Layer{}.Run(context.Background(), testEnv(t, filepath.Join(dirSub, "sub", "mod"), bin))
	if resSub.Outcome != gate.Pass {
		t.Fatalf("subdir + out-of-build expected pass, got %s: %s", resSub.Outcome, resSub.Reason)
	}
	if got, ok := resSub.Details["not_measured"].(int); !ok || got != 2 {
		t.Fatalf("not_measured = %d, want 2 (plat_linux.go)", got)
	}
}

func TestLayerVacuousCommentOnlyAndExemptionProblems(t *testing.T) {
	dir := setupGitModule(t, map[string]string{
		"go.mod":    "module example.com/v\n\ngo 1.26\n",
		"v.go":      "package v\nfunc F() int { return 1 }\n",
		"v_test.go": "package v\nimport \"testing\"\nfunc TestF(t *testing.T) {}\n",
	})
	// Test-only diff => vacuous with valid mutationDetails.
	writeFiles(t, dir, map[string]string{
		"v_test.go": "package v\nimport \"testing\"\nfunc TestF(t *testing.T) { _ = F() }\n",
	})
	env := testEnv(t, dir, gremlinsBinary(t))
	resVac := Layer{}.Run(context.Background(), env)
	if resVac.Outcome != gate.Vacuous || resVac.Details["mutants"] != 0 {
		t.Fatalf("expected vacuous with mutationDetails, got %+v", resVac)
	}

	// Unmatched mutation exemption even on vacuous diff => fail.
	envBadEx := testEnv(t, dir, "")
	envBadEx.Config.Exemptions = []report.Exemption{
		{Path: "does_not_exist.go", Layer: "mutation", Reason: "stale", Owner: "@a", Expires: "2027-01-01"},
	}
	resEx := Layer{}.Run(context.Background(), envBadEx)
	if resEx.Outcome != gate.Fail || !strings.Contains(resEx.Reason, "matches no production file") {
		t.Fatalf("expected exemption problem failure on vacuous diff, got %+v", resEx)
	}

	// Comment-only edit on prod file => pass with 0 mutants.
	writeFiles(t, dir, map[string]string{
		"v.go": "package v\n// doc comment\nfunc F() int { return 1 }\n",
	})
	resComment := Layer{}.Run(context.Background(), env)
	if resComment.Outcome != gate.Pass || !strings.Contains(resComment.Reason, "0 mutants") {
		t.Fatalf("expected pass '0 mutants' on comment-only diff, got %+v", resComment)
	}
}

func TestLayerReportValidatesAgainstLive045Schema(t *testing.T) {
	bin := gremlinsBinary(t)
	dir := setupGitModule(t, map[string]string{
		"go.mod":       "module example.com/schemaok\n\ngo 1.26\n",
		"calc.go":      "package calc\nfunc Ok(a int) bool { return a != 0 }\n",
		"calc_test.go": "package calc\nimport \"testing\"\nfunc TestOk(t *testing.T) { if !Ok(1) { t.Fail() } }\n",
	})
	// Check Fail (LIVED) + Pass + Vacuous all validate via report.Write + report.Load against live 0.4.5 schema.
	writeFiles(t, dir, map[string]string{
		"calc.go":      "package calc\nfunc Lt(a, b int) bool { return a < b }\n",
		"calc_test.go": "package calc\nimport \"testing\"\nfunc TestLt(t *testing.T) { if !Lt(1, 2) || Lt(3, 2) { t.Fatal(\"bad\") } }\n",
	})
	resFail := Layer{}.Run(context.Background(), testEnv(t, dir, bin))
	if resFail.Outcome != gate.Fail {
		t.Fatalf("expected Fail, got %s: %s", resFail.Outcome, resFail.Reason)
	}

	writeFiles(t, dir, map[string]string{
		"calc_test.go": "package calc\nimport \"testing\"\nfunc TestLt(t *testing.T) { if !Lt(1, 2) || Lt(2, 2) || Lt(3, 2) { t.Fatal(\"bad\") } }\n",
	})
	resPass := Layer{}.Run(context.Background(), testEnv(t, dir, bin))
	if resPass.Outcome != gate.Pass {
		t.Fatalf("expected Pass, got %s: %s", resPass.Outcome, resPass.Reason)
	}

	dirVac := setupGitModule(t, map[string]string{
		"go.mod": "module example.com/vac\n\ngo 1.26\n",
		"v.go":   "package vac\nfunc F() int { return 1 }\n",
	})
	writeFiles(t, dirVac, map[string]string{"README.md": "docs\n"})
	resVac := Layer{}.Run(context.Background(), testEnv(t, dirVac, bin))
	if resVac.Outcome != gate.Vacuous {
		t.Fatalf("expected Vacuous, got %s: %s", resVac.Outcome, resVac.Reason)
	}

	for _, tc := range []struct {
		name    string
		res     gate.Result
		outcome string
		exit    int
		failed  []string
	}{
		{"fail", resFail, "fail", 1, []string{"mutation"}},
		{"pass", resPass, "pass", 0, []string{}},
		{"vacuous", resVac, "pass", 0, []string{}},
	} {
		doc := report.Report{
			Protocol:    report.Protocol,
			ToolVersion: "0.1.0-test",
			StartedAt:   "2026-11-25T00:00:00Z",
			Module:      "example.com/test",
			Class:       "binary-cli",
			Enforce:     "hard",
			MaxTier:     1,
			LayerCounts: report.LayerCounts{EnabledInRange: 1, Ran: 1},
			Git:         report.Git{Head: strings.Repeat("a", 40), Base: "main"},
			Exemptions:  []report.Exemption{},
			Layers: []report.Layer{{
				ID: "mutation", Tier: 1, Outcome: string(tc.res.Outcome), Reason: tc.res.Reason, Details: tc.res.Details,
			}},
			Laws: report.Laws{
				OutOfRange: []string{},
				Results:    []report.LawResult{},
			},
			Summary: report.Summary{Outcome: tc.outcome, ExitCode: tc.exit, Failed: tc.failed, Enforced: true},
		}
		path := filepath.Join(t.TempDir(), tc.name+".json")
		if err := report.Write(path, doc); err != nil {
			t.Fatalf("%s report.Write schema validation failed: %v", tc.name, err)
		}
		if _, err := report.Load(path); err != nil {
			t.Fatalf("%s report.Load failed: %v", tc.name, err)
		}
	}
}
