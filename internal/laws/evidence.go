package laws

import (
	"github.com/Hedronite/go-tops/internal/lawpass"
	"github.com/Hedronite/go-tops/internal/report"
)

// Evidence is what the laws layer read from the gate run.
type Evidence struct {
	// LintPassOK is true when lint-laws.json was written and parsed.
	LintPassOK bool
	Lint       lawpass.LintResult
	// Hygiene is set when the hygiene layer ran in this gate run.
	Hygiene HygieneEvidence
}

// HygieneEvidence holds gate-1 outcomes for G-34 and G-40.
type HygieneEvidence struct {
	Ran              bool
	TidyPass         bool
	VerifyPass       bool
	VulnsPass        bool
	HygieneLayerFail bool
}

// HygieneFromLayers reads hygiene step outcomes for law judgment.
func HygieneFromLayers(layers []report.Layer) HygieneEvidence {
	for _, l := range layers {
		if l.ID != "hygiene" {
			continue
		}
		ev := HygieneEvidence{Ran: true, HygieneLayerFail: l.Outcome == "fail"}
		if d := l.Details; d != nil {
			ev.TidyPass = subOutcomePass(d, "tidy")
			ev.VerifyPass = subOutcomePass(d, "verify")
			ev.VulnsPass = subOutcomePass(d, "vulnerabilities")
		}
		return ev
	}
	return HygieneEvidence{}
}

func subOutcomePass(details map[string]any, key string) bool {
	raw, ok := details[key]
	if !ok {
		return false
	}
	if raw == nil {
		return false
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	if m == nil {
		return false
	}
	out, ok := m["outcome"].(string)
	return ok && out == "pass"
}
