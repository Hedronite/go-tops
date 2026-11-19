package ratchet_test

import (
	"testing"

	"github.com/Hedronite/go-tops/internal/crap"
	"github.com/Hedronite/go-tops/internal/gobuild"
	"github.com/Hedronite/go-tops/internal/ratchet"
)

// Only the ledger leg judges the ratchet (§6.7, ruling C3). Any other leg skips
// it, naming the ledger build, and is never reported as pass.

func TestOtherLegSkipsTheRatchet(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux)
	// Worse than the ledger in every kind: on the ledger leg it would fail.
	worse := setScore(m, key(0), 99)
	worse = setPackage(worse, "example.com/m/low", 100, 10)
	in := otherLegInput(l, based(l), worse, compiledAll(m))
	j := ratchet.Judge(in)
	if j.Outcome != ratchet.Skip {
		t.Fatalf("outcome %s, want skip", j.Outcome)
	}
	if !j.LedgerBuild.Equal(linux) || !j.RunBuild.Equal(darwin) {
		t.Errorf("builds %+v and %+v, want the ledger's and the run's", j.LedgerBuild, j.RunBuild)
	}
	if len(j.Failures) != 0 || len(j.HeldToCap) != 0 {
		t.Errorf("failures %v held %v: the ledger leg judges what the ledger covers", j.Failures, j.HeldToCap)
	}
}

func compiledAll(m ratchet.Measure) map[string]bool {
	set := map[string]bool{}
	for _, f := range m.Functions {
		set[f.Key] = true
	}
	return set
}

// A function the ledger leg does not compile is judged against the cap on a leg
// that compiles it, as a new function would be: no entry applies.
func TestOtherLegHoldsFunctionsTheLedgerLegDoesNotCompileToTheCap(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux)
	darwinOnly := crap.Function{Key: "example.com/m/p.Mac", File: "p/mac_darwin.go", Comp: 12, Cov: 1, CRAP: 12}
	fine := crap.Function{Key: "example.com/m/p.MacFine", File: "p/mac_darwin.go", Comp: 3, Cov: 1, CRAP: 3}
	m.Functions = append(m.Functions, darwinOnly, fine)
	compiled := compiledAll(legacy()) // the ledger leg does not have the two
	j := ratchet.Judge(otherLegInput(l, based(l), m, compiled))
	wantOutcome(t, j, ratchet.Skip)
	if len(j.HeldToCap) != 1 || j.HeldToCap[0].Key != "example.com/m/p.Mac" {
		t.Fatalf("held to the cap %v, want only the darwin-only function over it", j.HeldToCap)
	}
	if !j.Failed() {
		t.Error("a function over the cap that only this leg compiles must fail the run")
	}
}

// A function that both legs compile is the ledger leg's to judge: over the cap
// with an entry, it does not fail here.
func TestOtherLegLeavesSharedFunctionsToTheLedgerLeg(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux)
	j := ratchet.Judge(otherLegInput(l, based(l), m, compiledAll(m)))
	wantOutcome(t, j, ratchet.Skip)
	if j.Failed() {
		t.Errorf("held to the cap %v: the eight legacy functions have entries on the ledger leg", j.HeldToCap)
	}
}

// Not-measured functions on another leg: judged by their comp where the ledger
// leg does not compile them either; left to the ledger leg where it does.
func TestOtherLegNotMeasuredFunctions(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux)
	m.NotMeasured = []crap.NotMeasured{
		{Key: "example.com/m/p.OnWindows", Comp: 15},
		{Key: "example.com/m/p.OnLinux", Comp: 15},
	}
	compiled := compiledAll(m)
	compiled["example.com/m/p.OnLinux"] = true // the ledger leg compiles and judges it
	j := ratchet.Judge(otherLegInput(l, based(l), m, compiled))
	if len(j.HeldToCap) != 1 || j.HeldToCap[0].Key != "example.com/m/p.OnWindows" {
		t.Errorf("held to the cap %v, want only the function no ledger-leg build compiles", j.HeldToCap)
	}
}

// Without the set of files the ledger leg compiles, another leg cannot say
// which functions to hold to the cap: it fails, never guesses.
func TestOtherLegNeedsTheLedgerLegsCompiledSet(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux)
	j := ratchet.Judge(ratchet.Input{Ledger: l, Base: based(l), Run: darwin, M: m})
	wantOutcome(t, j, ratchet.Fail)
	wantFailure(t, j, "compiled set", "")
}

// The run on the ledger leg is judged however its tags were written.
func TestLedgerLegIsFoundByBuildNotByTagOrder(t *testing.T) {
	m := legacy()
	build := gobuild.Build{GOOS: "linux", GOARCH: "amd64", Tags: []string{"b", "a"}}
	l := initLedger(m, build)
	run := gobuild.Build{GOOS: "linux", GOARCH: "amd64", Tags: []string{"a", "b"}}
	j := ratchet.Judge(ratchet.Input{Ledger: l, Base: firstLedger, Run: run, M: m})
	wantOutcome(t, j, ratchet.Pass)
}

// The report object: outcome, ledger_build and run_build, on every leg.
func TestReportObject(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux)
	ledgerLeg := ratchet.Judge(input(m, l, firstLedger)).Report()
	if ledgerLeg.Outcome != "pass" || ledgerLeg.LedgerBuild.GOOS != "linux" || ledgerLeg.RunBuild.GOOS != "linux" {
		t.Errorf("ledger leg report %+v", ledgerLeg)
	}
	other := ratchet.Judge(otherLegInput(l, based(l), m, compiledAll(m))).Report()
	if other.Outcome != "skip" || other.LedgerBuild.GOOS != "linux" || other.RunBuild.GOOS != "darwin" || other.RunBuild.GOARCH != "arm64" || !other.RunBuild.CgoEnabled {
		t.Errorf("other leg report %+v: skip, naming both builds", other)
	}
	failing := ratchet.Judge(input(setScore(m, key(0), 99), l, based(l))).Report()
	if failing.Outcome != "fail" {
		t.Errorf("failing report %+v", failing)
	}
}

// Ruling C3d: what sits in a file or a package the ledger build does not compile is
// judged at hard on a leg that compiles it. The ledger leg never sees it, so no entry
// can cover it. One test per kind, and what the ledger build compiles stays reported.

func darwinOnly(m ratchet.Measure, edit func(*ratchet.Measure)) ratchet.Measure {
	edit(&m)
	return m
}

func judgedOnOtherLeg(t *testing.T, m ratchet.Measure) ratchet.Judgment {
	t.Helper()
	l := initLedger(legacy(), linux)
	j := ratchet.Judge(otherLegInput(l, based(l), m, compiledAll(m)))
	wantOutcome(t, j, ratchet.Skip)
	return j
}

func wantHeld(t *testing.T, j ratchet.Judgment, kind, key string) {
	t.Helper()
	for _, f := range j.HeldToCap {
		if f.Kind == kind && f.Key == key {
			return
		}
	}
	t.Errorf("no held %s failure for %q among %v", kind, key, j.HeldToCap)
}

func TestOtherLegJudgesFindingsOutsideTheLedgerBuildAtHard(t *testing.T) {
	m := darwinOnly(legacy(), func(m *ratchet.Measure) {
		m.Findings = ratchet.Counts{"errcheck": {"p/d_darwin.go": 1}, "revive": {"x.go": 3}}
	})
	j := judgedOnOtherLeg(t, m)
	wantHeld(t, j, "findings", "errcheck p/d_darwin.go")
	if !j.Failed() || len(j.HeldToCap) != 1 {
		t.Errorf("held %v: the finding in x.go is in a file the ledger build compiles, so it is reported only", j.HeldToCap)
	}
}

func TestOtherLegJudgesNolintOutsideTheLedgerBuildAtHard(t *testing.T) {
	m := darwinOnly(legacy(), func(m *ratchet.Measure) {
		m.Nolint = ratchet.Counts{"errcheck": {"a.go": 2, "p/d_darwin.go": 1}, "gosec": {"b.go": 1}}
	})
	j := judgedOnOtherLeg(t, m)
	wantHeld(t, j, "suppressions.nolint", "errcheck p/d_darwin.go")
	if len(j.HeldToCap) != 1 {
		t.Errorf("held %v: the ledger's own counts in a.go and b.go are not judged here", j.HeldToCap)
	}
}

func TestOtherLegJudgesAcksAndLawsOutsideTheLedgerBuildAtHard(t *testing.T) {
	m := darwinOnly(legacy(), func(m *ratchet.Measure) {
		m.Ack = ratchet.Counts{"G-12": {"p/d_darwin.go": 2}}
		m.Laws = ratchet.Counts{"G-11": {"p/d_darwin.go": 1}}
	})
	j := judgedOnOtherLeg(t, m)
	wantHeld(t, j, "suppressions.ack", "G-12 p/d_darwin.go")
	wantHeld(t, j, "laws", "G-11 p/d_darwin.go")
}

// A count that the ledger records for a file the ledger build does not compile holds
// up to its entry, absent meaning 0: suppressions are found in every file whatever
// the build, so the ledger can record one there.
func TestOtherLegHoldsACountOutsideTheLedgerBuildToItsEntry(t *testing.T) {
	base := legacy()
	base.Nolint = ratchet.Counts{"errcheck": {"p/d_darwin.go": 2}}
	l := initLedger(base, linux)
	for n, want := range map[int]bool{1: false, 2: false, 3: true} {
		m := legacy()
		m.Nolint = ratchet.Counts{"errcheck": {"p/d_darwin.go": n}}
		j := ratchet.Judge(otherLegInput(l, based(l), m, compiledAll(m)))
		if got := j.Failed(); got != want {
			t.Errorf("%d //nolint against an entry of 2: failed %v, want %v (%v)", n, got, want, j.HeldToCap)
		}
	}
}

func TestOtherLegHoldsPackagesOutsideTheLedgerBuildToTheFloor(t *testing.T) {
	m := darwinOnly(legacy(), func(m *ratchet.Measure) {
		m.Packages = map[string]ratchet.Package{
			"example.com/m/low":    {StatementsPct: 80, Uncovered: 20}, // the ledger's, listed
			"example.com/m/darwin": {StatementsPct: 20, Uncovered: 80}, // the ledger build does not compile it
		}
	})
	j := judgedOnOtherLeg(t, m)
	wantHeld(t, j, "packages", "example.com/m/darwin")
	if len(j.HeldToCap) != 1 {
		t.Errorf("held %v: the package under its floor that the ledger lists is the ledger leg's", j.HeldToCap)
	}
}

// What sits in the ledger build's files and packages is reported and never fails the
// leg (ruling C3c): worse than the ledger in every kind, nothing is held.
func TestOtherLegDoesNotJudgeWhatTheLedgerBuildCompiles(t *testing.T) {
	m := darwinOnly(legacy(), func(m *ratchet.Measure) {
		m.Findings = ratchet.Counts{"revive": {"x.go": 9}}
		m.Nolint = ratchet.Counts{"errcheck": {"a.go": 9}}
		m.Laws = ratchet.Counts{"G-11": {"y.go": 9}}
		m.Ack = ratchet.Counts{"G-12": {"z.go": 9}}
		m.Packages = map[string]ratchet.Package{"example.com/m/low": {StatementsPct: 10, Uncovered: 900}}
	})
	if j := judgedOnOtherLeg(t, m); j.Failed() {
		t.Errorf("held %v, want none", j.HeldToCap)
	}
}
