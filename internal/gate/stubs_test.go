package gate

import (
	"context"
	"strings"
	"testing"
)

// §3.3, §7: crap, mutation and every class layer are registered; enabled with
// no command each is not-run (exit 1), disabled each is skip with its reason.
func TestStubsAreRegisteredInGateOrder(t *testing.T) {
	var got []string
	tiers := map[string]int{}
	for _, l := range Layers() {
		got = append(got, l.ID())
		tiers[l.ID()] = l.Tier()
	}
	want := "hygiene,tests,coverage,crap,mutation,fuzz,stress,bench"
	if strings.Join(got, ",") != want {
		t.Fatalf("layers %v, want %s", got, want)
	}
	for _, id := range []string{"hygiene", "tests", "coverage", "crap", "mutation"} {
		if tiers[id] != 1 {
			t.Errorf("%s is tier %d, want 1 (§3.2)", id, tiers[id])
		}
	}
	for _, id := range []string{"fuzz", "stress", "bench"} {
		if tiers[id] != 2 {
			t.Errorf("%s is tier %d, want 2 (§7)", id, tiers[id])
		}
	}
}

func TestEnabledStubsAreNotRunAndFail(t *testing.T) {
	c := cfg()
	c.RequiredLaws = nil
	d := deps(nil)
	d.Layers = nil
	r := mustRun(t, c, Options{MaxTier: 2}, d)
	o := layerOutcomes(r)
	for _, id := range []string{"hygiene", "tests", "coverage", "crap", "mutation", "fuzz", "stress", "bench"} {
		// coverage, crap and mutation need a layer that never passes
		want := "not-run"
		if id == "coverage" || id == "crap" || id == "mutation" {
			want = "not-reached"
		}
		if o[id] != want {
			t.Errorf("%s is %s, want %s", id, o[id], want)
		}
	}
	if r.ExitCode != 1 || r.Report.LayerCounts.EnabledInRange != 8 || r.Report.LayerCounts.Ran != 0 {
		t.Fatalf("exit %d counts %+v", r.ExitCode, r.Report.LayerCounts)
	}
}

// A stub reached on its own is not-run, not not-reached: --only crap pulls in
// its needs, which are stubs, so they fail first.
func TestDisabledStubsAreSkipWithReason(t *testing.T) {
	c := cfg()
	c.RequiredLaws = nil
	c.Layers = map[string]LayerConfig{
		"crap":     {Enabled: false, Reason: "lands at M2", Expires: "2027-03-01"},
		"mutation": {Enabled: false, Reason: "lands at M4", Expires: "2027-06-01"},
		"fuzz":     {Enabled: false, Reason: "binary-cli has no fuzz targets yet"},
		"stress":   {Enabled: false, Reason: "binary-cli does not stress"},
		"bench":    {Enabled: false, Reason: "binary-cli does not gate benchmarks"},
	}
	r := mustRun(t, c, Options{MaxTier: 2}, deps(nil))
	for _, l := range r.Report.Layers {
		switch l.ID {
		case "crap", "mutation", "fuzz", "stress", "bench":
			if l.Outcome != "skip" || l.Reason == "" {
				t.Errorf("%s: %+v, want skip with a reason", l.ID, l)
			}
		}
	}
	if !strings.Contains(r.Text, "lands at M4") {
		t.Errorf("text hides the reason:\n%s", r.Text)
	}
}

// A real layer registered under a stub's ID replaces it (G4 and G5 do this).
func TestRegisterReplacesAStub(t *testing.T) {
	mu.Lock()
	saved := append([]Layer(nil), registry...)
	mu.Unlock()
	defer func() {
		mu.Lock()
		registry = saved
		mu.Unlock()
	}()
	Register(&fakeLayer{id: "hygiene", tier: 1, res: Result{Outcome: Pass}})
	n := 0
	for _, l := range Layers() {
		if l.ID() == "hygiene" {
			n++
			if _, isStub := l.(stub); isStub {
				t.Fatal("the stub was not replaced")
			}
		}
	}
	if n != 1 {
		t.Fatalf("%d hygiene layers registered", n)
	}
}

func TestStubRunIsNotRun(t *testing.T) {
	for _, l := range Layers() {
		if r := l.Run(context.Background(), &Env{}); r.Outcome != NotRun || r.Reason == "" {
			t.Errorf("%s: %+v", l.ID(), r)
		}
	}
}
