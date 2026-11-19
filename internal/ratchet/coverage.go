package ratchet

import (
	"context"

	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/layers/tests"
)

func init() { gate.Register(WrapCoverage(tests.Coverage{})) }

// WrapCoverage makes the coverage layer ratchet-aware (§6.7). The CRAP layer
// needs coverage to pass (§3.5), and the coverage layer judges the floor by the
// hard rule, so a legacy package under its floor would leave CRAP not reached on
// every ratchet run. Under enforce: ratchet the ratchet's packages judgment
// replaces the floor verdict: a layer that failed only because a package is
// under its floor is a pass here, CRAP runs on the profile, and Apply gives the
// layer its final outcome from the ledger. A layer that failed for any other
// reason (no profile, an empty profile, no block of a production file) stays
// failed, and under hard and advisory the layer is untouched.
func WrapCoverage(inner gate.Layer) gate.Layer { return ratchetCoverage{inner} }

type ratchetCoverage struct{ gate.Layer }

func (c ratchetCoverage) Run(ctx context.Context, env *gate.Env) gate.Result {
	res := c.Layer.Run(ctx, env)
	if env.Config.Enforce != "ratchet" || res.Outcome != gate.Fail || !floorOnly(res.Details) {
		return res
	}
	res.Outcome = gate.Pass
	res.Reason = "under enforce ratchet the packages judgment replaces the floor verdict"
	return res
}

// floorOnly reports whether the details are those of a floor failure: the
// figures are there and the layer found nothing wrong with the profile.
func floorOnly(details map[string]any) bool {
	_, figures := details["statements_pct"]
	_, broken := details["error"]
	return figures && !broken
}
