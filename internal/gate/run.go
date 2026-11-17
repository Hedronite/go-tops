package gate

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Hedronite/go-tops/internal/laws"
	"github.com/Hedronite/go-tops/internal/report"
)

// LawsLayer is the name `--only` takes for the laws layer. It is not a
// registered Layer: the report keeps laws apart from layers (§12.3).
const LawsLayer = "laws"

// Options are the flags of `gotops gate` (§12.1).
type Options struct {
	MaxTier  int
	FailFast bool
	DryRun   bool
	Only     string
	Base     string
}

// Deps are the run's collaborators. Zero values take the real ones.
type Deps struct {
	Dir         string
	Layers      []Layer // default: Layers()
	Exec        Executor
	Tools       Resolver
	CI          bool // CI=true: refuse --dry-run and a partial range (§3.4 rule 6)
	Now         func() time.Time
	ToolVersion string
	// Laws judges the laws layer; default laws.Judge, the stub until M5.
	Laws func(laws.Input) []laws.Result
}

// BinaryVerifier builds the verify hook of the default ToolResolver for a
// repository: it checks a downloaded binary against go-tops.lock for the host
// platform before the binary runs (§10). It is nil until the command wiring
// sets it from internal/lock, so the runner does not import the lock.
var BinaryVerifier func(dir string) func(name, path string) error

// Run is the result of a gate run that got as far as a plan.
type Run struct {
	Report   report.Report
	ExitCode int
	Text     string
	// DryRun is set for a --dry-run: Text is the plan, nothing ran, and there
	// is no report to write.
	DryRun bool
}

type planned struct {
	layer   Layer
	inRange bool
	cfg     LayerConfig
}

func (p planned) enabled() bool { return p.cfg.Enabled }

// Execute plans and runs the gate. A run refused or stopped before any layer
// ran returns an error carrying its exit code (§12.2: 2, 3 or 4). A run that
// starts returns a Run, whose exit code is 0 or 1, and whose report the caller
// writes.
func Execute(ctx context.Context, cfg Config, opts Options, d Deps) (*Run, error) {
	if d.Exec == nil {
		d.Exec = OS{}
	}
	if d.Tools == nil {
		r := &ToolResolver{Dir: d.Dir, Exec: d.Exec, Binary: DefaultBinaries()}
		if BinaryVerifier != nil {
			r.Verify = BinaryVerifier(d.Dir)
		}
		d.Tools = r
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Laws == nil {
		d.Laws = laws.Judge
	}
	if d.Layers == nil {
		d.Layers = Layers()
	}
	if err := check(d.Layers); err != nil {
		return nil, exitf(ExitEnv, "%v", err)
	}
	base := cfg.base(opts.Base)

	plan, err := makePlan(d.Layers, cfg, opts)
	if err != nil {
		return nil, err
	}
	lawsIn := opts.Only == "" || opts.Only == LawsLayer
	required := cfg.RequiredLaws
	static := d.Laws(laws.Input{Class: cfg.Class, MaxTier: opts.MaxTier, InRange: lawsIn, Reached: true, Required: required})
	partial := false
	for _, r := range static {
		if r.Required && r.OutOfRange {
			partial = true
		}
	}

	// Policy first (§12.2 code 4): a dry run, and a range that would end partial.
	if d.CI && opts.DryRun {
		return nil, exitf(ExitRefused, "--dry-run is refused when CI=true: a dry run judges nothing (§3.4 rule 6)")
	}
	if d.CI && partial {
		return nil, exitf(ExitRefused, "this range leaves a required law out of range and would end partial; it is refused when CI=true (§3.4 rule 6)")
	}
	if opts.DryRun {
		return &Run{DryRun: true, Text: renderPlan(cfg, opts, plan, static)}, nil
	}

	env := &Env{Dir: d.Dir, Config: cfg, Base: base, Exec: d.Exec, Tools: d.Tools}
	for _, p := range plan {
		if !p.inRange || !p.enabled() {
			continue
		}
		if pf, ok := p.layer.(Preflighter); ok {
			if err := pf.Preflight(ctx, env); err != nil {
				return nil, &ExitError{Code: exitCodeFor(err), Err: fmt.Errorf("%s: %w", p.layer.ID(), err)}
			}
		}
	}
	started := d.Now()
	git, err := gitState(ctx, d.Exec, d.Dir, base)
	if err != nil {
		return nil, &ExitError{Code: ExitEnv, Err: err}
	}

	layers, stopped, vulnDB := runLayers(ctx, env, plan, opts)

	results := d.Laws(laws.Input{Class: cfg.Class, MaxTier: opts.MaxTier, InRange: lawsIn, Reached: !stopped, Required: required})
	rep := assemble(cfg, opts, git, started, d.Now(), d.ToolVersion, vulnDB, layers, results)
	return &Run{Report: rep, ExitCode: rep.Summary.ExitCode, Text: report.RenderText(rep)}, nil
}

// makePlan decides, for each layer, whether it is in range (§3.2, §3.5) and
// what its switch says.
func makePlan(ls []Layer, cfg Config, opts Options) ([]planned, error) {
	only := map[string]bool{}
	if opts.Only != "" && opts.Only != LawsLayer {
		known := map[string]Layer{}
		for _, l := range ls {
			known[l.ID()] = l
		}
		if _, ok := known[opts.Only]; !ok {
			var names []string
			for _, l := range ls {
				names = append(names, l.ID())
			}
			return nil, exitf(ExitConfig, "--only %q: no such layer; layers are %s, %s", opts.Only, strings.Join(names, ", "), LawsLayer)
		}
		var add func(id string)
		add = func(id string) {
			if only[id] {
				return
			}
			only[id] = true
			for _, n := range known[id].Needs() {
				add(n)
			}
		}
		add(opts.Only)
	}
	out := make([]planned, 0, len(ls))
	for _, l := range ls {
		in := l.Tier() <= opts.MaxTier
		if opts.Only != "" {
			in = in && only[l.ID()]
		}
		out = append(out, planned{layer: l, inRange: in, cfg: cfg.layer(l.ID())})
	}
	return out, nil
}

// runLayers runs every enabled layer in range in gate order, each judged
// (§3.5): a failure does not stop the layers that do not depend on it. It
// returns the report entries and whether --fail-fast stopped the run.
func runLayers(ctx context.Context, env *Env, plan []planned, opts Options) ([]report.Layer, bool, string) {
	var out []report.Layer
	vulnDB := ""
	outcome := map[string]Outcome{}
	stopped := false
	for _, p := range plan {
		if !p.inRange {
			continue
		}
		l := p.layer
		entry := report.Layer{ID: l.ID(), Tier: l.Tier()}
		var res Result
		switch {
		case !p.enabled():
			reason := p.cfg.Reason
			if reason == "" {
				reason = "disabled in go-tops.yaml"
			}
			res = Result{Outcome: Skip, Reason: reason}
			entry.Expires = p.cfg.Expires
		case !hasCommand(l):
			// Before the needs rule and --fail-fast: not-run is "no command
			// mapped", not-reached is "has a command, but a layer it needs did
			// not pass" (§3.3).
			res = NoCommand(l.ID())
		case stopped:
			res = Result{Outcome: NotReached, Reason: "--fail-fast stopped the run before this layer"}
		default:
			if n := unmet(l, outcome); n != "" {
				res = Result{Outcome: NotReached, Reason: fmt.Sprintf("needs %s, which is %s", n, describe(outcome[n]))}
			} else {
				res = judge(l, l.Run(ctx, env))
			}
		}
		outcome[l.ID()] = res.Outcome
		if res.VulnDBTimestamp != "" {
			vulnDB = res.VulnDBTimestamp
		}
		entry.Outcome, entry.Reason, entry.Details = string(res.Outcome), res.Reason, res.Details
		out = append(out, entry)
		if opts.FailFast && p.enabled() && !stopped && (res.Outcome == Fail || res.Outcome == NotRun || res.Outcome == ToolMissing) {
			stopped = true
		}
	}
	return out, stopped, vulnDB
}

func describe(o Outcome) string {
	if o == "" {
		return "not in this run"
	}
	return string(o)
}

// unmet returns the first layer l needs that did not pass, or "".
func unmet(l Layer, outcome map[string]Outcome) string {
	for _, n := range l.Needs() {
		if outcome[n] != Pass {
			return n
		}
	}
	return ""
}

// judge refuses a result no layer may return. An empty or invalid result is
// no result (§3.4 rule 5): the layer fails, and says why in details.error,
// the form of the report schema for a failed layer that has no figures. skip
// and not-reached belong to the runner, and vacuous is for the empty mutation
// diff only (§3.3).
func judge(l Layer, r Result) Result {
	bad := func(format string, a ...any) Result {
		msg := fmt.Sprintf(format, a...) + "; an empty or invalid result is a failure"
		return Result{Outcome: Fail, Reason: msg, Details: map[string]any{"error": msg}}
	}
	switch {
	case !r.Outcome.valid():
		return bad("the %s layer returned no valid outcome (%q)", l.ID(), r.Outcome)
	case r.Outcome == Skip || r.Outcome == NotReached:
		return bad("the %s layer returned %s, which only the runner decides", l.ID(), r.Outcome)
	case r.Outcome == Vacuous && l.ID() != "mutation":
		return bad("the %s layer returned vacuous, which only an empty mutation diff may be (§3.3)", l.ID())
	}
	return r
}

// assemble builds the report and the verdict of the run (§3.3, §12.3).
func assemble(cfg Config, opts Options, git report.Git, started, ended time.Time, toolVersion, vulnDB string, layers []report.Layer, results []laws.Result) report.Report {
	rep := report.Report{
		Protocol: report.Protocol, ToolVersion: toolVersion, Module: cfg.Module, Class: cfg.Class, Enforce: cfg.Enforce,
		Git: git, MaxTier: opts.MaxTier,
		StartedAt:  started.UTC().Format(time.RFC3339),
		DurationMS: ended.Sub(started).Milliseconds(),
		Exemptions: cfg.Exemptions,
		Layers:     layers,
	}
	if vulnDB != "" {
		rep.VulnDBTimestamp = &vulnDB
	}
	var failed []string
	for _, l := range layers {
		o := Outcome(l.Outcome)
		if o != Skip {
			rep.LayerCounts.EnabledInRange++
		}
		if o.ran() {
			rep.LayerCounts.Ran++
		}
		if o.fails() {
			failed = append(failed, l.ID)
		}
	}

	rel := laws.Release()
	lw := &rep.Laws
	lw.Catalog, lw.Judged, lw.NotEvaluated = rel.Catalog, rel.Judged, rel.NotEvaluated
	refused, partial := false, false
	for _, r := range results {
		lw.Applicable++
		if r.Verdict != laws.NotEvaluated {
			lw.ApplicableJudged++
		} else {
			switch r.Reason {
			case laws.NoReader:
				lw.NotEvaluatedByReason.NoReader++
			case laws.ConfigOff:
				lw.NotEvaluatedByReason.ConfigOff++
			case laws.InputMissing:
				lw.NotEvaluatedByReason.InputMissing++
			}
		}
		if r.Required {
			lw.Required++
			if r.OutOfRange {
				partial = true
			} else if r.Verdict == laws.NotEvaluated {
				lw.RequiredNotEvaluated++
			}
		}
		if r.OutOfRange {
			lw.OutOfRange = append(lw.OutOfRange, r.ID)
		}
		if r.Verdict == laws.Refuse {
			refused = true
		}
		lr := report.LawResult{ID: r.ID, Verdict: string(r.Verdict), Reader: string(r.Reader)}
		if r.Verdict == laws.NotEvaluated {
			lr.Reason = string(r.Reason)
		}
		lw.Results = append(lw.Results, lr)
	}
	if refused || lw.RequiredNotEvaluated > 0 {
		failed = append(failed, LawsLayer)
	}
	if rep.LayerCounts.Ran != rep.LayerCounts.EnabledInRange && len(failed) == 0 {
		failed = append(failed, "layer_counts")
	}

	s := &rep.Summary
	s.Failed = failed
	switch {
	case len(failed) > 0:
		s.Outcome, s.ExitCode = string(RunFail), ExitFail
	case partial:
		s.Outcome = string(RunPartial)
	default:
		s.Outcome = string(RunPass)
	}
	s.Enforced = true
	if cfg.Enforce == "advisory" {
		// B1: the run is honest about its outcome and exits 0 (§6.7).
		s.Enforced, s.ExitCode, s.AdvisoryExpires = false, ExitOK, cfg.AdvisoryExpires
	}
	return rep
}
