package gate

import (
	"context"
	"testing"

	"github.com/Hedronite/go-tops/internal/lawpass"
	"github.com/Hedronite/go-tops/internal/report"
)

// GO_TOPS §8.1: the gate runs the delegated law pass after tier-1 layers when laws are in range.
func TestLawPassHookRunsAfterLayers(t *testing.T) {
	var calls int
	d := deps(gateLayers(nil, nil))
	d.LawPass = func(ctx context.Context, env *Env) (lawpass.LintResult, error) {
		calls++
		return StubLawPass(ctx, env)
	}
	mustRun(t, cfg(), opts(), d)
	if calls != 1 {
		t.Fatalf("law pass calls = %d, want 1 after a full tier-1 run", calls)
	}
}

func TestLawPassHookSkippedWhenMaxTierZero(t *testing.T) {
	var calls int
	d := deps(gateLayers(nil, nil))
	d.LawPass = func(ctx context.Context, env *Env) (lawpass.LintResult, error) {
		calls++
		return StubLawPass(ctx, env)
	}
	mustRun(t, cfg(), Options{MaxTier: 0}, d)
	if calls != 0 {
		t.Fatalf("law pass calls = %d, want 0 at max tier 0", calls)
	}
}

func TestLawPassErrorFailsGate(t *testing.T) {
	d := deps(gateLayers(nil, nil))
	d.LawPass = func(context.Context, *Env) (lawpass.LintResult, error) {
		return lawpass.LintResult{}, lawpass.ErrLintPass{Msg: "lint-laws.json missing"}
	}
	r := mustRun(t, cfg(), opts(), d)
	if r.ExitCode != 1 {
		t.Fatalf("exit %d, want 1 when the law pass cannot run", r.ExitCode)
	}
	for _, id := range []string{"G-02", "G-07", "G-11", "G-12", "G-16", "G-18", "G-37"} {
		var lr report.LawResult
		for _, x := range r.Report.Laws.Results {
			if x.ID == id {
				lr = x
				break
			}
		}
		if lr.Verdict != "not-evaluated" || lr.Reason != "input_missing" {
			t.Errorf("%s = %s/%s, want not-evaluated/input_missing", id, lr.Verdict, lr.Reason)
		}
	}
}
