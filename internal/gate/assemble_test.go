package gate

import (
	"testing"
	"time"

	"github.com/Hedronite/go-tops/internal/laws"
	"github.com/Hedronite/go-tops/internal/report"
)

func TestAssembleAdvisoryExitZeroDespiteFailures(t *testing.T) {
	layers := []report.Layer{{ID: "hygiene", Outcome: "fail"}}
	cfg := Config{Module: "example.com/m", Class: "binary-cli", Enforce: "advisory", AdvisoryExpires: "2027-03-31"}
	rep := assemble(cfg, Options{MaxTier: 1}, report.Git{}, time.Now(), time.Now(), "dev", "", layers, nil, &RatchetVerdict{})
	if rep.Summary.ExitCode != ExitOK || rep.Summary.Enforced {
		t.Fatalf("summary %+v", rep.Summary)
	}
	found := false
	for _, id := range rep.Summary.Failed {
		if id == "hygiene" {
			found = true
		}
	}
	if !found {
		t.Fatalf("failed %+v", rep.Summary.Failed)
	}
}

func TestAttachLawResultsCountsNoReader(t *testing.T) {
	rep := report.Report{}
	refused, partial := attachLawResults(&rep, []laws.Result{{
		ID: "G-41", Verdict: laws.NotEvaluated, Reason: laws.NoReader, Required: false,
	}})
	if refused || partial {
		t.Fatalf("refused=%v partial=%v", refused, partial)
	}
	if rep.Laws.NotEvaluatedByReason.NoReader != 1 {
		t.Fatalf("counts %+v", rep.Laws.NotEvaluatedByReason)
	}
}
