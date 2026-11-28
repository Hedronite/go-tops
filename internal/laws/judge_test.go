package laws

import (
	"testing"

	"github.com/Hedronite/go-tops/internal/lawpass"
)

var cliRequired = func() []string {
	var out []string
	for _, l := range Catalog() {
		if l.V01 {
			out = append(out, l.ID)
		}
	}
	return out
}()

func byID(rs []Result) map[string]Result {
	m := map[string]Result{}
	for _, r := range rs {
		m[r.ID] = r
	}
	return m
}

// Without lint-laws.json, tool laws are input_missing; analyzers stay no_reader.
func TestJudgeToolLawsNeedLintPass(t *testing.T) {
	rs := Judge(Input{Evidence: Evidence{}, Class: "binary-cli", MaxTier: 1, InRange: true, Reached: true, Required: cliRequired})
	if len(rs) != 19 {
		t.Fatalf("%d results, want the 19 laws that apply to binary-cli", len(rs))
	}
	analyzer := map[string]bool{"G-08": true, "G-17": true, "G-19": true, "G-25": true, "G-26": true, "G-33": true, "G-38": true}
	for _, r := range rs {
		want := NoReader
		if lawpass.IsToolLaw(r.ID) || analyzer[r.ID] || r.ID == "G-34" || r.ID == "G-40" {
			want = InputMissing
		}
		if r.Verdict != NotEvaluated || r.Reason != want || r.OutOfRange {
			t.Errorf("%s = %+v, want not-evaluated, in range", r.ID, r)
		}
	}
	m := byID(rs)
	for _, id := range cliRequired {
		if !m[id].Required {
			t.Errorf("%s should be marked required", id)
		}
	}
	if m["G-41"].Required {
		t.Error("G-41 is not in laws.required")
	}
}

// §3.2: at --max-tier 0 the tool-reader laws are out of range, listed as
// not-evaluated with input_missing; the analyzer laws are in range.
func TestJudgeMaxTierZero(t *testing.T) {
	clean := Evidence{AnalyzersOK: true, Analyzers: map[string]int{}}
	m := byID(Judge(Input{Evidence: clean, Class: "binary-cli", MaxTier: 0, InRange: true, Reached: true, Required: cliRequired}))
	for _, id := range []string{"G-02", "G-07", "G-11", "G-12", "G-16", "G-18", "G-34", "G-37", "G-40"} {
		if r := m[id]; !r.OutOfRange || r.Reason != InputMissing || r.Verdict != NotEvaluated {
			t.Errorf("%s = %+v, want out of range / input_missing", id, r)
		}
	}
	for _, id := range []string{"G-08", "G-17", "G-19", "G-25", "G-26", "G-33", "G-38"} {
		if r := m[id]; r.OutOfRange || r.Verdict != Allow {
			t.Errorf("%s = %+v, want in range / allow with clean analyzers", id, r)
		}
	}
}

func TestVerdictForAllowAtZeroInstances(t *testing.T) {
	if verdictFor("G-11", Evidence{}, 0) != Allow {
		t.Fatal("want allow at zero")
	}
}

func TestVerdictForAckWhenLedgerCovers(t *testing.T) {
	ev := Evidence{
		Refusals: map[string]map[string]int{"G-30": {"a.go": 1}},
		Acks:     map[string]map[string]int{"G-30": {"a.go": 1}},
	}
	if verdictFor("G-30", ev, 1) != Ack {
		t.Fatal("want ack when counts covered")
	}
}

func TestLawSeverityUnknownIDIsRefuse(t *testing.T) {
	if lawSeverity("G-999") != SeverityR {
		t.Fatal("want refuse severity for unknown catalog id")
	}
}

func TestJudgeAnalyzerEvidence(t *testing.T) {
	m := byID(Judge(Input{
		Class: "binary-cli", MaxTier: 0, InRange: true, Reached: true, Required: cliRequired,
		Evidence: Evidence{AnalyzersOK: true, Analyzers: map[string]int{"G-17": 2}},
	}))
	if m["G-17"].Verdict != Refuse {
		t.Fatalf("G-17=%+v want refuse", m["G-17"])
	}
}

func TestJudgeG11MergesErrmatch(t *testing.T) {
	m := byID(Judge(Input{
		Class: "binary-cli", MaxTier: 1, InRange: true, Reached: true, Required: cliRequired,
		Evidence: Evidence{
			LintPassOK:  true,
			Lint:        lawpass.LintResult{ByLaw: map[string]int{"G-11": 1}},
			AnalyzersOK: true,
			Analyzers:   map[string]int{"G-11": 2},
			Hygiene:     HygieneEvidence{Ran: true, TidyPass: true, VerifyPass: true, VulnsPass: true},
		},
	}))
	if m["G-11"].Verdict != Refuse {
		t.Fatalf("G-11=%+v want refuse when lint+analyzer counts > 0", m["G-11"])
	}
}

// The default range (tier 1) leaves only tier 3 laws out of range.
func TestJudgeTierThreeIsOutOfRangeAtTierOne(t *testing.T) {
	m := byID(Judge(Input{Evidence: Evidence{}, Class: "library-core", MaxTier: 1, InRange: true, Reached: true, Required: cliRequired}))
	if r := m["G-22"]; !r.OutOfRange || r.Reason != InputMissing {
		t.Errorf("G-22 = %+v, want out of range", r)
	}
	if r := m["G-21"]; r.OutOfRange {
		t.Errorf("G-21 = %+v, want in range", r)
	}
}

// §3.5: --only that leaves the layer out puts every law out of range.
func TestJudgeOnlyLeavesLawsOut(t *testing.T) {
	for _, r := range Judge(Input{Evidence: Evidence{}, Class: "binary-cli", MaxTier: 1, InRange: false, Reached: false, Required: cliRequired}) {
		if !r.OutOfRange || r.Reason != InputMissing {
			t.Errorf("%s = %+v, want out of range / input_missing", r.ID, r)
		}
	}
}

// --fail-fast: the layer was in range but never reached.
func TestJudgeNotReached(t *testing.T) {
	for _, r := range Judge(Input{Evidence: Evidence{}, Class: "binary-cli", MaxTier: 1, InRange: true, Reached: false, Required: cliRequired}) {
		if r.OutOfRange || r.Reason != InputMissing || r.Verdict != NotEvaluated {
			t.Errorf("%s = %+v, want in range / input_missing", r.ID, r)
		}
	}
}

func TestJudgeHygieneAndToolEvidence(t *testing.T) {
	in := Input{
		Class: "binary-cli", MaxTier: 1, InRange: true, Reached: true, Required: cliRequired,
		Evidence: Evidence{
			LintPassOK: true,
			Lint:       lawpass.LintResult{ByLaw: map[string]int{"G-02": 0, "G-12": 2}},
			Hygiene: HygieneEvidence{
				Ran: true, TidyPass: true, VerifyPass: true, VulnsPass: true,
			},
		},
	}
	m := byID(Judge(in))
	if m["G-34"].Verdict != Allow || m["G-40"].Verdict != Allow {
		t.Fatalf("hygiene laws: G-34=%+v G-40=%+v", m["G-34"], m["G-40"])
	}
	if m["G-02"].Verdict != Allow {
		t.Fatalf("clean tool law G-02 = %+v", m["G-02"])
	}
	if m["G-12"].Verdict != Refuse {
		t.Fatalf("dirty tool law G-12 = %+v", m["G-12"])
	}
}

func TestJudgeHygieneNotRan(t *testing.T) {
	m := byID(Judge(Input{
		Class: "binary-cli", MaxTier: 1, InRange: true, Reached: true, Required: cliRequired,
		Evidence: Evidence{LintPassOK: true, Lint: lawpass.LintResult{ByLaw: map[string]int{}}},
	}))
	if m["G-34"].Reason != InputMissing || m["G-40"].Reason != InputMissing {
		t.Fatalf("G-34=%+v G-40=%+v", m["G-34"], m["G-40"])
	}
}

func TestJudgeToolWithoutLintPass(t *testing.T) {
	m := byID(Judge(Input{
		Class: "binary-cli", MaxTier: 1, InRange: true, Reached: true, Required: cliRequired,
		Evidence: Evidence{Hygiene: HygieneEvidence{Ran: true, TidyPass: true, VerifyPass: true, VulnsPass: true}},
	}))
	if m["G-02"].Reason != InputMissing {
		t.Fatalf("G-02 = %+v", m["G-02"])
	}
}

func TestJudgeHygieneRefusesWhenTidyFails(t *testing.T) {
	m := byID(Judge(Input{
		Class: "binary-cli", MaxTier: 1, InRange: true, Reached: true, Required: cliRequired,
		Evidence: Evidence{
			LintPassOK: true,
			Lint:       lawpass.LintResult{ByLaw: map[string]int{}},
			Hygiene: HygieneEvidence{
				Ran: true, TidyPass: false, VerifyPass: true, VulnsPass: true,
			},
		},
	}))
	if m["G-34"].Verdict != Refuse {
		t.Fatalf("G-34 = %+v, want refuse when tidy failed", m["G-34"])
	}
}
