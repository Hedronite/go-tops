package gate

import (
	"context"
	"testing"
)

func TestFillDefaultDepsWiresToolResolver(t *testing.T) {
	d := Deps{Dir: t.TempDir()}
	fillDefaultDeps(&d)
	if d.Exec == nil || d.Tools == nil || d.Laws == nil || len(d.Layers) == 0 {
		t.Fatalf("deps not filled: %+v", d)
	}
}

func TestLawEvidenceSkipsWhenStopped(t *testing.T) {
	d := deps(nil)
	ev, err := lawEvidence(context.Background(), d, &Env{Dir: "."}, cfg(), Options{MaxTier: 1}, false, true, nil)
	if err != nil || ev.LintPassOK {
		t.Fatalf("ev=%+v err=%v", ev, err)
	}
}
