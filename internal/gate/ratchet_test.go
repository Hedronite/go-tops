package gate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/report"
)

// The ratchet hook (§6.7): under enforce: ratchet the runner asks it to judge
// the run, puts its object in the report, and fails the run when it says so.

var (
	ledgerBuild = report.Build{GOOS: "linux", GOARCH: "amd64", Tags: []string{}, CgoEnabled: true}
	otherBuild  = report.Build{GOOS: "darwin", GOARCH: "arm64", Tags: []string{}, CgoEnabled: true}
)

func ratchetConfig(enforce string) Config {
	c := cfg()
	c.Enforce = enforce
	if enforce == "advisory" {
		c.AdvisoryExpires = "2027-03-31"
	}
	return c
}

// spy is a RatchetJudge that records whether it was called.
type spy struct {
	called  bool
	verdict *RatchetVerdict
	err     error
	seen    []report.Layer
}

func (s *spy) judge(_ context.Context, _ *Env, layers []report.Layer) (*RatchetVerdict, error) {
	s.called = true
	s.seen = layers
	return s.verdict, s.err
}

func passVerdict(layers []report.Layer) *RatchetVerdict {
	return &RatchetVerdict{Report: &report.Ratchet{Outcome: "pass", LedgerBuild: ledgerBuild, RunBuild: ledgerBuild}, Layers: layers}
}

// Under hard and advisory there is no ledger to judge against: the hook is not
// asked, and the report has no ratchet object.
func TestRatchetIsNotJudgedUnderHardOrAdvisory(t *testing.T) {
	for _, enforce := range []string{"hard", "advisory"} {
		s := &spy{verdict: passVerdict(nil)}
		d := deps(gateLayers(nil, nil))
		d.Ratchet = s.judge
		r := mustRun(t, ratchetConfig(enforce), opts(), d)
		if s.called || r.Report.Ratchet != nil {
			t.Errorf("%s: judged %v, ratchet object %v; want neither", enforce, s.called, r.Report.Ratchet)
		}
	}
}

func TestRatchetObjectIsReported(t *testing.T) {
	ls := gateLayers(nil, nil)
	s := &spy{}
	d := deps(ls)
	d.Ratchet = func(ctx context.Context, env *Env, layers []report.Layer) (*RatchetVerdict, error) {
		s.verdict = passVerdict(layers)
		return s.judge(ctx, env, layers)
	}
	r := mustRun(t, ratchetConfig("ratchet"), opts(), d)
	if !s.called {
		t.Fatal("the ratchet was not judged")
	}
	got := r.Report.Ratchet
	if got == nil || got.Outcome != "pass" || got.LedgerBuild.GOOS != "linux" || got.RunBuild.GOOS != "linux" {
		t.Fatalf("ratchet object %+v", got)
	}
	if len(s.seen) == 0 || s.seen[0].ID != "hygiene" {
		t.Errorf("the hook saw layers %v, want the run's layers in order", s.seen)
	}
}

// A leg that is not the ledger leg skips, and the report says which build it
// skips for; the skip alone does not fail the run.
func TestRatchetSkipNamesBothBuilds(t *testing.T) {
	s := &spy{verdict: &RatchetVerdict{Report: &report.Ratchet{Outcome: "skip", LedgerBuild: ledgerBuild, RunBuild: otherBuild}}}
	s.verdict.Layers = nil
	d := deps(gateLayers(nil, nil))
	d.Ratchet = func(ctx context.Context, env *Env, layers []report.Layer) (*RatchetVerdict, error) {
		s.verdict.Layers = layers
		return s.judge(ctx, env, layers)
	}
	r := mustRun(t, ratchetConfig("ratchet"), opts(), d)
	if r.Report.Ratchet.Outcome != "skip" || r.ExitCode != 0 && len(r.Report.Summary.Failed) == 1 && r.Report.Summary.Failed[0] != "laws" {
		t.Errorf("ratchet %+v exit %d failed %v", r.Report.Ratchet, r.ExitCode, r.Report.Summary.Failed)
	}
	want := "ratchet      skip  (ledger build linux/amd64 cgo, this run darwin/arm64 cgo)"
	if !strings.Contains(r.Text, want) {
		t.Errorf("the text does not name the builds: want %q in\n%s", want, r.Text)
	}
}

func TestRatchetFailureFailsTheRun(t *testing.T) {
	d := deps(gateLayers(nil, nil))
	d.Ratchet = func(_ context.Context, _ *Env, layers []report.Layer) (*RatchetVerdict, error) {
		v := passVerdict(layers)
		v.Report.Outcome, v.Failed = "fail", true
		v.Notes = []string{"ratchet: functions p.F: scores 22.00, over its ledger entry 21.00 plus 0.01\n"}
		return v, nil
	}
	c := ratchetConfig("ratchet")
	c.RequiredLaws = nil
	r := mustRun(t, c, opts(), d)
	if r.ExitCode != ExitFail || !contains(r.Report.Summary.Failed, "ratchet") {
		t.Errorf("exit %d failed %v, want a failure named ratchet", r.ExitCode, r.Report.Summary.Failed)
	}
	if !strings.Contains(r.Text, "scores 22.00, over its ledger entry") {
		t.Errorf("the text does not say what failed:\n%s", r.Text)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// The layers the verdict returns are the ones reported: under ratchet a crap
// layer the ledger covers is a pass.
func TestRatchetVerdictReplacesTheLayers(t *testing.T) {
	d := deps(gateLayers(nil, map[string]Outcome{"crap": Fail}))
	d.Ratchet = func(_ context.Context, _ *Env, layers []report.Layer) (*RatchetVerdict, error) {
		out := make([]report.Layer, len(layers))
		copy(out, layers)
		for i := range out {
			if out[i].ID == "crap" {
				out[i].Outcome, out[i].Reason = "pass", "within the ledger"
			}
		}
		return passVerdict(out), nil
	}
	c := ratchetConfig("ratchet")
	c.RequiredLaws = nil
	r := mustRun(t, c, opts(), d)
	if got := layerOutcomes(r)["crap"]; got != "pass" {
		t.Errorf("crap %s, want pass", got)
	}
	if r.ExitCode != ExitOK {
		t.Errorf("exit %d, want 0: the only failure was covered by the ledger", r.ExitCode)
	}
}

// A judge that cannot judge fails the run, and the run still reports: the layers
// as they ran, no ratchet object, the reason in the text.
func TestRatchetFaultFailsTheRunAndStillReports(t *testing.T) {
	d := deps(gateLayers(nil, nil))
	d.Ratchet = func(context.Context, *Env, []report.Layer) (*RatchetVerdict, error) {
		return nil, errors.New("git cannot run")
	}
	c := ratchetConfig("ratchet")
	c.RequiredLaws = nil
	r := mustRun(t, c, opts(), d)
	if r.ExitCode != ExitFail || !contains(r.Report.Summary.Failed, "ratchet") || r.Report.Ratchet != nil {
		t.Errorf("exit %d failed %v ratchet %v, want a failure with no ratchet object", r.ExitCode, r.Report.Summary.Failed, r.Report.Ratchet)
	}
	if !strings.Contains(r.Text, "ratchet: git cannot run") || len(r.Report.Layers) == 0 {
		t.Errorf("the run did not report:\n%s", r.Text)
	}
}

// The default judge is the one the command wiring sets, and Deps wins over it.
func TestRatchetDefaultJudgeIsUsedWhenDepsNameNone(t *testing.T) {
	s := &spy{verdict: passVerdict(nil)}
	old := DefaultRatchet
	DefaultRatchet = s.judge
	t.Cleanup(func() { DefaultRatchet = old })
	mustRun(t, ratchetConfig("ratchet"), opts(), deps(gateLayers(nil, nil)))
	if !s.called {
		t.Error("the default judge was not asked")
	}
	own := &spy{verdict: passVerdict(nil)}
	s.called = false
	d := deps(gateLayers(nil, nil))
	d.Ratchet = own.judge
	mustRun(t, ratchetConfig("ratchet"), opts(), d)
	if s.called || !own.called {
		t.Errorf("default asked %v, the run's own asked %v: the run's own judge wins", s.called, own.called)
	}
}

// A run that stopped before its layers ran cannot be judged against the ledger.
func TestRatchetIsNotJudgedWhenTheRunStopped(t *testing.T) {
	s := &spy{verdict: passVerdict(nil)}
	d := deps(gateLayers(nil, map[string]Outcome{"hygiene": Fail}))
	d.Ratchet = s.judge
	o := opts()
	o.FailFast = true
	mustRun(t, ratchetConfig("ratchet"), o, d)
	if s.called {
		t.Error("the ratchet judged a run that --fail-fast stopped")
	}
}

// With no hook wired, a ratchet run has no ratchet object: nothing is claimed.
func TestRatchetWithNoHookReportsNothing(t *testing.T) {
	r := mustRun(t, ratchetConfig("ratchet"), opts(), deps(gateLayers(nil, nil)))
	if r.Report.Ratchet != nil {
		t.Errorf("ratchet object %+v with no judge", r.Report.Ratchet)
	}
}
