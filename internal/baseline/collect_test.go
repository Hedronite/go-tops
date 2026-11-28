package baseline_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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
	f.in.Now = func() time.Time { return time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC) }
	f.in.Config.Exemptions = []report.Exemption{
		{Path: "a/**", Layer: "crap", Expires: "2027-03-01"},
		{Path: "b/**", Layer: "mutation", Expires: "2026-11-20"},
	}
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
		"crap switched off": func(f *feeds) {
			f.setLayer("crap", report.Layer{ID: "crap", Outcome: "skip", Reason: "calibrating", Expires: "2027-01-01"})
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

func TestCollectNamesTheLayerItRefuses(t *testing.T) {
	f := newFeeds(t)
	f.setLayer("tests", report.Layer{ID: "tests", Outcome: "fail", Reason: "TestX failed"})
	_, err := baseline.Collect(f.in)
	var ref *baseline.RefusalError
	if !errors.As(err, &ref) || !strings.Contains(ref.Msg, "tests") || !strings.Contains(ref.Msg, "TestX failed") {
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

// The kit's lint pass may read a config under .go-tops/, and the linter then names a
// file from that directory ("../a/a.go"). The ledger counts it under the module-relative
// name the rest of the gate uses, so the leg's file sets and the count agree.
func TestFindingsFromAConfigUnderGoTopsAreCountedUnderModuleRelativeNames(t *testing.T) {
	f := newFeeds(t)
	f.in.Config.Hygiene.LintConfig = ".go-tops/golangci.yml"
	f.write(".go-tops/lint-style.json", `{"Issues":[
 {"FromLinter":"errcheck","Text":"x","Pos":{"Filename":"../a/a.go","Line":3}},
 {"FromLinter":"errcheck","Text":"y","Pos":{"Filename":"../a/a.go","Line":9}},
 {"FromLinter":"errcheck","Text":"z","Pos":{"Filename":"../b/b.go","Line":4}}
]}`)
	got, err := baseline.Collect(f.in)
	if err != nil {
		t.Fatal(err)
	}
	want := baseline.Counts{"errcheck": {"a/a.go": 2, "b/b.go": 1}}
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
	f.write("_skip/s.go", "package s\n\nvar _ = 1 //nolint:unused // the go command ignores _ directories\n")
	f.write("nested/go.mod", "module example.com/nested\n")
	f.write("nested/n.go", "package n\n\nvar _ = 1 //nolint:unused // another module\n")
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

//tops:ack G-30 the table is generated by hand
func F() {}

//tops:ack G-30 a second one in the file
func G() {}

//tops:ack G-30 a different law entirely
func H() {}
`)
	f.write(".go-tops/acks.yaml", `acks:
  - law: G-30
    file: a/a.go
    reason: the third in that file
    owner: maintainer
  - law: G-30
    file: b/b.go
    reason: a law in another file
    owner: maintainer
`)
	got, err := baseline.Collect(f.in)
	if err != nil {
		t.Fatal(err)
	}
	want := baseline.Counts{"G-30": {"a/a.go": 4, "b/b.go": 1}}
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

func TestNolintDirectiveForms(t *testing.T) {
	f := newFeeds(t)
	f.write("a/a.go", `package a

var _ = 1 //nolint:a,,b // a blank name is skipped
var _ = 1 //nolint:  // a colon and no name silences every linter
var _ = 1 //nolint:, // a list of nothing does the same
var _ = 1 //nolintlint is another linter's word
var _ = 1 //nolint-ish is not a directive
var _ = 1 /* nolint:c */
`)
	got, err := baseline.Collect(f.in)
	if err != nil {
		t.Fatal(err)
	}
	want := baseline.Counts{"a": {"a/a.go": 1}, "b": {"a/a.go": 1}, "all": {"a/a.go": 2}}
	if !reflect.DeepEqual(got.Nolint, want) {
		t.Errorf("nolint %v, want %v", got.Nolint, want)
	}
}

func TestCollectRefusesACrapRowItCannotRecord(t *testing.T) {
	good := cf("example.com/app/a.Alpha", "a/a.go", 21.5)
	for name, rows := range map[string][]map[string]any{
		"no identity":            {cf("", "a/a.go", 21.5)},
		"negative score":         {cf("example.com/app/a.Neg", "a/a.go", -1)},
		"two rows, one identity": {good, good},
	} {
		f := newFeeds(t)
		f.setLayer("crap", report.Layer{ID: "crap", Outcome: "fail", Details: map[string]any{"cap": 10.0, "violations": rows}})
		_, err := baseline.Collect(f.in)
		var ref *baseline.RefusalError
		if !errors.As(err, &ref) {
			t.Errorf("%s: err %v, want a refusal", name, err)
		}
	}
}

// §6.8: the exemptions count of the ledger is the records in force on the day of
// the run. A record is in force while today is before its expiry.
func TestCollectCountsOnlyTheExemptionsInForce(t *testing.T) {
	f := newFeeds(t)
	f.in.Now = func() time.Time { return time.Date(2026, 11, 19, 23, 59, 0, 0, time.UTC) }
	f.in.Config.Exemptions = []report.Exemption{
		{Path: "a/**", Layer: "crap", Expires: "2026-11-20"},     // in force today
		{Path: "b/**", Layer: "crap", Expires: "2026-11-19"},     // expires today: out
		{Path: "c/**", Layer: "mutation", Expires: "2027-01-01"}, // another layer, still counted
		{Path: "d/**", Layer: "crap", Expires: "2026-01-01"},     // long expired
	}
	got, err := baseline.Collect(f.in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Exemptions != 2 {
		t.Errorf("exemptions %d, want 2", got.Exemptions)
	}
}

// A partial range (--only coverage, --only hygiene) holds fewer layers than the
// ledger is measured from, and Collect refuses it. The per-kind readers read what
// one layer measured, so the ratchet's partial path and the ledger read the same
// figures from the same files, with one set of rules for naming and flooring.
func TestPackagesReadsTheCoverageLayerAlone(t *testing.T) {
	f := newFeeds(t)
	whole, err := baseline.Collect(f.in)
	if err != nil {
		t.Fatal(err)
	}
	only := f.in
	only.Report = report.Report{Layers: []report.Layer{{ID: "coverage", Outcome: "fail", Details: map[string]any{"statements_pct": 62.5, "floor": 90.0, "profile_bytes": 1}}}}
	got, err := only.Packages()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, whole.Packages) || len(got) == 0 {
		t.Errorf("Packages %v, the whole read %v", got, whole.Packages)
	}
}

func TestFindingsReadsTheHygieneLayerAlone(t *testing.T) {
	f := newFeeds(t)
	abs := filepath.Join(f.dir, "b", "b.go")
	f.write(".go-tops/lint-style.json", `{"Issues":[
 {"FromLinter":"revive","Text":"x","Pos":{"Filename":"./a/a.go","Line":3}},
 {"FromLinter":"revive","Text":"y","Pos":{"Filename":"`+filepath.ToSlash(abs)+`","Line":9}},
 {"FromLinter":"errcheck","Text":"z","Pos":{"Filename":"","Line":1}}
]}`)
	whole, err := baseline.Collect(f.in)
	if err != nil {
		t.Fatal(err)
	}
	only := f.in
	only.Report = report.Report{Layers: []report.Layer{{ID: "hygiene", Outcome: "fail"}}}
	got, err := only.Findings()
	if err != nil {
		t.Fatal(err)
	}
	want := baseline.Counts{"revive": {"a/a.go": 1, "b/b.go": 1}, "errcheck": {"(no file)": 1}}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(got, whole.Findings) {
		t.Errorf("Findings %v, want %v, the whole read %v", got, want, whole.Findings)
	}
}

func TestTheReadersRefuseWhatTheirLayerDidNotMeasure(t *testing.T) {
	f := newFeeds(t)
	empty := f.in
	empty.Report = report.Report{}
	notRun := f.in
	notRun.Report = report.Report{Layers: []report.Layer{{ID: "coverage", Outcome: "not-reached"}, {ID: "hygiene", Outcome: "not-run"}}}
	for name, feeds := range map[string]baseline.Feeds{"no layers": empty, "layers that did not run": notRun} {
		for read, fn := range map[string]func() error{
			"Packages": func() error { _, err := feeds.Packages(); return err },
			"Findings": func() error { _, err := feeds.Findings(); return err },
		} {
			var ref *baseline.RefusalError
			if err := fn(); !errors.As(err, &ref) {
				t.Errorf("%s, %s: err %v, want a refusal", name, read, err)
			}
		}
	}
}
