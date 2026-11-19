package ratchet_test

import (
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/crap"
	"github.com/Hedronite/go-tops/internal/ratchet"
)

// The cheat suite: every move that would let debt in without paying for it
// fails (GO_TOPS.md §6.7, SPEC-m2 G4).

// A hand edit that raises an entry, with no regressions record, fails; with the
// record that --accept-regression writes it passes.
func TestCheatHandEditRaisingAnEntry(t *testing.T) {
	m := legacy()
	old := initLedger(m, linux)
	raised := clone(old)
	raised.Functions[key(0)] = 30 // 21.0 -> 30, the code unchanged

	j := ratchet.Judge(input(m, raised, based(old)))
	wantOutcome(t, j, ratchet.Fail)
	wantFailure(t, j, "regressions", key(0))

	// The same raise, accepted: a record naming the kind, the key, the old and
	// the new value.
	raised.Regressions = []ratchet.Regression{{
		Kind: "functions", Key: key(0), From: ptr(21.0), To: 30, Reason: "legacy parser rewrite", Owner: "platform",
	}}
	wantOutcome(t, ratchet.Judge(input(m, raised, based(old))), ratchet.Pass)

	// A record for another value, another key or another old value does not
	// cover the raise.
	for name, r := range map[string]ratchet.Regression{
		"another value": {Kind: "functions", Key: key(0), From: ptr(21.0), To: 31},
		"another key":   {Kind: "functions", Key: key(1), From: ptr(21.0), To: 30},
		"another kind":  {Kind: "packages", Key: key(0), From: ptr(21.0), To: 30},
	} {
		raised.Regressions = []ratchet.Regression{r}
		if j := ratchet.Judge(input(m, raised, based(old))); j.Outcome != ratchet.Fail {
			t.Errorf("a record with %s covered the raise", name)
		}
	}
}

// An added entry with no record fails: it is new debt.
func TestCheatAddedEntry(t *testing.T) {
	m := legacy()
	old := initLedger(m, linux)
	added := clone(old)
	added.Functions[key(50)] = 10.5 // a function that was never in the ledger
	j := ratchet.Judge(input(m, added, based(old)))
	wantOutcome(t, j, ratchet.Fail)
	wantFailure(t, j, "regressions", key(50))
	added.Regressions = []ratchet.Regression{{Kind: "functions", Key: key(50), From: nil, To: 10.5}}
	wantOutcome(t, ratchet.Judge(input(m, added, based(old))), ratchet.Pass)
}

// A raised package entry, a raised count, a raised exemption count and an added
// count entry are the same moves and need the same record.
func TestCheatEveryKindNeedsItsRecord(t *testing.T) {
	m := legacy()
	old := initLedger(m, linux)
	moves := map[string]func(*ratchet.Ledger){
		"packages": func(l *ratchet.Ledger) {
			l.Packages["example.com/m/low"] = ratchet.Package{StatementsPct: 80, Uncovered: 40}
		},
		"packages added": func(l *ratchet.Ledger) {
			l.Packages["example.com/m/a"] = ratchet.Package{StatementsPct: 80, Uncovered: 40}
		},
		"nolint raised": func(l *ratchet.Ledger) { l.Nolint = ratchet.Counts{"errcheck": {"a.go": 9}, "gosec": {"b.go": 1}} },
		"nolint added": func(l *ratchet.Ledger) {
			l.Nolint = ratchet.Counts{"errcheck": {"a.go": 2, "z.go": 1}, "gosec": {"b.go": 1}}
		},
		"exemptions":          func(l *ratchet.Ledger) { l.Exemptions = 4 },
		"judged_laws lowered": func(l *ratchet.Ledger) { l.JudgedLaws = 1 },
	}
	for name, move := range moves {
		cheat := clone(old)
		move(cheat)
		j := ratchet.Judge(input(m, cheat, based(old)))
		if j.Outcome != ratchet.Fail {
			t.Errorf("%s: %s, want fail", name, j.Outcome)
			continue
		}
		wantFailure(t, j, "regressions", "")
	}
}

// Lowering an entry, or removing one, needs no record: the refresh only lowers.
func TestLoweringNeedsNoRecord(t *testing.T) {
	m := legacy()
	old := initLedger(m, linux)
	lower := clone(old)
	lower.Functions[key(0)] = 15
	delete(lower.Functions, key(1)) // refactored under the cap below
	lower.JudgedLaws = old.JudgedLaws + 2
	run := setScore(setScore(m, key(0), 15), key(1), 8)
	run.JudgedLaws = lower.JudgedLaws
	wantOutcome(t, ratchet.Judge(input(run, lower, based(old))), ratchet.Pass)
}

// Deleting the baseline is not a reset: under ratchet a missing baseline fails.
func TestCheatDeletedBaseline(t *testing.T) {
	j := ratchet.Judge(ratchet.Input{Ledger: nil, Base: based(initLedger(legacy(), linux)), Run: linux, M: legacy()})
	wantOutcome(t, j, ratchet.Fail)
	wantFailure(t, j, "baseline", "")
}

// A padded first ledger: one function padded by 0.02 fails, because every entry
// must equal what the run measures within 0.01.
func TestCheatPaddedFirstLedger(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux)
	for name, pad := range map[string]float64{"0.02": 0.02, "5": 5} {
		padded := clone(l)
		padded.Functions[key(3)] += pad
		j := ratchet.Judge(input(m, padded, firstLedger))
		if j.Outcome != ratchet.Fail {
			t.Errorf("padded by %s: %s, want fail", name, j.Outcome)
			continue
		}
		wantFailure(t, j, "first ledger", key(3))
	}
	// Within 0.01 is the measure's own noise: it passes.
	near := clone(l)
	near.Functions[key(3)] += 0.01
	wantOutcome(t, ratchet.Judge(input(m, near, firstLedger)), ratchet.Pass)
	// A ledger entry below the measure is not allowance either: it fails the
	// first-ledger equality, and the run's score is over it.
	under := clone(l)
	under.Functions[key(3)] -= 0.5
	wantOutcome(t, ratchet.Judge(input(m, under, firstLedger)), ratchet.Fail)
}

// A first ledger with an entry for a function the run measures within the line
// fails: no entry exists for anything within the line.
func TestCheatFirstLedgerEntryForAFunctionUnderTheCap(t *testing.T) {
	m := legacy()
	l := clone(initLedger(m, linux))
	l.Functions[key(50)] = 99 // measured at 2..9, under the cap
	j := ratchet.Judge(input(m, l, firstLedger))
	wantOutcome(t, j, ratchet.Fail)
	wantFailure(t, j, "first ledger", key(50))
}

// A first ledger that carries an entry for a function nothing measures fails:
// a hand-written ledger carries no extra allowance (ruling C3).
func TestCheatFirstLedgerEntryForAFunctionNotMeasured(t *testing.T) {
	m := legacy()
	l := clone(initLedger(m, linux))
	l.Functions["example.com/m/p.OnlyOnWindows"] = 40
	j := ratchet.Judge(input(m, l, firstLedger))
	wantOutcome(t, j, ratchet.Fail)
	wantFailure(t, j, "first ledger", "example.com/m/p.OnlyOnWindows")

	// Even one the run lists as not measured.
	m.NotMeasured = []crap.NotMeasured{{Key: "example.com/m/p.OnlyOnWindows", Comp: 40}}
	if j := ratchet.Judge(input(m, l, firstLedger)); j.Outcome != ratchet.Fail {
		t.Errorf("an entry for a not-measured function: %s, want fail", j.Outcome)
	}
}

func TestCheatFirstLedgerPackageEntries(t *testing.T) {
	m := legacy()
	good := initLedger(m, linux)
	for name, move := range map[string]func(*ratchet.Ledger){
		"percentage padded": func(l *ratchet.Ledger) {
			l.Packages["example.com/m/low"] = ratchet.Package{StatementsPct: 70, Uncovered: 20}
		},
		"uncovered padded": func(l *ratchet.Ledger) {
			l.Packages["example.com/m/low"] = ratchet.Package{StatementsPct: 80, Uncovered: 25}
		},
		"entry for a package over the floor": func(l *ratchet.Ledger) {
			l.Packages["example.com/m/a"] = ratchet.Package{StatementsPct: 95, Uncovered: 10}
		},
		"entry for a package not measured": func(l *ratchet.Ledger) {
			l.Packages["example.com/m/nowhere"] = ratchet.Package{StatementsPct: 10, Uncovered: 90}
		},
	} {
		l := clone(good)
		move(l)
		j := ratchet.Judge(input(m, l, firstLedger))
		if j.Outcome != ratchet.Fail {
			t.Errorf("%s: %s, want fail", name, j.Outcome)
			continue
		}
		wantFailure(t, j, "first ledger", "")
	}
}

func TestCheatFirstLedgerCountsAndJudgedLaws(t *testing.T) {
	m := legacy()
	good := initLedger(m, linux)
	for name, move := range map[string]func(*ratchet.Ledger){
		"nolint padded": func(l *ratchet.Ledger) { l.Nolint = ratchet.Counts{"errcheck": {"a.go": 5}, "gosec": {"b.go": 1}} },
		"nolint for nothing": func(l *ratchet.Ledger) {
			l.Nolint = ratchet.Counts{"errcheck": {"a.go": 2, "ghost.go": 1}, "gosec": {"b.go": 1}}
		},
		"exemptions padded":  func(l *ratchet.Ledger) { l.Exemptions = 3 },
		"judged_laws lower":  func(l *ratchet.Ledger) { l.JudgedLaws = 2 },
		"judged_laws higher": func(l *ratchet.Ledger) { l.JudgedLaws = 9 },
	} {
		l := clone(good)
		move(l)
		j := ratchet.Judge(input(m, l, firstLedger))
		if j.Outcome != ratchet.Fail {
			t.Errorf("%s: %s, want fail", name, j.Outcome)
		}
	}
}

// A first ledger can carry no regressions records to excuse itself: its
// entries equal the measure or it fails.
func TestFirstLedgerRecordsDoNotExcuseAPadding(t *testing.T) {
	m := legacy()
	l := clone(initLedger(m, linux))
	l.Functions[key(3)] += 5
	l.Regressions = []ratchet.Regression{{Kind: "functions", Key: key(3), From: nil, To: l.Functions[key(3)]}}
	wantOutcome(t, ratchet.Judge(input(m, l, firstLedger)), ratchet.Fail)
}

// Not-measured functions are judged on every leg: one whose comp is above the cap
// fails like any function over it, unless an exemption covers it; at or under the
// cap it is counted and not judged.
func TestNotMeasuredAboveTheCapFails(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux)
	m.NotMeasured = []crap.NotMeasured{
		{Key: "example.com/m/p.Win", Comp: 12},
		{Key: "example.com/m/p.Small", Comp: 10},
	}
	j := ratchet.Judge(input(m, l, based(l)))
	wantOutcome(t, j, ratchet.Fail)
	wantFailure(t, j, "not measured", "example.com/m/p.Win")
	if len(j.Failures) != 1 {
		t.Errorf("failures %v: comp 10 is at the cap and is not judged", j.Failures)
	}
	m.Exempt = map[string]bool{"example.com/m/p.Win": true}
	wantOutcome(t, ratchet.Judge(input(m, l, based(l))), ratchet.Pass)
}

// A first-ledger entry that equals the score of a function within the cap is
// still an entry for something within the line: it fails.
func TestCheatFirstLedgerEntryEqualToAScoreWithinTheCap(t *testing.T) {
	m := legacy()
	l := clone(initLedger(m, linux))
	l.Functions[key(50)] = 5 // the function scores exactly 5, under the cap of 10
	m = setScore(m, key(50), 5)
	j := ratchet.Judge(input(m, l, firstLedger))
	wantOutcome(t, j, ratchet.Fail)
	wantFailure(t, j, "first ledger", key(50))
}

// An exemption that matches nothing fails the run whatever the ledger holds: it
// leaves the file with the code it covered (§6.8). The legacy debt it was
// written for is not forgiven by a ledger entry that never existed.
func TestExemptionThatMatchesNothingFailsTheRatchet(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux)
	m.Problems = []ratchet.Failure{{Kind: ratchet.ProblemKind, Key: "p/gone.go", Why: "matches no production file"}}
	j := ratchet.Judge(input(m, l, based(l)))
	wantOutcome(t, j, ratchet.Fail)
	wantFailure(t, j, ratchet.ProblemKind, "p/gone.go")
	// On another leg the ratchet skips and the entry still fails the run.
	jo := ratchet.Judge(ratchet.Input{Ledger: l, Base: based(l), Run: darwin, M: m, LedgerCompiled: compiledAll(m)})
	wantOutcome(t, jo, ratchet.Skip)
	if !jo.Failed() || len(jo.HeldToCap) != 1 || jo.HeldToCap[0].Kind != ratchet.ProblemKind {
		t.Errorf("held %v: an unmatched exemption fails every leg", jo.HeldToCap)
	}
}

// A record outlives the merge that brought it in, and a later accepted raise of
// the same entry keeps the first record's old value. The record matches on kind,
// key and the new value, not on an old value the diff base no longer holds.
func TestRecordMatchesOnKindKeyAndNewValueOnly(t *testing.T) {
	m := legacy()
	base := initLedger(m, linux)
	base.Functions[key(0)] = 12 // the entry as merged: raised from 10.5 to 12 with a record
	base.Regressions = []ratchet.Regression{{Kind: "functions", Key: key(0), From: ptr(10.5), To: 12, Reason: "legacy parser rewrite", Owner: "platform"}}
	cur := clone(base)
	cur.Functions[key(0)] = 13
	cur.Regressions = []ratchet.Regression{{Kind: "functions", Key: key(0), From: ptr(10.5), To: 13, Reason: "one more parser change", Owner: "platform"}}
	m = setScore(m, key(0), 13)
	wantOutcome(t, ratchet.Judge(input(m, cur, based(base))), ratchet.Pass)

	// The same raise with the base's record left as it was (To 12) is not accepted.
	stale := clone(cur)
	stale.Regressions = base.Regressions
	j := ratchet.Judge(input(m, stale, based(base)))
	wantOutcome(t, j, ratchet.Fail)
	wantFailure(t, j, "regressions", key(0))

	// An entry added and raised later: the first record had no old value.
	added := clone(base)
	added.Regressions = []ratchet.Regression{{Kind: "functions", Key: key(0), From: nil, To: 13, Reason: "legacy parser rewrite", Owner: "platform"}}
	added.Functions[key(0)] = 13
	wantOutcome(t, ratchet.Judge(input(m, added, based(base))), ratchet.Pass)
}

// A change of the ledger's build against the base copy is said, never judged:
// the verdict is the same with the line and without it (the guard is the CI
// assertion that the ledger leg never skips).
func TestBuildChangedAgainstTheBaseIsATextLineOnly(t *testing.T) {
	m := legacy()
	cur := initLedger(m, linux)
	old := clone(cur)
	old.Build = darwin
	j := ratchet.Judge(input(m, cur, based(old)))
	wantOutcome(t, j, ratchet.Pass)
	notes := ratchet.Notes(j)
	if len(notes) != 1 || !strings.Contains(notes[0], "linux/amd64") || !strings.Contains(notes[0], "darwin/arm64") || !strings.HasPrefix(notes[0], "ratchet: the ledger's build") {
		t.Errorf("notes %q, want one line naming both builds", notes)
	}
	same := ratchet.Judge(input(m, cur, based(cur)))
	if len(ratchet.Notes(same)) != 0 {
		t.Errorf("notes %q for an unchanged build", ratchet.Notes(same))
	}
	if first := ratchet.Judge(input(m, cur, firstLedger)); len(ratchet.Notes(first)) != 0 {
		t.Errorf("notes %q for a first ledger, which has no copy to differ from", ratchet.Notes(first))
	}
	// Another leg says it too, and still skips.
	other := ratchet.Judge(ratchet.Input{Ledger: cur, Base: based(old), Run: darwin, M: m, LedgerCompiled: compiledAll(m)})
	wantOutcome(t, other, ratchet.Skip)
	if len(ratchet.Notes(other)) != 1 {
		t.Errorf("another leg: notes %q", ratchet.Notes(other))
	}
}
