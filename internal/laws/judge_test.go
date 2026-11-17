package laws

import "testing"

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

// §3.3: with no reader, every required law is not-evaluated with reason
// no_reader. Nothing is ever allow.
func TestJudgeStubNeverAllows(t *testing.T) {
	rs := Judge(Input{Class: "binary-cli", MaxTier: 1, InRange: true, Reached: true, Required: cliRequired})
	if len(rs) != 19 {
		t.Fatalf("%d results, want the 19 laws that apply to binary-cli", len(rs))
	}
	for _, r := range rs {
		if r.Verdict != NotEvaluated || r.Reason != NoReader || r.OutOfRange {
			t.Errorf("%s = %+v, want not-evaluated / no_reader, in range", r.ID, r)
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
	m := byID(Judge(Input{Class: "binary-cli", MaxTier: 0, InRange: true, Reached: true, Required: cliRequired}))
	for _, id := range []string{"G-02", "G-07", "G-11", "G-12", "G-16", "G-18", "G-34", "G-37", "G-40"} {
		if r := m[id]; !r.OutOfRange || r.Reason != InputMissing || r.Verdict != NotEvaluated {
			t.Errorf("%s = %+v, want out of range / input_missing", id, r)
		}
	}
	for _, id := range []string{"G-08", "G-17", "G-19", "G-25", "G-26", "G-33", "G-38"} {
		if r := m[id]; r.OutOfRange || r.Reason != NoReader {
			t.Errorf("%s = %+v, want in range / no_reader", id, r)
		}
	}
}

// The default range (tier 1) leaves only tier 3 laws out of range.
func TestJudgeTierThreeIsOutOfRangeAtTierOne(t *testing.T) {
	m := byID(Judge(Input{Class: "library-core", MaxTier: 1, InRange: true, Reached: true, Required: cliRequired}))
	if r := m["G-22"]; !r.OutOfRange || r.Reason != InputMissing {
		t.Errorf("G-22 = %+v, want out of range", r)
	}
	if r := m["G-21"]; r.OutOfRange {
		t.Errorf("G-21 = %+v, want in range", r)
	}
}

// §3.5: --only that leaves the layer out puts every law out of range.
func TestJudgeOnlyLeavesLawsOut(t *testing.T) {
	for _, r := range Judge(Input{Class: "binary-cli", MaxTier: 1, InRange: false, Reached: false, Required: cliRequired}) {
		if !r.OutOfRange || r.Reason != InputMissing {
			t.Errorf("%s = %+v, want out of range / input_missing", r.ID, r)
		}
	}
}

// --fail-fast: the layer was in range but never reached.
func TestJudgeNotReached(t *testing.T) {
	for _, r := range Judge(Input{Class: "binary-cli", MaxTier: 1, InRange: true, Reached: false, Required: cliRequired}) {
		if r.OutOfRange || r.Reason != InputMissing || r.Verdict != NotEvaluated {
			t.Errorf("%s = %+v, want in range / input_missing", r.ID, r)
		}
	}
}
