package ratchet

import (
	"testing"

	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/report"
)

func TestAttachRatchetLawsSkipsZeroCounts(t *testing.T) {
	var rep report.Report
	attachRatchetLaws(&rep, &gate.Env{})
	if rep.Laws.ApplicableJudged != 0 {
		t.Fatalf("Laws = %+v, want zero value", rep.Laws)
	}
	env := &gate.Env{RatchetLaws: report.Laws{ApplicableJudged: 0, Judged: 5}}
	attachRatchetLaws(&rep, env)
	if rep.Laws.Judged != 0 || rep.Laws.ApplicableJudged != 0 {
		t.Fatalf("Laws = %+v, must not attach when ApplicableJudged is 0", rep.Laws)
	}
	attachRatchetLaws(&rep, nil)
}

func TestAttachRatchetLawsCopiesPositiveCounts(t *testing.T) {
	var rep report.Report
	want := report.Laws{ApplicableJudged: 5, Judged: 5, Required: 16}
	attachRatchetLaws(&rep, &gate.Env{RatchetLaws: want})
	if rep.Laws.ApplicableJudged != want.ApplicableJudged {
		t.Fatalf("ApplicableJudged = %d, want %d", rep.Laws.ApplicableJudged, want.ApplicableJudged)
	}
}
