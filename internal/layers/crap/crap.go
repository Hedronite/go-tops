// Package crap is gate 4 of GO_TOPS.md §3.1: the CRAP layer. It scores every
// production function of the module with internal/crap, from the coverage
// profile the tests layer wrote, and fails any function over the cap (§6.2).
//
// Importing the package registers the layer with the runner.
package crap

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Hedronite/go-tops/internal/crap"
	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/layers/tests"
)

// ID is the layer's ID.
const ID = "crap"

func init() { gate.Register(Layer{}) }

// Layer is gate 4.
type Layer struct{}

// ID implements gate.Layer.
func (Layer) ID() string { return ID }

// Tier implements gate.Layer.
func (Layer) Tier() int { return 1 }

// Needs implements gate.Layer: CRAP reads the coverage profile, so it needs the
// coverage layer (§3.5).
func (Layer) Needs() []string { return []string{tests.CoverageID} }

// Run implements gate.Layer. It never runs the tests: it reads the profile of
// the run the tests layer made, under the build that run used (§3.1).
func (Layer) Run(_ context.Context, env *gate.Env) gate.Result {
	cc := env.Config.Crap
	rep, err := crap.Score(env.Dir, filepath.Join(env.Dir, filepath.FromSlash(tests.ProfilePath)), crap.Options{
		ErrGuardDiscount: cc.ErrGuardDiscount,
		WrapFunctions:    cc.WrapFunctions,
		Build:            tests.Build,
	})
	if err != nil {
		return failure(err)
	}
	return judge(rep, cc.Cap)
}

// judge turns a scored module into the layer's result. Under every mode the
// layer reports honestly what is over the cap; what a mode does with it is the
// runner's and the ratchet's (§6.7).
func judge(rep crap.Report, limit float64) gate.Result {
	measured, unmeasured := rep.Over(limit)
	violations := make([]any, 0, len(measured)+len(unmeasured))
	for _, f := range measured {
		violations = append(violations, violation(f.Key, f.File, f.Comp, f.Cov*100, f.CRAP))
	}
	// A function that is not measured on this build is held to its complexity
	// alone: at full coverage its score is its comp, the least any coverage can
	// give it, so over the cap it fails at every coverage level (§6.2).
	for _, f := range unmeasured {
		violations = append(violations, violation(f.Key, f.File, f.Comp, 100, float64(f.Comp)))
	}
	details := map[string]any{"cap": limit, "violations": violations}
	note := notMeasuredNote(len(rep.NotMeasured))
	if len(violations) == 0 {
		return gate.Result{Outcome: gate.Pass, Reason: note, Details: details}
	}
	reason := fmt.Sprintf("%d function(s) over the CRAP cap %g: %s", len(violations), limit, list(measured, unmeasured))
	if note != "" {
		reason += "; " + note
	}
	return gate.Result{Outcome: gate.Fail, Reason: reason, Details: details}
}

func violation(key, file string, comp int, covPct, score float64) map[string]any {
	return map[string]any{"func": key, "file": file, "cc": comp, "cov_pct": covPct, "crap": score, "baseline": nil}
}

func notMeasuredNote(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("%d production function(s) not measured on this build", n)
}

func list(measured []crap.Function, unmeasured []crap.NotMeasured) string {
	var parts []string
	for _, f := range measured {
		if len(parts) == 5 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s %.1f", f.Key, f.CRAP))
	}
	for _, f := range unmeasured {
		if len(parts) == 5 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s comp %d, not measured", f.Key, f.Comp))
	}
	text := strings.Join(parts, "; ")
	if total := len(measured) + len(unmeasured); total > len(parts) {
		text += fmt.Sprintf("; and %d more", total-len(parts))
	}
	return text
}

func failure(err error) gate.Result {
	msg := err.Error()
	if errors.Is(err, crap.ErrNoBlocks) {
		msg = "the coverage profile holds no block of a production file: nothing was measured"
	}
	return gate.Result{Outcome: gate.Fail, Reason: msg, Details: map[string]any{"error": msg}}
}
