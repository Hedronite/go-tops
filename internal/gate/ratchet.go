package gate

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/Hedronite/go-tops/internal/report"
)

// RatchetVerdict is the ratchet's judgment of a run.
type RatchetVerdict struct {
	// Report is the report's top-level ratchet object: the outcome, which is skip
	// on a leg that is not the ledger leg and never pass there, and both builds.
	// Nil when the ratchet could not be judged at all.
	Report *report.Ratchet
	// Layers are the layers of the run with the thresholds the ledger replaces
	// applied: under ratchet a function over the cap, or a package under the floor,
	// fails only past its entry (§6.7).
	Layers []report.Layer
	// Failed reports whether the run fails because of the ratchet.
	Failed bool
	// Notes say what failed, one line each, for the text output.
	Notes []string
}

// RatchetJudge judges a run that reached its layers, under enforce: ratchet. A
// non-nil error is a fault that kept it from judging: the run fails, reported.
type RatchetJudge func(ctx context.Context, env *Env, layers []report.Layer) (*RatchetVerdict, error)

// DefaultRatchet is the judge a run takes when its Deps name none. The command
// wiring sets it from internal/ratchet, so the runner does not import it; with
// none, a ratchet run has no ratchet judgment.
var DefaultRatchet RatchetJudge

// ratchetStage asks the judge about a run that reached its layers, under enforce:
// ratchet, and returns the verdict with the layers the run reports. It returns
// the layers unchanged and no verdict under any other mode, with no judge, when
// the run stopped before its layers ran, and when the run's range does not hold
// the layers the ledger is measured from. A judge that cannot judge fails the
// run: nothing is guessed, and the layers still report.
func (d Deps) ratchetStage(ctx context.Context, cfg Config, env *Env, layers []report.Layer, stopped bool) (*RatchetVerdict, []report.Layer) {
	judge := d.Ratchet
	if judge == nil {
		judge = DefaultRatchet
	}
	if cfg.Enforce != "ratchet" || judge == nil || stopped || !holdsFeeds(layers) {
		return nil, layers
	}
	v, err := judge(ctx, env, layers)
	if err != nil {
		return &RatchetVerdict{Layers: layers, Failed: true, Notes: []string{fmt.Sprintf("ratchet: %v\n", err)}}, layers
	}
	if v == nil || v.Layers == nil {
		return v, layers
	}
	return v, v.Layers
}

// mark puts the verdict in the report and in the list of what failed. A nil
// verdict, no judgment, changes nothing.
func (rv *RatchetVerdict) mark(rep *report.Report, failed []string) []string {
	if rv == nil {
		return failed
	}
	rep.Ratchet = rv.Report
	if rv.Failed {
		failed = append(failed, "ratchet")
	}
	return failed
}

// text appends what the ratchet failed on to the run's text output.
func (rv *RatchetVerdict) text(rendered string) string {
	if rv == nil {
		return rendered
	}
	return rendered + strings.Join(rv.Notes, "")
}

// feedingLayers are the layers the ledger is measured from (§6.7): a run judged
// against the ledger must have run all four.
var feedingLayers = []string{"hygiene", "tests", "coverage", "crap"}

// holdsFeeds reports whether the run's range holds every feeding layer. A partial
// run (`--only crap`, `--only tests`, `--max-tier 0`) holds fewer, and the ratchet
// is then left out of the report, never failed for what the range left out: the run
// ends partial as it would without a ledger.
func holdsFeeds(layers []report.Layer) bool {
	for _, id := range feedingLayers {
		if !slices.ContainsFunc(layers, func(l report.Layer) bool { return l.ID == id }) {
			return false
		}
	}
	return true
}
