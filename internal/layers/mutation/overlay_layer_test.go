package mutation

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/mutate"
	"github.com/Hedronite/go-tops/internal/report"
)

func overlayEnv(t *testing.T, dir string) *gate.Env {
	t.Helper()
	return &gate.Env{
		Dir:   dir,
		Base:  "main",
		Exec:  gate.OS{},
		Tools: staticResolver{},
		Config: gate.Config{
			Base: "main",
			Mutation: gate.MutationConfig{
				Engine:         "overlay",
				UnexplainedMax: 0,
			},
		},
	}
}

func TestOverlayLayerPreflightNeedsOnlyGo(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(goBin))
	env := &gate.Env{
		Dir:    t.TempDir(),
		Exec:   gate.OS{},
		Tools:  &gate.ToolResolver{Dir: t.TempDir(), Exec: gate.OS{}},
		Config: gate.Config{Mutation: gate.MutationConfig{Engine: "overlay"}},
	}
	if err := (Layer{}).Preflight(t.Context(), env); err != nil {
		t.Fatalf("overlay preflight should only require go, got %v", err)
	}
}

type fakeLedger struct{ keys map[mutate.SurvivorKey]bool }

func (l fakeLedger) Explains(k mutate.SurvivorKey) bool { return l.keys[k] }

func TestOverlayLayerWeakStrongVacuousAndLedger(t *testing.T) {
	dir := setupGitModule(t, map[string]string{
		"go.mod": "module example.com/ovlayer\n\ngo 1.26\n",
		"lt.go":  "package ovlayer\n\nfunc Lt(a, b int) bool { return a == b }\n",
		"lt_test.go": `package ovlayer

import "testing"

func TestBase(t *testing.T) { _ = Lt(1, 2) }
`,
	})
	writeFiles(t, dir, map[string]string{
		"lt.go": "package ovlayer\n\nfunc Lt(a, b int) bool { return a < b }\n",
		"lt_test.go": `package ovlayer

import "testing"

func TestWeak(t *testing.T) {
	if !Lt(1, 5) || Lt(6, 5) {
		t.Fatal("wrong")
	}
}
`,
	})
	env := overlayEnv(t, dir)
	resFail := Layer{}.Run(t.Context(), env)
	if resFail.Outcome != gate.Fail {
		t.Fatalf("weak overlay expected Fail, got %s: %s", resFail.Outcome, resFail.Reason)
	}
	if !strings.Contains(resFail.Reason, "lt.go:Lt:CONDITIONALS_BOUNDARY:") {
		t.Fatalf("expected survivor key in reason, got %q", resFail.Reason)
	}

	origLoader := mutate.DefaultLedger
	t.Cleanup(func() { mutate.DefaultLedger = origLoader })
	mutate.DefaultLedger = func(_, _ string, _ time.Time) (mutate.Ledger, error) {
		h, herr := mutate.MutatedLineHash([]byte("package ovlayer\n\nfunc Lt(a, b int) bool { return a < b }\n"), 3, 35, "CONDITIONALS_BOUNDARY")
		if herr != nil {
			return nil, herr
		}
		k := mutate.SurvivorKey{File: "lt.go", Function: "Lt", Mutator: "CONDITIONALS_BOUNDARY", LineHash: h}
		return fakeLedger{keys: map[mutate.SurvivorKey]bool{k: true}}, nil
	}
	ledgerEnv := overlayEnv(t, dir)
	ledgerEnv.Config.Mutation.Ledger = ".go-tops/survivors.yaml"
	if resExplained := (Layer{}).Run(t.Context(), ledgerEnv); resExplained.Outcome != gate.Pass {
		t.Fatalf("explained overlay survivor expected Pass, got %s: %s", resExplained.Outcome, resExplained.Reason)
	}
	mutate.DefaultLedger = origLoader

	writeFiles(t, dir, map[string]string{
		"lt_test.go": `package ovlayer

import "testing"

func TestStrong(t *testing.T) {
	if !Lt(1, 5) || Lt(5, 5) || Lt(6, 5) {
		t.Fatal("wrong")
	}
}
`,
	})
	resPass := Layer{}.Run(t.Context(), env)
	if resPass.Outcome != gate.Pass {
		t.Fatalf("strong overlay expected Pass, got %s: %s", resPass.Outcome, resPass.Reason)
	}

	dirVac := setupGitModule(t, map[string]string{
		"go.mod": "module example.com/ovvac\n\ngo 1.26\n",
		"v.go":   "package ovvac\n\nfunc F() int { return 1 }\n",
	})
	writeFiles(t, dirVac, map[string]string{"README.md": "doc update\n"})
	resVac := Layer{}.Run(t.Context(), overlayEnv(t, dirVac))
	if resVac.Outcome != gate.Vacuous {
		t.Fatalf("expected Vacuous, got %s: %s", resVac.Outcome, resVac.Reason)
	}
	badExEnv := overlayEnv(t, dirVac)
	badExEnv.Config.Exemptions = []report.Exemption{{
		Layer: "mutation", Path: "missing.go", Symbol: "Missing", Reason: "stale", Owner: "x", Expires: "2099-01-01",
	}}
	if got := (Layer{}).Run(t.Context(), badExEnv); got.Outcome != gate.Fail {
		t.Fatalf("unmatched exemption on vacuous diff must Fail, got %s", got.Outcome)
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
			StartedAt:   "2026-11-26T00:00:00Z",
			Module:      "example.com/ovlayer",
			Class:       "binary-cli",
			Enforce:     "hard",
			MaxTier:     1,
			LayerCounts: report.LayerCounts{EnabledInRange: 1, Ran: 1},
			Git:         report.Git{Head: strings.Repeat("a", 40), Base: "main"},
			Exemptions:  []report.Exemption{},
			Layers:      []report.Layer{{ID: "mutation", Tier: 1, Outcome: string(tc.res.Outcome), Reason: tc.res.Reason, Details: tc.res.Details}},
			Laws:        report.Laws{OutOfRange: []string{}, Results: []report.LawResult{}},
			Summary:     report.Summary{Outcome: tc.outcome, ExitCode: tc.exit, Failed: tc.failed, Enforced: true},
		}
		path := filepath.Join(t.TempDir(), tc.name+".json")
		if err := report.Write(path, doc); err != nil {
			t.Fatalf("%s report.Write: %v", tc.name, err)
		}
		if _, err := report.Load(path); err != nil {
			t.Fatalf("%s report.Load: %v", tc.name, err)
		}
	}
}

// G6: overlay must NOT reuse .go-tops/cover.out even if present; it must run its own
// per-package -vet=off -coverprofile run without -coverpkg.
func TestOverlayLayerNeverReusesCrossPackageCoverOut(t *testing.T) {
	dir := setupGitModule(t, map[string]string{
		"go.mod": "module example.com/nocovreuse\n\ngo 1.26\n",
		"lt.go":  "package nocovreuse\n\nfunc Lt(a, b int) bool { return a == b }\n",
		"lt_test.go": `package nocovreuse

import "testing"

func TestBase(t *testing.T) {
	if Lt(1, 2) {
		t.Fatal("bad")
	}
}
`,
	})
	writeFiles(t, dir, map[string]string{
		"lt.go": "package nocovreuse\n\nfunc Lt(a, b int) bool { return a < b }\n",
		"lt_test.go": `package nocovreuse

import "testing"

func TestStrong(t *testing.T) {
	if !Lt(1, 5) || Lt(5, 5) || Lt(6, 5) {
		t.Fatal("wrong")
	}
}
`,
		// Plant a stale corrupt .go-tops/cover.out: overlay must ignore it and run per-pkg coverage.
		".go-tops/cover.out": "corrupt not parsed\n",
	})
	spy := &overlayLayerSpyExec{}
	env := overlayEnv(t, dir)
	env.Exec = spy
	res := Layer{}.Run(t.Context(), env)
	if res.Outcome != gate.Pass {
		t.Fatalf("expected Pass (ignoring stale cover.out), got %s: %s", res.Outcome, res.Reason)
	}
	if len(spy.coverArgvs) != 1 || !slices.Contains(spy.coverArgvs[0], "-vet=off") || slices.Contains(spy.coverArgvs[0], "-coverpkg=./...") {
		t.Fatalf("expected 1 targeted per-pkg -vet=off cover call without -coverpkg, got %v", spy.coverArgvs)
	}
	if !spy.sawCompileVetOff {
		t.Errorf("expected compile step with -vet=off")
	}
}

type overlayLayerSpyExec struct {
	mu               sync.Mutex
	coverArgvs       [][]string
	sawCompileVetOff bool
}

func (s *overlayLayerSpyExec) Run(ctx context.Context, c gate.Cmd) (gate.Output, error) {
	if len(c.Argv) >= 2 && c.Argv[0] == "go" && c.Argv[1] == "test" {
		s.mu.Lock()
		if slices.Contains(c.Argv, "-c=true") && slices.Contains(c.Argv, "-vet=off") {
			s.sawCompileVetOff = true
		}
		for _, a := range c.Argv {
			if strings.HasPrefix(a, "-coverprofile=") {
				s.coverArgvs = append(s.coverArgvs, slices.Clone(c.Argv))
				break
			}
		}
		s.mu.Unlock()
	}
	return gate.OS{}.Run(ctx, c)
}

type spyToolsResolver struct {
	names []string
}

func (s *spyToolsResolver) Resolve(_ context.Context, name string) ([]string, error) {
	s.names = append(s.names, name)
	return []string{name}, nil
}

func TestOverlayLayerPreflightNilAndEngineVariants(t *testing.T) {
	if err := (Layer{}).Preflight(t.Context(), nil); err == nil {
		t.Fatal("expected Preflight(nil) to return error without panicking")
	}
	spyOverlay := &spyToolsResolver{}
	overlayEnv := &gate.Env{
		Dir:    t.TempDir(),
		Tools:  spyOverlay,
		Config: gate.Config{Mutation: gate.MutationConfig{Engine: "overlay"}},
	}
	if err := (Layer{}).Preflight(t.Context(), overlayEnv); err != nil {
		t.Fatalf("overlay preflight: %v", err)
	}
	if !slices.Equal(spyOverlay.names, []string{"go"}) {
		t.Fatalf("expected overlay preflight to resolve [go], got %v", spyOverlay.names)
	}
	spyDefault := &spyToolsResolver{}
	defaultEnv := &gate.Env{
		Dir:   t.TempDir(),
		Tools: spyDefault,
	}
	if err := (Layer{}).Preflight(t.Context(), defaultEnv); err != nil {
		t.Fatalf("default preflight: %v", err)
	}
	if !slices.Equal(spyDefault.names, []string{"go"}) {
		t.Fatalf("expected default preflight to resolve [go], got %v", spyDefault.names)
	}
}

type failingOverlayExec struct {
	err error
}

func (f failingOverlayExec) Run(ctx context.Context, c gate.Cmd) (gate.Output, error) {
	if len(c.Argv) >= 3 && c.Argv[0] == "go" && c.Argv[1] == "list" && slices.Contains(c.Argv, "-m") {
		return gate.Output{}, f.err
	}
	return gate.OS{}.Run(ctx, c)
}

func TestOverlayLayerExecuteMissingToolErrors(t *testing.T) {
	dir := setupGitModule(t, map[string]string{
		"go.mod":  "module example.com/misstool\n\ngo 1.26\n",
		"calc.go": "package misstool\n\nfunc Add(a, b int) int { return a - b }\n",
	})
	writeFiles(t, dir, map[string]string{
		"calc.go": "package misstool\n\nfunc Add(a, b int) int { return a + b }\n",
	})
	for _, tc := range []struct {
		name        string
		err         error
		wantOutcome gate.Outcome
	}{
		{name: "tool_missing", err: gate.ErrToolMissing, wantOutcome: gate.ToolMissing},
		{name: "not_found", err: gate.ErrNotFound, wantOutcome: gate.ToolMissing},
		{name: "generic_error", err: errors.New("boom"), wantOutcome: gate.Fail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := overlayEnv(t, dir)
			env.Exec = failingOverlayExec{err: tc.err}
			res := Layer{}.Run(t.Context(), env)
			if res.Outcome != tc.wantOutcome {
				t.Fatalf("outcome = %s, want %s (%s)", res.Outcome, tc.wantOutcome, res.Reason)
			}
		})
	}
}
