package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/baseline"
)

// `gotops baseline --status [--base REF]` answers one question for CI: is a first
// ledger due? It reads and writes nothing, prints one JSON line, and exits 0 when
// a first ledger is due (the diff base has none and the tree has none), 1 when
// not (present, deleted, advisory), 2 when it cannot tell. The base is read as
// the ratchet reads it (GO_TOPS.md §6.7), so a shallow clone, an unknown ref or a
// base ledger that does not parse is a 2 and never a guess.

type statusOut struct {
	State   string          `json:"state"`
	Path    string          `json:"path"`
	BaseRef string          `json:"base_ref"`
	Tree    bool            `json:"tree"`
	Base    *bool           `json:"base"`
	Build   *baseline.Build `json:"build"`
	Reason  string          `json:"reason"`
}

func statusGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func statusLedger() string {
	return string(baseline.Marshal(baseline.Init(baseline.Measure{Build: cmdBuild})))
}

// statusRepo is a module in root/sub (the root when sub is empty) whose branch main
// is the diff base, origin/main, with baseLedger committed when it is not empty. The
// pull request branch pr is checked out and has not changed anything yet.
func statusRepo(t *testing.T, cfg, sub, baseLedger string) (root, mod string) {
	t.Helper()
	newFixture().install(t)
	root = t.TempDir()
	mod = filepath.Join(root, sub)
	for rel, data := range map[string]string{"go.mod": "module example.com/app\n\ngo 1.26\n", "go-tops.lock": goodLock(t), "go-tops.yaml": cfg} {
		writeFile(t, mod, rel, data)
	}
	if baseLedger != "" {
		writeFile(t, mod, ledgerRel, baseLedger)
	}
	statusGit(t, root, "init", "-q", "-b", "main")
	statusGit(t, root, "add", "-A")
	statusGit(t, root, "commit", "-q", "-m", "base")
	statusGit(t, root, "update-ref", "refs/remotes/origin/main", "main")
	statusGit(t, root, "checkout", "-q", "-b", "pr")
	return root, mod
}

func commitPR(t *testing.T, root, msg string) {
	t.Helper()
	statusGit(t, root, "add", "-A")
	statusGit(t, root, "commit", "-q", "-m", msg)
}

// statusIn runs --status in dir and checks that stdout is exactly one JSON line.
func statusIn(t *testing.T, dir string, args ...string) (int, statusOut, string) {
	t.Helper()
	code, stdout, stderr := baselineIn(t, dir, append([]string{"--status"}, args...)...)
	if strings.Count(strings.TrimRight(stdout, "\n"), "\n") != 0 || !strings.HasSuffix(stdout, "\n") {
		t.Fatalf("stdout is not one line: %q (stderr %q)", stdout, stderr)
	}
	var got statusOut
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	return code, got, stderr
}

func wantStatus(t *testing.T, code int, got statusOut, wantCode int, wantState string) {
	t.Helper()
	if code != wantCode || got.State != wantState {
		t.Fatalf("exit %d state %q, want exit %d state %q: %+v", code, got.State, wantCode, wantState, got)
	}
}

func wantBase(t *testing.T, got statusOut, want bool) {
	t.Helper()
	if got.Base == nil || *got.Base != want {
		t.Errorf("base = %v, want %v", got.Base, want)
	}
}

// A1: the adoption pull request before it carries a ledger. Neither the base nor
// the tree has one, so the first ledger is due, in a module at the root and in one
// in a subdirectory, where a ledger the repository root tracks is not the module's.
func TestStatusFirstLedgerIsDueWhenNeitherTheBaseNorTheTreeHasOne(t *testing.T) {
	for _, sub := range []string{"", "mod"} {
		t.Run("module in "+sub, func(t *testing.T) {
			root, mod := statusRepo(t, ratchetConfig(t), sub, "")
			if sub != "" {
				writeFile(t, root, ledgerRel, statusLedger())
				commitPR(t, root, "a ledger of the root module")
			}
			code, got, stderr := statusIn(t, mod)
			wantStatus(t, code, got, 0, "first-ledger-due")
			if stderr != "" {
				t.Errorf("stderr %q, want none", stderr)
			}
			if got.Path != ledgerRel || got.BaseRef != "origin/main" || got.Tree {
				t.Errorf("path %q, base_ref %q, tree %v", got.Path, got.BaseRef, got.Tree)
			}
			wantBase(t, got, false)
			if got.Build == nil || !got.Build.Equal(cmdBuild) {
				t.Errorf("build %+v, want this run's build %+v", got.Build, cmdBuild)
			}
		})
	}
}

// The question is read-only: it writes no ledger, runs no layer and leaves the
// tree as it was.
func TestStatusWritesNothingAndRunsNoLayer(t *testing.T) {
	root, mod := statusRepo(t, ratchetConfig(t), "", "")
	fx := newFixture()
	fx.install(t)
	if code, got, _ := statusIn(t, mod); code != 0 || got.State != "first-ledger-due" {
		t.Fatalf("exit %d: %+v", code, got)
	}
	if fx.ran != 0 {
		t.Errorf("the feeding layers ran %d times", fx.ran)
	}
	if _, err := os.Stat(filepath.Join(mod, ledgerRel)); err == nil {
		t.Error("a ledger was written")
	}
	out, err := exec.CommandContext(t.Context(), "git", "-C", root, "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil || len(out) != 0 {
		t.Errorf("the tree changed: %q (%v)", out, err)
	}
}

// A3 and B1: a ledger in the tree needs no bootstrap, whether the base has none (the
// first ledger rides in the pull request) or has one (an ordinary pull request).
func TestStatusPresentWhenTheTreeHasTheLedger(t *testing.T) {
	t.Run("the pull request adds the first ledger", func(t *testing.T) {
		root, mod := statusRepo(t, ratchetConfig(t), "", "")
		writeFile(t, mod, ledgerRel, statusLedger())
		commitPR(t, root, "the first ledger")
		code, got, _ := statusIn(t, mod)
		wantStatus(t, code, got, 1, "present")
		if !got.Tree || got.Build != nil {
			t.Errorf("tree %v build %+v, want tree and no build", got.Tree, got.Build)
		}
	})
	t.Run("the base has it and the pull request leaves it", func(t *testing.T) {
		_, mod := statusRepo(t, ratchetConfig(t), "", statusLedger())
		code, got, _ := statusIn(t, mod)
		wantStatus(t, code, got, 1, "present")
	})
}

// B2: a ledger the base has and the tree lost is not a reset. No bootstrap is due:
// the gate fails the missing file, and --init would refuse it too.
func TestStatusDeletedWhenTheBaseHasTheLedgerAndTheTreeDoesNot(t *testing.T) {
	for _, sub := range []string{"", "mod"} {
		t.Run("module in "+sub, func(t *testing.T) {
			root, mod := statusRepo(t, ratchetConfig(t), sub, statusLedger())
			if err := os.Remove(filepath.Join(mod, ledgerRel)); err != nil {
				t.Fatal(err)
			}
			commitPR(t, root, "delete the ledger")
			code, got, _ := statusIn(t, mod)
			wantStatus(t, code, got, 1, "deleted")
			wantBase(t, got, true)
			if got.Tree {
				t.Error("tree is true for a deleted ledger")
			}
		})
	}
}

// A ledger the pull request head tracks and the working tree lost is a deletion as
// well, although the base has none: `baseline --init` refuses it for the same reason.
func TestStatusDeletedWhenHEADTracksTheLedgerAndTheTreeDoesNot(t *testing.T) {
	root, mod := statusRepo(t, ratchetConfig(t), "mod", "")
	writeFile(t, mod, ledgerRel, statusLedger())
	commitPR(t, root, "the first ledger")
	if err := os.Remove(filepath.Join(mod, ledgerRel)); err != nil {
		t.Fatal(err)
	}
	code, got, _ := statusIn(t, mod)
	wantStatus(t, code, got, 1, "deleted")
}

// A5, A6, A8 and a base ledger that does not parse: the kit cannot tell whether a
// base holds a ledger, so it says so with a 2 and never offers a bootstrap.
func TestStatusCannotTellFailsClosed(t *testing.T) {
	t.Run("shallow clone", func(t *testing.T) {
		root, _ := statusRepo(t, ratchetConfig(t), "", "")
		clone := filepath.Join(t.TempDir(), "clone")
		statusGit(t, root, "clone", "-q", "--depth=1", "file://"+root, clone)
		code, got, _ := statusIn(t, clone)
		wantStatus(t, code, got, 2, "error")
		if !strings.Contains(got.Reason, "shallow") {
			t.Errorf("reason %q should say the clone is shallow", got.Reason)
		}
	})
	t.Run("the base ref is unknown", func(t *testing.T) {
		_, mod := statusRepo(t, ratchetConfig(t), "", "")
		old := runBuild
		t.Cleanup(func() { runBuild = old })
		runBuild = func(context.Context, string) (baseline.Build, error) {
			return baseline.Build{GOOS: "linux", GOARCH: "amd64", CGOEnabled: true}, nil
		}
		code, got, _ := statusIn(t, mod, "--base", "no-such-ref")
		wantStatus(t, code, got, 2, "error")
		if !strings.Contains(got.Reason, "no-such-ref") {
			t.Errorf("reason %q should name the ref", got.Reason)
		}
	})
	t.Run("the base ledger does not parse", func(t *testing.T) {
		root, mod := statusRepo(t, ratchetConfig(t), "", "not a ledger\n")
		if err := os.Remove(filepath.Join(mod, ledgerRel)); err != nil {
			t.Fatal(err)
		}
		commitPR(t, root, "delete the ledger")
		code, got, _ := statusIn(t, mod)
		wantStatus(t, code, got, 2, "error")
	})
	t.Run("the configuration is missing", func(t *testing.T) {
		_, mod := statusRepo(t, ratchetConfig(t), "", "")
		old := runBuild
		t.Cleanup(func() { runBuild = old })
		runBuild = func(context.Context, string) (baseline.Build, error) {
			return baseline.Build{GOOS: "linux", GOARCH: "amd64", CGOEnabled: true}, nil
		}
		if err := os.Remove(filepath.Join(mod, "go-tops.yaml")); err != nil {
			t.Fatal(err)
		}
		code, got, _ := statusIn(t, mod)
		wantStatus(t, code, got, 2, "error")
		if got.Reason == "" {
			t.Error("an error says why")
		}
	})
}

// Only enforce: ratchet reads a ledger. Under advisory none is written, and under
// hard the thresholds are the floor and the cap, so none is ever due: the answer is
// not-ratchet, exit 1, whatever the tree or the base hold.
func TestStatusOutsideRatchetHasNoLedgerDue(t *testing.T) {
	for name, cfg := range map[string]func(*testing.T) string{"advisory": advisoryConfig, "hard": goodConfig} {
		t.Run(name, func(t *testing.T) {
			_, mod := statusRepo(t, cfg(t), "", "")
			code, got, _ := statusIn(t, mod)
			wantStatus(t, code, got, 1, "not-ratchet")
			if got.Tree || got.Base != nil || got.Build != nil {
				t.Errorf("tree %v base %v build %v: nothing was read for a mode with no ledger", got.Tree, got.Base, got.Build)
			}
		})
	}
	t.Run("hard with a ledger in the tree", func(t *testing.T) {
		_, mod := statusRepo(t, goodConfig(t), "", statusLedger())
		code, got, _ := statusIn(t, mod)
		wantStatus(t, code, got, 1, "not-ratchet")
	})
}

// --base names the diff base: the ratchet's --base of `gate`. A ref the repository
// has gives the same answer as the default does.
func TestStatusReadsTheBaseItIsGiven(t *testing.T) {
	_, mod := statusRepo(t, ratchetConfig(t), "", "")
	old := runBuild
	t.Cleanup(func() { runBuild = old })
	runBuild = func(context.Context, string) (baseline.Build, error) {
		return baseline.Build{GOOS: "linux", GOARCH: "amd64", CGOEnabled: true}, nil
	}
	code, got, _ := statusIn(t, mod, "--base", "main")
	wantStatus(t, code, got, 0, "first-ledger-due")
	if got.BaseRef != "main" {
		t.Errorf("base_ref %q, want main", got.BaseRef)
	}
}

func TestStatusIsOneModeWithItsOwnFlag(t *testing.T) {
	_, mod := statusRepo(t, ratchetConfig(t), "", "")
	old := runBuild
	t.Cleanup(func() { runBuild = old })
	runBuild = func(context.Context, string) (baseline.Build, error) {
		return baseline.Build{GOOS: "linux", GOARCH: "amd64", CGOEnabled: true}, nil
	}
	for _, args := range [][]string{
		{"--status", "--init"},
		{"--status", "--accept-regression", "--reason", "x y z", "--owner", "me"},
		{"--status", "--rename", "a", "b"},
		{"--base", "main"},
		{"--status", "stray"},
	} {
		code, stdout, stderr := baselineIn(t, mod, args...)
		if code != 2 || stdout != "" || !strings.Contains(stderr, "usage:") {
			t.Errorf("%v: exit %d, stdout %q, stderr %q; want a usage error", args, code, stdout, stderr)
		}
	}
}

func TestStatusSerializesEmptyTagsAsArray(t *testing.T) {
	_, mod := statusRepo(t, ratchetConfig(t), "", "")
	old := runBuild
	t.Cleanup(func() { runBuild = old })
	runBuild = func(context.Context, string) (baseline.Build, error) {
		return baseline.Build{GOOS: "linux", GOARCH: "amd64", CGOEnabled: true}, nil
	}
	code, stdout, stderr := baselineIn(t, mod, "--status")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, `"tags":[]`) || strings.Contains(stdout, `"tags":null`) {
		t.Fatalf("stdout %q: want \"tags\":[] and never \"tags\":null", stdout)
	}
}
