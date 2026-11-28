package ratchet_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
	if s.id == "hygiene" {
		pass := func() map[string]any { return map[string]any{"outcome": "pass"} }
		return gate.Result{Outcome: gate.Pass, Details: map[string]any{
			"tidy": pass(), "verify": pass(), "vulnerabilities": pass(),
		}}
	}
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

type ratchetProject struct {
	dir string
	// ledgerBuild, when set, is the build the ledger records instead of this run's.
	ledgerBuild *gobuild.Build
	// findings are the lint findings the ledger records, and noPackage leaves the
	// package under its floor out of it.
	findings  ratchet.Counts
	noPackage bool
}

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
	if p.ledgerBuild != nil {
		build = *p.ledgerBuild
	}
	pkg := cover.Package{Path: "example.com/m/low", Statements: 140, Covered: 40}
	ledger := map[string]any{
		"functions": functions,
		"packages":  map[string]any{pkg.Path: map[string]any{"statements_pct": pkg.Pct(), "uncovered": pkg.Uncovered()}},
	}
	if exemptions > 0 {
		ledger["exemptions"] = exemptions
	}
	if len(p.findings) > 0 {
		ledger["findings"] = p.findings
	}
	if p.noPackage {
		delete(ledger, "packages")
	}
	doc := map[string]any{
		"build":       map[string]any{"goos": build.GOOS, "goarch": build.GOARCH, "tags": append([]string{}, build.Tags...), "cgo_enabled": build.CGOEnabled},
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
	return runGateWith(t, p, enforce, layers(t), exemptions...)
}

func runGateWith(t *testing.T, p *ratchetProject, enforce string, ls []gate.Layer, exemptions ...report.Exemption) *gate.Run {
	t.Helper()
	return execute(t, p, gateConfig(enforce, exemptions), ls)
}

// runGateRequired runs the gate with the given laws required.
func runGateRequired(t *testing.T, p *ratchetProject, enforce string, required []string) *gate.Run {
	t.Helper()
	cfg := gateConfig(enforce, nil)
	cfg.RequiredLaws = required
	return execute(t, p, cfg, layers(t))
}

func gateConfig(enforce string, exemptions []report.Exemption) gate.Config {
	cfg := gate.Config{
		Module: "example.com/m", Class: "binary-cli", Enforce: enforce,
		Crap:     gate.CrapConfig{Cap: 10, ErrGuardDiscount: true},
		Coverage: gate.CoverageConfig{StatementsFloor: 90}, Exemptions: exemptions,
	}
	if enforce == "advisory" {
		cfg.AdvisoryExpires = "2027-03-31"
	}
	return cfg
}

func execute(t *testing.T, p *ratchetProject, cfg gate.Config, ls []gate.Layer) *gate.Run {
	t.Helper()
	return executeWith(t, p, cfg, ls, gate.Options{MaxTier: 1, Base: "main"})
}

func executeWith(t *testing.T, p *ratchetProject, cfg gate.Config, ls []gate.Layer, opts gate.Options) *gate.Run {
	t.Helper()
	d := gate.Deps{Dir: p.dir, Layers: ls, ToolVersion: "test", LawPass: gate.StubLawPass, Ratchet: ratchet.NewJudge(ratchet.CollectMeasure(), defaultPaths)}
	run, err := gate.Execute(context.Background(), cfg, opts, d)
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

// fixedLayer is a layer that returns a result chosen by the test.
type fixedLayer struct {
	id  string
	res gate.Result
}

func (f fixedLayer) ID() string                                 { return f.id }
func (f fixedLayer) Tier() int                                  { return 1 }
func (f fixedLayer) Needs() []string                            { return nil }
func (f fixedLayer) Run(context.Context, *gate.Env) gate.Result { return f.res }

// lintFailure is a hygiene layer that failed on five lint findings and nothing
// else, with the saved lint output that matches.
func lintFailure(t *testing.T, dir string) gate.Layer {
	t.Helper()
	return lintFailureIn(t, dir, "revive", "low/low.go")
}

// lintFailureIn is the same layer with its five findings in the given file.
func lintFailureIn(t *testing.T, dir, linter, file string) gate.Layer {
	t.Helper()
	issue := `{"FromLinter":"` + linter + `","Text":"x","Pos":{"Filename":"` + file + `","Line":1}}`
	doc := `{"Issues":[` + strings.Repeat(issue+",", 4) + issue + `]}`
	if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(hygiene.LintJSON)), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	pass := func() map[string]any { return map[string]any{"outcome": "pass"} }
	return fixedLayer{id: "hygiene", res: gate.Result{Outcome: gate.Fail, Reason: "1 of 8 checks failed: lint: 5 finding(s)", Details: map[string]any{
		"golangci_lint": "v2.15.0", "findings": 5,
		"tidy": pass(), "verify": pass(), "toolchain": pass(), "format": pass(),
		"lint":            map[string]any{"outcome": "fail", "message": "5 finding(s)", "findings": 5, "by_linter": map[string]any{linter: 5}},
		"vulnerabilities": pass(), "licenses": pass(),
	}}}
}

// otherLegProject is the legacy repo with a ledger recorded under another build:
// this run is not the ledger leg.
func otherLegProject(t *testing.T) *ratchetProject {
	t.Helper()
	p := legacyProject(t)
	run, err := gobuild.Resolve(p.dir, tests.Build)
	if err != nil {
		t.Fatal(err)
	}
	other := gobuild.Build{GOOS: "plan9", GOARCH: "amd64", Tags: []string{}}
	if run.GOOS == "plan9" {
		other.GOOS = "openbsd"
	}
	p.ledgerBuild = &other
	p.writeFirstLedger(t)
	return p
}

func otherLegLayers(t *testing.T, p *ratchetProject) []gate.Layer {
	t.Helper()
	ls := layers(t)
	ls[0] = lintFailure(t, p.dir) // hygiene
	return ls
}

// Ruling C3c: on a leg that is not the ledger leg, what the ledger holds is reported
// and never fails the leg: the package under its floor, the lint findings and the
// legacy function over the cap. The ratchet is a skip that names both builds.
func TestOtherLegReportsLedgerHeldDebtAndFailsNothing(t *testing.T) {
	p := otherLegProject(t)
	run := runGateWith(t, p, "ratchet", otherLegLayers(t, p))
	o := outcomes(run)
	if o["coverage"] != "pass" || o["crap"] != "pass" || o["hygiene"] != "pass" || run.ExitCode != 0 {
		t.Fatalf("layers %v exit %d, want every layer pass and exit 0\n%s", o, run.ExitCode, run.Text)
	}
	r := run.Report.Ratchet
	if r == nil || r.Outcome != "skip" || r.LedgerBuild.GOOS == r.RunBuild.GOOS {
		t.Fatalf("ratchet %+v, want skip naming two different builds", r)
	}
	// Reported: the figures are still in the layers' details.
	for _, l := range run.Report.Layers {
		switch l.ID {
		case "coverage":
			if l.Details["statements_pct"] == nil {
				t.Errorf("coverage no longer reports its figures: %v", l.Details)
			}
		case "hygiene":
			if lint, ok := l.Details["lint"].(map[string]any); !ok || num(lint["findings"]) != 5 {
				t.Errorf("hygiene no longer reports its findings: %v", l.Details["lint"])
			}
		case "crap":
			if rows, ok := l.Details["violations"].([]any); !ok || len(rows) != 1 {
				t.Errorf("crap no longer lists the legacy function: %v", l.Details["violations"])
			}
		}
	}
}

// A function over the cap that only this leg compiles has no entry anywhere and
// fails the leg, as does an exemption that matches nothing.
func TestOtherLegFailsOnlyWhatC3cNames(t *testing.T) {
	p := otherLegProject(t)
	run, err := gobuild.Resolve(p.dir, tests.Build)
	if err != nil {
		t.Fatal(err)
	}
	big := "//go:build " + run.GOOS + "\n\npackage low\n\nfunc OnlyHere(a bool) int {\n" + strings.Repeat("\tif a {\n\t\treturn 1\n\t}\n", 11) + "\treturn 0\n}\n"
	if err := os.WriteFile(filepath.Join(p.dir, "low", "only_here.go"), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	res := runGateWith(t, p, "ratchet", otherLegLayers(t, p))
	if o := outcomes(res); o["crap"] != "fail" || res.ExitCode != 1 || !strings.Contains(res.Text, "example.com/m/low.OnlyHere") {
		t.Errorf("layers %v exit %d, want crap fail naming the function the ledger build does not compile\n%s", o, res.ExitCode, res.Text)
	}
	if res.Report.Ratchet == nil || res.Report.Ratchet.Outcome != "skip" {
		t.Errorf("ratchet %+v, want skip: the judgment is still the ledger leg's", res.Report.Ratchet)
	}

	q := otherLegProject(t)
	bad := runGateWith(t, q, "ratchet", otherLegLayers(t, q), exemptionFor("low/vanished.go"))
	if o := outcomes(bad); o["crap"] != "fail" || bad.ExitCode != 1 {
		t.Errorf("layers %v exit %d, want crap fail for an exemption that matches nothing", o, bad.ExitCode)
	}
}

// onlyHere is a file this host compiles and the ledger build (another platform) does
// not, with a function under the cap.
func (p *ratchetProject) writeOnlyHere(t *testing.T, dir, body string) {
	t.Helper()
	run, err := gobuild.Resolve(p.dir, tests.Build)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(p.dir, filepath.FromSlash(dir), "only_here.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Base(dir)
	src := "//go:build " + run.GOOS + "\n\npackage " + pkg + "\n\nfunc Here() {}\n" + body
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Ruling C3d, through the runner: a lint finding in a file the ledger build does not
// compile fails the leg at hard; the same finding in a file it compiles is reported.
func TestOtherLegFailsALintFindingOutsideTheLedgerBuild(t *testing.T) {
	p := otherLegProject(t)
	p.writeOnlyHere(t, "low", "")
	ls := layers(t)
	ls[0] = lintFailureIn(t, p.dir, "errcheck", "low/only_here.go")
	run := runGateWith(t, p, "ratchet", ls)
	if o := outcomes(run); o["hygiene"] != "fail" || run.ExitCode != 1 || !strings.Contains(run.Text, "errcheck low/only_here.go") {
		t.Errorf("layers %v exit %d, want hygiene fail naming the file\n%s", o, run.ExitCode, run.Text)
	}
	q := otherLegProject(t)
	ok := runGateWith(t, q, "ratchet", otherLegLayers(t, q))
	if o := outcomes(ok); o["hygiene"] != "pass" || ok.ExitCode != 0 {
		t.Errorf("the same findings in low/low.go, which the ledger build compiles: layers %v exit %d\n%s", o, ok.ExitCode, ok.Text)
	}
}

// A new //nolint in a file the ledger build does not compile fails the leg: the ledger
// has no entry for it, and the leg that compiles the file is the one that judges it.
func TestOtherLegFailsANolintOutsideTheLedgerBuild(t *testing.T) {
	p := otherLegProject(t)
	p.writeOnlyHere(t, "low", "\nfunc skipped() { //nolint:errcheck // a reason for the suppression\n}\n")
	run := runGateWith(t, p, "ratchet", layers(t))
	if run.ExitCode != 1 || run.Report.Ratchet == nil || run.Report.Ratchet.Outcome != "skip" || !strings.Contains(run.Text, "errcheck low/only_here.go") {
		t.Errorf("ratchet %+v exit %d, want a skip that still fails the leg for the //nolint\n%s", run.Report.Ratchet, run.ExitCode, run.Text)
	}
}

// A package the ledger build does not compile, under the coverage floor, is held to
// the floor on the leg that compiles it.
func TestOtherLegFailsAPackageUnderTheFloorOutsideTheLedgerBuild(t *testing.T) {
	p := otherLegProject(t)
	p.writeOnlyHere(t, "onlyhere", "")
	prof := filepath.Join(p.dir, filepath.FromSlash(tests.ProfilePath))
	f, err := os.OpenFile(prof, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("example.com/m/onlyhere/only_here.go:3.1,9.2 20 1\nexample.com/m/onlyhere/only_here.go:10.1,19.2 80 0\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	run := runGateWith(t, p, "ratchet", layers(t))
	if o := outcomes(run); o["coverage"] != "fail" || run.ExitCode != 1 || !strings.Contains(run.Text, "example.com/m/onlyhere") {
		t.Errorf("layers %v exit %d, want coverage fail naming the package\n%s", o, run.ExitCode, run.Text)
	}
}

// Ruling C9, what "the gate passes under ratchet" means at M2: the report has
// ratchet.outcome pass and every tier 0-1 layer pass, and the only failing item is
// the class's required laws, reported as required and not evaluated until M5. With
// no required laws the gate exits 0. This is the 6.7 walk-through's step 5 read from
// the report, the way the kit assertion step reads it.
func TestWalkThroughStep5ThroughTheReport(t *testing.T) {
	for _, required := range [][]string{nil, {"G-02", "G-07"}} {
		p := legacyProject(t)
		p.writeFirstLedger(t)
		run := runGateRequired(t, p, "ratchet", required)
		rep := run.Report
		if rep.Ratchet == nil || rep.Ratchet.Outcome != "pass" {
			t.Fatalf("required %v: ratchet %+v, want pass\n%s", required, rep.Ratchet, run.Text)
		}
		for _, id := range []string{"hygiene", "tests", "coverage", "crap"} {
			if got := outcomes(run)[id]; got != "pass" {
				t.Errorf("required %v: layer %s %s, want pass", required, id, got)
			}
		}
		if len(required) == 0 {
			if run.ExitCode != 0 || len(rep.Summary.Failed) != 0 {
				t.Errorf("no required laws: exit %d failed %v, want 0 and nothing failed", run.ExitCode, rep.Summary.Failed)
			}
			continue
		}
		if run.ExitCode != 0 || len(rep.Summary.Failed) != 0 {
			t.Errorf("required %v: exit %d failed %v, want pass when delegated laws allow", required, run.ExitCode, rep.Summary.Failed)
		}
		if rep.Laws.Required != len(required) || rep.Laws.RequiredNotEvaluated != 0 {
			t.Errorf("laws %+v, want %d required and all judged", rep.Laws, len(required))
		}
	}
}

// Through the runner, in report.json: the tests layer's shuffle seed is byte-identical
// under ratchet, whatever the ratchet rewrites in the layers it judges.
func TestRatchetKeepsTheSeedByteIdenticalInTheReport(t *testing.T) {
	p := legacyProject(t)
	p.writeFirstLedger(t)
	ls := layers(t)
	ls[1] = fixedLayer{id: "tests", res: gate.Result{Outcome: gate.Pass, Details: map[string]any{
		"packages": 3, "tests": 12, "race": true, "skipped": 0, "shuffle_seed": int64(1768300000000000123),
	}}}
	run := runGateWith(t, p, "ratchet", ls)
	if run.Report.Ratchet == nil || run.Report.Ratchet.Outcome != "pass" {
		t.Fatalf("ratchet %+v\n%s", run.Report.Ratchet, run.Text)
	}
	raw, err := report.Marshal(run.Report)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`"shuffle_seed":\s*1768300000000000123\b`).Match(raw) {
		t.Errorf("report.json does not carry the seed byte-identical:\n%s", raw)
	}
}

// Ruling C3e: in a partial run under ratchet the thresholds the ledger replaces are
// not judged at all. The ledger here holds Big at 5 and no finding: against it Big at
// 11 and five findings fail a full run, and a partial run reports them as not judged.
func TestPartialRunUnderRatchetJudgesNothingTheLedgerReplaces(t *testing.T) {
	p := legacyProject(t)
	p.findings = ratchet.Counts{"revive": {"low/low.go": 1}}
	p.writeLedgerFor(t, map[string]float64{"example.com/m/low.Big": 5}, 0)

	full := runGateWith(t, p, "ratchet", layers(t))
	if full.ExitCode != 1 {
		t.Fatalf("the full range exits %d, want 1: Big is over its entry\n%s", full.ExitCode, full.Text)
	}
	for _, only := range []string{"crap", "coverage", "hygiene"} {
		ls := layers(t)
		ls[0] = lintFailure(t, p.dir)
		cfg := gateConfig("ratchet", nil)
		run := executeWith(t, p, cfg, ls, gate.Options{MaxTier: 1, Base: "main", Only: only})
		if run.ExitCode != 0 || run.Report.Ratchet != nil || contains(run.Report.Summary.Failed, "ratchet") {
			t.Errorf("--only %s: exit %d ratchet %v failed %v, want a run with no ratchet object that fails nothing\n%s", only, run.ExitCode, run.Report.Ratchet, run.Report.Summary.Failed, run.Text)
		}
		for _, l := range run.Report.Layers {
			if l.ID == only && (l.Outcome != "pass" || !strings.Contains(l.Reason, "not judged: partial run under ratchet")) {
				t.Errorf("--only %s: layer %s %s %q, want a pass booked as not judged", only, l.ID, l.Outcome, l.Reason)
			}
		}
	}
}

func contains(list []string, want string) bool { return slices.Contains(list, want) }

// What the ledger does not replace stands in a partial run.
func TestPartialRunKeepsAnUnmatchedExemptionAndANotMeasuredFunction(t *testing.T) {
	p := legacyProject(t)
	p.writeFirstLedger(t)
	cfg := gateConfig("ratchet", []report.Exemption{exemptionFor("low/vanished.go")})
	run := executeWith(t, p, cfg, layers(t), gate.Options{MaxTier: 1, Base: "main", Only: "crap"})
	if o := outcomes(run); o["crap"] != "fail" || run.ExitCode != 1 {
		t.Errorf("an unmatched exemption: layers %v exit %d, want crap fail\n%s", o, run.ExitCode, run.Text)
	}
	q := legacyProject(t)
	q.writeFirstLedger(t)
	big := "package low\n\nfunc OnWindows(a bool) int {\n" + strings.Repeat("\tif a {\n\t\treturn 1\n\t}\n", 11) + "\treturn 0\n}\n"
	if err := os.WriteFile(filepath.Join(q.dir, "low", "big_windows.go"), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	run = executeWith(t, q, gateConfig("ratchet", nil), layers(t), gate.Options{MaxTier: 1, Base: "main", Only: "crap"})
	if o := outcomes(run); o["crap"] != "fail" || !strings.Contains(run.Text, "example.com/m/low.OnWindows") {
		t.Errorf("a not-measured function over the cap: layers %v\n%s", o, run.Text)
	}
}

// Held verdicts are booked to the layer that owns them: a finding or a suppression
// to hygiene and a package to coverage, never to the CRAP layer, which fails for
// functions only. summary.failed names the layer that failed.
func TestOtherLegBooksHeldVerdictsToTheirOwnLayers(t *testing.T) {
	p := otherLegProject(t)
	p.writeOnlyHere(t, "low", "\nfunc skipped() { //nolint:errcheck // a reason for the suppression\n}\n")
	ls := layers(t)
	ls[0] = lintFailureIn(t, p.dir, "errcheck", "low/only_here.go")
	run := runGateWith(t, p, "ratchet", ls)
	o := outcomes(run)
	if o["hygiene"] != "fail" || o["crap"] != "pass" || o["coverage"] != "pass" || !slices.Contains(run.Report.Summary.Failed, "hygiene") {
		t.Errorf("layers %v failed %v, want only hygiene\n%s", o, run.Report.Summary.Failed, run.Text)
	}
	q := otherLegProject(t)
	q.writeOnlyHere(t, "onlyhere", "")
	prof := filepath.Join(q.dir, filepath.FromSlash(tests.ProfilePath))
	f, err := os.OpenFile(prof, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("example.com/m/onlyhere/only_here.go:3.1,9.2 20 1\nexample.com/m/onlyhere/only_here.go:10.1,19.2 80 0\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	pk := runGateWith(t, q, "ratchet", layers(t))
	if o := outcomes(pk); o["coverage"] != "fail" || o["crap"] != "pass" || !slices.Equal(pk.Report.Summary.Failed, []string{"coverage"}) {
		t.Errorf("layers %v failed %v, want only coverage\n%s", o, pk.Report.Summary.Failed, pk.Text)
	}
}

// Ruling C3e, refined: a partial run does not judge an item that has an entry in the
// working-tree ledger, and judges one with no entry at hard. A partial run never hides
// new debt. The same repo and the same run, with and without the entry.

func partialRun(t *testing.T, p *ratchetProject, only string, ls []gate.Layer) *gate.Run {
	t.Helper()
	return executeWith(t, p, gateConfig("ratchet", nil), ls, gate.Options{MaxTier: 1, Base: "main", Only: only})
}

func TestPartialRunJudgesAFunctionWithNoEntryAtTheCap(t *testing.T) {
	p := legacyProject(t)
	p.writeLedgerFor(t, map[string]float64{}, 0)
	run := partialRun(t, p, "crap", layers(t))
	if o := outcomes(run); o["crap"] != "fail" || run.ExitCode != 1 || !strings.Contains(run.Text, "example.com/m/low.Big") {
		t.Errorf("layers %v exit %d, want crap fail naming the function with no entry\n%s", o, run.ExitCode, run.Text)
	}
	q := legacyProject(t)
	q.writeFirstLedger(t) // Big has an entry
	held := partialRun(t, q, "crap", layers(t))
	if o := outcomes(held); o["crap"] != "pass" || held.ExitCode != 0 {
		t.Errorf("with an entry: layers %v exit %d, want a pass not judged\n%s", o, held.ExitCode, held.Text)
	}
}

func TestPartialRunJudgesAPackageWithNoEntryAtTheFloor(t *testing.T) {
	p := legacyProject(t)
	p.noPackage = true
	p.writeLedgerFor(t, map[string]float64{"example.com/m/low.Big": 11}, 0)
	run := partialRun(t, p, "coverage", layers(t))
	if o := outcomes(run); o["coverage"] != "fail" || run.ExitCode != 1 || !strings.Contains(run.Text, "example.com/m/low") {
		t.Errorf("layers %v exit %d, want coverage fail naming the package with no entry\n%s", o, run.ExitCode, run.Text)
	}
	q := legacyProject(t)
	q.writeFirstLedger(t) // the package has an entry
	held := partialRun(t, q, "coverage", layers(t))
	if o := outcomes(held); o["coverage"] != "pass" || held.ExitCode != 0 {
		t.Errorf("with an entry: layers %v exit %d, want a pass not judged\n%s", o, held.ExitCode, held.Text)
	}
}

func TestPartialRunJudgesALintFindingWithNoEntryAtZero(t *testing.T) {
	p := legacyProject(t)
	p.writeFirstLedger(t)
	ls := layers(t)
	ls[0] = lintFailure(t, p.dir) // five revive findings in low/low.go, the ledger records none
	run := partialRun(t, p, "hygiene", ls)
	if o := outcomes(run); o["hygiene"] != "fail" || run.ExitCode != 1 || !strings.Contains(run.Text, "revive low/low.go") {
		t.Errorf("layers %v exit %d, want hygiene fail naming the finding with no entry\n%s", o, run.ExitCode, run.Text)
	}
	q := legacyProject(t)
	q.findings = ratchet.Counts{"revive": {"low/low.go": 1}}
	q.writeFirstLedger(t)
	lq := layers(t)
	lq[0] = lintFailure(t, q.dir)
	held := partialRun(t, q, "hygiene", lq)
	if o := outcomes(held); o["hygiene"] != "pass" || held.ExitCode != 0 {
		t.Errorf("with an entry: layers %v exit %d, want a pass not judged\n%s", o, held.ExitCode, held.Text)
	}
}
