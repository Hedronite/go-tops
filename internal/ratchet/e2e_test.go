package ratchet_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/cover"
	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/gobuild"
	_ "github.com/Hedronite/go-tops/internal/layers/crap"
	"github.com/Hedronite/go-tops/internal/layers/hygiene"
	"github.com/Hedronite/go-tops/internal/layers/tests"
	"github.com/Hedronite/go-tops/internal/ratchet"
	"github.com/Hedronite/go-tops/internal/report"
)

// The legacy repo under ratchet, through the gate runner with the real coverage
// and CRAP layers: a package under its floor and a function over the cap, both in
// the ledger, give a ratchet pass on the first ledger. The CRAP layer must run:
// it needs the coverage layer, and a red floor must not leave it not reached.

type stubLayer struct {
	id    string
	needs []string
}

func (s stubLayer) ID() string      { return s.id }
func (s stubLayer) Tier() int       { return 1 }
func (s stubLayer) Needs() []string { return s.needs }
func (s stubLayer) Run(context.Context, *gate.Env) gate.Result {
	return gate.Result{Outcome: gate.Pass}
}

const legacySource = "package low\n\n" +
	"func Big(a bool) int {\n" + "\tif a {\n\t\treturn 1\n\t}\n" + // 10 ifs: comp 11
	"\tif a {\n\t\treturn 1\n\t}\n\tif a {\n\t\treturn 1\n\t}\n\tif a {\n\t\treturn 1\n\t}\n\tif a {\n\t\treturn 1\n\t}\n" +
	"\tif a {\n\t\treturn 1\n\t}\n\tif a {\n\t\treturn 1\n\t}\n\tif a {\n\t\treturn 1\n\t}\n\tif a {\n\t\treturn 1\n\t}\n\tif a {\n\t\treturn 1\n\t}\n" +
	"\treturn 0\n}\n\nfunc Other() { println() }\n"

// legacyProject writes the module and its profile: Big (comp 11) is fully
// covered, so it scores 11, over the cap of 10 at every coverage; Other has 100
// statements, none run, so the package covers 40 of 140.
func legacyProject(t *testing.T) *ratchetProject {
	t.Helper()
	dir := repo(t)
	commit(t, dir, "adopt", map[string]string{
		"go.mod":          "module example.com/m\n\ngo 1.26\n",
		"low/low.go":      legacySource,
		"low/low_test.go": "package low\n\nimport \"testing\"\n\nfunc TestBig(t *testing.T) { Big(true) }\n",
	})
	last := strings.Count(legacySource, "\n") // the line of the closing brace of Other
	other := last
	profile := "mode: atomic\n" +
		"example.com/m/low/low.go:3.1,35.2 40 1\n" +
		"example.com/m/low/low.go:" + itoa(other) + ".16," + itoa(other) + ".26 100 0\n"
	p := filepath.Join(dir, filepath.FromSlash(tests.ProfilePath))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(profile), 0o600); err != nil {
		t.Fatal(err)
	}
	// What the hygiene layer saves: a clean lint run.
	if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(hygiene.LintJSON)), []byte(`{"Issues":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return &ratchetProject{dir: dir}
}

type ratchetProject struct{ dir string }

func itoa(n int) string {
	return strings.TrimSpace(strings.Repeat(" ", 0) + string(rune('0'+n/10)) + string(rune('0'+n%10)))
}

// writeFirstLedger records what the run measures, as `baseline --init` does.
func (p *ratchetProject) writeFirstLedger(t *testing.T) {
	t.Helper()
	p.writeLedgerFor(t, map[string]float64{"example.com/m/low.Big": 11}, 0)
}

// writeLedgerFor writes a first ledger with the given function entries and
// exemption count, and the package under its floor.
func (p *ratchetProject) writeLedgerFor(t *testing.T, functions map[string]float64, exemptions int) {
	t.Helper()
	build, err := gobuild.Resolve(p.dir, tests.Build)
	if err != nil {
		t.Fatal(err)
	}
	pkg := cover.Package{Path: "example.com/m/low", Statements: 140, Covered: 40}
	ledger := map[string]any{
		"functions": functions,
		"packages":  map[string]any{pkg.Path: map[string]any{"statements_pct": pkg.Pct(), "uncovered": pkg.Uncovered()}},
	}
	if exemptions > 0 {
		ledger["exemptions"] = exemptions
	}
	doc := map[string]any{
		"build":       map[string]any{"goos": build.GOOS, "goarch": build.GOARCH, "tags": []string{}, "cgo_enabled": build.CGOEnabled},
		"ledger":      ledger,
		"judged_laws": 0,
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(p.dir, filepath.FromSlash(ratchet.LedgerPath))
	if err := os.MkdirAll(filepath.Dir(f), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// layers is the run's layer set: the real coverage layer as registered (wrapped
// by the ratchet package) and the real CRAP layer, with stand-ins for the layers
// that would run tools.
func layers(t *testing.T) []gate.Layer {
	t.Helper()
	real := map[string]gate.Layer{}
	for _, l := range gate.Layers() {
		real[l.ID()] = l
	}
	return []gate.Layer{
		stubLayer{id: "hygiene"},
		stubLayer{id: "tests"},
		real["coverage"], real["crap"],
	}
}

func runGate(t *testing.T, p *ratchetProject, enforce string, exemptions ...report.Exemption) *gate.Run {
	t.Helper()
	cfg := gate.Config{
		Module: "example.com/m", Class: "binary-cli", Enforce: enforce,
		Crap:     gate.CrapConfig{Cap: 10, ErrGuardDiscount: true},
		Coverage: gate.CoverageConfig{StatementsFloor: 90}, Exemptions: exemptions,
	}
	if enforce == "advisory" {
		cfg.AdvisoryExpires = "2027-03-31"
	}
	d := gate.Deps{Dir: p.dir, Layers: layers(t), ToolVersion: "test", Ratchet: ratchet.NewJudge(ratchet.CollectMeasure(func(string) (string, error) { return "", nil }))}
	run, err := gate.Execute(context.Background(), cfg, gate.Options{MaxTier: 1, Base: "main"}, d)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return run
}

func outcomes(r *gate.Run) map[string]string {
	m := map[string]string{}
	for _, l := range r.Report.Layers {
		m[l.ID] = l.Outcome
	}
	return m
}

func TestRatchetPassesALegacyPackageAndFunctionOnTheFirstLedger(t *testing.T) {
	p := legacyProject(t)
	p.writeFirstLedger(t)
	run := runGate(t, p, "ratchet")
	o := outcomes(run)
	if o["crap"] == "not-reached" {
		t.Fatalf("crap was not reached: a red coverage floor must not stop it under ratchet (%v)", o)
	}
	if o["coverage"] != "pass" || o["crap"] != "pass" {
		t.Errorf("layers %v, want coverage and crap pass: the ledger covers the package and the function", o)
	}
	if run.Report.Ratchet == nil || run.Report.Ratchet.Outcome != "pass" {
		t.Fatalf("ratchet %+v, want pass on the first ledger\n%s", run.Report.Ratchet, run.Text)
	}
	if run.ExitCode != 0 {
		t.Errorf("exit %d, want 0\n%s", run.ExitCode, run.Text)
	}
	for _, l := range run.Report.Layers {
		if l.ID != "crap" {
			continue
		}
		rows, ok := l.Details["violations"].([]any)
		if !ok || len(rows) != 1 {
			t.Fatalf("violations %v, want the one legacy function", l.Details["violations"])
		}
		if row, ok := rows[0].(map[string]any); !ok || row["baseline"] != 11.0 {
			t.Errorf("row %v, want its ledger entry 11 as the baseline", rows[0])
		}
	}
}

// The control: the same repo and the same ledger under hard fails the floor,
// and the CRAP layer is not reached, which is exactly what ratchet must not do.
func TestTheSameRepoUnderHardFailsTheFloorAndSkipsCrap(t *testing.T) {
	p := legacyProject(t)
	p.writeFirstLedger(t)
	run := runGate(t, p, "hard")
	o := outcomes(run)
	if o["coverage"] != "fail" || o["crap"] != "not-reached" {
		t.Errorf("layers %v, want coverage fail and crap not-reached under hard", o)
	}
	if run.Report.Ratchet != nil {
		t.Errorf("a hard run has a ratchet object %+v", run.Report.Ratchet)
	}
}

// Past its entry the same package fails the ratchet, and the layer says so.
func TestRatchetFailsAPackageThatGotWorse(t *testing.T) {
	p := legacyProject(t)
	p.writeFirstLedger(t)
	// Rewrite the ledger so the package was better: 60 uncovered at 57%.
	f := filepath.Join(p.dir, filepath.FromSlash(ratchet.LedgerPath))
	raw, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	worse := strings.Replace(string(raw), `"uncovered":100`, `"uncovered":60`, 1)
	if err := os.WriteFile(f, []byte(strings.Replace(worse, `"statements_pct":28.571428571428573`, `"statements_pct":57`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	run := runGate(t, p, "ratchet")
	if o := outcomes(run); o["coverage"] != "fail" || run.ExitCode != 1 {
		t.Errorf("layers %v exit %d, want coverage fail and exit 1\n%s", o, run.ExitCode, run.Text)
	}
}

// A coverage layer that failed for a reason other than the floor stays failed
// under ratchet: no profile is not a legacy package. CRAP is then not reached.
func TestRatchetDoesNotForgiveAMissingProfile(t *testing.T) {
	p := legacyProject(t)
	p.writeFirstLedger(t)
	if err := os.Remove(filepath.Join(p.dir, filepath.FromSlash(tests.ProfilePath))); err != nil {
		t.Fatal(err)
	}
	run := runGate(t, p, "ratchet")
	if o := outcomes(run); o["coverage"] != "fail" || o["crap"] != "not-reached" || run.ExitCode != 1 {
		t.Errorf("layers %v exit %d, want coverage fail and crap not-reached", o, run.ExitCode)
	}
}

// The wrapper changes nothing outside ratchet and nothing that is not a floor
// verdict.
func TestWrapCoverageLeavesOtherModesAlone(t *testing.T) {
	for _, mode := range []string{"hard", "advisory"} {
		p := legacyProject(t)
		run := runGate(t, p, mode)
		if o := outcomes(run); o["coverage"] != "fail" {
			t.Errorf("%s: coverage %s, want the hard floor verdict, fail", mode, o["coverage"])
		}
	}
}

func exemptionFor(path string) report.Exemption {
	return report.Exemption{Path: path, Layer: "crap", Reason: "legacy dispatch under refactor", Owner: "platform", Expires: "2099-01-01"}
}

// A function a crap exemption covers has no ledger entry and is not judged: the
// first ledger of a repo whose legacy function is exempt carries the exemption
// count and no function entry, and the run passes.
func TestRatchetWithAnExemptLegacyFunction(t *testing.T) {
	p := legacyProject(t)
	p.writeLedgerFor(t, map[string]float64{}, 1)
	run := runGate(t, p, "ratchet", exemptionFor("low/low.go"))
	if o := outcomes(run); o["crap"] != "pass" || run.Report.Ratchet == nil || run.Report.Ratchet.Outcome != "pass" {
		t.Fatalf("layers %v ratchet %+v\n%s", o, run.Report.Ratchet, run.Text)
	}
}

// The same ledger without the exemption in force: the function has no entry and
// is over the cap, so it fails; and an entry for an exempt function in a first
// ledger fails.
func TestRatchetExemptionCountAndEntries(t *testing.T) {
	p := legacyProject(t)
	p.writeLedgerFor(t, map[string]float64{}, 1)
	if run := runGate(t, p, "ratchet"); run.ExitCode != 1 {
		t.Errorf("exit %d, want 1: the function is over the cap, has no entry and no exemption\n%s", run.ExitCode, run.Text)
	}
	q := legacyProject(t)
	q.writeLedgerFor(t, map[string]float64{"example.com/m/low.Big": 11}, 1)
	run := runGate(t, q, "ratchet", exemptionFor("low/low.go"))
	if run.Report.Ratchet == nil || run.Report.Ratchet.Outcome != "fail" {
		t.Errorf("ratchet %+v, want fail: an exempt function has no entry in a first ledger\n%s", run.Report.Ratchet, run.Text)
	}
}

// An exemption that matches nothing fails the run under ratchet, with the legacy
// debt covered by the ledger.
func TestRatchetFailsAnExemptionThatMatchesNothing(t *testing.T) {
	p := legacyProject(t)
	p.writeFirstLedger(t)
	run := runGate(t, p, "ratchet", exemptionFor("low/vanished.go"))
	if o := outcomes(run); o["crap"] != "fail" || run.ExitCode != 1 {
		t.Errorf("layers %v exit %d, want crap fail and exit 1\n%s", o, run.ExitCode, run.Text)
	}
}

// The exemption count of the ledger is the records in force today: an expired
// record exempts nothing and is not counted, so a first ledger that records none
// passes with one expired entry in the configuration.
func TestRatchetCountsOnlyExemptionsInForce(t *testing.T) {
	p := legacyProject(t)
	p.writeFirstLedger(t)
	expired := exemptionFor("low/low.go")
	expired.Expires = "2020-01-01"
	run := runGate(t, p, "ratchet", expired)
	if run.Report.Ratchet == nil || run.Report.Ratchet.Outcome != "pass" {
		t.Errorf("ratchet %+v, want pass: the expired record is neither in force nor counted\n%s", run.Report.Ratchet, run.Text)
	}
}

// A function in a file this leg does not compile, with a complexity over the cap,
// fails the ratchet like any function over it: it has no score and no ledger
// entry, and the measurer reads its row from the CRAP layer (§6.2, ruling C2b).
func TestRatchetFailsANotMeasuredFunctionOverTheCap(t *testing.T) {
	p := legacyProject(t)
	p.writeFirstLedger(t)
	big := "package low\n\nfunc OnWindows(a bool) int {\n" + strings.Repeat("\tif a {\n\t\treturn 1\n\t}\n", 11) + "\treturn 0\n}\n"
	if err := os.WriteFile(filepath.Join(p.dir, "low", "big_windows.go"), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	run := runGate(t, p, "ratchet")
	if o := outcomes(run); o["crap"] != "fail" || run.Report.Ratchet == nil || run.Report.Ratchet.Outcome != "fail" || run.ExitCode != 1 {
		t.Fatalf("layers %v ratchet %+v exit %d\n%s", o, run.Report.Ratchet, run.ExitCode, run.Text)
	}
	if !strings.Contains(run.Text, "example.com/m/low.OnWindows") || !strings.Contains(run.Text, "not measured") {
		t.Errorf("the text does not name the not-measured function:\n%s", run.Text)
	}
}
