package laws

import (
	"testing"

	"github.com/Hedronite/go-tops/internal/lawpass"
)

func TestJudgeG11DoesNotMergeOtherTools(t *testing.T) {
	m := byID(Judge(Input{
		Class: "binary-cli", MaxTier: 1, InRange: true, Reached: true, Required: cliRequired,
		Evidence: Evidence{
			LintPassOK:  true,
			Lint:        lawpass.LintResult{ByLaw: map[string]int{"G-02": 0}},
			AnalyzersOK: true,
			Analyzers:   map[string]int{"G-11": 5},
			Hygiene:     HygieneEvidence{Ran: true, TidyPass: true, VerifyPass: true, VulnsPass: true},
		},
	}))
	if m["G-02"].Verdict != Allow {
		t.Fatalf("G-02=%+v, analyzer G-11 count must not affect G-02", m["G-02"])
	}
}

func TestJudgeG11CleanWhenBothZero(t *testing.T) {
	m := byID(Judge(Input{
		Class: "binary-cli", MaxTier: 1, InRange: true, Reached: true, Required: cliRequired,
		Evidence: Evidence{
			LintPassOK:  true,
			Lint:        lawpass.LintResult{ByLaw: map[string]int{"G-11": 0}},
			AnalyzersOK: true,
			Analyzers:   map[string]int{"G-11": 0},
			Hygiene:     HygieneEvidence{Ran: true, TidyPass: true, VerifyPass: true, VulnsPass: true},
		},
	}))
	if m["G-11"].Verdict != Allow {
		t.Fatalf("G-11=%+v", m["G-11"])
	}
}
