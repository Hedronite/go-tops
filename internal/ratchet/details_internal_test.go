package ratchet

import (
	"encoding/json"
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
