package gate

import (
	"context"
	"testing"

	"github.com/Hedronite/go-tops/internal/laws"
)

func TestLawsRunHookRunsAtMaxTierZero(t *testing.T) {
	var calls int
	d := deps(gateLayers(nil, nil))
	d.LawsRun = func(ctx context.Context, dir, class string) (laws.RunnerResult, error) {
		calls++
		return StubLawsRun(ctx, dir, class)
	}
	mustRun(t, cfg(), Options{MaxTier: 0}, d)
	if calls != 1 {
		t.Fatalf("laws run calls = %d, want 1 at max tier 0", calls)
	}
}

func TestLawsRunHookSkippedWhenLawsOutOfRange(t *testing.T) {
	var calls int
	d := deps(gateLayers(nil, nil))
	d.LawsRun = func(ctx context.Context, dir, class string) (laws.RunnerResult, error) {
		calls++
		return StubLawsRun(ctx, dir, class)
	}
	mustRun(t, cfg(), Options{MaxTier: 0, Only: "tests"}, d)
	if calls != 0 {
		t.Fatalf("laws run calls = %d, want 0 when laws layer is out of range", calls)
	}
}
