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
	"github.com/Hedronite/go-tops/internal/gobuild"
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
	// The build of the test run, with the platform the go command picks named,
	// so the files scored are the files the profile covers.
	build, err := gobuild.Resolve(env.Dir, tests.Build)
	if err != nil {
		return failure(err)
	}
	rep, err := crap.Score(env.Dir, filepath.Join(env.Dir, filepath.FromSlash(tests.ProfilePath)), crap.Options{
		ErrGuardDiscount: cc.ErrGuardDiscount,
		WrapFunctions:    cc.WrapFunctions,
		Build:            build,
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
	// A function that is not measured has no coverage and so no score, and its
	// row says so: null cov_pct and crap. Its complexity is above the cap, which
	// fails at every coverage (§6.2).
	for _, f := range unmeasured {
		violations = append(violations, violation(f.Key, f.File, f.Comp, nil, nil))
	}
	details := map[string]any{"cap": limit, "violations": violations, "not_measured": len(rep.NotMeasured)}
	note := notMeasuredNote(len(rep.NotMeasured), rep.PlatformLoads)
	if len(violations) == 0 {
		return gate.Result{Outcome: gate.Pass, Reason: note, Details: details}
	}
	reason := fmt.Sprintf("%d function(s) over the CRAP cap %g: %s", len(violations), limit, list(measured, unmeasured))
	if note != "" {
		reason += "; " + note
	}
	return gate.Result{Outcome: gate.Fail, Reason: reason, Details: details}
}

// violation is one crapFunction row. cov and score are nil for a function that
// is not measured; baseline is the ratchet's to fill.
func violation(key, file string, comp int, cov, score any) map[string]any {
	return map[string]any{"func": key, "file": file, "cc": comp, "cov_pct": cov, "crap": score, "baseline": nil}
}

// notMeasuredNote is the count the text output prints on every run where it is
// above zero (§6.2), and what typing those functions cost.
func notMeasuredNote(n, loads int) string {
	switch {
	case n == 0:
		return ""
	case loads == 0:
		return fmt.Sprintf("%d production function(s) not measured on this build", n)
	}
	return fmt.Sprintf("%d production function(s) not measured on this build, typed by loading the module for %d other platform(s)", n, loads)
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
