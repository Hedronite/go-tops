package gate

import (
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/report"
)

// §3.5: every enabled layer in range runs in the order of §3.1, and a layer
// that fails does not stop the layers that do not depend on it.
func TestRunOrderAndIndependence(t *testing.T) {
	var ran []string
	c := cfg()
	c.RequiredLaws = nil
	r := mustRun(t, c, opts(), deps(gateLayers(&ran, map[string]Outcome{"hygiene": Fail})))
	want := []string{"hygiene", "tests", "coverage", "crap", "mutation"}
	if strings.Join(ran, ",") != strings.Join(want, ",") {
		t.Fatalf("ran %v, want %v: a failed hygiene must not stop the others", ran, want)
	}
	if strings.Join(ids(r), ",") != strings.Join(want, ",") {
		t.Fatalf("report order %v", ids(r))
	}
}

// The dependency table of §3.5: coverage needs tests, crap needs coverage,
// mutation needs tests; a layer whose need is not pass is not-reached, and
// hygiene, which needs nothing, still runs.
func TestDependencyNotReached(t *testing.T) {
	cases := []struct {
		fail    string
		reached map[string]string
	}{
		{"tests", map[string]string{"hygiene": "pass", "coverage": "not-reached", "crap": "not-reached", "mutation": "not-reached"}},
		{"coverage", map[string]string{"hygiene": "pass", "tests": "pass", "crap": "not-reached", "mutation": "pass"}},
		{"hygiene", map[string]string{"tests": "pass", "coverage": "pass", "crap": "pass", "mutation": "pass"}},
	}
	for _, tc := range cases {
		var ran []string
		c := cfg()
		c.RequiredLaws = nil
		r := mustRun(t, c, opts(), deps(gateLayers(&ran, map[string]Outcome{tc.fail: Fail})))
		o := layerOutcomes(r)
		for id, want := range tc.reached {
			if o[id] != want {
				t.Errorf("%s fails: %s is %s, want %s", tc.fail, id, o[id], want)
			}
		}
		for _, id := range ran {
			if o[id] == "not-reached" {
				t.Errorf("%s ran though not-reached", id)
			}
		}
	}
}

// A need that is itself not-reached passes the not-reached on down the chain.
func TestNotReachedPropagates(t *testing.T) {
	c := cfg()
	c.RequiredLaws = nil
	r := mustRun(t, c, opts(), deps(gateLayers(nil, map[string]Outcome{"tests": NotRun})))
	if layerOutcomes(r)["crap"] != "not-reached" {
		t.Fatalf("outcomes %v", layerOutcomes(r))
	}
}

// §3.5: a need that is skipped is not pass either.
func TestNeedMustBePass(t *testing.T) {
	c := cfg()
	c.RequiredLaws = nil
	c.Layers = map[string]LayerConfig{"stress": {Enabled: false, Reason: "test only"}}
	ls := []Layer{
		&fakeLayer{id: "stress", tier: 1, res: Result{Outcome: Pass}},
		&fakeLayer{id: "bench", tier: 1, needs: []string{"stress"}, res: Result{Outcome: Pass}},
	}
	r := mustRun(t, c, opts(), deps(ls))
	if o := layerOutcomes(r); o["stress"] != "skip" || o["bench"] != "not-reached" {
		t.Fatalf("outcomes %v", o)
	}
}

// --fail-fast stops after the first fail, not-run or tool-missing; every
// enabled layer in range that has not run is then not-reached, laws included.
func TestFailFast(t *testing.T) {
	for _, o := range []Outcome{Fail, NotRun, ToolMissing} {
		var ran []string
		c := cfg()
		r := mustRun(t, c, Options{MaxTier: 1, FailFast: true}, deps(gateLayers(&ran, map[string]Outcome{"tests": o})))
		if strings.Join(ran, ",") != "hygiene,tests" {
			t.Errorf("%s: ran %v, want hygiene,tests", o, ran)
		}
		got := layerOutcomes(r)
		for _, id := range []string{"coverage", "crap", "mutation"} {
			if got[id] != "not-reached" {
				t.Errorf("%s: %s is %s, want not-reached", o, id, got[id])
			}
		}
		for _, lr := range r.Report.Laws.Results {
			if lr.Verdict != "not-evaluated" || lr.Reason != "input_missing" {
				t.Errorf("%s: law %s = %s/%s, want not-evaluated/input_missing after --fail-fast", o, lr.ID, lr.Verdict, lr.Reason)
			}
		}
		if r.ExitCode != 1 {
			t.Errorf("%s: exit %d", o, r.ExitCode)
		}
	}
}

// Without --fail-fast, a pass stops nothing; with it, a run with no failure goes all the way.
func TestFailFastLeavesAGreenRunAlone(t *testing.T) {
	var ran []string
	c := cfg()
	c.RequiredLaws = nil
	r := mustRun(t, c, Options{MaxTier: 1, FailFast: true}, deps(gateLayers(&ran, nil)))
	if len(ran) != 5 || r.ExitCode != 0 {
		t.Fatalf("ran %v exit %d", ran, r.ExitCode)
	}
}

// A disabled layer is skip with its reason, is not counted, and does not make
// the run fail (§3.3); crap and mutation carry their expires.
func TestSkipCarriesReasonAndExpires(t *testing.T) {
	c := cfg()
	c.RequiredLaws = nil
	c.Layers = map[string]LayerConfig{
		"crap":     {Enabled: false, Reason: "lands at M2", Expires: "2027-03-01"},
		"mutation": {Enabled: false, Reason: "lands at M4", Expires: "2027-06-01"},
	}
	r := mustRun(t, c, opts(), deps(gateLayers(nil, nil)))
	if r.ExitCode != 0 {
		t.Fatalf("exit %d", r.ExitCode)
	}
	for _, l := range r.Report.Layers {
		if l.ID == "crap" && (l.Outcome != "skip" || l.Reason != "lands at M2" || l.Expires != "2027-03-01") {
			t.Errorf("crap entry %+v", l)
		}
	}
	if r.Report.LayerCounts.EnabledInRange != 3 || r.Report.LayerCounts.Ran != 3 {
		t.Errorf("counts %+v, want 3 enabled and 3 ran: a skip is not counted", r.Report.LayerCounts)
	}
	if !strings.Contains(r.Text, "lands at M2") {
		t.Errorf("the skip reason is not printed:\n%s", r.Text)
	}
}

// §3.5: the report records layers enabled in range and layers that ran; the
// run fails when they differ.
func TestFewerRanThanEnabledFails(t *testing.T) {
	c := cfg()
	c.RequiredLaws = nil
	r := mustRun(t, c, opts(), deps(gateLayers(nil, map[string]Outcome{"tests": NotRun})))
	lc := r.Report.LayerCounts
	if lc.EnabledInRange != 5 || lc.Ran != 1 {
		t.Fatalf("counts %+v, want 5 enabled and 1 ran", lc)
	}
	if r.ExitCode != 1 {
		t.Fatalf("exit %d", r.ExitCode)
	}
	green := mustRun(t, c, opts(), deps(gateLayers(nil, nil)))
	if g := green.Report.LayerCounts; g.EnabledInRange != 5 || g.Ran != 5 {
		t.Fatalf("green counts %+v", g)
	}
}

// §3.2: a layer above --max-tier is not run, not counted and not listed.
func TestMaxTierRange(t *testing.T) {
	ls := append(gateLayers(nil, nil), &fakeLayer{id: "fuzz", tier: 2, res: Result{Outcome: Fail}})
	c := cfg()
	c.RequiredLaws = nil
	r := mustRun(t, c, opts(), deps(ls))
	if has(ids(r), "fuzz") {
		t.Fatalf("a tier 2 layer is in a tier 1 report: %v", ids(r))
	}
	if r.ExitCode != 0 {
		t.Fatalf("exit %d: an out-of-range failing layer must not count", r.ExitCode)
	}
	r = mustRun(t, c, Options{MaxTier: 2}, deps(ls))
	if !has(ids(r), "fuzz") || r.ExitCode != 1 || r.Report.MaxTier != 2 {
		t.Fatalf("at tier 2: layers %v exit %d", ids(r), r.ExitCode)
	}
}

// §3.2: at --max-tier 0 nothing at tier 1 runs, and the tool-reader laws are
// listed as out of range with input_missing.
func TestMaxTierZeroListsToolLawsOutOfRange(t *testing.T) {
	var ran []string
	d := deps(gateLayers(&ran, nil))
	d.Laws = analyzersAllow
	r := mustRun(t, cfg(), Options{MaxTier: 0}, d)
	if len(ran) != 0 || len(r.Report.Layers) != 0 {
		t.Fatalf("tier 1 layers ran at --max-tier 0: %v", ran)
	}
	oor := r.Report.Laws.OutOfRange
	for _, id := range []string{"G-02", "G-12", "G-34", "G-40"} {
		if !has(oor, id) {
			t.Errorf("%s not listed out of range: %v", id, oor)
		}
	}
	if has(oor, "G-08") {
		t.Errorf("G-08 is an analyzer law and sits at tier 0")
	}
	if r.Report.Summary.Outcome != "partial" {
		t.Errorf("outcome %s, want partial", r.Report.Summary.Outcome)
	}
}

// §3.5: --only runs that layer and the layers it needs; the others are out of range.
func TestOnlyRunsTheLayerAndItsNeeds(t *testing.T) {
	var ran []string
	r := mustRun(t, cfg(), Options{MaxTier: 1, Only: "crap"}, deps(gateLayers(&ran, nil)))
	if strings.Join(ran, ",") != "tests,coverage,crap" {
		t.Fatalf("ran %v, want tests,coverage,crap", ran)
	}
	if strings.Join(ids(r), ",") != "tests,coverage,crap" {
		t.Fatalf("report layers %v", ids(r))
	}
	if r.Report.Summary.Outcome != "partial" || r.ExitCode != 0 {
		t.Fatalf("outcome %s exit %d", r.Report.Summary.Outcome, r.ExitCode)
	}
	if len(r.Report.Laws.OutOfRange) != len(r.Report.Laws.Results) {
		t.Errorf("--only left %d of %d laws out of range, want all", len(r.Report.Laws.OutOfRange), len(r.Report.Laws.Results))
	}
}

// --only laws runs the laws layer alone; the layers are out of range.
func TestOnlyLaws(t *testing.T) {
	var ran []string
	r := mustRun(t, cfg(), Options{MaxTier: 1, Only: "laws"}, deps(gateLayers(&ran, nil)))
	if len(ran) != 0 || len(r.Report.Layers) != 0 {
		t.Fatalf("layers ran under --only laws: %v", ran)
	}
	if r.ExitCode != 1 || r.Report.Laws.RequiredNotEvaluated != 2 {
		t.Fatalf("exit %d required_not_evaluated %d; the stub judges nothing, so 16 required laws fail", r.ExitCode, r.Report.Laws.RequiredNotEvaluated)
	}
}

// An empty result is a failure (§3.4 rule 5), and so is a result no layer may return.
func TestEmptyAndForbiddenResultsFail(t *testing.T) {
	cases := map[string]Result{
		"empty":       {},
		"unknown":     {Outcome: "green"},
		"skip":        {Outcome: Skip, Reason: "x"},
		"not-reached": {Outcome: NotReached},
		"vacuous":     {Outcome: Vacuous, Details: details("tests")},
	}
	for name, res := range cases {
		c := cfg()
		c.RequiredLaws = nil
		ls := gateLayers(nil, nil)
		ls[1] = &fakeLayer{id: "tests", tier: 1, res: res}
		r := mustRun(t, c, opts(), deps(ls))
		if got := layerOutcomes(r)["tests"]; got != "fail" {
			t.Errorf("%s result: tests is %s, want fail", name, got)
		}
		for _, l := range r.Report.Layers {
			if l.ID == "tests" {
				if msg, ok := l.Details["error"].(string); !ok || msg == "" || len(l.Details) != 1 {
					t.Errorf("%s result: details %v, want {error: why}", name, l.Details)
				}
			}
		}
		if r.ExitCode != 1 {
			t.Errorf("%s result: exit %d", name, r.ExitCode)
		}
	}
}

// vacuous is the one place an empty result is allowed: the mutation layer.
func TestVacuousIsForMutationOnly(t *testing.T) {
	c := cfg()
	c.RequiredLaws = nil
	r := mustRun(t, c, opts(), deps(gateLayers(nil, map[string]Outcome{"mutation": Vacuous})))
	if layerOutcomes(r)["mutation"] != "vacuous" || r.ExitCode != 0 {
		t.Fatalf("mutation %s exit %d", layerOutcomes(r)["mutation"], r.ExitCode)
	}
	if r.Report.LayerCounts.Ran != 5 {
		t.Errorf("a vacuous layer ran: counts %+v", r.Report.LayerCounts)
	}
}

// A layer set whose Needs do not come first, or that repeats an ID, is rejected.
func TestBadLayerSet(t *testing.T) {
	_, err := Execute(t.Context(), cfg(), opts(), deps([]Layer{
		&fakeLayer{id: "coverage", tier: 1, needs: []string{"tests"}},
	}))
	if err == nil {
		t.Fatal("a layer needing an unregistered layer was accepted")
	}
	_, err = Execute(t.Context(), cfg(), opts(), deps([]Layer{
		&fakeLayer{id: "tests", tier: 1}, &fakeLayer{id: "tests", tier: 1},
	}))
	if err == nil {
		t.Fatal("a repeated layer ID was accepted")
	}
}

// §12.3: the report names module, class, enforce, git state and the base.
func TestReportHeader(t *testing.T) {
	d := deps(gateLayers(nil, nil))
	fx(t, d.Exec).dirty = true
	c := cfg()
	c.Exemptions = []report.Exemption{{Path: "a.go", Symbol: "F", Layer: "crap", Reason: "dispatch table, one call per arm", Owner: "platform", Expires: "2027-03-01"}}
	r := mustRun(t, c, Options{MaxTier: 1, Base: "origin/release"}, d)
	rep := r.Report
	if rep.Protocol != report.Protocol || rep.Module != "example.com/m" || rep.Class != "binary-cli" || rep.Enforce != "hard" || rep.ToolVersion != "0.0.0-dev" {
		t.Errorf("header %+v", rep)
	}
	if rep.Git.Head != "abc1234" || !rep.Git.Dirty || rep.Git.Base != "origin/release" {
		t.Errorf("git %+v", rep.Git)
	}
	if rep.StartedAt != "2026-11-17T18:00:00Z" || rep.DurationMS != 0 {
		t.Errorf("clock %s %d", rep.StartedAt, rep.DurationMS)
	}
	if len(rep.Exemptions) != 1 || rep.Exemptions[0].Symbol != "F" {
		t.Errorf("exemptions %+v", rep.Exemptions)
	}
	if rep.VulnDBTimestamp != nil {
		t.Errorf("vuln_db_timestamp set though no layer ran govulncheck")
	}
	if c.Base == "" && rep.Git.Base == "" {
		t.Error("no base")
	}
}

func TestBaseDefaultsToOriginMain(t *testing.T) {
	r := mustRun(t, cfg(), opts(), deps(gateLayers(nil, nil)))
	if r.Report.Git.Base != "origin/main" {
		t.Fatalf("base %q", r.Report.Git.Base)
	}
}

// The laws block states the catalog and the class counts (§12.3): 43 catalog
// rows; G2 judges 9 delegated/hygiene laws when the stub law pass and fake hygiene pass.
func TestLawsBlockCounts(t *testing.T) {
	r := mustRun(t, cfg(), opts(), deps(gateLayers(nil, nil)))
	l := r.Report.Laws
	if l.Catalog != 43 || l.Judged != 16 || l.NotEvaluated != 26 {
		t.Errorf("release counts %+v", l)
	}
	if l.Applicable != 19 || l.ApplicableJudged != 16 || l.Required != 16 || l.RequiredNotEvaluated != 0 {
		t.Errorf("class counts %+v", l)
	}
	by := l.NotEvaluatedByReason
	if by.NoReader+by.ConfigOff+by.InputMissing != l.Applicable-l.ApplicableJudged {
		t.Errorf("by reason %+v does not sum to applicable minus judged", by)
	}
	if len(l.Results) != 19 {
		t.Errorf("%d results", len(l.Results))
	}
	wired := map[string]bool{"G-02": true, "G-07": true, "G-08": true, "G-11": true, "G-12": true, "G-16": true, "G-17": true, "G-18": true, "G-19": true, "G-25": true, "G-26": true, "G-33": true, "G-34": true, "G-37": true, "G-38": true, "G-40": true}
	for _, res := range l.Results {
		if wired[res.ID] && res.Verdict != "allow" {
			t.Errorf("%s is %s, want allow from G2 readers", res.ID, res.Verdict)
		}
		if !wired[res.ID] && res.Verdict == "allow" {
			t.Errorf("%s is allow, want not-evaluated or refuse", res.ID)
		}
	}
}

// §3.1, §12.3: the vulnerability layer is time-dependent. The timestamp of
// the database it read reaches the report's vuln_db_timestamp and the text; a
// run whose layers read none leaves it null.
func TestVulnDBTimestampReachesTheReport(t *testing.T) {
	c := cfg()
	c.RequiredLaws = nil
	ls := gateLayers(nil, nil)
	ls[0] = &fakeLayer{id: "hygiene", tier: 1, res: Result{Outcome: Pass, Details: details("hygiene"), VulnDBTimestamp: "2026-11-17T09:12:44Z"}}
	r := mustRun(t, c, opts(), deps(ls))
	if r.Report.VulnDBTimestamp == nil || *r.Report.VulnDBTimestamp != "2026-11-17T09:12:44Z" {
		t.Fatalf("vuln_db_timestamp = %v", r.Report.VulnDBTimestamp)
	}
	if !strings.Contains(r.Text, "2026-11-17T09:12:44Z") || !strings.Contains(r.Text, "time-dependent") {
		t.Errorf("text does not say the layer is time-dependent:\n%s", r.Text)
	}
}

// §3.3: not-run is "no command mapped" and not-reached is "has a command, but a
// layer it needs did not pass". A layer with no command is not-run whatever its
// needs did, and under --fail-fast; a layer with a command whose need is such a
// layer is not-reached.
func TestNoCommandIsNotRunWhateverItsNeeds(t *testing.T) {
	c := cfg()
	c.RequiredLaws = nil
	mk := func() []Layer {
		return []Layer{
			&fakeLayer{id: "tests", tier: 1, res: res("tests", Fail)},
			&fakeLayer{id: "coverage", tier: 1, needs: []string{"tests"}, noCmd: true},
			&fakeLayer{id: "crap", tier: 1, needs: []string{"coverage"}, res: res("crap", Pass)},
		}
	}
	o := layerOutcomes(mustRun(t, c, opts(), deps(mk())))
	if o["tests"] != "fail" || o["coverage"] != "not-run" || o["crap"] != "not-reached" {
		t.Fatalf("outcomes %v: coverage has no command so it is not-run although tests failed; crap has a command but its need did not pass", o)
	}
	o = layerOutcomes(mustRun(t, c, Options{MaxTier: 1, FailFast: true}, deps(mk())))
	if o["coverage"] != "not-run" || o["crap"] != "not-reached" {
		t.Fatalf("under --fail-fast: %v", o)
	}
	o = layerOutcomes(mustRun(t, c, Options{MaxTier: 1, Only: "crap"}, deps(mk())))
	if o["coverage"] != "not-run" || o["crap"] != "not-reached" {
		t.Fatalf("under --only crap: %v", o)
	}
}
