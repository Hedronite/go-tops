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
	"slices"
	"strings"
	"time"

	"github.com/Hedronite/go-tops/internal/crap"
	"github.com/Hedronite/go-tops/internal/exempt"
	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/gobuild"
	"github.com/Hedronite/go-tops/internal/layers/tests"
	"github.com/Hedronite/go-tops/internal/report"
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
	return judge(rep, cc.Cap, env.Config.Exemptions, time.Now())
}

// judge turns a scored module into the layer's result. Under every mode the
// layer reports honestly what is over the cap; what a mode does with it is the
// runner's and the ratchet's (§6.7). The exemptions in force (§6.8) take the
// functions they cover out of the cap check: those are still scored and listed
// under exempt. An entry that matches nothing fails the layer, naming it.
func judge(rep crap.Report, limit float64, exemptions []report.Exemption, now time.Time) gate.Result {
	m := exempt.Match(exemptions, exemptFuncs(rep), now)
	covered := make(map[string]bool, len(m.Exempt))
	for _, f := range m.Exempt {
		covered[f.Key] = true
	}
	measured, unmeasured := over(rep, limit, covered)
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
	details := map[string]any{
		"cap": limit, "violations": violations, "functions": scoreRows(rep),
		"not_measured": len(rep.NotMeasured),
	}
	if rows := exemptRows(rep, covered); len(rows) > 0 {
		details["exempt"] = rows
	}
	reason := joinReasons(
		violationNote(measured, unmeasured, limit),
		problemNote(m.Problems),
		lostNote(m.Lost),
		notMeasuredNote(len(rep.NotMeasured), rep.PlatformLoads),
	)
	outcome := gate.Pass
	if len(violations) > 0 || len(m.Problems) > 0 {
		outcome = gate.Fail
	}
	return gate.Result{Outcome: outcome, Reason: reason, Details: details}
}

// exemptFuncs lists every production function for the exemption matcher,
// measured or not: a path matches production files whatever their build tags.
func exemptFuncs(rep crap.Report) []exempt.Func {
	out := make([]exempt.Func, 0, len(rep.Measured)+len(rep.NotMeasured))
	for _, f := range rep.Measured {
		out = append(out, exempt.Func{Key: f.Key, File: f.File, Recv: f.Recv, Name: f.Name, Table: f.Table})
	}
	for _, f := range rep.NotMeasured {
		out = append(out, exempt.Func{Key: f.Key, File: f.File, Recv: f.Recv, Name: f.Name, Table: f.Table})
	}
	return out
}

// scoreRows is every production function the run scored (F18).
func scoreRows(rep crap.Report) []any {
	rows := make([]any, 0, len(rep.Measured)+len(rep.NotMeasured))
	for _, f := range rep.Measured {
		cov := f.Cov * 100
		crap := f.CRAP
		rows = append(rows, violation(f.Key, f.File, f.Comp, cov, crap))
	}
	for _, f := range rep.NotMeasured {
		rows = append(rows, violation(f.Key, f.File, f.Comp, nil, nil))
	}
	return rows
}

// over is the functions over the cap that no exemption covers.
func over(rep crap.Report, limit float64, covered map[string]bool) ([]crap.Function, []crap.NotMeasured) {
	m, n := rep.Over(limit)
	m = slices.DeleteFunc(m, func(f crap.Function) bool { return covered[f.Key] })
	n = slices.DeleteFunc(n, func(f crap.NotMeasured) bool { return covered[f.Key] })
	return m, n
}

// exemptRows are the covered functions as crapFunction rows, scored (null for a
// function that is not measured), in the scorer's order.
func exemptRows(rep crap.Report, covered map[string]bool) []any {
	var rows []any
	for _, f := range rep.Measured {
		if covered[f.Key] {
			rows = append(rows, violation(f.Key, f.File, f.Comp, f.Cov*100, f.CRAP))
		}
	}
	for _, f := range rep.NotMeasured {
		if covered[f.Key] {
			rows = append(rows, violation(f.Key, f.File, f.Comp, nil, nil))
		}
	}
	return rows
}

func violationNote(measured []crap.Function, unmeasured []crap.NotMeasured, limit float64) string {
	if len(measured)+len(unmeasured) == 0 {
		return ""
	}
	return fmt.Sprintf("%d function(s) over the CRAP cap %g: %s", len(measured)+len(unmeasured), limit, list(measured, unmeasured))
}

func problemNote(ps []exempt.Problem) string {
	msgs := make([]string, 0, len(ps))
	for _, p := range ps {
		msgs = append(msgs, p.Message)
	}
	return strings.Join(msgs, "; ")
}

// lostNote names each table-switch exemption whose function stopped being a
// table: it answers to the cap now (§6.8).
func lostNote(ls []exempt.Lost) string {
	msgs := make([]string, 0, len(ls))
	for _, l := range ls {
		msgs = append(msgs, fmt.Sprintf("the table-switch exemption for %s no longer holds: it is not table-shaped, so it answers to the cap", l.Func.Key))
	}
	return strings.Join(msgs, "; ")
}

func joinReasons(parts ...string) string {
	return strings.Join(slices.DeleteFunc(parts, func(s string) bool { return s == "" }), "; ")
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
