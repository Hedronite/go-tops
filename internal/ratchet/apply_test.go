package ratchet_test

import (
	"strings"
	"testing"

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
		if !ok || row["baseline"] != overScores[i] {
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
	j := ratchet.Judge(ratchet.Input{Ledger: l, Base: based(l), Run: darwin, M: m, LedgerCompiled: compiledAll(m)})
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
	held := ratchet.Judge(ratchet.Input{Ledger: l, Base: based(l), Run: darwin, M: m, LedgerCompiled: compiledAll(legacy())})
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
	jo := ratchet.Judge(ratchet.Input{Ledger: l, Base: based(l), Run: darwin, M: bad, LedgerCompiled: compiledAll(bad)})
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
	if lint["outcome"] != "pass" || lint["message"] != nil || lint["findings"] != 3 {
		t.Errorf("lint %v: a pass that still reports its 3 findings", lint)
	}
	if out.Details["findings"] != 3 {
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
	j := ratchet.Judge(ratchet.Input{Ledger: l, Base: based(l), Run: darwin, M: m, LedgerCompiled: compiledAll(m)})
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
