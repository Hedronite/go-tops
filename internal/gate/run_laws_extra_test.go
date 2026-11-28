package gate

import (
	"context"
	"errors"
	"testing"

	"github.com/Hedronite/go-tops/internal/lawpass"
	"github.com/Hedronite/go-tops/internal/laws"
)

func TestLawEvidenceScanAcksErrorFails(t *testing.T) {
	d := deps(nil)
	d.ScanAcks = func(string, string) (laws.FileCounts, error) {
		return nil, errors.New("broken acks")
	}
	_, err := lawEvidence(context.Background(), d, &Env{Dir: "."}, cfg(), Options{MaxTier: 1}, true, false, nil)
	if err == nil {
		t.Fatal("want error from ScanAcks")
	}
}

func TestLawEvidenceStoppedSkipsRefusals(t *testing.T) {
	d := deps(nil)
	ev, err := lawEvidence(context.Background(), d, &Env{Dir: "."}, cfg(), Options{MaxTier: 1}, true, true, nil)
	if err != nil || ev.LintPassOK || len(ev.Refusals) != 0 {
		t.Fatalf("ev=%+v err=%v", ev, err)
	}
}

func TestLawEvidenceSkipsRefusalsWhenLintFails(t *testing.T) {
	d := deps(nil)
	d.LawPass = func(context.Context, *Env) (lawpass.LintResult, error) {
		return lawpass.LintResult{}, errors.New("lint broke")
	}
	ev, err := lawEvidence(context.Background(), d, &Env{Dir: "."}, cfg(), Options{MaxTier: 1}, true, false, nil)
	if err != nil || ev.LintPassOK || ev.Refusals != nil {
		t.Fatalf("ev=%+v err=%v", ev, err)
	}
}

func TestLawEvidenceSkipsRefusalsBelowTierOne(t *testing.T) {
	d := deps(nil)
	ev, err := lawEvidence(context.Background(), d, &Env{Dir: "."}, cfg(), Options{MaxTier: 0}, true, false, nil)
	if err != nil || len(ev.Refusals) != 0 {
		t.Fatalf("ev=%+v err=%v", ev, err)
	}
}

func TestLawEvidenceAttachesRefusals(t *testing.T) {
	d := deps(nil)
	d.LawPass = func(context.Context, *Env) (lawpass.LintResult, error) {
		return lawpass.LintResult{
			ByLaw: map[string]int{"G-12": 1},
			Issues: []lawpass.LintIssue{{
				FromLinter: "errcheck",
				Pos: struct {
					Filename string
					Line     int
				}{Filename: "a.go", Line: 1},
			}},
		}, nil
	}
	ev, err := lawEvidence(context.Background(), d, &Env{Dir: "."}, cfg(), Options{MaxTier: 1}, true, false, nil)
	if err != nil || !ev.LintPassOK {
		t.Fatalf("ev=%+v err=%v", ev, err)
	}
	if ev.Refusals["G-12"]["a.go"] != 1 {
		t.Fatalf("refusals=%+v", ev.Refusals)
	}
}

func TestLawEvidenceCollectsAcks(t *testing.T) {
	want := laws.FileCounts{"G-12": {"a.go": 1}}
	d := deps(nil)
	d.ScanAcks = func(string, string) (laws.FileCounts, error) { return want, nil }
	ev, err := lawEvidence(context.Background(), d, &Env{Dir: "."}, cfg(), Options{MaxTier: 1}, true, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Acks["G-12"]["a.go"] != 1 {
		t.Fatalf("acks=%+v", ev.Acks)
	}
}
