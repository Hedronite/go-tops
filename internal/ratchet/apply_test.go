package ratchet_test

import (
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/gobuild"
	"github.com/Hedronite/go-tops/internal/ratchet"
	"github.com/Hedronite/go-tops/internal/report"
)

// Under ratchet the CRAP layer, the coverage layer and the lint-finding verdict
// of the hygiene layer judge by the ledger. The layers run by the hard rules, so
// a legacy repo's layers fail; Apply gives them the ledger's thresholds.

func layer(id, outcome string, details map[string]any) report.Layer {
	return report.Layer{ID: id, Tier: 1, Outcome: outcome, Reason: "the hard rule", Details: details}
}

func crapRows() []any {
	var rows []any
	for i, score := range overScores {
		rows = append(rows, map[string]any{"func": key(i), "file": "p/p.go", "cc": 12, "cov_pct": 50.0, "crap": score, "baseline": nil})
	}
	return rows
}

func crapLayer() report.Layer {
	return layer("crap", "fail", map[string]any{"cap": 10.0, "violations": crapRows(), "not_measured": 0})
}

func list(t *testing.T, v any) []any {
	t.Helper()
	l, ok := v.([]any)
	if !ok {
		t.Fatalf("%T is not a list", v)
	}
	return l
}

func mapOf(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%T is not a map", v)
	}
	return m
}

func find2(t *testing.T, ls []report.Layer, id string) report.Layer {
	t.Helper()
	for _, l := range ls {
		if l.ID == id {
			return l
		}
	}
	t.Fatalf("no %s layer", id)
	return report.Layer{}
}

// The legacy repo: eight functions over the cap fail the CRAP layer by the hard
// rule, and pass under the ratchet because the ledger has an entry for each.
func TestApplyCrapPassesLegacyDebtAndFillsTheEntries(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux)
	j := ratchet.Judge(input(m, l, based(l)))
	out := find2(t, ratchet.Apply([]report.Layer{crapLayer()}, j, l), "crap")
	if out.Outcome != "pass" {
		t.Fatalf("crap %s: %s", out.Outcome, out.Reason)
	}
	rows, ok := out.Details["violations"].([]any)
	if !ok || len(rows) != 8 {
		t.Fatalf("violations %v, want the 8 rows kept", out.Details["violations"])
	}
	for i, r := range rows {
		row, ok := r.(map[string]any)
		if !ok || num(row["baseline"]) != overScores[i] {
			t.Errorf("row %d: %v, want its ledger entry %v as the baseline", i, r, overScores[i])
		}
	}
}

func TestApplyCrapFailsPastTheEntryAndNamesTheFunction(t *testing.T) {
	m := setScore(legacy(), key(0), 22)
	l := initLedger(legacy(), linux)
	j := ratchet.Judge(input(m, l, based(l)))
	out := find2(t, ratchet.Apply([]report.Layer{crapLayer()}, j, l), "crap")
	if out.Outcome != "fail" || !strings.Contains(out.Reason, key(0)) {
		t.Errorf("crap %s: %s", out.Outcome, out.Reason)
	}
}

// A not-measured function over the cap fails the layer on every leg; the count
// of not-measured functions stays in the text.
func TestApplyCrapKeepsTheNotMeasuredCountAndFailure(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux)
	m.NotMeasured = append(m.NotMeasured, notMeasured("example.com/m/p.Win", 12))
	j := ratchet.Judge(input(m, l, based(l)))
	layerIn := layer("crap", "fail", map[string]any{"cap": 10.0, "violations": crapRows(), "not_measured": 1})
	out := find2(t, ratchet.Apply([]report.Layer{layerIn}, j, l), "crap")
	if out.Outcome != "fail" || !strings.Contains(out.Reason, "1 production function(s) not measured on this build") {
		t.Errorf("crap %s: %s", out.Outcome, out.Reason)
	}
}

// On another leg the ratchet is skipped: legacy debt the ledger leg judges does
// not fail the layer, and the baseline column stays null.
func TestApplyCrapOnAnotherLeg(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux)
	j := ratchet.Judge(otherLegInput(l, based(l), m, compiledAll(m)))
	out := find2(t, ratchet.Apply([]report.Layer{crapLayer()}, j, l), "crap")
	if out.Outcome != "pass" {
		t.Fatalf("crap %s: %s", out.Outcome, out.Reason)
	}
	rows := list(t, out.Details["violations"])
	if row := mapOf(t, rows[0]); row["baseline"] != nil {
		t.Errorf("row %v: another leg fills no baseline", rows[0])
	}
	// ...and a function only this leg compiles, over the cap, does fail it.
	m.Functions = append(m.Functions, overFunction("example.com/m/p.Mac", 12))
	held := ratchet.Judge(otherLegInput(l, based(l), m, compiledAll(legacy())))
	failing := find2(t, ratchet.Apply([]report.Layer{crapLayer()}, held, l), "crap")
	if failing.Outcome != "fail" || !strings.Contains(failing.Reason, "example.com/m/p.Mac") {
		t.Errorf("crap %s: %s", failing.Outcome, failing.Reason)
	}
}

// A crap layer that failed for another reason stays failed, and a pass stays a pass.
func TestApplyLeavesOtherCrapOutcomesAlone(t *testing.T) {
	l := initLedger(legacy(), linux)
	j := ratchet.Judge(input(legacy(), l, based(l)))
	broken := layer("crap", "fail", map[string]any{"error": "the profile holds no block"})
	passing := layer("crap", "pass", map[string]any{"cap": 10.0, "violations": []any{}, "not_measured": 0})
	for _, in := range []report.Layer{broken, passing} {
		if out := find2(t, ratchet.Apply([]report.Layer{in}, j, l), "crap"); out.Outcome != in.Outcome {
			t.Errorf("a %s layer became %s", in.Outcome, out.Outcome)
		}
	}
}

func coverageLayer() report.Layer {
	return layer("coverage", "fail", map[string]any{"statements_pct": 91.0, "floor": 90.0, "profile_bytes": 100})
}

func TestApplyCoverage(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux)
	// m/low is under the floor and listed: within the ledger, the layer passes.
	j := ratchet.Judge(input(m, l, based(l)))
	if out := find2(t, ratchet.Apply([]report.Layer{coverageLayer()}, j, l), "coverage"); out.Outcome != "pass" {
		t.Errorf("coverage %s: %s", out.Outcome, out.Reason)
	}
	// A package under the floor with no entry fails it, named.
	bad := setPackage(m, "example.com/m/a", 100, 85)
	jb := ratchet.Judge(input(bad, l, based(l)))
	out := find2(t, ratchet.Apply([]report.Layer{coverageLayer()}, jb, l), "coverage")
	if out.Outcome != "fail" || !strings.Contains(out.Reason, "example.com/m/a") {
		t.Errorf("coverage %s: %s", out.Outcome, out.Reason)
	}
	// Another leg does not judge the packages.
	jo := ratchet.Judge(otherLegInput(l, based(l), bad, compiledAll(bad)))
	if out := find2(t, ratchet.Apply([]report.Layer{coverageLayer()}, jo, l), "coverage"); out.Outcome != "pass" {
		t.Errorf("another leg: coverage %s: %s", out.Outcome, out.Reason)
	}
	// A missing profile stays failed.
	broken := layer("coverage", "fail", map[string]any{"error": "the coverage profile does not exist"})
	if out := find2(t, ratchet.Apply([]report.Layer{broken}, j, l), "coverage"); out.Outcome != "fail" {
		t.Errorf("an unreadable profile became %s", out.Outcome)
	}
}

func hygieneLayer(findings int, mutate func(map[string]any)) report.Layer {
	pass := func() map[string]any { return map[string]any{"outcome": "pass"} }
	d := map[string]any{
		"golangci_lint": "v2.15.0", "findings": findings,
		"tidy": pass(), "verify": pass(), "toolchain": pass(), "format": pass(),
		"lint":            map[string]any{"outcome": "fail", "message": "3 finding(s)", "findings": findings, "by_linter": map[string]any{"revive": findings}},
		"vulnerabilities": pass(), "licenses": pass(),
	}
	if mutate != nil {
		mutate(d)
	}
	return layer("hygiene", "fail", d)
}

// Under ratchet a lint finding fails only past the ledger's count for its linter
// and file; the other checks keep their own verdicts.
func TestApplyHygieneJudgesLintFindingsByTheLedger(t *testing.T) {
	m := legacy()
	m.Findings = ratchet.Counts{"revive": {"x.go": 3}}
	l := initLedger(m, linux)
	j := ratchet.Judge(input(m, l, based(l)))
	out := find2(t, ratchet.Apply([]report.Layer{hygieneLayer(3, nil)}, j, l), "hygiene")
	if out.Outcome != "pass" {
		t.Fatalf("hygiene %s: %s", out.Outcome, out.Reason)
	}
	lint := mapOf(t, out.Details["lint"])
	if lint["outcome"] != "pass" || lint["message"] != nil || num(lint["findings"]) != 3 {
		t.Errorf("lint %v: a pass that still reports its 3 findings", lint)
	}
	if num(out.Details["findings"]) != 3 {
		t.Errorf("findings %v, want the count kept", out.Details["findings"])
	}

	// One more finding in the same file is over its entry.
	worse := m
	worse.Findings = ratchet.Counts{"revive": {"x.go": 4}}
	jw := ratchet.Judge(input(worse, l, based(l)))
	failing := find2(t, ratchet.Apply([]report.Layer{hygieneLayer(4, nil)}, jw, l), "hygiene")
	if failing.Outcome != "fail" || !strings.Contains(failing.Reason, "revive x.go") {
		t.Errorf("hygiene %s: %s", failing.Outcome, failing.Reason)
	}
}

// Another check that failed still fails the layer, named alone.
func TestApplyHygieneKeepsTheOtherChecks(t *testing.T) {
	m := legacy()
	m.Findings = ratchet.Counts{"revive": {"x.go": 3}}
	l := initLedger(m, linux)
	j := ratchet.Judge(input(m, l, based(l)))
	in := hygieneLayer(3, func(d map[string]any) {
		d["format"] = map[string]any{"outcome": "fail", "message": "1 file(s) need formatting", "files": []any{"a.go"}}
	})
	out := find2(t, ratchet.Apply([]report.Layer{in}, j, l), "hygiene")
	if out.Outcome != "fail" || !strings.Contains(out.Reason, "format: 1 file(s) need formatting") || strings.Contains(out.Reason, "lint") {
		t.Errorf("hygiene %s: %s", out.Outcome, out.Reason)
	}
}

// A lint run that failed for another reason, or a layer with a missing tool, is
// not the ratchet's to forgive.
func TestApplyHygieneLeavesLintFailuresThatAreNotFindings(t *testing.T) {
	l := initLedger(legacy(), linux)
	j := ratchet.Judge(input(legacy(), l, based(l)))
	noResult := hygieneLayer(0, func(d map[string]any) {
		d["lint"] = map[string]any{"outcome": "fail", "message": "no result: .go-tops/lint-style.json was not written", "findings": 0, "by_linter": map[string]any{}}
	})
	if out := find2(t, ratchet.Apply([]report.Layer{noResult}, j, l), "hygiene"); out.Outcome != "fail" {
		t.Errorf("a lint run with no result became %s", out.Outcome)
	}
	missing := layer("hygiene", "tool-missing", hygieneLayer(3, nil).Details)
	if out := find2(t, ratchet.Apply([]report.Layer{missing}, j, l), "hygiene"); out.Outcome != "tool-missing" {
		t.Errorf("a missing tool became %s", out.Outcome)
	}
}

// Another leg does not judge the findings.
func TestApplyHygieneOnAnotherLeg(t *testing.T) {
	m := legacy()
	m.Findings = ratchet.Counts{"revive": {"x.go": 9}}
	l := initLedger(legacy(), linux)
	j := ratchet.Judge(otherLegInput(l, based(l), m, compiledAll(m)))
	if out := find2(t, ratchet.Apply([]report.Layer{hygieneLayer(9, nil)}, j, l), "hygiene"); out.Outcome != "pass" {
		t.Errorf("another leg: hygiene %s: %s", out.Outcome, out.Reason)
	}
}

func TestApplyDoesNotChangeItsInput(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux)
	j := ratchet.Judge(input(m, l, based(l)))
	in := []report.Layer{crapLayer(), coverageLayer(), hygieneLayer(3, nil)}
	ratchet.Apply(in, j, l)
	if in[0].Outcome != "fail" || in[1].Outcome != "fail" || in[2].Outcome != "fail" {
		t.Errorf("the input layers changed: %s %s %s", in[0].Outcome, in[1].Outcome, in[2].Outcome)
	}
	rows := list(t, in[0].Details["violations"])
	if row := mapOf(t, rows[0]); row["baseline"] != nil {
		t.Error("the input rows were filled in place")
	}
	lint := mapOf(t, in[2].Details["lint"])
	if lint["outcome"] != "fail" {
		t.Error("the input lint check changed")
	}
}

func TestNotesNameEveryFailure(t *testing.T) {
	m := setScore(legacy(), key(0), 22)
	l := initLedger(legacy(), linux)
	notes := ratchet.Notes(ratchet.Judge(input(m, l, based(l))))
	if len(notes) != 1 || !strings.HasPrefix(notes[0], "ratchet: functions "+key(0)) || !strings.HasSuffix(notes[0], "\n") {
		t.Errorf("notes %q", notes)
	}
}

// The CRAP layer that failed because an exemption matches nothing stays failed
// under ratchet: the ledger covers legacy debt, never a broken exemption.
func TestApplyCrapDoesNotForgiveAnUnmatchedExemption(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux)
	m.Problems = []ratchet.Failure{{Kind: ratchet.ProblemKind, Key: "p/gone.go", Why: "matches no production file"}}
	j := ratchet.Judge(input(m, l, based(l)))
	out := find2(t, ratchet.Apply([]report.Layer{crapLayer()}, j, l), "crap")
	if out.Outcome != "fail" || !strings.Contains(out.Reason, "p/gone.go") {
		t.Errorf("crap %s: %s", out.Outcome, out.Reason)
	}
}

// On a leg that is not the ledger leg the layers fail for what is held at hard
// (ruling C3d) and for nothing the ledger covers.
func TestApplyOtherLegFailsTheLayersForHeldSources(t *testing.T) {
	m := legacy()
	m.Findings = ratchet.Counts{"errcheck": {"p/d_darwin.go": 1}}
	m.Packages = map[string]ratchet.Package{"example.com/m/darwin": {StatementsPct: 20, Uncovered: 80}}
	l := initLedger(legacy(), linux)
	j := ratchet.Judge(otherLegInput(l, based(l), m, compiledAll(m)))
	if out := find2(t, ratchet.Apply([]report.Layer{hygieneLayer(1, nil)}, j, l), "hygiene"); out.Outcome != "fail" || !strings.Contains(out.Reason, "errcheck p/d_darwin.go") {
		t.Errorf("hygiene %s: %s", out.Outcome, out.Reason)
	}
	if out := find2(t, ratchet.Apply([]report.Layer{coverageLayer()}, j, l), "coverage"); out.Outcome != "fail" || !strings.Contains(out.Reason, "example.com/m/darwin") {
		t.Errorf("coverage %s: %s", out.Outcome, out.Reason)
	}
	// The CRAP layer fails for functions only: a finding and a package are not its.
	if out := find2(t, ratchet.Apply([]report.Layer{crapLayer()}, j, l), "crap"); out.Outcome != "pass" {
		t.Errorf("crap %s: %s, want a pass: the held verdicts are hygiene's and coverage's", out.Outcome, out.Reason)
	}
}

// A suppression or a law count in a file the ledger build does not compile is
// hygiene's verdict, on a leg that is not the ledger leg.
func TestApplyBooksHeldCountsToHygiene(t *testing.T) {
	m := legacy()
	m.Nolint = ratchet.Counts{"errcheck": {"p/d_darwin.go": 1}}
	m.Laws = ratchet.Counts{"G-11": {"p/d_darwin.go": 2}}
	l := initLedger(legacy(), linux)
	j := ratchet.Judge(otherLegInput(l, based(l), m, compiledAll(m)))
	clean := report.Layer{ID: "hygiene", Tier: 1, Outcome: "pass", Details: map[string]any{"golangci_lint": "v2.15.0", "findings": 0}}
	out := find2(t, ratchet.Apply([]report.Layer{clean}, j, l), "hygiene")
	if out.Outcome != "fail" || !strings.Contains(out.Reason, "errcheck p/d_darwin.go") || !strings.Contains(out.Reason, "G-11 p/d_darwin.go") {
		t.Errorf("hygiene %s: %s", out.Outcome, out.Reason)
	}
	if got := find2(t, ratchet.Apply([]report.Layer{crapLayer()}, j, l), "crap"); got.Outcome != "pass" {
		t.Errorf("crap %s: %s, want a pass", got.Outcome, got.Reason)
	}
	// On the ledger leg the same counts are the ratchet's own failures, not hygiene's.
	lj := ratchet.Judge(input(m, l, based(l)))
	if got := find2(t, ratchet.Apply([]report.Layer{clean}, lj, l), "hygiene"); got.Outcome != "pass" {
		t.Errorf("ledger leg: hygiene %s: %s", got.Outcome, got.Reason)
	}
}

// Ruling C3e, the layers of a partial run under ratchet: what the ledger replaces is
// not judged, and what it does not replace stands.
func TestApplyPartialDoesNotJudgeWhatTheLedgerReplaces(t *testing.T) {
	out := ratchet.ApplyPartial([]report.Layer{crapLayer(), coverageLayer(), hygieneLayer(5, nil)}, heldPartial())
	for _, id := range []string{"crap", "coverage", "hygiene"} {
		got := find2(t, out, id)
		if got.Outcome != "pass" || !strings.Contains(got.Reason, "not judged: partial run under ratchet") {
			t.Errorf("%s %s: %q, want a pass booked as not judged", id, got.Outcome, got.Reason)
		}
	}
	// Reported, not judged: the rows and the findings are still in the details.
	rows := list(t, find2(t, out, "crap").Details["violations"])
	if len(rows) != 8 {
		t.Errorf("crap rows %d, want the 8 legacy rows kept", len(rows))
	}
	if lint := mapOf(t, find2(t, out, "hygiene").Details["lint"]); num(lint["findings"]) != 5 || lint["outcome"] != "pass" {
		t.Errorf("lint %v", lint)
	}
}

func TestApplyPartialKeepsWhatTheLedgerDoesNotReplace(t *testing.T) {
	// A function that is not measured and over the cap, and an exemption that matches
	// nothing, still fail the CRAP layer.
	rows := append(crapRows(), map[string]any{"func": "example.com/m.Win", "file": "w_windows.go", "cc": 12, "cov_pct": nil, "crap": nil, "baseline": nil})
	layer := report.Layer{ID: "crap", Tier: 1, Outcome: "fail", Reason: "x", Details: map[string]any{"cap": 10.0, "violations": rows, "not_measured": 1}}
	got := find2(t, ratchet.ApplyPartial([]report.Layer{layer}, ratchet.Partial{Cap: 10, Ledger: initLedger(legacy(), linux)}), "crap")
	if got.Outcome != "fail" || !strings.Contains(got.Reason, "example.com/m.Win") {
		t.Errorf("crap %s: %s, want the not-measured function to stand", got.Outcome, got.Reason)
	}
	problem := []ratchet.Failure{{Kind: ratchet.ProblemKind, Key: "p/gone.go", Why: "matches no production file"}}
	got = find2(t, ratchet.ApplyPartial([]report.Layer{crapLayer()}, ratchet.Partial{Cap: 10, Ledger: initLedger(legacy(), linux), Problems: problem}), "crap")
	if got.Outcome != "fail" || !strings.Contains(got.Reason, "p/gone.go") {
		t.Errorf("crap %s: %s, want the unmatched exemption to stand", got.Outcome, got.Reason)
	}
	// Another hygiene check that failed stands, and the lint findings are not judged.
	h := hygieneLayer(5, func(d map[string]any) {
		d["format"] = map[string]any{"outcome": "fail", "message": "1 file(s) need formatting", "files": []any{"a.go"}}
	})
	hy := find2(t, ratchet.ApplyPartial([]report.Layer{h}, ratchet.Partial{Cap: 10, Ledger: initLedger(legacy(), linux)}), "hygiene")
	if hy.Outcome != "fail" || !strings.Contains(hy.Reason, "format: 1 file(s) need formatting") || strings.Contains(hy.Reason, "lint") {
		t.Errorf("hygiene %s: %s", hy.Outcome, hy.Reason)
	}
	// A coverage layer that failed for another reason stays failed.
	broken := report.Layer{ID: "coverage", Tier: 1, Outcome: "fail", Details: map[string]any{"error": "the coverage profile does not exist"}}
	if cov := find2(t, ratchet.ApplyPartial([]report.Layer{broken}, ratchet.Partial{Cap: 10}), "coverage"); cov.Outcome != "fail" {
		t.Errorf("coverage %s, want fail", cov.Outcome)
	}
}

// The messages say what happened: a judged_laws that fell is not "raised", and an
// entry that was added is not "raised from absent".
func TestRegressionMessagesSayWhatHappened(t *testing.T) {
	m := legacy()
	old := initLedger(m, linux)
	cur := clone(old)
	cur.JudgedLaws = old.JudgedLaws - 4
	cur.Functions[key(50)] = 10.5
	j := ratchet.Judge(input(m, cur, based(old)))
	var laws, added string
	for _, f := range j.Failures {
		switch f.Key {
		case "judged_laws":
			laws = f.Why
		case key(50):
			added = f.Why
		}
	}
	if !strings.HasPrefix(laws, "judged_laws fell from 5 to 1 with no regressions record") || strings.Contains(laws, "raised") {
		t.Errorf("judged_laws message %q", laws)
	}
	if !strings.HasPrefix(added, "functions added at 10.5 with no regressions record") || strings.Contains(added, "absent") {
		t.Errorf("added message %q", added)
	}
	cur.Functions[key(50)] = 0
	delete(cur.Functions, key(50))
	cur.Functions[key(0)] = old.Functions[key(0)] + 3
	for _, f := range ratchet.Judge(input(setScore(m, key(0), cur.Functions[key(0)]), cur, based(old))).Failures {
		if f.Key == key(0) && !strings.HasPrefix(f.Why, "functions raised from 21 to 24 with no regressions record") {
			t.Errorf("raised message %q", f.Why)
		}
	}
}

// The build text says whether cgo is on or off, and lists the tags.
func TestBuildTextSaysCgoOnOrOff(t *testing.T) {
	m := legacy()
	cur := initLedger(m, linux)
	old := clone(cur)
	old.Build = gobuild.Build{GOOS: "darwin", GOARCH: "arm64", Tags: []string{"integration"}}
	notes := ratchet.Notes(ratchet.Judge(input(m, cur, based(old))))
	if len(notes) != 1 || !strings.Contains(notes[0], "linux/amd64 cgo on") || !strings.Contains(notes[0], "darwin/arm64 cgo off tags=integration") {
		t.Errorf("notes %q, want cgo on and off said, and the tags listed", notes)
	}
}

// heldPartial is a partial run over the legacy repo with every item held by an entry:
// the eight functions, the package under its floor and the five lint findings.
func heldPartial() ratchet.Partial {
	m := legacy()
	m.Findings = ratchet.Counts{"revive": {"x.go": 5}}
	return ratchet.Partial{
		Cap: 10, Ledger: initLedger(m, linux),
		Packages: map[string]ratchet.Package{"example.com/m/low": {StatementsPct: 80, Uncovered: 20}},
		Findings: ratchet.Counts{"revive": {"x.go": 5}},
	}
}

// Ruling C3e, refined: in a partial run only an item with an entry in the working-tree
// ledger is not judged. One with no entry is judged at hard, absent meaning the cap, the
// floor or 0, so a partial run never hides new debt. One test per kind.

func TestApplyPartialJudgesAFunctionWithNoEntryAtTheCap(t *testing.T) {
	p := heldPartial()
	rows := append(crapRows(), map[string]any{"func": "example.com/m.Fresh", "file": "f.go", "cc": 11, "cov_pct": 100.0, "crap": 11.0, "baseline": nil})
	layer := report.Layer{ID: "crap", Tier: 1, Outcome: "fail", Reason: "x", Details: map[string]any{"cap": 10.0, "violations": rows, "not_measured": 0}}
	got := find2(t, ratchet.ApplyPartial([]report.Layer{layer}, p), "crap")
	if got.Outcome != "fail" || !strings.Contains(got.Reason, "example.com/m.Fresh") || strings.Contains(got.Reason, key(0)) {
		t.Errorf("crap %s: %s, want only the new function over the cap, with no entry, to fail", got.Outcome, got.Reason)
	}
	// With an entry it is held, whatever it scores: an entry is judged against the diff
	// base on a full run, not in a partial one.
	p.Ledger.Functions["example.com/m.Fresh"] = 11
	if held := find2(t, ratchet.ApplyPartial([]report.Layer{layer}, p), "crap"); held.Outcome != "pass" || !strings.Contains(held.Reason, "not judged") {
		t.Errorf("crap %s: %s, want a pass booked as not judged", held.Outcome, held.Reason)
	}
	// Without a ledger nothing has an entry: all eight legacy functions fail.
	none := ratchet.Partial{Cap: 10}
	if bare := find2(t, ratchet.ApplyPartial([]report.Layer{crapLayer()}, none), "crap"); bare.Outcome != "fail" || !strings.Contains(bare.Reason, "8 verdict(s)") {
		t.Errorf("crap %s: %s, want the eight to fail with no ledger", bare.Outcome, bare.Reason)
	}
}

func TestApplyPartialJudgesAPackageWithNoEntryAtTheFloor(t *testing.T) {
	p := heldPartial()
	p.Packages["example.com/m/fresh"] = ratchet.Package{StatementsPct: 40, Uncovered: 60}
	got := find2(t, ratchet.ApplyPartial([]report.Layer{coverageLayer()}, p), "coverage")
	if got.Outcome != "fail" || !strings.Contains(got.Reason, "example.com/m/fresh") || strings.Contains(got.Reason, "example.com/m/low") {
		t.Errorf("coverage %s: %s, want only the package with no entry to fail", got.Outcome, got.Reason)
	}
	delete(p.Packages, "example.com/m/fresh")
	if held := find2(t, ratchet.ApplyPartial([]report.Layer{coverageLayer()}, p), "coverage"); held.Outcome != "pass" || !strings.Contains(held.Reason, "not judged") {
		t.Errorf("coverage %s: %s, want the held package not judged", held.Outcome, held.Reason)
	}
}

func TestApplyPartialJudgesALintFindingWithNoEntryAtZero(t *testing.T) {
	p := heldPartial()
	p.Findings = ratchet.Counts{"revive": {"x.go": 50}, "errcheck": {"new.go": 1}} // x.go has an entry, new.go has none
	got := find2(t, ratchet.ApplyPartial([]report.Layer{hygieneLayer(51, nil)}, p), "hygiene")
	if got.Outcome != "fail" || !strings.Contains(got.Reason, "errcheck new.go") || strings.Contains(got.Reason, "revive x.go") {
		t.Errorf("hygiene %s: %s, want only the finding with no entry to fail", got.Outcome, got.Reason)
	}
	p.Findings = ratchet.Counts{"revive": {"x.go": 50}}
	if held := find2(t, ratchet.ApplyPartial([]report.Layer{hygieneLayer(50, nil)}, p), "hygiene"); held.Outcome != "pass" || !strings.Contains(held.Reason, "not judged") {
		t.Errorf("hygiene %s: %s, want the held findings not judged", held.Outcome, held.Reason)
	}
}

// A row at or under the cap needs no entry: only a function over the cap is debt.
func TestApplyPartialDoesNotJudgeAFunctionUnderTheCap(t *testing.T) {
	rows := []any{map[string]any{"func": "example.com/m.Fine", "file": "f.go", "cc": 6, "cov_pct": 100.0, "crap": 6.0, "baseline": nil}}
	layer := report.Layer{ID: "crap", Tier: 1, Outcome: "fail", Reason: "x", Details: map[string]any{"cap": 10.0, "violations": rows, "not_measured": 0}}
	if got := find2(t, ratchet.ApplyPartial([]report.Layer{layer}, ratchet.Partial{Cap: 10}), "crap"); got.Outcome != "pass" {
		t.Errorf("crap %s: %s, want a pass: the function is under the cap", got.Outcome, got.Reason)
	}
}
