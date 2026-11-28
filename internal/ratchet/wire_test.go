package ratchet_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hedronite/go-tops/internal/baseline"
	"github.com/Hedronite/go-tops/internal/crap"
	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/gobuild"
	"github.com/Hedronite/go-tops/internal/layers/tests"
	"github.com/Hedronite/go-tops/internal/ratchet"
	"github.com/Hedronite/go-tops/internal/report"
)

// The gate's hook, end to end on a real module and a real git repository.

// project is a module with one commit on main and the baseline file written in
// the tree; it returns the gate env for it.
func project(t *testing.T, ledger *ratchet.Ledger) *gate.Env {
	t.Helper()
	dir := repo(t)
	commit(t, dir, "adopt", map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.26\n",
		"m.go":   "package m\n\nfunc F() int { return 1 }\n",
	})
	if ledger != nil {
		writeLedger(t, dir, ledger)
	}
	return &gate.Env{Dir: dir, Base: "main", Config: gate.Config{Module: "example.com/m", Enforce: "ratchet"}}
}

func writeLedger(t *testing.T, dir string, l *ratchet.Ledger) {
	t.Helper()
	writeLedgerAt(t, dir, ratchet.LedgerPath, l)
}

func writeLedgerAt(t *testing.T, dir, rel string, l *ratchet.Ledger) {
	t.Helper()
	ledger := map[string]any{"functions": l.Functions}
	if len(l.Findings) > 0 {
		ledger["findings"] = l.Findings
	}
	doc := map[string]any{
		"build":  map[string]any{"goos": l.Build.GOOS, "goarch": l.Build.GOARCH, "tags": nonNil(l.Build.Tags), "cgo_enabled": l.Build.CGOEnabled},
		"ledger": ledger, "judged_laws": l.JudgedLaws,
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func resolved(t *testing.T, dir string) gobuild.Build {
	t.Helper()
	b, err := gobuild.Resolve(context.Background(), dir, tests.Build)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// measurerWithPaths adapts a fixed measure to NewJudge on the default paths.
func measurerWithPaths(m ratchet.Measure, err error) (ratchet.Measurer, func(context.Context, string) (ratchet.Paths, error)) {
	return measurer(m, err), defaultPaths
}

func measurer(m ratchet.Measure, err error) ratchet.Measurer {
	return func(context.Context, *gate.Env, ratchet.Inputs) (ratchet.Measure, error) {
		return m, err
	}
}

func emptyMeasure() ratchet.Measure { return ratchet.Measure{Cap: 10, Floor: 90} }

// layersIn is a whole range: the four layers the ledger is measured from.
func layersIn() []report.Layer {
	return []report.Layer{
		{ID: "hygiene", Tier: 1, Outcome: "pass", Details: map[string]any{"golangci_lint": "v2.15.0", "findings": 0}},
		{ID: "tests", Tier: 1, Outcome: "pass", Details: map[string]any{"packages": 3, "tests": 12, "race": true, "skipped": 0, "shuffle_seed": 7}},
		{ID: "coverage", Tier: 1, Outcome: "pass", Details: map[string]any{"statements_pct": 95.0, "floor": 90.0, "profile_bytes": 100}},
		{ID: "crap", Tier: 1, Outcome: "pass", Details: map[string]any{"cap": 10.0, "violations": []any{}, "not_measured": 0}},
	}
}

func TestJudgeHookOnTheLedgerLegWithAFirstLedgerPasses(t *testing.T) {
	env := project(t, nil)
	build := resolved(t, env.Dir)
	writeLedger(t, env.Dir, &ratchet.Ledger{Build: build})
	v, err := ratchet.NewJudge(measurerWithPaths(emptyMeasure(), nil))(context.Background(), env, layersIn())
	if err != nil {
		t.Fatal(err)
	}
	if v.Failed || v.Report.Outcome != "pass" || v.Report.LedgerBuild.GOOS != build.GOOS || v.Report.RunBuild.GOOS != build.GOOS {
		t.Errorf("verdict %+v notes %v", v, v.Notes)
	}
	if len(v.Layers) != 4 {
		t.Errorf("layers %v, want the run's layers back", v.Layers)
	}
}

// A ledger recorded under another build makes this run another leg: the ratchet
// is skipped, naming both builds, and the compiled set comes from loading with
// the recorded build.
func TestJudgeHookOnAnotherLegSkips(t *testing.T) {
	env := project(t, nil)
	run := resolved(t, env.Dir)
	other := gobuild.Build{GOOS: "windows", GOARCH: "arm64"}
	if run.GOOS == "windows" {
		other.GOOS = "linux"
	}
	writeLedger(t, env.Dir, &ratchet.Ledger{Build: other})
	v, err := ratchet.NewJudge(measurerWithPaths(emptyMeasure(), nil))(context.Background(), env, layersIn())
	if err != nil {
		t.Fatal(err)
	}
	if v.Report.Outcome != "skip" || v.Report.LedgerBuild.GOOS != other.GOOS || v.Report.RunBuild.GOOS != run.GOOS {
		t.Errorf("report %+v, want skip naming the ledger's %s and this run's %s", v.Report, other.GOOS, run.GOOS)
	}
	if v.Failed {
		t.Errorf("a skip failed the run: %v", v.Notes)
	}
}

// A function this leg compiles that the ledger leg does not is held to the cap.
func TestJudgeHookHoldsFunctionsTheLedgerBuildDoesNotCompile(t *testing.T) {
	env := project(t, nil)
	run := resolved(t, env.Dir)
	if err := os.WriteFile(filepath.Join(env.Dir, "only_here.go"), []byte("package m\n\nfunc Here() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The ledger build is one that excludes the file: put a constraint on it.
	if err := os.WriteFile(filepath.Join(env.Dir, "only_here.go"), []byte("//go:build "+run.GOOS+"\n\npackage m\n\nfunc Here() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	other := gobuild.Build{GOOS: "plan9", GOARCH: "amd64"}
	writeLedger(t, env.Dir, &ratchet.Ledger{Build: other})
	m := emptyMeasure()
	m.Functions = []crap.Function{{Key: "example.com/m.Here", File: "only_here.go", Comp: 12, Cov: 1, CRAP: 12}}
	v, err := ratchet.NewJudge(measurerWithPaths(m, nil))(context.Background(), env, layersIn())
	if err != nil {
		t.Fatal(err)
	}
	if !v.Failed || v.Report.Outcome != "skip" || len(v.Notes) != 1 || !strings.Contains(v.Notes[0], "example.com/m.Here") {
		t.Errorf("verdict %+v notes %v, want a skip that still fails for the function over the cap", v.Report, v.Notes)
	}
}

func TestJudgeHookFailsWithoutALedger(t *testing.T) {
	env := project(t, nil)
	v, err := ratchet.NewJudge(measurerWithPaths(emptyMeasure(), nil))(context.Background(), env, layersIn())
	if err != nil {
		t.Fatal(err)
	}
	if !v.Failed || v.Report.Outcome != "fail" || !strings.Contains(strings.Join(v.Notes, ""), "no baseline file") {
		t.Errorf("verdict %+v notes %v, want a failure for the missing baseline", v.Report, v.Notes)
	}
}

func TestJudgeHookFailsOnAnUnreadableLedger(t *testing.T) {
	env := project(t, nil)
	p := filepath.Join(env.Dir, filepath.FromSlash(ratchet.LedgerPath))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	v, err := ratchet.NewJudge(measurerWithPaths(emptyMeasure(), nil))(context.Background(), env, layersIn())
	if err != nil {
		t.Fatal(err)
	}
	if !v.Failed || v.Report.Outcome != "fail" || !strings.Contains(strings.Join(v.Notes, ""), "baseline") {
		t.Errorf("verdict %+v notes %v", v.Report, v.Notes)
	}
}

func TestJudgeHookFailsOnAShallowCloneOrNoMergeBase(t *testing.T) {
	env := project(t, nil)
	writeLedger(t, env.Dir, &ratchet.Ledger{Build: resolved(t, env.Dir)})
	env.Base = "origin/main" // no such ref
	v, err := ratchet.NewJudge(measurerWithPaths(emptyMeasure(), nil))(context.Background(), env, layersIn())
	if err != nil {
		t.Fatal(err)
	}
	if !v.Failed || !strings.Contains(strings.Join(v.Notes, ""), "diff base") {
		t.Errorf("verdict %+v notes %v, want a failure naming the diff base", v.Report, v.Notes)
	}
}

// A run that could not be measured is a failed ratchet that says so, with the
// layers as they ran: nothing is judged and nothing is guessed.
func TestJudgeHookApplyPartialWhenTestsLayerBlocksMeasure(t *testing.T) {
	env := project(t, nil)
	build := resolved(t, env.Dir)
	writeLedger(t, env.Dir, &ratchet.Ledger{
		Build: build,
		Findings: ratchet.Counts{
			"revive": {"x.go": 5},
		},
	})
	yaml, err := os.ReadFile(filepath.Join("..", "..", "go-tops.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.Dir, "go-tops.yaml"), yaml, 0o600); err != nil {
		t.Fatal(err)
	}
	lintJSON := `{"Issues":[`
	for i := 0; i < 5; i++ {
		if i > 0 {
			lintJSON += ","
		}
		lintJSON += `{"FromLinter":"revive","Pos":{"Filename":"x.go","Line":1}}`
	}
	lintJSON += `]}`
	if err := os.MkdirAll(filepath.Join(env.Dir, ".go-tops"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.Dir, ".go-tops/lint-style.json"), []byte(lintJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	env.Config = gate.Config{Module: "example.com/m", Enforce: "ratchet", Crap: gate.CrapConfig{Cap: 10}}
	in := layersIn()
	in[0] = report.Layer{
		ID: "hygiene", Tier: 1, Outcome: "fail",
		Details: map[string]any{
			"golangci_lint": "v2.15.0", "findings": 5,
			"lint": map[string]any{"outcome": "fail", "findings": 5, "by_linter": map[string]any{"revive": 5}},
		},
	}
	ref := &baseline.RefusalError{Msg: "tests layer is red"}
	v, err := ratchet.NewJudge(measurerWithPaths(ratchet.Measure{}, ref))(context.Background(), env, in)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Failed {
		t.Fatal("want failed ratchet verdict")
	}
	if v.Layers[0].Outcome != "pass" {
		t.Fatalf("hygiene after partial=%s reason=%q", v.Layers[0].Outcome, v.Layers[0].Reason)
	}
}

func TestJudgeHookTestsRefusalWithoutLedgerDoesNotPartial(t *testing.T) {
	env := project(t, nil)
	yaml, err := os.ReadFile(filepath.Join("..", "..", "go-tops.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.Dir, "go-tops.yaml"), yaml, 0o600); err != nil {
		t.Fatal(err)
	}
	lintJSON := `{"Issues":[`
	for i := 0; i < 5; i++ {
		if i > 0 {
			lintJSON += ","
		}
		lintJSON += `{"FromLinter":"revive","Pos":{"Filename":"x.go","Line":1}}`
	}
	lintJSON += `]}`
	if err := os.MkdirAll(filepath.Join(env.Dir, ".go-tops"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.Dir, ".go-tops/lint-style.json"), []byte(lintJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	env.Config = gate.Config{Module: "example.com/m", Enforce: "ratchet", Crap: gate.CrapConfig{Cap: 10}}
	ref := &baseline.RefusalError{Msg: "tests layer red"}
	in := layersIn()
	in[0] = report.Layer{
		ID: "hygiene", Tier: 1, Outcome: "fail",
		Details: map[string]any{
			"golangci_lint": "v2.15.0", "findings": 5,
			"lint": map[string]any{"outcome": "fail", "findings": 5, "by_linter": map[string]any{"revive": 5}},
		},
	}
	v, err := ratchet.NewJudge(measurerWithPaths(ratchet.Measure{}, ref))(context.Background(), env, in)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Failed || v.Layers[0].Outcome != "fail" {
		t.Fatalf("hygiene=%s layers=%+v", v.Layers[0].Outcome, v.Layers)
	}
	if v.Layers[0].Reason != in[0].Reason {
		t.Fatalf("reason=%q want %q (partial without ledger)", v.Layers[0].Reason, in[0].Reason)
	}
}

func TestJudgeHookSkipsPartialWhenFindingsFail(t *testing.T) {
	env := project(t, nil)
	writeLedger(t, env.Dir, &ratchet.Ledger{Build: resolved(t, env.Dir)})
	ref := &baseline.RefusalError{Msg: "tests layer red"}
	in := layersIn()
	v, err := ratchet.NewJudge(measurerWithPaths(ratchet.Measure{}, ref))(context.Background(), env, in)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Failed || v.Layers[0].Outcome != in[0].Outcome {
		t.Fatalf("layers=%+v", v.Layers)
	}
}

func TestJudgeHookNoPartialOnNonTestsMeasureError(t *testing.T) {
	env := project(t, nil)
	build := resolved(t, env.Dir)
	writeLedger(t, env.Dir, &ratchet.Ledger{Build: build, Findings: ratchet.Counts{"revive": {"x.go": 5}}})
	yaml, err := os.ReadFile(filepath.Join("..", "..", "go-tops.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.Dir, "go-tops.yaml"), yaml, 0o600); err != nil {
		t.Fatal(err)
	}
	lintJSON := `{"Issues":[`
	for i := 0; i < 5; i++ {
		if i > 0 {
			lintJSON += ","
		}
		lintJSON += `{"FromLinter":"revive","Pos":{"Filename":"x.go","Line":1}}`
	}
	lintJSON += `]}`
	if err := os.MkdirAll(filepath.Join(env.Dir, ".go-tops"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.Dir, ".go-tops/lint-style.json"), []byte(lintJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	env.Config = gate.Config{Module: "example.com/m", Enforce: "ratchet", Crap: gate.CrapConfig{Cap: 10}}
	in := layersIn()
	in[0] = report.Layer{
		ID: "hygiene", Tier: 1, Outcome: "fail",
		Details: map[string]any{
			"golangci_lint": "v2.15.0", "findings": 5,
			"lint": map[string]any{"outcome": "fail", "findings": 5, "by_linter": map[string]any{"revive": 5}},
		},
	}
	v, err := ratchet.NewJudge(measurerWithPaths(ratchet.Measure{}, errors.New("coverage profile missing")))(context.Background(), env, in)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Failed || v.Layers[0].Outcome != "fail" {
		t.Fatalf("hygiene=%s", v.Layers[0].Outcome)
	}
}

func TestJudgeHookNoPartialWithoutLedgerOnTestsRefusal(t *testing.T) {
	env := project(t, nil)
	ref := &baseline.RefusalError{Msg: "tests layer failed"}
	v, err := ratchet.NewJudge(measurerWithPaths(ratchet.Measure{}, ref))(context.Background(), env, layersIn())
	if err != nil {
		t.Fatal(err)
	}
	if !v.Failed {
		t.Fatal("want failed verdict")
	}
}

func TestJudgeHookRefusesARunItCouldNotMeasure(t *testing.T) {
	env := project(t, nil)
	writeLedger(t, env.Dir, &ratchet.Ledger{Build: resolved(t, env.Dir)})
	in := layersIn()
	v, err := ratchet.NewJudge(measurerWithPaths(ratchet.Measure{}, errors.New("no profile")))(context.Background(), env, in)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Failed || v.Report.Outcome != "fail" || !strings.Contains(strings.Join(v.Notes, ""), "no profile") {
		t.Errorf("verdict %+v notes %v", v.Report, v.Notes)
	}
	if len(v.Layers) != len(in) || v.Layers[0].Outcome != in[0].Outcome {
		t.Errorf("layers %v, want the run's layers unchanged", v.Layers)
	}
}

// The ledger is read at the path the configuration names, never at a built-in one:
// `check` and `baseline` read the same key.
func TestJudgeHookReadsTheConfiguredBaselinePath(t *testing.T) {
	env := project(t, nil)
	build := resolved(t, env.Dir)
	const custom = "ledgers/main.json"
	writeLedgerAt(t, env.Dir, custom, &ratchet.Ledger{Build: build})
	// A file at the default path that would fail the judge if it were read.
	if err := os.MkdirAll(filepath.Join(env.Dir, ".go-tops"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.Dir, filepath.FromSlash(ratchet.LedgerPath)), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := func(context.Context, string) (ratchet.Paths, error) { return ratchet.Paths{Ledger: custom}, nil }
	v, err := ratchet.NewJudge(measurer(emptyMeasure(), nil), paths)(context.Background(), env, layersIn())
	if err != nil {
		t.Fatal(err)
	}
	if v.Failed || v.Report.Outcome != "pass" {
		t.Errorf("verdict %+v notes %v, want a pass from the ledger at %s", v.Report, v.Notes, custom)
	}
	// And a missing ledger at the configured path is a failure, whatever sits at the default.
	missing := func(context.Context, string) (ratchet.Paths, error) {
		return ratchet.Paths{Ledger: "ledgers/absent.json"}, nil
	}
	v, err = ratchet.NewJudge(measurer(emptyMeasure(), nil), missing)(context.Background(), env, layersIn())
	if err != nil {
		t.Fatal(err)
	}
	if !v.Failed || !strings.Contains(strings.Join(v.Notes, ""), "no baseline file") {
		t.Errorf("verdict %+v notes %v, want a failure for the missing ledger", v.Report, v.Notes)
	}
}

// A configuration that cannot be read fails the ratchet and says so.
func TestJudgeHookRefusesWhenTheConfigurationCannotBeRead(t *testing.T) {
	env := project(t, nil)
	bad := func(context.Context, string) (ratchet.Paths, error) {
		return ratchet.Paths{}, errors.New("go-tops.yaml is invalid")
	}
	v, err := ratchet.NewJudge(measurer(emptyMeasure(), nil), bad)(context.Background(), env, layersIn())
	if err != nil {
		t.Fatal(err)
	}
	if !v.Failed || !strings.Contains(strings.Join(v.Notes, ""), "go-tops.yaml is invalid") {
		t.Errorf("verdict %+v notes %v", v.Report, v.Notes)
	}
}

// The base copy is read at the configured path as well: a ledger committed there
// with an entry, and unchanged in the tree, is no first ledger. Read at the default
// path the base would hold nothing, the entry would have no function to equal, and
// the first-ledger rule would fail it.
func TestJudgeHookReadsTheBaseCopyAtTheConfiguredPath(t *testing.T) {
	env := project(t, nil)
	build := resolved(t, env.Dir)
	const custom = "ledgers/main.json"
	writeLedgerAt(t, env.Dir, custom, &ratchet.Ledger{Build: build, Functions: map[string]float64{"example.com/m.F": 12}})
	if err := os.WriteFile(filepath.Join(env.Dir, "go-tops.yaml"), []byte("baseline: "+custom+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, env.Dir, "add", "-A")
	git(t, env.Dir, "commit", "-q", "-m", "adopt the ledger")
	paths := func(context.Context, string) (ratchet.Paths, error) { return ratchet.Paths{Ledger: custom}, nil }
	v, err := ratchet.NewJudge(measurer(emptyMeasure(), nil), paths)(context.Background(), env, layersIn())
	if err != nil {
		t.Fatal(err)
	}
	if v.Failed || v.Report.Outcome != "pass" {
		t.Errorf("verdict %+v notes %v, want a pass: the ledger is unchanged against its committed copy", v.Report, v.Notes)
	}
}

// The hook returns the layers as they ran: the tests layer's shuffle seed, an int64
// of 19 digits, comes out byte-identical, so a failing run can be replayed with
// -test.shuffle from the report.
func TestJudgeHookKeepsTheShuffleSeedByteIdentical(t *testing.T) {
	env := project(t, nil)
	writeLedger(t, env.Dir, &ratchet.Ledger{Build: resolved(t, env.Dir)})
	layers := layersIn()
	layers[1].Details = map[string]any{"packages": 3, "tests": 12, "race": true, "skipped": 0, "shuffle_seed": int64(1768300000000000123)}
	v, err := ratchet.NewJudge(measurer(emptyMeasure(), nil), defaultPaths)(context.Background(), env, layers)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(v.Layers)
	if err != nil || !strings.Contains(string(raw), `"shuffle_seed":1768300000000000123`) {
		t.Errorf("seed after the hook: %s (%v)", raw, err)
	}
	// And a refused judgment returns the layers as they ran too.
	bad, err := ratchet.NewJudge(measurer(ratchet.Measure{}, errors.New("no profile")), defaultPaths)(context.Background(), env, layers)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(bad.Layers)
	if err != nil || !strings.Contains(string(raw), `"shuffle_seed":1768300000000000123`) {
		t.Errorf("seed after a refused judgment: %s (%v)", raw, err)
	}
}

// The relocation cheat: a branch sets `baseline:` to a new path, deletes the old
// ledger and writes a new one as --init would, with a function worsened. At the new
// path there is nothing at the base, so read there it would be a first ledger and
// pass. The base copy is read where the base kept it, so the worsened entry needs a
// record and the line says the baseline moved.
func TestRelocatingTheLedgerDoesNotResetTheRatchet(t *testing.T) {
	env := project(t, nil)
	build := resolved(t, env.Dir)
	writeLedger(t, env.Dir, &ratchet.Ledger{Build: build, Functions: map[string]float64{"example.com/m.F": 11}})
	git(t, env.Dir, "add", "-A")
	git(t, env.Dir, "commit", "-q", "-m", "adopt the ratchet")
	git(t, env.Dir, "checkout", "-q", "-b", "feature")

	const custom = "ledger/debt.json"
	if err := os.Remove(filepath.Join(env.Dir, filepath.FromSlash(ratchet.LedgerPath))); err != nil {
		t.Fatal(err)
	}
	writeLedgerAt(t, env.Dir, custom, &ratchet.Ledger{Build: build, Functions: map[string]float64{"example.com/m.F": 17.3}})
	if err := os.WriteFile(filepath.Join(env.Dir, "go-tops.yaml"), []byte("baseline: "+custom+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, env.Dir, "add", "-A")
	git(t, env.Dir, "commit", "-q", "-m", "move the baseline and worsen a function")

	worse := ratchet.Measure{Cap: 10, Floor: 90, Functions: []crap.Function{{Key: "example.com/m.F", CRAP: 17.3}}}
	paths := func(context.Context, string) (ratchet.Paths, error) { return ratchet.Paths{Ledger: custom}, nil }
	v, err := ratchet.NewJudge(measurer(worse, nil), paths)(context.Background(), env, layersIn())
	if err != nil {
		t.Fatal(err)
	}
	notes := strings.Join(v.Notes, "")
	if !v.Failed || v.Report.Outcome != "fail" || !strings.Contains(notes, "functions raised from 11 to 17.3 with no regressions record") {
		t.Errorf("verdict %+v notes %q: the relocated ledger passed as a first ledger", v.Report, notes)
	}
	if !strings.Contains(notes, "the baseline moved from "+ratchet.LedgerPath+" at the diff base to "+custom) {
		t.Errorf("notes %q do not name both paths", notes)
	}
}

// Through the hook, with the module in a subdirectory: a hand-raised entry equal to what
// the run measures fails for want of a record, and a base configuration that does not
// parse fails the judgment.
func TestJudgeHookForAModuleInASubdirectory(t *testing.T) {
	root := repo(t)
	commit(t, root, "adopt", map[string]string{"mod/go.mod": "module example.com/m\n\ngo 1.26\n", "mod/m.go": "package m\n\nfunc F() int { return 1 }\n"})
	env := &gate.Env{Dir: filepath.Join(root, "mod"), Base: "main", Config: gate.Config{Module: "example.com/m", Enforce: "ratchet"}}
	build := resolved(t, env.Dir)
	writeLedger(t, env.Dir, &ratchet.Ledger{Build: build, Functions: map[string]float64{"example.com/m.F": 12}})
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "adopt the ratchet")
	git(t, root, "checkout", "-q", "-b", "feature")
	writeLedger(t, env.Dir, &ratchet.Ledger{Build: build, Functions: map[string]float64{"example.com/m.F": 17}})
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "raise by hand")

	worse := ratchet.Measure{Cap: 10, Floor: 90, Functions: []crap.Function{{Key: "example.com/m.F", CRAP: 17}}}
	v, err := ratchet.NewJudge(measurerWithPaths(worse, nil))(context.Background(), env, layersIn())
	if err != nil {
		t.Fatal(err)
	}
	if !v.Failed || !strings.Contains(strings.Join(v.Notes, ""), "functions raised from 12 to 17 with no regressions record") {
		t.Errorf("verdict %+v notes %v: the hand-raised entry passed as a first ledger", v.Report, v.Notes)
	}

	// And a base go-tops.yaml that does not parse.
	git(t, root, "checkout", "-q", "main")
	if err := os.WriteFile(filepath.Join(env.Dir, "go-tops.yaml"), []byte("baseline: [unclosed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "a broken configuration")
	git(t, root, "checkout", "-q", "-b", "other")
	git(t, root, "commit", "-q", "--allow-empty", "-m", "work")
	bad, err := ratchet.NewJudge(measurerWithPaths(worse, nil))(context.Background(), env, layersIn())
	if err != nil {
		t.Fatal(err)
	}
	if !bad.Failed || !strings.Contains(strings.Join(bad.Notes, ""), "go-tops.yaml at the diff base") {
		t.Errorf("verdict %+v notes %v, want a failure naming the base go-tops.yaml", bad.Report, bad.Notes)
	}
}

func writeCollectFeeds(t *testing.T, dir string) {
	t.Helper()
	write := func(rel, data string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".go-tops/cover.out", "mode: atomic\nexample.com/m/m.go:3.10,5.2 4 1\n")
	write(".go-tops/lint-style.json", `{"Issues":[]}`)
}

func TestCollectMeasureUsesRatchetLaws(t *testing.T) {
	env := project(t, nil)
	env.Config.Coverage = gate.CoverageConfig{StatementsFloor: 90}
	env.Config.Crap = gate.CrapConfig{Cap: 10}
	writeCollectFeeds(t, env.Dir)
	env.RatchetLaws = report.Laws{ApplicableJudged: 9}
	build := resolved(t, env.Dir)
	m, err := ratchet.CollectMeasure()(context.Background(), env, ratchet.Inputs{
		Layers: layersIn(), Build: build, Paths: ratchet.Paths{Ledger: ratchet.LedgerPath, Acks: ".go-tops/acks.yaml"}, Now: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.JudgedLaws != 9 {
		t.Fatalf("JudgedLaws = %d, want 9", m.JudgedLaws)
	}
}

func TestCollectMeasureWithOneJudgedLaw(t *testing.T) {
	env := project(t, nil)
	env.Config.Coverage = gate.CoverageConfig{StatementsFloor: 90}
	env.Config.Crap = gate.CrapConfig{Cap: 10}
	writeCollectFeeds(t, env.Dir)
	env.RatchetLaws = report.Laws{ApplicableJudged: 1}
	build := resolved(t, env.Dir)
	m, err := ratchet.CollectMeasure()(context.Background(), env, ratchet.Inputs{
		Layers: layersIn(), Build: build, Paths: ratchet.Paths{Ledger: ratchet.LedgerPath, Acks: ".go-tops/acks.yaml"}, Now: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.JudgedLaws != 1 {
		t.Fatalf("JudgedLaws = %d, want 1", m.JudgedLaws)
	}
}

func TestCollectMeasureWithoutRatchetLawsLeavesJudgedZero(t *testing.T) {
	env := project(t, nil)
	env.Config.Coverage = gate.CoverageConfig{StatementsFloor: 90}
	env.Config.Crap = gate.CrapConfig{Cap: 10}
	writeCollectFeeds(t, env.Dir)
	build := resolved(t, env.Dir)
	m, err := ratchet.CollectMeasure()(context.Background(), env, ratchet.Inputs{
		Layers: layersIn(), Build: build, Paths: ratchet.Paths{Ledger: ratchet.LedgerPath, Acks: ".go-tops/acks.yaml"}, Now: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.JudgedLaws != 0 {
		t.Fatalf("JudgedLaws = %d, want 0", m.JudgedLaws)
	}
}
