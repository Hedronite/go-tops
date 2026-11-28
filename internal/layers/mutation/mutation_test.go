package mutation

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/report"
)

type staticResolver struct{ err error }

func (r staticResolver) Resolve(_ context.Context, name string) ([]string, error) {
	if r.err != nil {
		return nil, r.err
	}
	return []string{name}, nil
}

func gitExec(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
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

func TestLayerMetadataAndPreflightLockError(t *testing.T) {
	l := Layer{}
	if l.ID() != "mutation" || l.Tier() != 1 || len(l.Needs()) != 1 || l.Needs()[0] != "tests" {
		t.Fatalf("unexpected layer metadata: %s %d %v", l.ID(), l.Tier(), l.Needs())
	}
	lockErr := &gate.LockError{Err: errors.New("lock mismatch")}
	if err := l.Preflight(context.Background(), &gate.Env{Tools: staticResolver{err: lockErr}}); !errors.Is(err, lockErr) {
		t.Fatalf("Preflight error = %v, want LockError", err)
	}
}

func TestLayerPackageMainMultiEditSubdirAndOutOfBuild(t *testing.T) {
	// 1. package main (E18) + E1 multi-edit hunk in one file.
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
	gitExec(t, dirMain, "config", "diff.interHunkContext", "10")
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
	resMainFail := Layer{}.Run(context.Background(), overlayEnv(t, dirMain))
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
	resMainPass := Layer{}.Run(context.Background(), overlayEnv(t, dirMain))
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
		"sub/mod/plat_never.go": "//go:build never_this_os\n\npackage mod\n\nfunc Lin(a, b int) bool { return a < b }\n",
	})
	resSub := Layer{}.Run(context.Background(), overlayEnv(t, filepath.Join(dirSub, "sub", "mod")))
	if resSub.Outcome != gate.Pass {
		t.Fatalf("subdir + out-of-build expected pass, got %s: %s", resSub.Outcome, resSub.Reason)
	}
	if got, ok := resSub.Details["not_measured"].(int); !ok || got != 2 {
		t.Fatalf("not_measured = %d, want 2 (plat_never.go)", got)
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
	env := overlayEnv(t, dir)
	resVac := Layer{}.Run(context.Background(), env)
	if resVac.Outcome != gate.Vacuous || resVac.Details["mutants"] != 0 {
		t.Fatalf("expected vacuous with mutationDetails, got %+v", resVac)
	}

	// Unmatched mutation exemption even on vacuous diff => fail.
	envBadEx := overlayEnv(t, dir)
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

func TestLayerReportValidatesAgainstLiveSchema(t *testing.T) {
	dir := setupGitModule(t, map[string]string{
		"go.mod":       "module example.com/schemaok\n\ngo 1.26\n",
		"calc.go":      "package calc\nfunc Ok(a int) bool { return a != 0 }\n",
		"calc_test.go": "package calc\nimport \"testing\"\nfunc TestOk(t *testing.T) { if !Ok(1) { t.Fail() } }\n",
	})
	// Check Fail (LIVED) + Pass + Vacuous all validate via report.Write + report.Load against live schema.
	writeFiles(t, dir, map[string]string{
		"calc.go":      "package calc\nfunc Lt(a, b int) bool { return a < b }\n",
		"calc_test.go": "package calc\nimport \"testing\"\nfunc TestLt(t *testing.T) { if !Lt(1, 2) || Lt(3, 2) { t.Fatal(\"bad\") } }\n",
	})
	resFail := Layer{}.Run(context.Background(), overlayEnv(t, dir))
	if resFail.Outcome != gate.Fail {
		t.Fatalf("expected Fail, got %s: %s", resFail.Outcome, resFail.Reason)
	}

	writeFiles(t, dir, map[string]string{
		"calc_test.go": "package calc\nimport \"testing\"\nfunc TestLt(t *testing.T) { if !Lt(1, 2) || Lt(2, 2) || Lt(3, 2) { t.Fatal(\"bad\") } }\n",
	})
	resPass := Layer{}.Run(context.Background(), overlayEnv(t, dir))
	if resPass.Outcome != gate.Pass {
		t.Fatalf("expected Pass, got %s: %s", resPass.Outcome, resPass.Reason)
	}

	dirVac := setupGitModule(t, map[string]string{
		"go.mod": "module example.com/vac\n\ngo 1.26\n",
		"v.go":   "package vac\nfunc F() int { return 1 }\n",
	})
	writeFiles(t, dirVac, map[string]string{"README.md": "docs\n"})
	resVac := Layer{}.Run(context.Background(), overlayEnv(t, dirVac))
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

func TestLayerFailsAWeakTestUnderASpacedPath(t *testing.T) {
	// B5: module directory path containing a space still catches weak package main survivor.
	dirSpace := filepath.Join(t.TempDir(), "with space")
	if err := os.MkdirAll(dirSpace, 0o755); err != nil {
		t.Fatal(err)
	}
	gitExec(t, dirSpace, "init", "-b", "main")
	gitExec(t, dirSpace, "config", "user.name", "Test Author")
	gitExec(t, dirSpace, "config", "user.email", "author@example.com")
	writeFiles(t, dirSpace, map[string]string{
		"go.mod":                "module example.com/sp\n\ngo 1.26\n",
		"cmd/tool/main.go":      "package main\nfunc lt(a, b int) bool { return a == b }\n",
		"cmd/tool/main_test.go": "package main\nimport \"testing\"\nfunc TestLt(t *testing.T) { if !lt(1, 2) { t.Fatal(\"bad\") } }\n",
	})
	gitExec(t, dirSpace, "add", ".")
	gitExec(t, dirSpace, "commit", "-m", "base")
	gitExec(t, dirSpace, "checkout", "-b", "feat")
	writeFiles(t, dirSpace, map[string]string{
		"cmd/tool/main.go":      "package main\nfunc lt(a, b int) bool { return a < b }\n",
		"cmd/tool/main_test.go": "package main\nimport \"testing\"\nfunc TestLt(t *testing.T) { if !lt(1, 2) || lt(3, 2) { t.Fatal(\"bad\") } }\n",
	})
	resSpace := Layer{}.Run(context.Background(), overlayEnv(t, dirSpace))
	if resSpace.Outcome != gate.Fail {
		t.Fatalf("expected fail in directory with space, got %s: %s", resSpace.Outcome, resSpace.Reason)
	}
}

func TestLayerF1ColorConfigAndF3DiffOpts(t *testing.T) {
	dir := setupGitModule(t, map[string]string{
		"go.mod":     "module example.com/f13\n\ngo 1.26\n",
		"fx.go":      "package fx\nfunc Add(a, b int) int { return a - b }\n",
		"fx_test.go": "package fx\nimport \"testing\"\nfunc TestAdd(t *testing.T) { if Add(3, 1) != 2 { t.Fatal(\"bad\") } }\n",
	})
	writeFiles(t, dir, map[string]string{
		"fx.go":      "package fx\nfunc Add(a, b int) int { return a + b }\nfunc Label(a string) string { return a + \"!\" }\n",
		"fx_test.go": "package fx\nimport \"testing\"\nfunc TestAdd(t *testing.T) {\n\tif Add(2, 3) != 5 { t.Fatal(\"bad\") }\n\tif Label(\"hi\") != \"hi!\" { t.Fatal(\"label\") }\n}\n",
	})

	// F1: color.ui=always and color.diff=always must still pass with killed + NOT VIABLE string concat (no false failure).
	gitExec(t, dir, "config", "color.ui", "always")
	gitExec(t, dir, "config", "color.diff", "always")
	// F3: GIT_DIFF_OPTS=-u5 must not inject context lines causing strong test to fail.
	t.Setenv("GIT_DIFF_OPTS", "-u5")
	res := Layer{}.Run(context.Background(), overlayEnv(t, dir))
	if res.Outcome != gate.Pass {
		t.Fatalf("expected Pass under color.ui/color.diff/GIT_DIFF_OPTS + string concat, got %s: %s", res.Outcome, res.Reason)
	}
	if res.Details["mutants"] != 1 || res.Details["dropped"] != 1 {
		t.Fatalf("expected the numeric + judged and the string + dropped, got %v", res.Details)
	}
}

func TestLayerF2LineDirectivesFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name      string
		directive string
	}{
		{name: "same_file_line1", directive: "//line fx.go:1\n"},
		{name: "other_file_line40", directive: "//line other.go:40\n"},
		{name: "with_col_line1", directive: "//line fx.go:1:1\n"},
		{name: "identity_line5_col1", directive: "//line fx.go:5:1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := setupGitModule(t, map[string]string{
				"go.mod":     "module example.com/linedir\n\ngo 1.26\n",
				"fx.go":      "package fx\nfunc Zero() int { return 0 }\n",
				"fx_test.go": "package fx\nimport \"testing\"\nfunc TestZero(t *testing.T) { _ = Zero() }\n",
			})
			writeFiles(t, dir, map[string]string{
				"fx.go": "package fx\nfunc Zero() int { return 0 }\n" + tc.directive + "func Add(a, b int) int { return a + b }\n",
			})
			res := Layer{}.Run(context.Background(), overlayEnv(t, dir))
			if res.Outcome != gate.Fail {
				t.Fatalf("%s: expected Fail closed on //line directive, got %s: %s", tc.name, res.Outcome, res.Reason)
			}
		})
	}

	t.Run("inline_line_directive_with_col", func(t *testing.T) {
		dir := setupGitModule(t, map[string]string{
			"go.mod":     "module example.com/inline\n\ngo 1.26\n",
			"fx.go":      "package fx\nfunc Zero() int { return 0 }\n",
			"fx_test.go": "package fx\nimport \"testing\"\nfunc TestZero(t *testing.T) { _ = Zero() }\n",
		})
		writeFiles(t, dir, map[string]string{
			"fx.go": "package fx\nfunc Zero() int { return 0 }\nfunc Add(a, b int) int { return /*line fx.go:1:1*/ a + b }\n",
		})
		res := Layer{}.Run(context.Background(), overlayEnv(t, dir))
		if res.Outcome != gate.Fail {
			t.Fatalf("expected Fail closed on inline /*line*/ directive, got %s: %s", res.Outcome, res.Reason)
		}
	})
}
