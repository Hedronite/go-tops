package gate

import (
	"errors"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/laws"
)

// §12.2 is tested row for row. A run that starts returns 0 or 1; codes 2, 3
// and 4 are decided before any layer runs, and then no layer ran and no
// report exists.

// ---- code 0 ----

// Every layer in range is pass, skip or vacuous and no law is required: exit 0, pass.
func TestExit0PassSkipVacuous(t *testing.T) {
	c := cfg()
	c.RequiredLaws = nil // nothing required, so the not-evaluated laws block nothing
	c.Layers = map[string]LayerConfig{"crap": {Enabled: false, Reason: "lands at M2", Expires: "2027-01-31"}}
	r := mustRun(t, c, opts(), deps(gateLayers(nil, map[string]Outcome{"mutation": Vacuous})))
	if r.ExitCode != 0 || r.Report.Summary.Outcome != "pass" {
		t.Fatalf("exit %d, outcome %s; want 0, pass\n%s", r.ExitCode, r.Report.Summary.Outcome, r.Text)
	}
	o := layerOutcomes(r)
	if o["crap"] != "skip" || o["mutation"] != "vacuous" || o["tests"] != "pass" {
		t.Fatalf("outcomes %v", o)
	}
}

// A run that leaves a required law out of range exits 0 and reports partial,
// never pass; its text says PARTIAL (§3.3).
func TestExit0PartialIsNeverPass(t *testing.T) {
	d := deps(gateLayers(nil, nil))
	d.Laws = analyzersAllow
	r := mustRun(t, cfg(), Options{MaxTier: 1, Only: "tests"}, d)
	if r.ExitCode != 0 {
		t.Fatalf("exit %d, want 0", r.ExitCode)
	}
	if r.Report.Summary.Outcome != "partial" {
		t.Fatalf("outcome %q, want partial", r.Report.Summary.Outcome)
	}
	if !strings.Contains(r.Text, "PARTIAL") {
		t.Errorf("text does not say PARTIAL:\n%s", r.Text)
	}
}

// A required law that is not-evaluated but out of range is not a failure.
func TestExit0RequiredLawOutOfRangeAtMaxTier0(t *testing.T) {
	d := deps(gateLayers(nil, nil))
	d.Laws = analyzersAllow
	r := mustRun(t, cfg(), Options{MaxTier: 0}, d)
	if r.ExitCode != 0 || r.Report.Summary.Outcome != "partial" {
		t.Fatalf("exit %d outcome %s, want 0 partial", r.ExitCode, r.Report.Summary.Outcome)
	}
}

// ---- code 1 ----

func TestExit1EachFailingOutcome(t *testing.T) {
	for _, o := range []Outcome{Fail, NotRun, ToolMissing} {
		c := cfg()
		c.RequiredLaws = nil
		r := mustRun(t, c, opts(), deps(gateLayers(nil, map[string]Outcome{"hygiene": o})))
		if r.ExitCode != 1 || r.Report.Summary.Outcome != "fail" {
			t.Errorf("hygiene %s: exit %d outcome %s, want 1 fail", o, r.ExitCode, r.Report.Summary.Outcome)
		}
		if !has(r.Report.Summary.Failed, "hygiene") {
			t.Errorf("hygiene %s: failed list %v", o, r.Report.Summary.Failed)
		}
	}
}

// A tool-missing layer exits 1, never another code (§12.2: "A missing tool has one code: 1").
func TestExit1MissingToolIsOne(t *testing.T) {
	c := cfg()
	c.RequiredLaws = nil
	ls := gateLayers(nil, nil)
	ls[0] = &fakeLayer{id: "hygiene", tier: 1, res: MissingTool("govulncheck", ErrToolMissing)}
	r := mustRun(t, c, opts(), deps(ls))
	if r.ExitCode != 1 || layerOutcomes(r)["hygiene"] != "tool-missing" {
		t.Fatalf("exit %d, hygiene %s", r.ExitCode, layerOutcomes(r)["hygiene"])
	}
}

func TestExit1NotReachedFails(t *testing.T) {
	c := cfg()
	c.RequiredLaws = nil
	r := mustRun(t, c, opts(), deps(gateLayers(nil, map[string]Outcome{"tests": Fail})))
	if r.ExitCode != 1 {
		t.Fatalf("exit %d", r.ExitCode)
	}
	o := layerOutcomes(r)
	if o["coverage"] != "not-reached" || o["crap"] != "not-reached" || o["mutation"] != "not-reached" {
		t.Fatalf("outcomes %v", o)
	}
}

// Any law is refuse: exit 1.
func TestExit1LawRefused(t *testing.T) {
	c := cfg()
	c.RequiredLaws = nil
	d := deps(gateLayers(nil, nil))
	d.Laws = func(in laws.Input) []laws.Result {
		return []laws.Result{{ID: "G-12", Verdict: laws.Refuse, Reader: laws.ReaderTool}, {ID: "G-02", Verdict: laws.Allow, Reader: laws.ReaderTool}}
	}
	r := mustRun(t, c, opts(), d)
	if r.ExitCode != 1 || !has(r.Report.Summary.Failed, "laws") {
		t.Fatalf("exit %d failed %v", r.ExitCode, r.Report.Summary.Failed)
	}
}

// ack and allow do not fail (§3.3).
func TestExit0LawAckAndAllow(t *testing.T) {
	c := cfg()
	c.RequiredLaws = []string{"G-02", "G-12"}
	d := deps(gateLayers(nil, nil))
	d.Laws = func(in laws.Input) []laws.Result {
		return []laws.Result{
			{ID: "G-12", Verdict: laws.Ack, Reader: laws.ReaderTool, Required: true},
			{ID: "G-02", Verdict: laws.Allow, Reader: laws.ReaderTool, Required: true},
		}
	}
	r := mustRun(t, c, opts(), d)
	if r.ExitCode != 0 || r.Report.Summary.Outcome != "pass" {
		t.Fatalf("exit %d outcome %s", r.ExitCode, r.Report.Summary.Outcome)
	}
}

// A required law in range that is not-evaluated fails, whatever the reason; a
// law outside the list that is not-evaluated never blocks (§3.3).
func TestExit1RequiredNotEvaluatedFailsOptionalDoesNot(t *testing.T) {
	d := func() Deps {
		x := deps(gateLayers(nil, nil))
		x.Laws = func(in laws.Input) []laws.Result {
			var out []laws.Result
			for _, id := range []string{"G-02", "G-41"} {
				out = append(out, laws.Result{ID: id, Verdict: laws.NotEvaluated, Reason: laws.NoReader, Reader: laws.ReaderTool, Required: has(in.Required, id)})
			}
			return out
		}
		return x
	}
	c := cfg()
	c.RequiredLaws = []string{"G-02"}
	r := mustRun(t, c, opts(), d())
	if r.ExitCode != 1 || r.Report.Laws.RequiredNotEvaluated != 1 {
		t.Fatalf("required: exit %d, required_not_evaluated %d", r.ExitCode, r.Report.Laws.RequiredNotEvaluated)
	}
	c.RequiredLaws = nil
	r = mustRun(t, c, opts(), d())
	if r.ExitCode != 0 {
		t.Fatalf("optional not-evaluated: exit %d, want 0", r.ExitCode)
	}
}

// The stub judges nothing: with the profile's required laws, a default run
// of the real kit can never pass until M5 (never a silent allow).
func TestExit1StubLawsNeverPass(t *testing.T) {
	r := mustRun(t, cfg(), opts(), deps(gateLayers(nil, nil)))
	if r.ExitCode != 0 || r.Report.Summary.Outcome != "pass" {
		t.Fatalf("exit %d outcome %s; G3 stub analyzers and lint let required laws pass", r.ExitCode, r.Report.Summary.Outcome)
	}
	if r.Report.Laws.RequiredNotEvaluated != 0 {
		t.Fatalf("laws block %+v", r.Report.Laws)
	}
}

// ---- code 2 ----

// An error that is a configuration or lock failure found by a layer's
// preflight exits 2, and the run writes no report.
func TestExit2LockMismatchBeforeAnyLayer(t *testing.T) {
	var ran []string
	ls := gateLayers(&ran, nil)
	fl(t, ls[0]).preErr = &LockError{Err: errors.New("govulncheck is v1.8.0, the lock says v1.9.0")}
	mustRefuse(t, cfg(), opts(), deps(ls), 2)
	if len(ran) != 0 {
		t.Fatalf("layers ran before the exit 2: %v", ran)
	}
}

func TestExit2ConfigErrorWrapped(t *testing.T) {
	ls := gateLayers(nil, nil)
	fl(t, ls[1]).preErr = &ConfigError{Err: errors.New("bad")}
	mustRefuse(t, cfg(), opts(), deps(ls), 2)
}

// --only with a layer that does not exist is a usage error: exit 2.
func TestExit2UnknownOnlyLayer(t *testing.T) {
	err := refusal(t, cfg(), Options{MaxTier: 1, Only: "nope"}, deps(gateLayers(nil, nil)), 2)
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("message does not name the layer: %v", err)
	}
}

// ---- code 3 ----

func TestExit3EnvironmentError(t *testing.T) {
	var ran []string
	ls := gateLayers(&ran, nil)
	fl(t, ls[1]).preErr = &EnvError{Err: errors.New("no C compiler; -race needs cgo")}
	mustRefuse(t, cfg(), opts(), deps(ls), 3)
	if len(ran) != 0 {
		t.Fatalf("layers ran before the exit 3: %v", ran)
	}
}

func TestExit3UnreadableRepository(t *testing.T) {
	d := deps(gateLayers(nil, nil))
	fx(t, d.Exec).gitFail = true
	mustRefuse(t, cfg(), opts(), d, 3)
}

// A layer that is out of range or disabled never preflights.
func TestPreflightOnlyForLayersThatRun(t *testing.T) {
	ls := gateLayers(nil, nil)
	fl(t, ls[2]).preErr = &EnvError{Err: errors.New("x")}
	r := mustRun(t, cfg(), Options{MaxTier: 1, Only: "hygiene"}, deps(ls))
	if r.ExitCode != 0 {
		t.Fatalf("exit %d: a layer outside --only must not preflight", r.ExitCode)
	}
	if fl(t, ls[2]).preSeen {
		t.Error("coverage preflighted though out of range")
	}
}

// ---- code 4 ----

func TestExit4DryRunInCI(t *testing.T) {
	var ran []string
	d := deps(gateLayers(&ran, nil))
	d.CI = true
	mustRefuse(t, cfg(), Options{MaxTier: 1, DryRun: true}, d, 4)
	if len(ran) != 0 {
		t.Fatal("a layer ran")
	}
}

func TestExit4OnlyThatLeavesRequiredLawOutInCI(t *testing.T) {
	d := deps(gateLayers(nil, nil))
	d.CI = true
	mustRefuse(t, cfg(), Options{MaxTier: 1, Only: "tests"}, d, 4)
}

func TestExit4MaxTierBelowReaderInCI(t *testing.T) {
	d := deps(gateLayers(nil, nil))
	d.CI = true
	mustRefuse(t, cfg(), Options{MaxTier: 0}, d, 4)
}

// The same ranges run locally: a dry run prints the plan and runs nothing, a
// partial range exits 0.
func TestLocalDryRunAndPartialAreAllowed(t *testing.T) {
	var ran []string
	d := deps(gateLayers(&ran, nil))
	r := mustRun(t, cfg(), Options{MaxTier: 1, DryRun: true}, d)
	if !r.DryRun || r.ExitCode != 0 || len(ran) != 0 {
		t.Fatalf("dry run: DryRun=%v exit=%d ran=%v", r.DryRun, r.ExitCode, ran)
	}
	if !strings.Contains(r.Text, "Nothing ran") {
		t.Errorf("plan text:\n%s", r.Text)
	}
	r = mustRun(t, cfg(), Options{MaxTier: 0}, deps(gateLayers(nil, nil)))
	if r.ExitCode != 0 || r.Report.Summary.Outcome != "partial" {
		t.Fatalf("--max-tier 0 with G3 analyzers: exit %d outcome %s, want 0 partial", r.ExitCode, r.Report.Summary.Outcome)
	}
}

// A full-range run in CI is allowed: refusal is for ranges that would end partial.
func TestCIAcceptsTheFullRange(t *testing.T) {
	d := deps(gateLayers(nil, nil))
	d.CI = true
	mustRun(t, cfg(), opts(), d)
}

// ---- advisory (B1) ----

// Under advisory a run that starts exits 0; the outcome stays the honest one;
// the report says it is not enforced and when that ends; the text prints it.
func TestAdvisoryExitsZeroAndSaysSo(t *testing.T) {
	c := cfg()
	c.Enforce, c.AdvisoryExpires = "advisory", "2027-01-31"
	r := mustRun(t, c, opts(), deps(gateLayers(nil, map[string]Outcome{"tests": Fail})))
	s := r.Report.Summary
	if r.ExitCode != 0 || s.ExitCode != 0 {
		t.Fatalf("exit %d / summary %d, want 0", r.ExitCode, s.ExitCode)
	}
	if s.Outcome != "fail" || len(s.Failed) == 0 {
		t.Fatalf("outcome %q failed %v: advisory must keep the honest outcome", s.Outcome, s.Failed)
	}
	if s.Enforced || s.AdvisoryExpires != "2027-01-31" {
		t.Fatalf("enforced=%v advisory_expires=%q", s.Enforced, s.AdvisoryExpires)
	}
	want := "NOT ENFORCED (advisory, expires 2027-01-31): outcome FAIL"
	if !strings.Contains(r.Text, want) {
		t.Errorf("text lacks %q:\n%s", want, r.Text)
	}
}

func TestEnforcedModesSayEnforced(t *testing.T) {
	for _, mode := range []string{"hard", "ratchet"} {
		c := cfg()
		c.Enforce = mode
		r := mustRun(t, c, opts(), deps(gateLayers(nil, nil)))
		if !r.Report.Summary.Enforced || r.Report.Summary.AdvisoryExpires != "" {
			t.Errorf("%s: %+v", mode, r.Report.Summary)
		}
		if strings.Contains(r.Text, "NOT ENFORCED") {
			t.Errorf("%s: text says not enforced", mode)
		}
		if r.ExitCode != 0 {
			t.Errorf("%s: exit %d, want 0 (G3 stub laws pass)", mode, r.ExitCode)
		}
	}
}

// Codes 2, 3 and 4 still apply first under advisory.
func TestAdvisoryStillRefusesAndStillErrors(t *testing.T) {
	c := cfg()
	c.Enforce, c.AdvisoryExpires = "advisory", "2027-01-31"

	d := deps(gateLayers(nil, nil))
	d.CI = true
	mustRefuse(t, c, Options{MaxTier: 1, Only: "tests"}, d, 4)

	ls := gateLayers(nil, nil)
	fl(t, ls[1]).preErr = &EnvError{Err: errors.New("no cc")}
	mustRefuse(t, c, opts(), deps(ls), 3)

	ls = gateLayers(nil, nil)
	fl(t, ls[0]).preErr = &LockError{Err: errors.New("sha")}
	mustRefuse(t, c, opts(), deps(ls), 2)
}
