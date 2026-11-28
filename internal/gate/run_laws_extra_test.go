package gate

import (
	"context"
	"errors"
	"testing"

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
