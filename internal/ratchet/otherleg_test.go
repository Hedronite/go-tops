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
	in := ratchet.Input{Ledger: l, Base: based(l), Run: darwin, M: worse, LedgerCompiled: compiledAll(m)}
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
	j := ratchet.Judge(ratchet.Input{Ledger: l, Base: based(l), Run: darwin, M: m, LedgerCompiled: compiled})
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
	j := ratchet.Judge(ratchet.Input{Ledger: l, Base: based(l), Run: darwin, M: m, LedgerCompiled: compiledAll(m)})
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
	j := ratchet.Judge(ratchet.Input{Ledger: l, Base: based(l), Run: darwin, M: m, LedgerCompiled: compiled})
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
	other := ratchet.Judge(ratchet.Input{Ledger: l, Base: based(l), Run: darwin, M: m, LedgerCompiled: compiledAll(m)}).Report()
	if other.Outcome != "skip" || other.LedgerBuild.GOOS != "linux" || other.RunBuild.GOOS != "darwin" || other.RunBuild.GOARCH != "arm64" || !other.RunBuild.CgoEnabled {
		t.Errorf("other leg report %+v: skip, naming both builds", other)
	}
	failing := ratchet.Judge(input(setScore(m, key(0), 99), l, based(l))).Report()
	if failing.Outcome != "fail" {
		t.Errorf("failing report %+v", failing)
	}
}
