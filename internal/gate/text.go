package gate

import (
	"fmt"
	"strings"

	"github.com/Hedronite/go-tops/internal/laws"
)

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
