package ratchet

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/report"
)

// Layer details reach the ratchet as a layer built them or as a stored report gives
// them back. A number is an int in one and a float64 in the other, and a layer may
// build a typed slice of rows. Every form must read the same, never as nothing.

func crapLayerWithRows(rows any, notMeasured any) report.Layer {
	return report.Layer{ID: "crap", Tier: 1, Outcome: "fail", Reason: "the hard rule", Details: map[string]any{
		"cap": 10.0, "violations": rows, "not_measured": notMeasured,
	}}
}

func typedRows() []map[string]any {
	return []map[string]any{
		{"func": "example.com/m.Win", "file": "w_windows.go", "cc": 12, "cov_pct": nil, "crap": nil, "baseline": nil},
		{"func": "example.com/m.Scored", "file": "a.go", "cc": 11, "cov_pct": 50.0, "crap": 14.5, "baseline": nil},
	}
}

func roundTripped(t *testing.T, ls []report.Layer) []report.Layer {
	t.Helper()
	raw, err := json.Marshal(report.Report{Layers: ls})
	if err != nil {
		t.Fatal(err)
	}
	var back report.Report
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	return back.Layers
}

func TestNotMeasuredRowsReadEveryFormOfTheDetails(t *testing.T) {
	forms := map[string][]report.Layer{
		"built in process, typed rows": {crapLayerWithRows(typedRows(), 1)},
		"built in process, []any rows": {crapLayerWithRows([]any{map[string]any{"func": "example.com/m.Win", "cc": 12, "crap": nil}}, 1)},
		"after a JSON round trip":      roundTripped(t, []report.Layer{crapLayerWithRows(typedRows(), 1)}),
	}
	for name, ls := range forms {
		got := notMeasuredRows(ls)
		if len(got) != 1 || got[0].Key != "example.com/m.Win" || got[0].Comp != 12 {
			t.Errorf("%s: rows %+v, want the one function with no score and comp 12", name, got)
		}
	}
}

func TestApplyReadsDetailsAfterAJSONRoundTrip(t *testing.T) {
	ledger := &Ledger{Functions: map[string]float64{"example.com/m.Scored": 14.5}}
	j := Judgment{Outcome: Pass}
	forms := map[string][]report.Layer{
		"in process": {crapLayerWithRows(typedRows(), 1)},
		"round trip": roundTripped(t, []report.Layer{crapLayerWithRows(typedRows(), 1)}),
	}
	for name, ls := range forms {
		out := Apply(ls, j, ledger)[0]
		if out.Outcome != "pass" || !strings.Contains(out.Reason, "1 production function(s) not measured") {
			t.Errorf("%s: crap %s: %q, want a pass that keeps the not-measured count", name, out.Outcome, out.Reason)
		}
		rows := listOf(out.Details["violations"])
		if len(rows) != 2 || rows[1]["baseline"] != 14.5 {
			t.Errorf("%s: rows %v, want the scored row's baseline filled with its entry", name, rows)
		}
	}
}

func TestApplyHygieneReadsFindingsAfterAJSONRoundTrip(t *testing.T) {
	pass := map[string]any{"outcome": "pass"}
	in := report.Layer{ID: "hygiene", Tier: 1, Outcome: "fail", Reason: "x", Details: map[string]any{
		"golangci_lint": "v2.15.0", "findings": 3,
		"tidy": pass, "verify": pass, "toolchain": pass, "format": pass, "vulnerabilities": pass, "licenses": pass,
		"lint": map[string]any{"outcome": "fail", "message": "3 finding(s)", "findings": 3, "by_linter": map[string]any{"revive": 3}},
	}}
	ledger := &Ledger{Findings: Counts{"revive": {"x.go": 3}}}
	for name, ls := range map[string][]report.Layer{"in process": {in}, "round trip": roundTripped(t, []report.Layer{in})} {
		if out := Apply(ls, Judgment{Outcome: Pass}, ledger)[0]; out.Outcome != "pass" {
			t.Errorf("%s: hygiene %s: %s, want a pass: the findings are within the ledger", name, out.Outcome, out.Reason)
		}
	}
}

func TestReadersTolerateWhatIsNotThere(t *testing.T) {
	if notMeasuredRows(nil) != nil || number(nil) != 0 || text(1) != "" || listOf("x") != nil {
		t.Error("a missing or wrongly typed value did not read as empty")
	}
	if got := plain([]report.Layer{{ID: "x", Details: map[string]any{"bad": make(chan int)}}}); got[0].ID != "x" {
		t.Errorf("a layer whose details cannot be marshalled was lost: %+v", got)
	}
}

// A number above 2^53 does not survive a float64. The tests layer's shuffle seed is
// an int64 of 19 digits, and a failing run is replayed from it with -test.shuffle: a
// ratchet that rewrote it would leave the report with a seed that replays another run.
const bigSeed = int64(1768300000000000123)

func layersWithSeed() []report.Layer {
	pass := map[string]any{"outcome": "pass"}
	return []report.Layer{
		{ID: "hygiene", Tier: 1, Outcome: "fail", Reason: "x", Details: map[string]any{
			"golangci_lint": "v2.15.0", "findings": 3,
			"tidy": pass, "verify": pass, "toolchain": pass, "format": pass, "vulnerabilities": pass, "licenses": pass,
			"lint": map[string]any{"outcome": "fail", "message": "3 finding(s)", "findings": 3, "by_linter": map[string]any{"revive": 3}},
		}},
		{ID: "tests", Tier: 1, Outcome: "pass", Details: map[string]any{
			"packages": 3, "tests": 12, "race": true, "skipped": 0, "shuffle_seed": bigSeed,
		}},
		{ID: "coverage", Tier: 1, Outcome: "fail", Reason: "floor", Details: map[string]any{
			"statements_pct": 91.5, "floor": 92.0, "profile_bytes": int64(1) << 60,
		}},
		crapLayerWithRows(typedRows(), 1),
	}
}

func TestApplyReturnsTheLayersAsTheyRanWithOnlyTheVerdictChanged(t *testing.T) {
	in := layersWithSeed()
	ledger := &Ledger{Findings: Counts{"revive": {"x.go": 3}}, Functions: map[string]float64{"example.com/m.Scored": 14.5}}
	out := Apply(in, Judgment{Outcome: Pass}, ledger)
	// The layers the ratchet does not judge are the layers that ran, value for value.
	if !reflect.DeepEqual(out[1], in[1]) {
		t.Errorf("tests layer changed: %+v, ran as %+v", out[1], in[1])
	}
	raw, err := json.Marshal(out[1])
	if err != nil || !strings.Contains(string(raw), `"shuffle_seed":1768300000000000123`) {
		t.Errorf("the shuffle seed is not byte-identical: %s (%v)", raw, err)
	}
	// A layer the ratchet judges keeps every detail it does not judge, in its own type.
	if cov := out[2]; cov.Details["profile_bytes"] != int64(1)<<60 || cov.Details["statements_pct"] != 91.5 {
		t.Errorf("coverage details rewritten: %v", cov.Details)
	}
	crapOut := out[3]
	if crapOut.Details["not_measured"] != 1 || crapOut.Details["cap"] != 10.0 {
		t.Errorf("crap details outside the rows rewritten: %v", crapOut.Details)
	}
	if hy := out[0]; hy.Details["findings"] != 3 || hy.Details["golangci_lint"] != "v2.15.0" {
		t.Errorf("hygiene details outside the lint check rewritten: %v", hy.Details)
	}
	// The input is untouched.
	if in[3].Outcome != "fail" || !reflect.DeepEqual(in[1].Details["shuffle_seed"], bigSeed) {
		t.Error("the input layers changed")
	}
}

// The same holds for layers a stored report gives back: whatever a layer carries,
// the ratchet adds no rounding of its own, so a layer it does not judge comes out
// equal to the one that went in. (A stored report's own float64 has rounded a large
// number already; that is the reader's, and not the ratchet's, to keep exact.)
func TestApplyLeavesALayerFromAStoredReportEqual(t *testing.T) {
	stored := roundTripped(t, layersWithSeed())
	out := Apply(stored, Judgment{Outcome: Pass}, &Ledger{})
	if !reflect.DeepEqual(out[1], stored[1]) {
		t.Errorf("tests layer changed: %+v, was %+v", out[1], stored[1])
	}
}

func TestNumberReadsEveryForm(t *testing.T) {
	for _, v := range []any{7, 7.0, json.Number("7"), json.Number("7.0")} {
		if got := number(v); got != 7 {
			t.Errorf("number(%T %v) = %d, want 7", v, v, got)
		}
	}
	if number(json.Number("x")) != 0 || number("7") != 0 {
		t.Error("a non-number read as a number")
	}
}

func TestHoldsFeedsNeedsEveryFeedingLayer(t *testing.T) {
	all := []report.Layer{{ID: "hygiene"}, {ID: "tests"}, {ID: "coverage"}, {ID: "crap"}, {ID: "mutation"}}
	if !holdsFeeds(all) {
		t.Error("the full set was refused")
	}
	for _, drop := range []string{"hygiene", "tests", "coverage", "crap"} {
		var some []report.Layer
		for _, l := range all {
			if l.ID != drop {
				some = append(some, l)
			}
		}
		if holdsFeeds(some) {
			t.Errorf("a run without %s was treated as whole", drop)
		}
	}
	if holdsFeeds(nil) {
		t.Error("a run with no layers was treated as whole")
	}
}
