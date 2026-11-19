package baseline_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/baseline"
	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/report"
)

// GO_TOPS.md §6.7, "Writing the baseline": --init measures and writes when the
// tests are green and every feeding layer produced non-empty output (a
// non-empty coverage profile, a CRAP report with at least one production
// function, saved lint output). Red thresholds do not stop it; red tests or an
// empty output do.

const module = "example.com/app"

// profileOK has two packages: a (all four statements covered) and b (one of
// four covered), so b is under a 90% floor and a is not.
const profileOK = `mode: atomic
example.com/app/a/a.go:3.10,5.2 4 1
example.com/app/b/b.go:3.10,5.2 1 1
example.com/app/b/b.go:6.10,8.2 3 0
`

const lintJSON = `{"Issues":[
 {"FromLinter":"revive","Text":"x","Pos":{"Filename":"a/a.go","Line":3}},
 {"FromLinter":"revive","Text":"y","Pos":{"Filename":"a/a.go","Line":9}},
 {"FromLinter":"errcheck","Text":"z","Pos":{"Filename":"b/b.go","Line":4}}
]}`

// feeds is a repository the feeding layers have just run in.
type feeds struct {
	t     *testing.T
	dir   string
	in    baseline.Feeds
	write func(rel, data string)
}

func newFeeds(t *testing.T) *feeds {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, data string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".go-tops/cover.out", profileOK)
	write(".go-tops/lint-style.json", lintJSON)
	write("a/a.go", "package a\n")
	write("b/b.go", "package b\n")
	f := &feeds{t: t, dir: dir}
	f.in = baseline.Feeds{
		Dir:   dir,
		Build: testBuild,
		Config: gate.Config{
			Module: module, Coverage: gate.CoverageConfig{StatementsFloor: 90},
			Crap: gate.CrapConfig{Cap: 10},
		},
		Report: report.Report{
			Layers: []report.Layer{
				{ID: "hygiene", Outcome: "fail", Reason: "findings"},
				{ID: "tests", Outcome: "pass"},
				{ID: "coverage", Outcome: "fail", Details: map[string]any{"statements_pct": 62.5, "floor": 90.0, "profile_bytes": 100}},
				{ID: "crap", Outcome: "fail", Details: map[string]any{"cap": 10.0, "violations": fns(
					cf("example.com/app/a.Alpha", "a/a.go", 21.5),
					cf("example.com/app/b.T.Beta", "b/b.go", 132),
				)}},
			},
			Laws: report.Laws{ApplicableJudged: 4},
		},
	}
	f.write = write
	return f
}

// cf is one function of a crapDetails list, in the form of the report schema.
func cf(fn, file string, crap float64) map[string]any {
	return map[string]any{"func": fn, "file": file, "cc": 12, "cov_pct": 50.0, "crap": crap, "baseline": nil}
}

func fns(list ...map[string]any) []map[string]any { return append([]map[string]any{}, list...) }

func (f *feeds) remove(rel string) {
	if err := os.Remove(filepath.Join(f.dir, filepath.FromSlash(rel))); err != nil {
		f.t.Fatal(err)
	}
}

func collect(t *testing.T, f *feeds) baseline.Measure {
	t.Helper()
	m, err := baseline.Collect(f.in)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func (f *feeds) setLayer(id string, l report.Layer) {
	for i := range f.in.Report.Layers {
		if f.in.Report.Layers[i].ID == id {
			f.in.Report.Layers[i] = l
			return
		}
	}
	f.in.Report.Layers = append(f.in.Report.Layers, l)
}

func (f *feeds) dropLayer(id string) {
	var out []report.Layer
	for _, l := range f.in.Report.Layers {
		if l.ID != id {
			out = append(out, l)
		}
	}
	f.in.Report.Layers = out
}

func TestCollectMeasuresARunWhoseThresholdsAreRed(t *testing.T) {
	f := newFeeds(t)
	f.in.Config.Exemptions = []report.Exemption{{Path: "a/**", Layer: "crap"}, {Path: "b/**", Layer: "crap"}}
	got, err := baseline.Collect(f.in)
	if err != nil {
		t.Fatal(err)
	}
	want := baseline.Measure{
		Functions:  map[string]float64{"example.com/app/a.Alpha": 21.5, "example.com/app/b.T.Beta": 132},
		Packages:   map[string]baseline.Package{"example.com/app/b": {StatementsPct: 25, Uncovered: 3}},
		Findings:   baseline.Counts{"errcheck": {"b/b.go": 1}, "revive": {"a/a.go": 2}},
		Exemptions: 2,
		JudgedLaws: 4,
		Build:      testBuild,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Collect:\n%+v\nwant\n%+v", got, want)
	}
}

func TestCollectLeavesExemptFunctionsOutOfTheLedger(t *testing.T) {
	// An exempt function is scored and printed but not judged against the cap, so
	// it is not debt; the exemption count carries it.
	f := newFeeds(t)
	f.setLayer("crap", report.Layer{ID: "crap", Outcome: "pass", Details: map[string]any{
		"cap":        10.0,
		"violations": fns(cf("example.com/app/a.Alpha", "a/a.go", 21.5)),
		"exempt":     fns(cf("example.com/app/a.Table", "a/a.go", 40)),
	}})
	got, err := baseline.Collect(f.in)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Functions, map[string]float64{"example.com/app/a.Alpha": 21.5}) {
		t.Errorf("functions %v", got.Functions)
	}
}

// Protocol 0.4.3: a row for a function the run does not measure has null for
// cov_pct and crap. --init records what was measured, so it writes no entry for it.
func TestCollectWritesNoEntryForAFunctionThatWasNotMeasured(t *testing.T) {
	f := newFeeds(t)
	unmeasured := map[string]any{"func": "example.com/app/a.WinOnly", "file": "a/w_windows.go", "cc": 12, "cov_pct": nil, "crap": nil, "baseline": nil}
	f.setLayer("crap", report.Layer{ID: "crap", Outcome: "fail", Details: map[string]any{
		"cap": 10.0, "not_measured": 1,
		"violations": fns(cf("example.com/app/a.Alpha", "a/a.go", 21.5), unmeasured),
	}})
	got, err := baseline.Collect(f.in)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Functions, map[string]float64{"example.com/app/a.Alpha": 21.5}) {
		t.Errorf("functions %v: a function with a null score got an entry", got.Functions)
	}
}

func TestCollectReadsDetailsThatHaveBeenThroughJSON(t *testing.T) {
	// A layer's details are whatever it put in the map; a decoded report holds
	// generic maps. The reader must not care which.
	f := newFeeds(t)
	f.setLayer("crap", report.Layer{ID: "crap", Outcome: "fail", Details: map[string]any{
		"cap": 10.0,
		"violations": []any{
			map[string]any{"func": "example.com/app/a.Alpha", "file": "a/a.go", "cc": 12.0, "cov_pct": 50.0, "crap": 21.5, "baseline": nil},
		},
	}})
	got, err := baseline.Collect(f.in)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Functions, map[string]float64{"example.com/app/a.Alpha": 21.5}) {
		t.Errorf("functions %v", got.Functions)
	}
}

func TestCollectWithNothingOverTheLineIsAnEmptyMeasure(t *testing.T) {
	f := newFeeds(t)
	f.write(".go-tops/cover.out", "mode: atomic\nexample.com/app/a/a.go:3.10,5.2 4 1\n")
	f.write(".go-tops/lint-style.json", `{"Issues":[]}`)
	f.setLayer("crap", report.Layer{ID: "crap", Outcome: "pass", Details: map[string]any{"cap": 10.0, "violations": fns()}})
	got, err := baseline.Collect(f.in)
	if err != nil {
		t.Fatal(err)
	}
	want := baseline.Measure{JudgedLaws: 4, Build: testBuild}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%+v, want %+v", got, want)
	}
}

func TestCollectRefusesWhatWasNotMeasured(t *testing.T) {
	tests := map[string]func(f *feeds){
		"red tests": func(f *feeds) {
			f.setLayer("tests", report.Layer{ID: "tests", Outcome: "fail", Reason: "TestX failed"})
		},
		"tests not in the report": func(f *feeds) { f.dropLayer("tests") },
		"tests not run":           func(f *feeds) { f.setLayer("tests", report.Layer{ID: "tests", Outcome: "not-run"}) },
		"coverage not measured": func(f *feeds) {
			f.setLayer("coverage", report.Layer{ID: "coverage", Outcome: "fail", Details: map[string]any{"error": "the profile is empty"}})
		},
		"coverage not in report": func(f *feeds) { f.dropLayer("coverage") },
		"no profile file":        func(f *feeds) { f.remove(".go-tops/cover.out") },
		"empty profile":          func(f *feeds) { f.write(".go-tops/cover.out", "") },
		"profile with no blocks": func(f *feeds) { f.write(".go-tops/cover.out", "mode: atomic\n") },
		"profile only of generated": func(f *feeds) {
			f.write("a/a.go", "// Code generated by x. DO NOT EDIT.\n\npackage a\n")
			f.write(".go-tops/cover.out", "mode: atomic\nexample.com/app/a/a.go:3.10,5.2 4 1\n")
			f.in.Config.Coverage.ExcludeGenerated = true
		},
		"no lint output":           func(f *feeds) { f.remove(".go-tops/lint-style.json") },
		"empty lint output":        func(f *feeds) { f.write(".go-tops/lint-style.json", "  \n") },
		"lint output not golangci": func(f *feeds) { f.write(".go-tops/lint-style.json", `{"hello":1}`) },
		"crap did not run":         func(f *feeds) { f.setLayer("crap", report.Layer{ID: "crap", Outcome: "not-run", Reason: "no command"}) },
		"crap not reached":         func(f *feeds) { f.setLayer("crap", report.Layer{ID: "crap", Outcome: "not-reached"}) },
		"crap not measured": func(f *feeds) {
			f.setLayer("crap", report.Layer{ID: "crap", Outcome: "fail", Details: map[string]any{"error": "no block of a production file"}})
		},
		"crap not in the report": func(f *feeds) { f.dropLayer("crap") },
		"crap passed with no cap": func(f *feeds) {
			f.setLayer("crap", report.Layer{ID: "crap", Outcome: "pass", Details: map[string]any{"violations": fns()}})
		},
		"crap tool missing": func(f *feeds) { f.setLayer("crap", report.Layer{ID: "crap", Outcome: "tool-missing"}) },
	}
	for name, plant := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFeeds(t)
			plant(f)
			_, err := baseline.Collect(f.in)
			var ref *baseline.RefusalError
			if !errors.As(err, &ref) {
				t.Fatalf("err %v, want a refusal", err)
			}
		})
	}
}

func TestCollectAcceptsACrapLayerSwitchedOff(t *testing.T) {
	// enabled: false needs a reason and an expiry, so a switched-off layer is a
	// stated choice, not a missing output. No function is measured.
	f := newFeeds(t)
	f.setLayer("crap", report.Layer{ID: "crap", Outcome: "skip", Reason: "calibrating", Expires: "2027-01-01"})
	got, err := baseline.Collect(f.in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Functions) != 0 {
		t.Errorf("functions %v", got.Functions)
	}
}

func TestCollectNamesTheLayerItRefuses(t *testing.T) {
	f := newFeeds(t)
	f.setLayer("tests", report.Layer{ID: "tests", Outcome: "fail", Reason: "TestX failed"})
	_, err := baseline.Collect(f.in)
	if err == nil || !strings.Contains(err.Error(), "tests") || !strings.Contains(err.Error(), "TestX failed") {
		t.Errorf("err %v should name the layer and its reason", err)
	}
}

func TestPackagesUnderTheFloor(t *testing.T) {
	f := newFeeds(t)
	f.in.Config.Coverage.StatementsFloor = 20
	got, err := baseline.Collect(f.in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Packages) != 0 {
		t.Errorf("b is at 25%% and the floor is 20%%: %v", got.Packages)
	}
	f.in.Config.Coverage.StatementsFloor = 25
	if got = collect(t, f); len(got.Packages) != 0 {
		t.Errorf("a package at the floor meets it: %v", got.Packages)
	}
	f.in.Config.Coverage.StatementsFloor = 25.1
	if got = collect(t, f); len(got.Packages) != 1 {
		t.Errorf("b is under 25.1%%: %v", got.Packages)
	}
}

func TestFindingsAreCountedPerLinterAndFileAndPathsAreModuleRelative(t *testing.T) {
	f := newFeeds(t)
	abs := filepath.Join(f.dir, "b", "b.go")
	f.write(".go-tops/lint-style.json", `{"Issues":[
 {"FromLinter":"revive","Text":"x","Pos":{"Filename":"./a/a.go","Line":3}},
 {"FromLinter":"revive","Text":"y","Pos":{"Filename":"a/a.go","Line":9}},
 {"FromLinter":"revive","Text":"w","Pos":{"Filename":"`+filepath.ToSlash(abs)+`","Line":9}}
]}`)
	got, err := baseline.Collect(f.in)
	if err != nil {
		t.Fatal(err)
	}
	want := baseline.Counts{"revive": {"a/a.go": 2, "b/b.go": 1}}
	if !reflect.DeepEqual(got.Findings, want) {
		t.Errorf("findings %v, want %v", got.Findings, want)
	}
}

func TestSuppressionsCountNolintPerLinterAndFile(t *testing.T) {
	f := newFeeds(t)
	f.write("a/a.go", `package a

import "os"

var _ = os.Args //nolint:gosec // one
var _ = os.Args //nolint:gosec,revive // two linters: one each
var _ = os.Args // nolint:errcheck // a spaced one still suppresses
var _ = os.Args //nolint // bare: every linter
var s = "//nolint:gosec in a string is no comment"

// A doc comment.
//
//nolint:revive // on its own line
func F() {}
`)
	f.write("b/b_test.go", "package b\n\nvar _ = 1 //nolint:unused // tests count too\n")
	f.write("b/testdata/x.go", "package x\n\nvar _ = 1 //nolint:unused // a fixture is not the module's code\n")
	f.write("vendor/v/v.go", "package v\n\nvar _ = 1 //nolint:unused // vendored\n")
	f.write(".hidden/h.go", "package h\n\nvar _ = 1 //nolint:unused // hidden\n")
	got, err := baseline.Collect(f.in)
	if err != nil {
		t.Fatal(err)
	}
	want := baseline.Counts{
		"all":      {"a/a.go": 1},
		"errcheck": {"a/a.go": 1},
		"gosec":    {"a/a.go": 2},
		"revive":   {"a/a.go": 2},
		"unused":   {"b/b_test.go": 1},
	}
	if !reflect.DeepEqual(got.Nolint, want) {
		t.Errorf("nolint %v, want %v", got.Nolint, want)
	}
}

func TestSuppressionsCountAcksFromCommentsAndTheAcksFile(t *testing.T) {
	f := newFeeds(t)
	f.in.Acks = ".go-tops/acks.yaml"
	f.write("a/a.go", `package a

//tops:ack G-12 reason="the table is generated by hand" owner="ali"
func F() {}

//tops:ack G-12 reason="a second one in the same file" owner="ali"
func G() {}

//tops:ack G-30 reason="a different law" owner="ali"
func H() {}
`)
	f.write(".go-tops/acks.yaml", `acks:
  - law: G-12
    file: a/a.go
    reason: the third in that file
    owner: ali
  - law: G-05
    file: b/b.go
    reason: a law in another file
    owner: ali
`)
	got, err := baseline.Collect(f.in)
	if err != nil {
		t.Fatal(err)
	}
	want := baseline.Counts{"G-05": {"b/b.go": 1}, "G-12": {"a/a.go": 3}, "G-30": {"a/a.go": 1}}
	if !reflect.DeepEqual(got.Ack, want) {
		t.Errorf("ack %v, want %v", got.Ack, want)
	}
}

func TestSuppressionsWithNoAcksFileCountOnlyComments(t *testing.T) {
	f := newFeeds(t)
	f.in.Acks = ".go-tops/acks.yaml"
	got, err := baseline.Collect(f.in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Ack) != 0 {
		t.Errorf("ack %v", got.Ack)
	}
}

func TestSuppressionsRefuseAnUnreadableSource(t *testing.T) {
	// A file that does not parse cannot be shown to hold no suppression, so the
	// ledger cannot claim the count: it is an error, not a zero.
	f := newFeeds(t)
	f.write("a/a.go", "package a\nfunc {{{\n")
	if _, err := baseline.Collect(f.in); err == nil {
		t.Error("a source file that does not parse was counted as clean")
	}
}
