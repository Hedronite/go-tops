package ratchet_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	doc := map[string]any{
		"build":  map[string]any{"goos": l.Build.GOOS, "goarch": l.Build.GOARCH, "tags": nonNil(l.Build.Tags), "cgo_enabled": l.Build.CGOEnabled},
		"ledger": map[string]any{"functions": l.Functions}, "judged_laws": l.JudgedLaws,
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
	b, err := gobuild.Resolve(dir, tests.Build)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// measurerWithPaths adapts a fixed measure to NewJudge on the default paths.
func measurerWithPaths(m ratchet.Measure, err error) (ratchet.Measurer, func(string) (ratchet.Paths, error)) {
	return measurer(m, err), defaultPaths
}

func measurer(m ratchet.Measure, err error) ratchet.Measurer {
	return func(context.Context, *gate.Env, ratchet.Inputs) (ratchet.Measure, error) {
		return m, err
	}
}

func emptyMeasure() ratchet.Measure { return ratchet.Measure{Cap: 10, Floor: 90} }

func layersIn() []report.Layer {
	return []report.Layer{{ID: "crap", Tier: 1, Outcome: "pass", Details: map[string]any{"cap": 10.0, "violations": []any{}, "not_measured": 0}}}
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
	if len(v.Layers) != 1 {
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
	paths := func(string) (ratchet.Paths, error) { return ratchet.Paths{Ledger: custom}, nil }
	v, err := ratchet.NewJudge(measurer(emptyMeasure(), nil), paths)(context.Background(), env, layersIn())
	if err != nil {
		t.Fatal(err)
	}
	if v.Failed || v.Report.Outcome != "pass" {
		t.Errorf("verdict %+v notes %v, want a pass from the ledger at %s", v.Report, v.Notes, custom)
	}
	// And a missing ledger at the configured path is a failure, whatever sits at the default.
	missing := func(string) (ratchet.Paths, error) { return ratchet.Paths{Ledger: "ledgers/absent.json"}, nil }
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
	bad := func(string) (ratchet.Paths, error) { return ratchet.Paths{}, errors.New("go-tops.yaml is invalid") }
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
	git(t, env.Dir, "add", "-A")
	git(t, env.Dir, "commit", "-q", "-m", "adopt the ledger")
	paths := func(string) (ratchet.Paths, error) { return ratchet.Paths{Ledger: custom}, nil }
	v, err := ratchet.NewJudge(measurer(emptyMeasure(), nil), paths)(context.Background(), env, layersIn())
	if err != nil {
		t.Fatal(err)
	}
	if v.Failed || v.Report.Outcome != "pass" {
		t.Errorf("verdict %+v notes %v, want a pass: the ledger is unchanged against its committed copy", v.Report, v.Notes)
	}
}
