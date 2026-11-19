package ratchet

import (
	"fmt"
	"slices"

	"github.com/Hedronite/go-tops/internal/baseline"
)

// diffBase checks the ledger against its copy at the diff base (§6.7): a raised
// or added entry, or a lowered judged_laws, needs a regressions record; with no
// copy, the file is a first ledger and must equal what this run measured.
func (j *Judgment) diffBase(in Input) {
	switch {
	case in.Base.Err != nil:
		j.fail("diff base", "", "%v", in.Base.Err)
	case in.Base.Ledger == nil:
		j.firstLedger(in)
	default:
		j.regressions(in)
	}
}

func (j *Judgment) regressions(in Input) {
	for _, c := range baseline.Raised(in.Base.Ledger.File(), in.Ledger.File()) {
		if !slices.ContainsFunc(in.Ledger.Regressions, func(r Regression) bool { return recorded(c, r) }) {
			j.fail("regressions", c.Key, "%s", unrecorded(c))
		}
	}
}

// unrecorded says what a change that needs a record did: a judged_laws that fell,
// an entry that was added, or one that was raised.
func unrecorded(c baseline.Change) string {
	const need = "with no regressions record (baseline --accept-regression)"
	switch {
	case c.Kind == "judged_laws" && c.From != nil:
		return fmt.Sprintf("judged_laws fell from %g to %g %s", *c.From, c.To, need)
	case c.From == nil:
		return fmt.Sprintf("%s added at %g %s", c.Kind, c.To, need)
	}
	return fmt.Sprintf("%s raised from %g to %g %s", c.Kind, *c.From, c.To, need)
}

// firstLedger holds a ledger with no base copy to what this run measured: every
// entry equals the measure, no entry exists for anything within the line or
// anything not measured, and judged_laws equals the run's (§6.7, ruling C3). A
// hand-written ledger can carry no extra allowance.
func (j *Judgment) firstLedger(in Input) {
	j.firstFunctions(in)
	j.firstPackages(in)
	j.firstCounts("findings", in.M.Findings, in.Ledger.Findings)
	j.firstCounts("laws", in.M.Laws, in.Ledger.Laws)
	j.firstCounts("suppressions.nolint", in.M.Nolint, in.Ledger.Nolint)
	j.firstCounts("suppressions.ack", in.M.Ack, in.Ledger.Ack)
	if in.Ledger.Exemptions != in.M.Exemptions {
		j.fail("first ledger", "exemptions", "records %d live exemptions, this run has %d", in.Ledger.Exemptions, in.M.Exemptions)
	}
	if in.Ledger.JudgedLaws != in.M.JudgedLaws {
		j.fail("first ledger", "judged_laws", "records %d, this run judged %d", in.Ledger.JudgedLaws, in.M.JudgedLaws)
	}
}
