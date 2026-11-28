package gate

import (
	"context"
	"fmt"
	"time"

	"github.com/Hedronite/go-tops/internal/laws"
	"github.com/Hedronite/go-tops/internal/report"
)

func fillDefaultDeps(d *Deps) {
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
}

func tryDryRun(cfg Config, opts Options, d Deps, plan []planned) (*Run, error) {
	lawsIn := opts.Only == "" || opts.Only == LawsLayer
	static := d.Laws(laws.Input{Class: cfg.Class, MaxTier: opts.MaxTier, InRange: lawsIn, Reached: true, Required: cfg.RequiredLaws})
	partial := lawRangePartial(static)
	if d.CI && opts.DryRun {
		return nil, exitf(ExitRefused, "--dry-run is refused when CI=true: a dry run judges nothing (§3.4 rule 6)")
	}
	if d.CI && partial {
		return nil, exitf(ExitRefused, "this range leaves a required law out of range and would end partial; it is refused when CI=true (§3.4 rule 6)")
	}
	if opts.DryRun {
		return &Run{DryRun: true, Text: renderPlan(cfg, opts, plan, static)}, nil
	}
	return nil, nil
}

func lawRangePartial(results []laws.Result) bool {
	for _, r := range results {
		if r.Required && r.OutOfRange {
			return true
		}
	}
	return false
}

func executeRun(ctx context.Context, cfg Config, opts Options, d Deps, base string, plan []planned) (*Run, error) {
	env := &Env{Dir: d.Dir, Config: cfg, Base: base, Exec: d.Exec, Tools: d.Tools}
	if err := preflightLayers(ctx, env, plan); err != nil {
		return nil, err
	}
	started := d.Now()
	git, err := gitState(ctx, d.Exec, d.Dir, base)
	if err != nil {
		return nil, &ExitError{Code: ExitEnv, Err: err}
	}
	lawsIn := opts.Only == "" || opts.Only == LawsLayer
	layers, stopped, vulnDB := runLayers(ctx, env, plan, opts)
	ev, err := lawEvidence(ctx, d, env, cfg, opts, lawsIn, stopped, layers)
	if err != nil {
		return nil, err
	}
	results := d.Laws(laws.Input{
		Class: cfg.Class, MaxTier: opts.MaxTier, InRange: lawsIn, Reached: !stopped,
		Required: cfg.RequiredLaws, Evidence: ev,
	})
	env.RatchetLaws = report.Laws{ApplicableJudged: applicableJudged(results)}
	verdict, layers := d.ratchetStage(ctx, cfg, env, layers, stopped)
	rep := assemble(cfg, opts, git, started, d.Now(), d.ToolVersion, vulnDB, layers, results, verdict)
	return &Run{Report: rep, ExitCode: rep.Summary.ExitCode, Text: verdict.text(report.RenderText(rep))}, nil
}

func preflightLayers(ctx context.Context, env *Env, plan []planned) error {
	for _, p := range plan {
		if !p.inRange || !p.enabled() {
			continue
		}
		pf, ok := p.layer.(Preflighter)
		if !ok {
			continue
		}
		if err := pf.Preflight(ctx, env); err != nil {
			return &ExitError{Code: exitCodeFor(err), Err: fmt.Errorf("%s: %w", p.layer.ID(), err)}
		}
	}
	return nil
}
