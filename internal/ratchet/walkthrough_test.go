package ratchet_test

import (
	"testing"

	"github.com/Hedronite/go-tops/internal/ratchet"
)

// The first adoption of a legacy repo, GO_TOPS.md §6.7: a repo of 100
// production functions, 8 over the cap of 10, and no baseline. Each test is one
// numbered step and passes or fails by the rule the step names.

// Steps 2, 3 and 5: `baseline --init` writes the ledger as measured; the diff
// base has no baseline, so it is a first ledger, and the first gate passes with
// no source edit and no regressions record.
func TestWalkThroughStep5FirstLedgerPasses(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux)
	if len(l.Functions) != 8 {
		t.Fatalf("the ledger has %d function entries, want 8", len(l.Functions))
	}
	if len(l.Packages) != 1 {
		t.Fatalf("the ledger has %d package entries, want the one under the floor", len(l.Packages))
	}
	j := ratchet.Judge(input(m, l, firstLedger))
	wantOutcome(t, j, ratchet.Pass)
}

// Step 6: a rise of 0.01 or less is noise; 22.0 fails by the same rule.
func TestWalkThroughStep6NoiseAndNewDebt(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux)
	base := based(l)
	for _, tc := range []struct {
		score float64
		want  ratchet.Outcome
	}{
		{21.0, ratchet.Pass},
		{21.005, ratchet.Pass},
		{21.01, ratchet.Pass},
		{21.0101, ratchet.Fail},
		{22.0, ratchet.Fail},
	} {
		j := ratchet.Judge(input(setScore(m, key(0), tc.score), l, base))
		if j.Outcome != tc.want {
			t.Errorf("21.0 edited to %v: %s, want %s", tc.score, j.Outcome, tc.want)
		}
		if tc.want == ratchet.Fail {
			wantFailure(t, j, "functions", key(0))
		}
	}
}

// §6.7, CRAP: a function scoring 14.2 under a cap of 10 may not pass 14.21.
func TestFourteenPointTwoMayNotPassFourteenTwentyOne(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux)
	k := key(2) // 14.2
	if j := ratchet.Judge(input(setScore(m, k, 14.21), l, based(l))); j.Outcome != ratchet.Pass {
		t.Errorf("14.21: %s %v, want pass", j.Outcome, j.Failures)
	}
	if j := ratchet.Judge(input(setScore(m, k, 14.2101), l, based(l))); j.Outcome != ratchet.Fail {
		t.Errorf("14.2101: %s, want fail", j.Outcome)
	}
}

// A function at or under the cap may change up to the cap: 2 may rise to 10,
// and 10.0001 fails. Under ratchet no function within the cap is held
// stricter than under hard.
func TestFunctionWithinTheCapMayRiseToTheCap(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux)
	k := key(50)
	for _, tc := range []struct {
		score float64
		want  ratchet.Outcome
	}{{2, ratchet.Pass}, {10, ratchet.Pass}, {10.0000001, ratchet.Fail}} {
		if j := ratchet.Judge(input(setScore(m, k, tc.score), l, based(l))); j.Outcome != tc.want {
			t.Errorf("score %v: %s, want %s", tc.score, j.Outcome, tc.want)
		}
	}
}

// Step 7: a PR refactors one of the 8 down to 8. The gate passes, and the
// refresh removes its entry. From then on it is held to the cap of 10, like the
// other 92.
func TestWalkThroughStep7RefactoredFunctionIsHeldToTheCap(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux)
	refactored := setScore(m, key(0), 8)
	wantOutcome(t, ratchet.Judge(input(refactored, l, based(l))), ratchet.Pass)

	// After the refresh: the entry is gone.
	refreshed := clone(l)
	delete(refreshed.Functions, key(0))
	wantOutcome(t, ratchet.Judge(input(refactored, refreshed, based(l))), ratchet.Pass)
	// It is now like the other 92: held to the cap, not to its old score.
	back := setScore(m, key(0), 11)
	j := ratchet.Judge(input(back, refreshed, based(refreshed)))
	wantOutcome(t, j, ratchet.Fail)
	wantFailure(t, j, "functions", key(0))
}

// Step 8: a PR deletes tested code. In a package at or over its floor the PR
// passes while the package stays at or over it. In a package the ledger lists
// as under its floor, the percentage may fall but the uncovered-statement count
// does not rise, so the package does not fail.
func TestWalkThroughStep8DeletedTestedCode(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux)
	base := based(l)

	// m/a at 95%: deleting 10 tested statements leaves 180 of 190, 94.7%: over
	// the floor of 90, so it passes.
	if j := ratchet.Judge(input(setPackage(m, "example.com/m/a", 190, 180), l, base)); j.Outcome != ratchet.Pass {
		t.Errorf("a package that stays over its floor: %s %v", j.Outcome, j.Failures)
	}
	// ...and below the floor with no ledger entry it fails: 85 of 100.
	j := ratchet.Judge(input(setPackage(m, "example.com/m/a", 100, 85), l, base))
	wantOutcome(t, j, ratchet.Fail)
	wantFailure(t, j, "packages", "example.com/m/a")

	// m/low is listed under its floor at 80%, 20 uncovered. Deleting tested code:
	// 70 of 90 is 77.8%, still 20 uncovered. The percentage fell, the uncovered
	// count did not rise: it does not fail.
	if j := ratchet.Judge(input(setPackage(m, "example.com/m/low", 90, 70), l, base)); j.Outcome != ratchet.Pass {
		t.Errorf("percentage fell, uncovered did not rise: %s %v", j.Outcome, j.Failures)
	}
	// Deleting covered and uncovered code together: 60 of 75 is 80%, 15 uncovered.
	if j := ratchet.Judge(input(setPackage(m, "example.com/m/low", 75, 60), l, base)); j.Outcome != ratchet.Pass {
		t.Errorf("percentage unchanged, uncovered fell: %s %v", j.Outcome, j.Failures)
	}
}

// §6.7: a package under the floor fails only when its percentage falls and its
// uncovered-statement count rises. Both worse fails; a falling percentage with
// a falling uncovered count passes; a rising count with a rising percentage
// passes.
func TestPackagesTwoConditionRule(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux) // m/low: 80%, 20 uncovered
	cases := []struct {
		name                string
		statements, covered int
		want                ratchet.Outcome
	}{
		{"both worse: 78%, 22 uncovered", 100, 78, ratchet.Fail},
		{"percentage falls, uncovered falls: 60 of 77, 77.9%, 17", 77, 60, ratchet.Pass},
		{"percentage rises, uncovered rises: 180 of 200, 90% is the floor", 300, 262, ratchet.Pass}, // 87.3%, 38 uncovered
		{"unchanged", 100, 80, ratchet.Pass},
		{"percentage falls, uncovered equal", 90, 70, ratchet.Pass},
		{"percentage equal, uncovered rises", 110, 88, ratchet.Pass}, // 80%, 22 uncovered
	}
	for _, c := range cases {
		j := ratchet.Judge(input(setPackage(m, "example.com/m/low", c.statements, c.covered), l, based(l)))
		if j.Outcome != c.want {
			t.Errorf("%s: %s %v, want %s", c.name, j.Outcome, j.Failures, c.want)
		}
	}
}

// A package at or over the floor, or absent from the ledger, is held to the
// floor; a package that is in the ledger and now meets the floor passes.
func TestPackageThatNowMeetsTheFloorPasses(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux)
	if j := ratchet.Judge(input(setPackage(m, "example.com/m/low", 100, 95), l, based(l))); j.Outcome != ratchet.Pass {
		t.Errorf("%s %v", j.Outcome, j.Failures)
	}
}

// Counts are per file and never pooled: removing a finding in one file does not
// pay for adding one in another, and a new file starts at 0.
func TestCountsArePerFile(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux) // errcheck a.go 2, gosec b.go 1
	base := based(l)
	pooled := m
	pooled.Nolint = ratchet.Counts{"errcheck": {"a.go": 1, "c.go": 1}, "gosec": {"b.go": 1}}
	j := ratchet.Judge(input(pooled, l, base))
	wantOutcome(t, j, ratchet.Fail)
	wantFailure(t, j, "suppressions.nolint", "errcheck c.go")

	newFile := m
	newFile.Nolint = ratchet.Counts{"errcheck": {"a.go": 2}, "gosec": {"b.go": 1, "new.go": 1}}
	wantOutcome(t, ratchet.Judge(input(newFile, l, base)), ratchet.Fail)

	fewer := m
	fewer.Nolint = ratchet.Counts{"errcheck": {"a.go": 1}}
	wantOutcome(t, ratchet.Judge(input(fewer, l, base)), ratchet.Pass)
}

func TestFindingsLawsAndAcksAreCountsToo(t *testing.T) {
	m := legacy()
	m.Findings = ratchet.Counts{"revive": {"x.go": 3}}
	m.Laws = ratchet.Counts{"G-11": {"y.go": 1}}
	m.Ack = ratchet.Counts{"G-12": {"z.go": 2}}
	l := initLedger(m, linux)
	wantOutcome(t, ratchet.Judge(input(m, l, based(l))), ratchet.Pass)

	for _, bump := range []struct {
		kind string
		edit func(*ratchet.Measure)
	}{
		{"findings", func(m *ratchet.Measure) { m.Findings = ratchet.Counts{"revive": {"x.go": 4}} }},
		{"laws", func(m *ratchet.Measure) { m.Laws = ratchet.Counts{"G-11": {"y.go": 2}} }},
		{"suppressions.ack", func(m *ratchet.Measure) { m.Ack = ratchet.Counts{"G-12": {"z.go": 3}} }},
	} {
		worse := m
		bump.edit(&worse)
		j := ratchet.Judge(input(worse, l, based(l)))
		wantOutcome(t, j, ratchet.Fail)
		wantFailure(t, j, bump.kind, "")
	}
}

// The exemption count may only fall, and absent means 0.
func TestExemptionCount(t *testing.T) {
	m := legacy()
	m.Exemptions = 2
	l := initLedger(m, linux)
	wantOutcome(t, ratchet.Judge(input(m, l, based(l))), ratchet.Pass)
	less := m
	less.Exemptions = 1
	wantOutcome(t, ratchet.Judge(input(less, l, based(l))), ratchet.Pass)
	more := m
	more.Exemptions = 3
	j := ratchet.Judge(input(more, l, based(l)))
	wantOutcome(t, j, ratchet.Fail)
	wantFailure(t, j, "exemptions", "")

	none := legacy() // ledger holds 0: a first new exemption is a regression
	ln := initLedger(none, linux)
	one := none
	one.Exemptions = 1
	wantOutcome(t, ratchet.Judge(input(one, ln, based(ln))), ratchet.Fail)
}

// judged_laws is a floor, not a debt: it may not fall.
func TestJudgedLawsMayNotFall(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux)
	up := m
	up.JudgedLaws = 7
	wantOutcome(t, ratchet.Judge(input(up, l, based(l))), ratchet.Pass)
	down := m
	down.JudgedLaws = 4
	j := ratchet.Judge(input(down, l, based(l)))
	wantOutcome(t, j, ratchet.Fail)
	wantFailure(t, j, "judged_laws", "")
}

// An exempt function has no entry and is not judged; one that is not exempt and
// is over the cap with no entry meets the cap, which it does not.
func TestExemptFunctionsAreNotJudged(t *testing.T) {
	m := legacy()
	m.Exempt = map[string]bool{key(0): true}
	l := initLedger(m, linux)
	if _, has := l.Functions[key(0)]; has {
		t.Fatal("an exempt function has a ledger entry")
	}
	wantOutcome(t, ratchet.Judge(input(m, l, based(l))), ratchet.Pass)
	grown := setScore(m, key(0), 40)
	wantOutcome(t, ratchet.Judge(input(grown, l, based(l))), ratchet.Pass)
}

// A package exactly at the floor meets it; one just under it, with no entry,
// does not.
func TestPackageAtTheFloorMeetsIt(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux)
	if j := ratchet.Judge(input(setPackage(m, "example.com/m/a", 100, 90), l, based(l))); j.Outcome != ratchet.Pass {
		t.Errorf("exactly at the floor: %s %v", j.Outcome, j.Failures)
	}
	if j := ratchet.Judge(input(setPackage(m, "example.com/m/a", 1000, 899), l, based(l))); j.Outcome != ratchet.Fail {
		t.Errorf("89.9%% with no entry: %s, want fail", j.Outcome)
	}
}

// The uncovered count must rise, not merely stay: with the percentage falling,
// an equal count does not fail and one more does.
func TestPackageWithTheSameUncoveredCountDoesNotFail(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux)
	// m/low: 80%, 20 uncovered. 90 statements, 70 covered: 77.8%, 20 uncovered.
	if j := ratchet.Judge(input(setPackage(m, "example.com/m/low", 90, 70), l, based(l))); j.Outcome != ratchet.Pass {
		t.Errorf("%s %v", j.Outcome, j.Failures)
	}
	// 100 statements, 79 covered: 79%, 21 uncovered.
	if j := ratchet.Judge(input(setPackage(m, "example.com/m/low", 100, 79), l, based(l))); j.Outcome != ratchet.Fail {
		t.Errorf("79%% and 21 uncovered: %s, want fail", j.Outcome)
	}
}
