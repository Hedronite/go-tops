package gate

import (
	"context"

	"github.com/Hedronite/go-tops/internal/lawpass"
	"github.com/Hedronite/go-tops/internal/laws"
	"github.com/Hedronite/go-tops/internal/report"
)

func lawEvidence(ctx context.Context, d Deps, env *Env, cfg Config, opts Options, lawsIn, stopped bool, layers []report.Layer) (laws.Evidence, error) {
	ev := laws.Evidence{Hygiene: laws.HygieneFromLayers(layers)}
	if !lawsIn || stopped {
		return ev, nil
	}
	if err := fillAnalyzerEvidence(ctx, d, env, cfg, opts, &ev); err != nil {
		return ev, err
	}
	fillLintEvidence(ctx, d, env, opts, &ev)
	if opts.MaxTier >= 1 && ev.LintPassOK {
		attachRefusals(env, &ev)
	}
	if d.ScanAcks != nil {
		if err := scanAckLedger(d, env, cfg, &ev); err != nil {
			return ev, err
		}
	}
	return ev, nil
}

func fillAnalyzerEvidence(ctx context.Context, d Deps, env *Env, cfg Config, opts Options, ev *laws.Evidence) error {
	if opts.MaxTier < 0 {
		return nil
	}
	lawsRun := d.LawsRun
	if lawsRun == nil {
		lawsRun = func(ctx context.Context, dir, class string) (laws.RunnerResult, error) {
			return laws.Run(ctx, dir, class, nil)
		}
	}
	rr, err := lawsRun(ctx, env.Dir, cfg.Class)
	if err != nil {
		return exitf(ExitFail, "law analyzers: %v", err)
	}
	ev.AnalyzersOK, ev.Analyzers, ev.AnalyzersByFile = rr.OK, rr.ByLaw, rr.ByLawFile
	return nil
}

func fillLintEvidence(ctx context.Context, d Deps, env *Env, opts Options, ev *laws.Evidence) {
	if opts.MaxTier < 1 {
		return
	}
	runLaw := d.LawPass
	if runLaw == nil {
		runLaw = func(ctx context.Context, env *Env) (lawpass.LintResult, error) {
			return lawpass.Run(ctx, env.Dir, lawpassExec{env.Exec}, lawpassTools{env.Tools})
		}
	}
	lintRes, err := runLaw(ctx, env)
	if err != nil {
		ev.LintPassOK = false
		return
	}
	ev.Lint, ev.LintPassOK = lintRes, true
}

func attachRefusals(env *Env, ev *laws.Evidence) {
	rel := func(name string) string {
		return lawpass.ModuleRel(env.Dir, lawpass.LawsConfigRel, name)
	}
	ev.Refusals = laws.RefusalCounts(*ev, rel)
	env.RatchetLawRefusals = map[string]map[string]int(ev.Refusals)
}

func scanAckLedger(d Deps, env *Env, cfg Config, ev *laws.Evidence) error {
	acks, err := d.ScanAcks(env.Dir, cfg.AckLedger)
	if err != nil {
		return exitf(ExitFail, "suppressions: %v", err)
	}
	ev.Acks = acks
	return nil
}

func attachLawResults(rep *report.Report, results []laws.Result) (failedLaws bool, partial bool) {
	rel := laws.Release()
	lw := &rep.Laws
	lw.Catalog, lw.Judged, lw.NotEvaluated = rel.Catalog, rel.Judged, rel.NotEvaluated
	for _, r := range results {
		failedLaws = tallyLawResult(lw, r) || failedLaws
		if r.Required && r.OutOfRange {
			partial = true
		}
	}
	return failedLaws, partial
}

func tallyLawResult(lw *report.Laws, r laws.Result) (refused bool) {
	lw.Applicable++
	if r.Verdict != laws.NotEvaluated {
		lw.ApplicableJudged++
	} else {
		bumpNotEvalReason(lw, r.Reason)
	}
	if r.Required {
		lw.Required++
		if r.Verdict == laws.NotEvaluated && !r.OutOfRange {
			lw.RequiredNotEvaluated++
		}
	}
	if r.OutOfRange {
		lw.OutOfRange = append(lw.OutOfRange, r.ID)
	}
	refused = r.Verdict == laws.Refuse
	lr := report.LawResult{ID: r.ID, Verdict: string(r.Verdict), Reader: string(r.Reader)}
	if r.Verdict == laws.NotEvaluated {
		lr.Reason = string(r.Reason)
	}
	lw.Results = append(lw.Results, lr)
	return refused
}

func bumpNotEvalReason(lw *report.Laws, reason laws.Reason) {
	switch reason {
	case laws.NoReader:
		lw.NotEvaluatedByReason.NoReader++
	case laws.ConfigOff:
		lw.NotEvaluatedByReason.ConfigOff++
	case laws.InputMissing:
		lw.NotEvaluatedByReason.InputMissing++
	}
}
