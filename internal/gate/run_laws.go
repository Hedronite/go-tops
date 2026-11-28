package gate

import (
	"context"

	"github.com/Hedronite/go-tops/internal/lawpass"
	"github.com/Hedronite/go-tops/internal/laws"
	"github.com/Hedronite/go-tops/internal/report"
)

func lawEvidence(ctx context.Context, d Deps, env *Env, cfg Config, opts Options, lawsIn, stopped bool, layers []report.Layer) (laws.Evidence, error) {
	ev := laws.Evidence{Hygiene: laws.HygieneFromLayers(layers)}
	if lawsIn && !stopped && opts.MaxTier >= 0 {
		lawsRun := d.LawsRun
		if lawsRun == nil {
			lawsRun = func(ctx context.Context, dir, class string) (laws.RunnerResult, error) {
				return laws.Run(ctx, dir, class, nil)
			}
		}
		rr, err := lawsRun(ctx, env.Dir, cfg.Class)
		if err != nil {
			return ev, exitf(ExitFail, "law analyzers: %v", err)
		}
		ev.AnalyzersOK = rr.OK
		ev.Analyzers = rr.ByLaw
		ev.AnalyzersByFile = rr.ByLawFile
	}
	if lawsIn && !stopped && opts.MaxTier >= 1 {
		runLaw := d.LawPass
		if runLaw == nil {
			runLaw = func(ctx context.Context, env *Env) (lawpass.LintResult, error) {
				return lawpass.Run(ctx, env.Dir, lawpassExec{env.Exec}, lawpassTools{env.Tools})
			}
		}
		lintRes, err := runLaw(ctx, env)
		if err != nil {
			ev.LintPassOK = false
		} else {
			ev.Lint = lintRes
			ev.LintPassOK = true
		}
	}
	if lawsIn && !stopped && opts.MaxTier >= 1 && ev.LintPassOK {
		rel := func(name string) string {
			return lawpass.ModuleRel(env.Dir, lawpass.LawsConfigRel, name)
		}
		ev.Refusals = laws.RefusalCounts(ev, rel)
		env.RatchetLawRefusals = map[string]map[string]int(ev.Refusals)
	}
	if lawsIn && !stopped && d.ScanAcks != nil {
		acks, err := d.ScanAcks(env.Dir, cfg.AckLedger)
		if err != nil {
			return ev, exitf(ExitFail, "suppressions: %v", err)
		}
		ev.Acks = acks
	}
	return ev, nil
}

func attachLawResults(rep *report.Report, results []laws.Result) (failedLaws bool, partial bool) {
	rel := laws.Release()
	lw := &rep.Laws
	lw.Catalog, lw.Judged, lw.NotEvaluated = rel.Catalog, rel.Judged, rel.NotEvaluated
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
			failedLaws = true
		}
		lr := report.LawResult{ID: r.ID, Verdict: string(r.Verdict), Reader: string(r.Reader)}
		if r.Verdict == laws.NotEvaluated {
			lr.Reason = string(r.Reason)
		}
		lw.Results = append(lw.Results, lr)
	}
	return failedLaws, partial
}
