package gate

import (
	"time"

	"github.com/Hedronite/go-tops/internal/laws"
	"github.com/Hedronite/go-tops/internal/report"
)

// assemble builds the report and the verdict of the run (§3.3, §12.3).
func assemble(cfg Config, opts Options, git report.Git, started, ended time.Time, toolVersion, vulnDB string, layers []report.Layer, results []laws.Result, rv *RatchetVerdict) report.Report {
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

	refused, partial := attachLawResults(&rep, results)
	if refused || rep.Laws.RequiredNotEvaluated > 0 {
		failed = append(failed, LawsLayer)
	}
	failed = rv.mark(&rep, failed)
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
		s.Enforced, s.ExitCode, s.AdvisoryExpires = false, ExitOK, cfg.AdvisoryExpires
	}
	return rep
}

func applicableJudged(results []laws.Result) int {
	var n int
	for _, r := range results {
		if r.Verdict != laws.NotEvaluated {
			n++
		}
	}
	return n
}
