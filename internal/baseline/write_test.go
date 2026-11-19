package baseline_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/baseline"
)

// GO_TOPS.md §6.7, "Writing the baseline":
//   - --init writes what was measured: the ledger is the measure.
//   - Every later write only lowers. It lowers a value, removes an entry that is
//     no longer over the line, and raises judged_laws. It never adds an entry and
//     never raises one.
//   - --accept-regression raises an entry, adds one, or lowers judged_laws, and
//     records the change under regressions. A record is removed by the write that
//     pays the entry back to its earlier value.

const goodReason = "the parser grows one arm"

// legacy is the walk-through repo of §6.7 after step 2: eight functions over the
// cap, a package under its floor, three //nolint in two files.
func legacy() baseline.Measure {
	return baseline.Measure{
		Functions: map[string]float64{
			"m/a.F1": 21.0, "m/a.F2": 14.2, "m/a.F3": 12, "m/b.F4": 30.5,
			"m/b.T.F5": 11, "m/c.F6": 18, "m/c.F7": 10.5, "m/d.F8": 40,
		},
		Packages:   map[string]baseline.Package{"m/d": {StatementsPct: 62.5, Uncovered: 30}},
		Findings:   baseline.Counts{"revive": {"a.go": 2}},
		Nolint:     baseline.Counts{"gosec": {"a.go": 2, "b.go": 1}},
		Exemptions: 1,
		JudgedLaws: 0,
		Build:      testBuild,
	}
}

func ledgerOf(m baseline.Measure) baseline.File { return baseline.Init(m) }

func TestInitWritesWhatWasMeasured(t *testing.T) {
	m := legacy()
	f := baseline.Init(m)
	want := baseline.File{Build: testBuild, Ledger: baseline.Ledger{
		Functions: m.Functions, Packages: m.Packages, Findings: m.Findings, Nolint: m.Nolint, Exemptions: 1,
	}}
	if !reflect.DeepEqual(f, want) {
		t.Errorf("Init:\n%+v\nwant\n%+v", f, want)
	}
	if len(f.Regressions) != 0 {
		t.Errorf("a first ledger carries no regressions: %v", f.Regressions)
	}
}

func TestInitOmitsWhatIsNotOverTheLine(t *testing.T) {
	// The schema allows no zero exemption count and no zero finding count, and
	// the ledger lists only what is over the line.
	f := baseline.Init(baseline.Measure{Exemptions: 0, Findings: baseline.Counts{"revive": {}}, JudgedLaws: 3, Build: testBuild})
	if err := validate(t, baseline.Marshal(f)); err != nil {
		t.Fatal(err)
	}
	if f.Ledger.Exemptions != 0 || len(f.Ledger.Findings) != 0 || f.JudgedLaws != 3 {
		t.Errorf("Init kept empties: %+v", f)
	}
}

func TestInitDoesNotShareMemoryWithTheMeasure(t *testing.T) {
	m := legacy()
	f := baseline.Init(m)
	m.Functions["m/a.F1"] = 99
	m.Nolint["gosec"]["a.go"] = 99
	if f.Ledger.Functions["m/a.F1"] != 21 || f.Ledger.Nolint["gosec"]["a.go"] != 2 {
		t.Error("changing the measure changed the written ledger")
	}
}

func TestLowerWalkThroughStep6NoiseIsNotRaised(t *testing.T) {
	// Step 6: 21.0 edited to 21.005 is noise for the gate; the refresh does not
	// raise the entry to follow it.
	old := ledgerOf(legacy())
	m := legacy()
	m.Functions["m/a.F1"] = 21.005
	got := baseline.Lower(old, m)
	if got.Ledger.Functions["m/a.F1"] != 21 {
		t.Errorf("entry rose to %v", got.Ledger.Functions["m/a.F1"])
	}
}

func TestLowerWalkThroughStep7RefactoredFunctionLeavesTheLedger(t *testing.T) {
	// Step 7: F1 refactored to 8, under the cap, so it is no longer measured as
	// over it. The refresh removes its entry and changes nothing else.
	old := ledgerOf(legacy())
	m := legacy()
	delete(m.Functions, "m/a.F1")
	got := baseline.Lower(old, m)
	want := ledgerOf(m)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Lower:\n%+v\nwant\n%+v", got, want)
	}
	if _, ok := got.Ledger.Functions["m/a.F1"]; ok {
		t.Error("the paid entry is still in the ledger")
	}
}

func TestLowerLowersAValueStillOverTheCap(t *testing.T) {
	old := ledgerOf(legacy())
	m := legacy()
	m.Functions["m/b.F4"] = 25.25
	if got := baseline.Lower(old, m).Ledger.Functions["m/b.F4"]; got != 25.25 {
		t.Errorf("entry %v, want 25.25", got)
	}
}

func TestLowerNeverAddsAnEntry(t *testing.T) {
	old := ledgerOf(legacy())
	m := legacy()
	m.Functions["m/new.Grew"] = 33                                         // a new function over the cap
	m.Packages["m/e"] = baseline.Package{StatementsPct: 10, Uncovered: 90} // a new package under the floor
	m.Findings["errcheck"] = map[string]int{"z.go": 1}                     // a new finding
	m.Findings["revive"]["new.go"] = 4                                     // in a new file
	m.Nolint["gosec"]["c.go"] = 1                                          // a new suppression
	got := baseline.Lower(old, m)
	if !reflect.DeepEqual(got, old) {
		t.Errorf("Lower added something:\n%+v\nold\n%+v", got, old)
	}
}

func TestLowerNeverRaisesAnEntry(t *testing.T) {
	old := ledgerOf(legacy())
	m := legacy()
	m.Functions["m/a.F2"] = 50
	m.Findings["revive"]["a.go"] = 9
	m.Nolint["gosec"]["b.go"] = 9
	m.Exemptions = 5
	m.Packages["m/d"] = baseline.Package{StatementsPct: 40, Uncovered: 90}
	got := baseline.Lower(old, m)
	if !reflect.DeepEqual(got, old) {
		t.Errorf("Lower raised something:\n%+v\nold\n%+v", got, old)
	}
}

func TestLowerCountsFallAndLeaveAtZero(t *testing.T) {
	old := ledgerOf(legacy())
	m := legacy()
	m.Findings = baseline.Counts{"revive": {"a.go": 1}}
	m.Nolint = baseline.Counts{"gosec": {"a.go": 2}} // b.go's comment is gone
	m.Exemptions = 0
	got := baseline.Lower(old, m)
	if got.Ledger.Findings["revive"]["a.go"] != 1 {
		t.Errorf("findings %v", got.Ledger.Findings)
	}
	if _, ok := got.Ledger.Nolint["gosec"]["b.go"]; ok {
		t.Errorf("a count of zero stayed: %v", got.Ledger.Nolint)
	}
	if got.Ledger.Exemptions != 0 {
		t.Errorf("exemptions %d, want the key gone", got.Ledger.Exemptions)
	}
	if err := validate(t, baseline.Marshal(got)); err != nil {
		t.Errorf("the lowered file is not valid: %v", err)
	}
}

func TestLowerDoesNotPoolCountsAcrossFiles(t *testing.T) {
	// §6.7: removing a finding in one file does not pay for another.
	old := ledgerOf(baseline.Measure{Findings: baseline.Counts{"revive": {"a.go": 3, "b.go": 1}}})
	got := baseline.Lower(old, baseline.Measure{Findings: baseline.Counts{"revive": {"a.go": 1, "b.go": 1}}})
	want := baseline.Counts{"revive": {"a.go": 1, "b.go": 1}}
	if !reflect.DeepEqual(got.Ledger.Findings, want) {
		t.Errorf("findings %v, want %v", got.Ledger.Findings, want)
	}
}

func TestLowerPackages(t *testing.T) {
	// An entry tightens toward coverage: the percentage may only rise and the
	// uncovered count only fall. A package at or over its floor is not measured
	// as under it, so its entry is paid.
	old := baseline.File{Ledger: baseline.Ledger{Packages: map[string]baseline.Package{
		"m/up":    {StatementsPct: 60, Uncovered: 40},
		"m/mixed": {StatementsPct: 60, Uncovered: 40},
		"m/paid":  {StatementsPct: 60, Uncovered: 40},
		"m/gone":  {StatementsPct: 60, Uncovered: 40},
		"m/worse": {StatementsPct: 60, Uncovered: 40},
	}}}
	m := baseline.Measure{Packages: map[string]baseline.Package{
		"m/up":    {StatementsPct: 70, Uncovered: 30},
		"m/mixed": {StatementsPct: 65, Uncovered: 55}, // more tested code, more untested too
		"m/worse": {StatementsPct: 50, Uncovered: 60},
	}}
	got := baseline.Lower(old, m).Ledger.Packages
	want := map[string]baseline.Package{
		"m/up":    {StatementsPct: 70, Uncovered: 30},
		"m/mixed": {StatementsPct: 65, Uncovered: 40},
		"m/worse": {StatementsPct: 60, Uncovered: 40},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("packages %v, want %v", got, want)
	}
}

func TestLowerRaisesJudgedLawsAndNeverLowersIt(t *testing.T) {
	old := baseline.File{JudgedLaws: 5}
	if got := baseline.Lower(old, baseline.Measure{JudgedLaws: 9}).JudgedLaws; got != 9 {
		t.Errorf("judged_laws %d, want 9", got)
	}
	if got := baseline.Lower(old, baseline.Measure{JudgedLaws: 2}).JudgedLaws; got != 5 {
		t.Errorf("judged_laws %d, want 5: lowering it needs --accept-regression", got)
	}
}

func TestLowerCarriesWhatItDoesNotMeasure(t *testing.T) {
	// laws (M5) and survivors (M4) are fed by other commands.
	old := full()
	got := baseline.Lower(old, baseline.Measure{JudgedLaws: 7})
	if !reflect.DeepEqual(got.Ledger.Laws, old.Ledger.Laws) || !reflect.DeepEqual(got.Ledger.Survivors, old.Ledger.Survivors) {
		t.Errorf("laws %v survivors %v were not carried", got.Ledger.Laws, got.Ledger.Survivors)
	}
}

func TestLowerDoesNotChangeItsInput(t *testing.T) {
	old := ledgerOf(legacy())
	snapshot := baseline.Marshal(old)
	m := legacy()
	delete(m.Functions, "m/a.F1")
	baseline.Lower(old, m)
	if string(baseline.Marshal(old)) != string(snapshot) {
		t.Error("Lower wrote into the old ledger")
	}
}

func TestLowerRemovesARecordPaidBackToItsEarlierValue(t *testing.T) {
	old := baseline.File{
		Ledger: baseline.Ledger{Functions: map[string]float64{"m/a.F": 25, "m/a.G": 22, "m/a.H": 20}},
		Regressions: []baseline.Regression{
			{Kind: "functions", Key: "m/a.F", From: f64(21), To: 25, Reason: goodReason, Owner: "o"}, // paid back to 21
			{Kind: "functions", Key: "m/a.G", From: f64(21), To: 25, Reason: goodReason, Owner: "o"}, // lowered, not paid
			{Kind: "functions", Key: "m/a.H", From: nil, To: 20, Reason: goodReason, Owner: "o"},     // added, and now gone
		},
	}
	m := baseline.Measure{Functions: map[string]float64{"m/a.F": 21, "m/a.G": 22}}
	got := baseline.Lower(old, m)
	want := []baseline.Regression{{Kind: "functions", Key: "m/a.G", From: f64(21), To: 22, Reason: goodReason, Owner: "o"}}
	if !reflect.DeepEqual(got.Regressions, want) {
		t.Errorf("regressions %+v, want %+v", got.Regressions, want)
	}
}

func TestLowerKeepsARecordWhileItsEntryIsAboveTheEarlierValue(t *testing.T) {
	r := baseline.Regression{Kind: "functions", Key: "m/a.F", From: f64(21), To: 25, Reason: goodReason, Owner: "o"}
	old := baseline.File{Ledger: baseline.Ledger{Functions: map[string]float64{"m/a.F": 25}}, Regressions: []baseline.Regression{r}}
	got := baseline.Lower(old, baseline.Measure{Functions: map[string]float64{"m/a.F": 25}})
	if !reflect.DeepEqual(got.Regressions, []baseline.Regression{r}) {
		t.Errorf("regressions %+v", got.Regressions)
	}
}

func TestLowerRegressionOfJudgedLawsIsPaidWhenItRisesBack(t *testing.T) {
	old := baseline.File{JudgedLaws: 3, Regressions: []baseline.Regression{{Kind: "judged_laws", Key: "judged_laws", From: f64(5), To: 3, Reason: goodReason, Owner: "o"}}}
	if got := baseline.Lower(old, baseline.Measure{JudgedLaws: 5}); len(got.Regressions) != 0 || got.JudgedLaws != 5 {
		t.Errorf("%+v", got)
	}
}

func TestAcceptRaisesAnEntryAndRecordsIt(t *testing.T) {
	old := ledgerOf(legacy())
	m := legacy()
	m.Functions["m/a.F1"] = 25
	got, err := baseline.Accept(old, m, goodReason, "maintainer")
	if err != nil {
		t.Fatal(err)
	}
	if got.Ledger.Functions["m/a.F1"] != 25 {
		t.Errorf("entry %v, want 25", got.Ledger.Functions["m/a.F1"])
	}
	want := []baseline.Regression{{Kind: "functions", Key: "m/a.F1", From: f64(21), To: 25, Reason: goodReason, Owner: "maintainer"}}
	if !reflect.DeepEqual(got.Regressions, want) {
		t.Errorf("regressions %+v, want %+v", got.Regressions, want)
	}
}

func TestAcceptAddsAnEntryWithANullFrom(t *testing.T) {
	old := ledgerOf(legacy())
	m := legacy()
	m.Functions["m/new.Grew"] = 33
	m.Findings["errcheck"] = map[string]int{"z.go": 1}
	m.Nolint["gosec"]["c.go"] = 1
	m.Exemptions = 2
	got, err := baseline.Accept(old, m, goodReason, "maintainer")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Ledger, ledgerOf(m).Ledger) {
		t.Errorf("ledger %+v", got.Ledger)
	}
	want := []baseline.Regression{
		{Kind: "exemptions", Key: "exemptions", From: f64(1), To: 2, Reason: goodReason, Owner: "maintainer"},
		{Kind: "findings", Key: "errcheck:z.go", From: nil, To: 1, Reason: goodReason, Owner: "maintainer"},
		{Kind: "functions", Key: "m/new.Grew", From: nil, To: 33, Reason: goodReason, Owner: "maintainer"},
		{Kind: "suppressions.nolint", Key: "gosec:c.go", From: nil, To: 1, Reason: goodReason, Owner: "maintainer"},
	}
	if !reflect.DeepEqual(got.Regressions, want) {
		t.Errorf("regressions\n%+v\nwant\n%+v", got.Regressions, want)
	}
	if err := validate(t, baseline.Marshal(got)); err != nil {
		t.Errorf("not valid: %v", err)
	}
}

func TestAcceptRecordsAPackageThatGotWorse(t *testing.T) {
	old := ledgerOf(legacy())
	m := legacy()
	m.Packages["m/d"] = baseline.Package{StatementsPct: 50, Uncovered: 45} // pct fell, uncovered rose
	m.Packages["m/e"] = baseline.Package{StatementsPct: 10, Uncovered: 90} // a new package under the floor
	got, err := baseline.Accept(old, m, goodReason, "maintainer")
	if err != nil {
		t.Fatal(err)
	}
	if got.Ledger.Packages["m/d"] != (baseline.Package{StatementsPct: 50, Uncovered: 45}) || got.Ledger.Packages["m/e"] != (baseline.Package{StatementsPct: 10, Uncovered: 90}) {
		t.Errorf("packages %v", got.Ledger.Packages)
	}
	want := []baseline.Regression{
		{Kind: "packages.statements_pct", Key: "m/d", From: f64(62.5), To: 50, Reason: goodReason, Owner: "maintainer"},
		{Kind: "packages.statements_pct", Key: "m/e", From: nil, To: 10, Reason: goodReason, Owner: "maintainer"},
		{Kind: "packages.uncovered", Key: "m/d", From: f64(30), To: 45, Reason: goodReason, Owner: "maintainer"},
		{Kind: "packages.uncovered", Key: "m/e", From: nil, To: 90, Reason: goodReason, Owner: "maintainer"},
	}
	if !reflect.DeepEqual(got.Regressions, want) {
		t.Errorf("regressions\n%+v\nwant\n%+v", got.Regressions, want)
	}
}

func TestAcceptRecordsLoweredJudgedLaws(t *testing.T) {
	old := baseline.File{JudgedLaws: 6}
	got, err := baseline.Accept(old, baseline.Measure{JudgedLaws: 4}, goodReason, "maintainer")
	if err != nil {
		t.Fatal(err)
	}
	want := []baseline.Regression{{Kind: "judged_laws", Key: "judged_laws", From: f64(6), To: 4, Reason: goodReason, Owner: "maintainer"}}
	if got.JudgedLaws != 4 || !reflect.DeepEqual(got.Regressions, want) {
		t.Errorf("%+v", got)
	}
}

func TestAcceptAlsoLowersAndRemovesLikeAPlainWrite(t *testing.T) {
	old := ledgerOf(legacy())
	m := legacy()
	m.Functions["m/a.F1"] = 25 // raised
	m.Functions["m/b.F4"] = 20 // lowered
	delete(m.Functions, "m/d.F8")
	got, err := baseline.Accept(old, m, goodReason, "maintainer")
	if err != nil {
		t.Fatal(err)
	}
	if got.Ledger.Functions["m/b.F4"] != 20 {
		t.Errorf("F4 %v", got.Ledger.Functions["m/b.F4"])
	}
	if _, ok := got.Ledger.Functions["m/d.F8"]; ok {
		t.Error("the paid entry stayed")
	}
	if len(got.Regressions) != 1 || got.Regressions[0].Key != "m/a.F1" {
		t.Errorf("regressions %+v: only the raise is a regression", got.Regressions)
	}
}

func TestAcceptRaisedAgainKeepsTheEarliestFrom(t *testing.T) {
	// The record's from is the value the gate's diff base holds; a second
	// acceptance before merge must not hide how far the entry has moved.
	old := baseline.File{
		Ledger:      baseline.Ledger{Functions: map[string]float64{"m/a.F": 25}},
		Regressions: []baseline.Regression{{Kind: "functions", Key: "m/a.F", From: f64(21), To: 25, Reason: "first reason given", Owner: "first"}},
	}
	got, err := baseline.Accept(old, baseline.Measure{Functions: map[string]float64{"m/a.F": 30}}, goodReason, "maintainer")
	if err != nil {
		t.Fatal(err)
	}
	want := []baseline.Regression{{Kind: "functions", Key: "m/a.F", From: f64(21), To: 30, Reason: goodReason, Owner: "maintainer"}}
	if !reflect.DeepEqual(got.Regressions, want) {
		t.Errorf("regressions %+v, want %+v", got.Regressions, want)
	}
}

func TestAcceptNeedsAReasonOfThreeWordsAndAnOwner(t *testing.T) {
	old := ledgerOf(legacy())
	m := legacy()
	m.Functions["m/a.F1"] = 25
	for name, c := range map[string]struct{ reason, owner string }{
		"no reason":        {"", "maintainer"},
		"one word":         {"growth", "maintainer"},
		"two words":        {"needs growth", "maintainer"},
		"blank reason":     {"   ", "maintainer"},
		"no owner":         {goodReason, ""},
		"blank owner":      {goodReason, "  "},
		"owner with space": {goodReason, "two words"},
	} {
		if _, err := baseline.Accept(old, m, c.reason, c.owner); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestAcceptWithNothingToAcceptSaysSo(t *testing.T) {
	old := ledgerOf(legacy())
	m := legacy()
	delete(m.Functions, "m/a.F1") // only an improvement
	_, err := baseline.Accept(old, m, goodReason, "maintainer")
	if !errors.Is(err, baseline.ErrNothingToAccept) {
		t.Errorf("err %v, want ErrNothingToAccept", err)
	}
}

func TestRaisedListsEveryRaisedAddedOrLoweredEntry(t *testing.T) {
	// The one definition of what needs a regressions record: the gate reads the
	// same list when it compares the file with its copy at the diff base.
	old := ledgerOf(legacy())
	cur := ledgerOf(legacy())
	cur.Ledger.Functions["m/a.F1"] = 22
	cur.Ledger.Functions["m/new.G"] = 12
	delete(cur.Ledger.Functions, "m/a.F2") // lowered: not listed
	cur.Ledger.Findings["revive"]["a.go"] = 1
	cur.JudgedLaws = 0
	old.JudgedLaws = 2
	got := baseline.Raised(old, cur)
	want := []baseline.Change{
		{Kind: "functions", Key: "m/a.F1", From: f64(21), To: 22},
		{Kind: "functions", Key: "m/new.G", From: nil, To: 12},
		{Kind: "judged_laws", Key: "judged_laws", From: f64(2), To: 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Raised\n%+v\nwant\n%+v", got, want)
	}
}

func TestRaisedIsEmptyForAFirstLedgerAgainstItself(t *testing.T) {
	f := ledgerOf(legacy())
	if got := baseline.Raised(f, f); len(got) != 0 {
		t.Errorf("%+v", got)
	}
}

// inv is an Inventory over fixed key sets.
type inv struct{ fns, pkgs map[string]bool }

func (i inv) Has(k baseline.Kind, key string) bool {
	if k == baseline.KindPackage {
		return i.pkgs[key]
	}
	return i.fns[key]
}

func set(keys ...string) map[string]bool {
	m := map[string]bool{}
	for _, k := range keys {
		m[k] = true
	}
	return m
}

func TestRenameMovesOneFunctionKeyAndNeverChangesAValue(t *testing.T) {
	old := ledgerOf(legacy())
	got, err := baseline.Rename(old, "m/a.F1", "m/a.Renamed", inv{fns: set("m/a.Renamed", "m/a.F2")})
	if err != nil {
		t.Fatal(err)
	}
	want := ledgerOf(legacy())
	delete(want.Ledger.Functions, "m/a.F1")
	want.Ledger.Functions["m/a.Renamed"] = 21
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Rename:\n%+v\nwant\n%+v", got, want)
	}
}

func TestRenameMovesAPackageKey(t *testing.T) {
	old := ledgerOf(legacy())
	got, err := baseline.Rename(old, "m/d", "m/dd", inv{pkgs: set("m/dd")})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Ledger.Packages["m/d"]; ok || got.Ledger.Packages["m/dd"] != (baseline.Package{StatementsPct: 62.5, Uncovered: 30}) {
		t.Errorf("packages %v", got.Ledger.Packages)
	}
}

func TestRenameRefusals(t *testing.T) {
	old := ledgerOf(legacy())
	old.Ledger.Functions["m/a.Taken"] = 15
	for name, c := range map[string]struct {
		from, to string
		inv      inv
	}{
		"the old key is still in the code": {"m/a.F1", "m/a.New", inv{fns: set("m/a.F1", "m/a.New")}},
		"the new key does not exist":       {"m/a.F1", "m/a.New", inv{fns: set()}},
		"the old key has no entry":         {"m/a.Nope", "m/a.New", inv{fns: set("m/a.New")}},
		"the new key has an entry":         {"m/a.F1", "m/a.Taken", inv{fns: set("m/a.Taken")}},
		"the same key":                     {"m/a.F1", "m/a.F1", inv{fns: set("m/a.F1")}},
		"a package still in the code":      {"m/d", "m/dd", inv{pkgs: set("m/d", "m/dd")}},
	} {
		got, err := baseline.Rename(old, c.from, c.to, c.inv)
		var ref *baseline.RefusalError
		if !errors.As(err, &ref) {
			t.Errorf("%s: err %v, want a refusal", name, err)
		}
		if !reflect.DeepEqual(got, baseline.File{}) {
			t.Errorf("%s: a refusal returned a ledger", name)
		}
	}
}

func TestRenameFollowsTheKeyIntoItsRecords(t *testing.T) {
	old := baseline.File{
		Ledger:      baseline.Ledger{Functions: map[string]float64{"m/a.F": 25}, Packages: map[string]baseline.Package{"m/p": {StatementsPct: 50, Uncovered: 9}}},
		Regressions: []baseline.Regression{{Kind: "functions", Key: "m/a.F", From: f64(21), To: 25, Reason: goodReason, Owner: "o"}, {Kind: "packages.uncovered", Key: "m/p", From: nil, To: 9, Reason: goodReason, Owner: "o"}},
	}
	got, err := baseline.Rename(old, "m/a.F", "m/a.G", inv{fns: set("m/a.G")})
	if err != nil {
		t.Fatal(err)
	}
	if got.Regressions[0].Key != "m/a.G" || got.Regressions[1].Key != "m/p" {
		t.Errorf("regressions %+v", got.Regressions)
	}
	got, err = baseline.Rename(old, "m/p", "m/q", inv{pkgs: set("m/q")})
	if err != nil {
		t.Fatal(err)
	}
	if got.Regressions[0].Key != "m/a.F" || got.Regressions[1].Key != "m/q" {
		t.Errorf("regressions %+v", got.Regressions)
	}
}

// Ruling C3: the ledger leg is the only run that writes, and its ledger holds
// only what it measured. There are no carried entries for functions it does not
// measure, and an exempt function has no entry.

func TestLowerRemovesAnEntryTheRunDoesNotMeasure(t *testing.T) {
	// A function that is paid, exempt (scored, not a violation) or no longer
	// compiled on the ledger leg is not in the measure's over-the-cap list, and
	// its entry goes.
	old := ledgerOf(legacy())
	m := legacy()
	delete(m.Functions, "m/b.F4")
	got := baseline.Lower(old, m)
	if _, ok := got.Ledger.Functions["m/b.F4"]; ok {
		t.Error("an entry for a function the run did not list over the cap stayed")
	}
}

func TestWritesKeepTheLedgerBuild(t *testing.T) {
	old := baseline.Init(baseline.Measure{Build: testBuild, Functions: map[string]float64{"m/a.F": 21}})
	if !old.Build.Equal(testBuild) {
		t.Fatalf("Init dropped the build: %+v", old.Build)
	}
	if got := baseline.Lower(old, baseline.Measure{}); !got.Build.Equal(testBuild) {
		t.Errorf("Lower changed the build: %+v", got.Build)
	}
	got, err := baseline.Accept(old, baseline.Measure{Functions: map[string]float64{"m/a.F": 30}}, goodReason, "maintainer")
	if err != nil || !got.Build.Equal(testBuild) {
		t.Errorf("Accept: %v %+v", err, got.Build)
	}
	// A measure from another build does not rewrite the ledger's.
	other := baseline.Measure{Build: baseline.Build{GOOS: "darwin", GOARCH: "arm64"}}
	if got := baseline.Lower(old, other); !got.Build.Equal(testBuild) {
		t.Errorf("Lower took the build of the measure: %+v", got.Build)
	}
	got, err = baseline.Rename(old, "m/a.F", "m/a.G", inv{fns: set("m/a.G")})
	if err != nil || !got.Build.Equal(testBuild) {
		t.Errorf("Rename: %v %+v", err, got.Build)
	}
}

func TestKeySetsAnswerPerKind(t *testing.T) {
	s := baseline.KeySets{Functions: []string{"m/a.F"}, Packages: []string{"m/a"}}
	if !s.Has(baseline.KindFunction, "m/a.F") || s.Has(baseline.KindFunction, "m/a") || !s.Has(baseline.KindPackage, "m/a") || s.Has(baseline.KindPackage, "m/a.F") {
		t.Error("KeySets mixed functions and packages")
	}
}

func TestCheckAcceptArgs(t *testing.T) {
	for name, c := range map[string]struct {
		reason, owner string
		ok            bool
	}{
		"good":             {goodReason, "maintainer", true},
		"padded owner":     {goodReason, "  maintainer ", true},
		"two word reason":  {"needs growth", "maintainer", false},
		"no reason":        {"", "maintainer", false},
		"no owner":         {goodReason, "", false},
		"blank owner":      {goodReason, "   ", false},
		"owner with space": {goodReason, "two words", false},
	} {
		err := baseline.CheckAcceptArgs(c.reason, c.owner)
		var ref *baseline.RefusalError
		if (err == nil) != c.ok || (err != nil && !errors.As(err, &ref)) {
			t.Errorf("%s: err %v, want ok=%v and a refusal otherwise", name, err, c.ok)
		}
	}
}

func TestRenameRefusesAKeyThatIsBothAFunctionAndAPackage(t *testing.T) {
	old := baseline.File{Ledger: baseline.Ledger{
		Functions: map[string]float64{"m/x": 12},
		Packages:  map[string]baseline.Package{"m/x": {StatementsPct: 10, Uncovered: 3}},
	}}
	_, err := baseline.Rename(old, "m/x", "m/y", inv{fns: set("m/y"), pkgs: set("m/y")})
	var ref *baseline.RefusalError
	if !errors.As(err, &ref) || !strings.Contains(err.Error(), "both") {
		t.Errorf("err %v", err)
	}
}

// §6.7: a refresh must not tell the user to accept a regression the ratchet
// would not fail. A package fails only when its percentage falls and its
// uncovered count rises, so deleting tested code moves one number, not both.
func TestNeedsAcceptKeepsOnlyWhatTheRatchetWouldFail(t *testing.T) {
	pkg := baseline.Package{StatementsPct: 50, Uncovered: 10}
	old := baseline.File{Ledger: baseline.Ledger{
		Functions: map[string]float64{"m/a.F": 12},
		Packages:  map[string]baseline.Package{"m/pct": pkg, "m/unc": pkg, "m/both": pkg},
	}}
	cur := baseline.File{Ledger: baseline.Ledger{
		Functions: map[string]float64{"m/a.F": 15, "m/a.New": 13},
		Packages: map[string]baseline.Package{
			"m/pct":  {StatementsPct: 40, Uncovered: 10}, // percentage falls, count the same: deleted tested code
			"m/unc":  {StatementsPct: 50, Uncovered: 14}, // count rises, percentage the same
			"m/both": {StatementsPct: 40, Uncovered: 14}, // both: the rule fails
			"m/new":  {StatementsPct: 20, Uncovered: 30}, // absent: held to the floor
		},
	}}
	var keys []string
	for _, c := range baseline.NeedsAccept(baseline.Raised(old, cur)) {
		keys = append(keys, c.Kind+" "+c.Key)
	}
	want := []string{
		"functions m/a.F", "functions m/a.New",
		"packages.statements_pct m/both", "packages.statements_pct m/new",
		"packages.uncovered m/both", "packages.uncovered m/new",
	}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("NeedsAccept\n%v\nwant\n%v", keys, want)
	}
}
