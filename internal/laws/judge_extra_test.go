package laws

import (
	"testing"

	"github.com/Hedronite/go-tops/internal/lawpass"
)

func TestJudgeAnalyzerInputMissing(t *testing.T) {
	m := byID(Judge(Input{
		Class: "binary-cli", MaxTier: 1, InRange: true, Reached: true, Required: cliRequired,
		Evidence: Evidence{AnalyzersOK: false},
	}))
	if m["G-17"].Reason != InputMissing || m["G-17"].Verdict != NotEvaluated {
		t.Fatalf("G-17=%+v", m["G-17"])
	}
}

func TestJudgeAnalyzerAllowClean(t *testing.T) {
	m := byID(Judge(Input{
		Class: "binary-cli", MaxTier: 0, InRange: true, Reached: true, Required: cliRequired,
		Evidence: Evidence{AnalyzersOK: true, Analyzers: map[string]int{"G-19": 0}},
	}))
	if m["G-19"].Verdict != Allow {
		t.Fatalf("G-19=%+v", m["G-19"])
	}
}

func TestJudgeG11AnalyzersOnly(t *testing.T) {
	m := byID(Judge(Input{
		Class: "binary-cli", MaxTier: 1, InRange: true, Reached: true, Required: cliRequired,
		Evidence: Evidence{
			LintPassOK:  true,
			Lint:        lawpass.LintResult{ByLaw: map[string]int{"G-11": 0}},
			AnalyzersOK: true,
			Analyzers:   map[string]int{"G-11": 1},
			Hygiene:     HygieneEvidence{Ran: true, TidyPass: true, VerifyPass: true, VulnsPass: true},
		},
	}))
	if m["G-11"].Verdict != Refuse {
		t.Fatalf("G-11=%+v want refuse when analyzer count > 0", m["G-11"])
	}
}

func TestJudgeG11LintOnlyNoAnalyzerMerge(t *testing.T) {
	m := byID(Judge(Input{
		Class: "binary-cli", MaxTier: 1, InRange: true, Reached: true, Required: cliRequired,
		Evidence: Evidence{
			LintPassOK: true,
			Lint:       lawpass.LintResult{ByLaw: map[string]int{"G-11": 2}},
			Hygiene:    HygieneEvidence{Ran: true, TidyPass: true, VerifyPass: true, VulnsPass: true},
		},
	}))
	if m["G-11"].Verdict != Refuse {
		t.Fatalf("G-11=%+v", m["G-11"])
	}
}

func TestJudgeToolAllowZero(t *testing.T) {
	m := byID(Judge(Input{
		Class: "binary-cli", MaxTier: 1, InRange: true, Reached: true, Required: cliRequired,
		Evidence: Evidence{
			LintPassOK: true,
			Lint:       lawpass.LintResult{ByLaw: map[string]int{"G-02": 0}},
			Hygiene:    HygieneEvidence{Ran: true, TidyPass: true, VerifyPass: true, VulnsPass: true},
		},
	}))
	if m["G-02"].Verdict != Allow {
		t.Fatalf("G-02=%+v", m["G-02"])
	}
}
