package gate

import (
	"context"
	"testing"

	"github.com/Hedronite/go-tops/internal/lawpass"
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
	mustRefuse(t, cfg(), opts(), d, ExitFail)
}
