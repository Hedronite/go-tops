package gate

import (
	"fmt"
	"strings"

	"github.com/Hedronite/go-tops/internal/laws"
	"github.com/Hedronite/go-tops/internal/report"
)

// renderText prints the run: one line per layer, the laws, the verdict. A
// partial run says PARTIAL, and an advisory run says it is not enforced (§3.3,
// §6.7).
func renderText(rep report.Report, cfg Config) string {
	var b strings.Builder
	fmt.Fprintf(&b, "gotops gate: %s, class %s, enforce %s, max tier %d\n", rep.Module, rep.Class, rep.Enforce, rep.MaxTier)
	for _, l := range rep.Layers {
		fmt.Fprintf(&b, "  %-12s %s", l.ID, l.Outcome)
		if l.Reason != "" {
			fmt.Fprintf(&b, "  (%s)", l.Reason)
		}
		b.WriteByte('\n')
	}
	if rep.VulnDBTimestamp != nil {
		fmt.Fprintf(&b, "  vulnerability database as of %s; time-dependent: the same commit can change verdict when it moves\n", *rep.VulnDBTimestamp)
	}
	lw := rep.Laws
	fmt.Fprintf(&b, "  laws         %d of %d judged in this release; %d apply to the class, %d required, %d required not evaluated\n",
		lw.Judged, lw.Catalog, lw.Applicable, lw.Required, lw.RequiredNotEvaluated)
	if len(lw.OutOfRange) > 0 {
		fmt.Fprintf(&b, "  out of range: %s\n", strings.Join(lw.OutOfRange, " "))
	}
	fmt.Fprintf(&b, "layers: %d enabled in range, %d ran\n", rep.LayerCounts.EnabledInRange, rep.LayerCounts.Ran)
	verdict := strings.ToUpper(rep.Summary.Outcome)
	if rep.Summary.Outcome == string(RunPartial) {
		fmt.Fprintf(&b, "PARTIAL: nothing in range failed, but a required law is out of range, so this run did not judge everything the class requires (CI refuses it)\n")
	}
	if len(rep.Summary.Failed) > 0 {
		fmt.Fprintf(&b, "failed: %s\n", strings.Join(rep.Summary.Failed, ", "))
	}
	if !rep.Summary.Enforced {
		fmt.Fprintf(&b, "NOT ENFORCED (advisory, expires %s): outcome %s\n", rep.Summary.AdvisoryExpires, verdict)
	} else {
		fmt.Fprintf(&b, "%s (exit %d)\n", verdict, rep.Summary.ExitCode)
	}
	return b.String()
}

// renderPlan prints a dry run: what would run, and what would not.
func renderPlan(cfg Config, opts Options, plan []planned, results []laws.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "gotops gate --dry-run: %s, class %s, max tier %d. Nothing ran.\n", cfg.Module, cfg.Class, opts.MaxTier)
	for _, p := range plan {
		state := "out of range"
		switch {
		case p.inRange && !p.enabled():
			state = "skip: " + p.cfg.Reason
		case p.inRange:
			state = "would run"
		}
		fmt.Fprintf(&b, "  %-12s tier %d  %s\n", p.layer.ID(), p.layer.Tier(), state)
	}
	oor := 0
	for _, r := range results {
		if r.OutOfRange {
			oor++
		}
	}
	fmt.Fprintf(&b, "  laws         %d apply to the class, %d out of range\n", len(results), oor)
	return b.String()
}
