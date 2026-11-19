package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gotops "github.com/Hedronite/go-tops"
	"github.com/Hedronite/go-tops/internal/baseline"
	"github.com/Hedronite/go-tops/internal/cli"
	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/report"
)

// GO_TOPS.md §6.7 and §12.1 `baseline`: the only command that writes
// .go-tops/baseline.json. --init measures and writes when the tests are green and
// every feeding layer produced non-empty output; a plain write only lowers;
// --accept-regression records a raise; --rename moves one key. Every write is
// refused under advisory. Custody errors exit 2.

const ledgerRel = ".go-tops/baseline.json"

const profileFix = `mode: atomic
example.com/app/a/a.go:3.10,5.2 4 1
example.com/app/b/b.go:3.10,5.2 1 1
example.com/app/b/b.go:6.10,8.2 3 0
`

const lintFix = `{"Issues":[{"FromLinter":"revive","Text":"x","Pos":{"Filename":"a/a.go","Line":3}}]}`

// cmdBuild is the build the stubbed run reports: the ledger leg unless a test
// says otherwise.
var cmdBuild = baseline.Build{GOOS: "linux", GOARCH: "amd64", Tags: []string{"race"}, CGOEnabled: true}

// schemaHasBuild says whether the embedded baseline schema already carries the
// build object of ruling C3. Until the 0.4.3 schema (PR 26) is merged in, `check`
// rejects the key, so the tests that hand a written ledger to `check` wait for it.
func schemaHasBuild(t *testing.T) bool {
	t.Helper()
	raw, err := fs.ReadFile(gotops.Files(), "schema/core.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Contains(raw, []byte(`"build"`))
}

// fixture is what the stubbed feeding layers leave behind.
type fixture struct {
	build   baseline.Build
	rep     report.Report
	profile string
	lint    string
	ran     int
}

func cfn(fn string, crap float64) map[string]any {
	return map[string]any{"func": fn, "file": "a/a.go", "cc": 12, "cov_pct": 50.0, "crap": crap, "baseline": nil}
}

func newFixture() *fixture {
	return &fixture{
		build:   cmdBuild,
		profile: profileFix,
		lint:    lintFix,
		rep: report.Report{
			Layers: []report.Layer{
				{ID: "hygiene", Outcome: "fail"},
				{ID: "tests", Outcome: "pass"},
				{ID: "coverage", Outcome: "fail", Details: map[string]any{"statements_pct": 62.5, "floor": 90.0, "profile_bytes": 1}},
				{ID: "crap", Outcome: "fail", Details: map[string]any{"cap": 10.0, "violations": []map[string]any{
					cfn("example.com/app/a.Alpha", 21), cfn("example.com/app/b.Beta", 14.5),
				}}},
			},
			Laws: report.Laws{ApplicableJudged: 2},
		},
	}
}

func (fx *fixture) setCrap(fns ...map[string]any) {
	for i, l := range fx.rep.Layers {
		if l.ID == "crap" {
			fx.rep.Layers[i].Details = map[string]any{"cap": 10.0, "violations": append([]map[string]any{}, fns...)}
		}
	}
}

func (fx *fixture) setTests(outcome string) {
	for i, l := range fx.rep.Layers {
		if l.ID == "tests" {
			fx.rep.Layers[i].Outcome = outcome
		}
	}
}

// install replaces the feeding run with the fixture: it writes the profile and
// the lint output where the layers do, and returns the report.
func (fx *fixture) install(t *testing.T) {
	t.Helper()
	old, oldBuild := runFeeding, runBuild
	t.Cleanup(func() { runFeeding, runBuild = old, oldBuild })
	runBuild = func(context.Context, string) (baseline.Build, error) { return fx.build, nil }
	runFeeding = func(_ context.Context, dir string, _ gate.Config) (report.Report, error) {
		fx.ran++
		for rel, data := range map[string]string{".go-tops/cover.out": fx.profile, ".go-tops/lint-style.json": fx.lint} {
			p := filepath.Join(dir, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return report.Report{}, err
			}
			if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
				return report.Report{}, err
			}
		}
		return fx.rep, nil
	}
}

func ratchetConfig(t *testing.T) string {
	t.Helper()
	return strings.Replace(goodConfig(t), "enforce: hard", "enforce: ratchet", 1)
}

func advisoryConfig(t *testing.T) string {
	t.Helper()
	return strings.Replace(goodConfig(t), "enforce: hard", "enforce: advisory\nadvisory:\n  reason: the laws layer lands at M5\n  owner: maintainer\n  expires: \"2099-01-01\"", 1)
}

// repoWith is a git module with go-tops.yaml and, when ledger is not empty, a
// committed ledger. The tests mutate its working tree.
func repoWith(t *testing.T, cfg, ledger string) string {
	t.Helper()
	dir := module(t, cfg)
	if ledger != "" {
		writeFile(t, dir, ledgerRel, ledger)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A"},
		{"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "init"},
	} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func baselineIn(t *testing.T, dir string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	t.Chdir(dir)
	var o, e bytes.Buffer
	code = runBaseline(cli.NewEnv(&o, &e), args)
	return code, o.String(), e.String()
}

func readLedgerFile(t *testing.T, dir string) baseline.File {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, ledgerRel))
	if err != nil {
		t.Fatal(err)
	}
	f, err := baseline.Parse(data)
	if err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	return f
}

func commitAll(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{{"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "ledger"}} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func fileBytes(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		return "<absent>"
	}
	return string(b)
}

// initialised is a ratchet repo that has had `baseline --init` and committed.
func initialised(t *testing.T) (string, *fixture) {
	t.Helper()
	fx := newFixture()
	fx.install(t)
	dir := repoWith(t, ratchetConfig(t), "")
	if code, _, stderr := baselineIn(t, dir, "--init"); code != 0 {
		t.Fatalf("init exit %d: %s", code, stderr)
	}
	commitAll(t, dir)
	return dir, fx
}

func TestInitWritesTheMeasuredLedgerAndCheckAcceptsIt(t *testing.T) {
	fx := newFixture()
	fx.install(t)
	dir := repoWith(t, ratchetConfig(t), "")
	code, stdout, stderr := baselineIn(t, dir, "--init")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, ledgerRel) {
		t.Errorf("stdout %q does not say what was written", stdout)
	}
	got := readLedgerFile(t, dir)
	if got.Ledger.Functions["example.com/app/a.Alpha"] != 21 || got.Ledger.Functions["example.com/app/b.Beta"] != 14.5 {
		t.Errorf("functions %v", got.Ledger.Functions)
	}
	if got.Ledger.Packages["example.com/app/b"] != (baseline.Package{StatementsPct: 25, Uncovered: 3}) {
		t.Errorf("packages %v", got.Ledger.Packages)
	}
	if got.Ledger.Findings["revive"]["a/a.go"] != 1 || got.JudgedLaws != 2 || len(got.Regressions) != 0 {
		t.Errorf("ledger %+v", got)
	}
	if !got.Build.Equal(cmdBuild) {
		t.Errorf("the ledger records build %v, want %v", got.Build, cmdBuild)
	}
	// check reads the committed ledger and finds it valid against the schema.
	if schemaHasBuild(t) {
		commitAll(t, dir)
		if code, _, stderr := gotopsIn(t, dir, "check"); code != 0 {
			t.Errorf("check exit %d: %s", code, stderr)
		}
	}
}

func TestInitWritesAnEmptyLedgerWhenNothingIsOverTheLine(t *testing.T) {
	fx := newFixture()
	fx.profile = "mode: atomic\nexample.com/app/a/a.go:3.10,5.2 4 1\n"
	fx.lint = `{"Issues":[]}`
	fx.setCrap()
	fx.install(t)
	dir := repoWith(t, ratchetConfig(t), "")
	if code, _, stderr := baselineIn(t, dir, "--init"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	want := "{\n  \"build\": {\n    \"cgo_enabled\": true,\n    \"goarch\": \"amd64\",\n    \"goos\": \"linux\",\n    \"tags\": [\"race\"]\n  },\n  \"judged_laws\": 2,\n  \"ledger\": {}\n}\n"
	if got := fileBytes(t, dir, ledgerRel); got != want {
		t.Errorf("ledger %q, want %q", got, want)
	}
}

func TestInitIsRefusedWhenALedgerExists(t *testing.T) {
	dir, _ := initialised(t)
	before := fileBytes(t, dir, ledgerRel)
	code, _, stderr := baselineIn(t, dir, "--init")
	if code != 1 || !strings.Contains(stderr, "exists") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	if fileBytes(t, dir, ledgerRel) != before {
		t.Error("a refused --init changed the ledger")
	}
}

func TestDeletingTheLedgerIsNotAReset(t *testing.T) {
	// §6.7: under ratchet a missing baseline fails; deleting the file is not a
	// reset. The tracked copy is still the repo's ledger, so --init does not
	// write a fresh one over the debt, and a plain write has nothing to lower.
	dir, _ := initialised(t)
	if err := os.Remove(filepath.Join(dir, ledgerRel)); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := baselineIn(t, dir, "--init"); code != 1 || !strings.Contains(stderr, "HEAD") {
		t.Errorf("--init: exit %d, stderr %q", code, stderr)
	}
	if code, _, _ := baselineIn(t, dir); code != 2 {
		t.Errorf("plain: exit %d, want 2", code)
	}
	if _, err := os.Stat(filepath.Join(dir, ledgerRel)); err == nil {
		t.Error("a ledger was written over a deleted one")
	}
	if code, _, _ := gotopsIn(t, dir, "check"); code != 2 {
		t.Errorf("check exit %d, want 2 for a deleted baseline", code)
	}
}

func TestEveryWriteIsRefusedUnderAdvisory(t *testing.T) {
	fx := newFixture()
	fx.install(t)
	dir := repoWith(t, advisoryConfig(t), "")
	for _, args := range [][]string{
		{"--init"},
		{},
		{"--accept-regression", "--reason", "the parser grows one arm", "--owner", "maintainer"},
		{"--rename", "a", "b"},
	} {
		code, _, stderr := baselineIn(t, dir, args...)
		if code != 1 || !strings.Contains(stderr, "advisory") {
			t.Errorf("%v: exit %d, stderr %q", args, code, stderr)
		}
		if _, err := os.Stat(filepath.Join(dir, ledgerRel)); err == nil {
			t.Errorf("%v: a ledger was written under advisory", args)
		}
	}
	if fx.ran != 0 {
		t.Error("the feeding layers ran before the refusal")
	}
}

func TestAdvisoryRefusesEvenWithALedgerPresent(t *testing.T) {
	fx := newFixture()
	fx.install(t)
	dir := repoWith(t, advisoryConfig(t), `{"judged_laws":0,"ledger":{}}`+"\n")
	before := fileBytes(t, dir, ledgerRel)
	if code, _, _ := baselineIn(t, dir); code != 1 {
		t.Errorf("exit %d", code)
	}
	if fileBytes(t, dir, ledgerRel) != before {
		t.Error("changed under advisory")
	}
}

func TestInitRefusesWhatWasNotMeasured(t *testing.T) {
	tests := map[string]func(fx *fixture){
		"red tests":     func(fx *fixture) { fx.setTests("fail") },
		"empty profile": func(fx *fixture) { fx.profile = "" },
		"no lint file":  func(fx *fixture) { fx.lint = "" },
	}
	for name, plant := range tests {
		t.Run(name, func(t *testing.T) {
			fx := newFixture()
			plant(fx)
			fx.install(t)
			dir := repoWith(t, ratchetConfig(t), "")
			code, _, stderr := baselineIn(t, dir, "--init")
			if code != 1 || stderr == "" {
				t.Errorf("exit %d, stderr %q", code, stderr)
			}
			if _, err := os.Stat(filepath.Join(dir, ledgerRel)); err == nil {
				t.Error("a ledger was written from a run that measured nothing")
			}
		})
	}
}

func TestInitWritesWhenOnlyTheThresholdsAreRed(t *testing.T) {
	// The default fixture has a package under the floor, functions over the cap,
	// a lint finding and red coverage and CRAP outcomes: all measured.
	fx := newFixture()
	fx.install(t)
	dir := repoWith(t, ratchetConfig(t), "")
	if code, _, stderr := baselineIn(t, dir, "--init"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
}

func TestACustodyErrorExitsTwo(t *testing.T) {
	fx := newFixture()
	fx.install(t)
	t.Run("invalid go-tops.yaml", func(t *testing.T) {
		dir := repoWith(t, "protocol: nope\n", "")
		if code, _, stderr := baselineIn(t, dir, "--init"); code != 2 || stderr == "" {
			t.Errorf("exit %d, stderr %q", code, stderr)
		}
	})
	t.Run("no go-tops.yaml", func(t *testing.T) {
		if code, _, _ := baselineIn(t, repoWith(t, "", ""), "--init"); code != 2 {
			t.Errorf("exit %d", code)
		}
	})
	t.Run("plain write with no ledger", func(t *testing.T) {
		dir := repoWith(t, ratchetConfig(t), "")
		code, _, stderr := baselineIn(t, dir)
		if code != 2 || !strings.Contains(stderr, "--init") {
			t.Errorf("exit %d, stderr %q", code, stderr)
		}
	})
	for _, bad := range []string{"not json", `{"ledger":{}}`, `{"ledger":{},"judged_laws":0,"extra":1}`, ""} {
		for _, args := range [][]string{{}, {"--accept-regression", "--reason", "the parser grows one arm", "--owner", "maintainer"}, {"--rename", "a", "b"}} {
			t.Run("ledger "+bad+" "+strings.Join(args, " "), func(t *testing.T) {
				dir := repoWith(t, ratchetConfig(t), bad)
				if bad == "" {
					writeFile(t, dir, ledgerRel, "")
				}
				before := fileBytes(t, dir, ledgerRel)
				if code, _, stderr := baselineIn(t, dir, args...); code != 2 || stderr == "" {
					t.Errorf("exit %d, stderr %q", code, stderr)
				}
				if fileBytes(t, dir, ledgerRel) != before {
					t.Error("an invalid ledger was overwritten")
				}
			})
		}
	}
}

func TestPlainWriteLowersAndRemovesAndNeverAddsOrRaises(t *testing.T) {
	dir, fx := initialised(t)
	// Alpha improves to 15, Beta is paid (under the cap), Gamma is new debt, and
	// the one finding in a/a.go grows to two.
	fx.setCrap(cfn("example.com/app/a.Alpha", 15), cfn("example.com/app/a.Gamma", 30))
	fx.lint = `{"Issues":[{"FromLinter":"revive","Text":"x","Pos":{"Filename":"a/a.go","Line":3}},{"FromLinter":"revive","Text":"y","Pos":{"Filename":"a/a.go","Line":4}}]}`
	code, stdout, stderr := baselineIn(t, dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	got := readLedgerFile(t, dir)
	if got.Ledger.Functions["example.com/app/a.Alpha"] != 15 {
		t.Errorf("Alpha %v, want 15", got.Ledger.Functions["example.com/app/a.Alpha"])
	}
	if _, ok := got.Ledger.Functions["example.com/app/b.Beta"]; ok {
		t.Error("a paid entry stayed")
	}
	if _, ok := got.Ledger.Functions["example.com/app/a.Gamma"]; ok {
		t.Error("a plain write added an entry")
	}
	if got.Ledger.Findings["revive"]["a/a.go"] != 1 {
		t.Errorf("findings %v: a plain write raised a count", got.Ledger.Findings)
	}
	if !strings.Contains(stdout, "--accept-regression") {
		t.Errorf("stdout %q should point at --accept-regression for what was left", stdout)
	}
}

func TestPlainWriteWithNothingToLowerLeavesTheFileAlone(t *testing.T) {
	dir, _ := initialised(t)
	before := fileBytes(t, dir, ledgerRel)
	code, stdout, stderr := baselineIn(t, dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if fileBytes(t, dir, ledgerRel) != before || !strings.Contains(stdout, "unchanged") {
		t.Errorf("stdout %q", stdout)
	}
}

func TestPlainWriteNeedsGreenTests(t *testing.T) {
	dir, fx := initialised(t)
	before := fileBytes(t, dir, ledgerRel)
	fx.setTests("fail")
	fx.setCrap() // everything "paid", but the run measured nothing
	if code, _, _ := baselineIn(t, dir); code != 1 {
		t.Errorf("exit %d", code)
	}
	if fileBytes(t, dir, ledgerRel) != before {
		t.Error("a red test run emptied the ledger")
	}
}

func TestAcceptRegressionRaisesAndRecords(t *testing.T) {
	dir, fx := initialised(t)
	fx.setCrap(cfn("example.com/app/a.Alpha", 25), cfn("example.com/app/b.Beta", 14.5), cfn("example.com/app/a.Gamma", 30))
	code, _, stderr := baselineIn(t, dir, "--accept-regression", "--reason", "the parser grows one arm", "--owner", "maintainer")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	got := readLedgerFile(t, dir)
	if got.Ledger.Functions["example.com/app/a.Alpha"] != 25 || got.Ledger.Functions["example.com/app/a.Gamma"] != 30 {
		t.Errorf("functions %v", got.Ledger.Functions)
	}
	if len(got.Regressions) != 2 || got.Regressions[0].Key != "example.com/app/a.Alpha" || got.Regressions[0].Owner != "maintainer" || got.Regressions[1].From != nil {
		t.Errorf("regressions %+v", got.Regressions)
	}
	if schemaHasBuild(t) {
		commitAll(t, dir)
		if code, _, stderr := gotopsIn(t, dir, "check"); code != 0 {
			t.Errorf("check exit %d: %s", code, stderr)
		}
	}
}

func TestAcceptRegressionNeedsAReasonAndAnOwner(t *testing.T) {
	dir, fx := initialised(t)
	fx.setCrap(cfn("example.com/app/a.Alpha", 25))
	before := fileBytes(t, dir, ledgerRel)
	for _, args := range [][]string{
		{"--accept-regression"},
		{"--accept-regression", "--reason", "the parser grows one arm"},
		{"--accept-regression", "--owner", "maintainer"},
		{"--accept-regression", "--reason", "too short", "--owner", "maintainer"},
		{"--accept-regression", "--reason", "the parser grows one arm", "--owner", " "},
	} {
		fx.ran = 0
		if code, _, stderr := baselineIn(t, dir, args...); code != 2 || stderr == "" {
			t.Errorf("%v: exit %d, stderr %q", args, code, stderr)
		}
		if fx.ran != 0 {
			t.Errorf("%v: the layers ran for a request that is refused on its flags", args)
		}
	}
	if fileBytes(t, dir, ledgerRel) != before {
		t.Error("the ledger changed")
	}
}

func TestAcceptRegressionWithNothingToAcceptIsRefused(t *testing.T) {
	dir, _ := initialised(t)
	before := fileBytes(t, dir, ledgerRel)
	code, _, stderr := baselineIn(t, dir, "--accept-regression", "--reason", "the parser grows one arm", "--owner", "maintainer")
	if code != 1 || !strings.Contains(stderr, "nothing to accept") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	if fileBytes(t, dir, ledgerRel) != before {
		t.Error("the ledger changed")
	}
}

// stubInventory replaces the source scan with fixed key sets.
type stubInventory struct{ fns, pkgs map[string]bool }

func (s stubInventory) Has(k baseline.Kind, key string) bool {
	if k == baseline.KindPackage {
		return s.pkgs[key]
	}
	return s.fns[key]
}

func withInventory(t *testing.T, fns ...string) {
	t.Helper()
	old := scanInventory
	t.Cleanup(func() { scanInventory = old })
	set := map[string]bool{}
	for _, f := range fns {
		set[f] = true
	}
	scanInventory = func(string) (baseline.Inventory, error) { return stubInventory{fns: set}, nil }
}

func TestRenameMovesTheKeyAndNeverTheValue(t *testing.T) {
	dir, fx := initialised(t)
	withInventory(t, "example.com/app/a.Renamed", "example.com/app/b.Beta")
	code, _, stderr := baselineIn(t, dir, "--rename", "example.com/app/a.Alpha", "example.com/app/a.Renamed")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	got := readLedgerFile(t, dir)
	if _, ok := got.Ledger.Functions["example.com/app/a.Alpha"]; ok || got.Ledger.Functions["example.com/app/a.Renamed"] != 21 {
		t.Errorf("functions %v", got.Ledger.Functions)
	}
	if fx.ran != 1 {
		t.Errorf("the feeding layers ran %d times; a rename measures nothing (the one run is the init)", fx.ran)
	}
}

func TestRenameRefusalsExitOneAndChangeNothing(t *testing.T) {
	dir, _ := initialised(t)
	before := fileBytes(t, dir, ledgerRel)
	withInventory(t, "example.com/app/a.Alpha", "example.com/app/a.Renamed") // the old key is still there
	code, _, stderr := baselineIn(t, dir, "--rename", "example.com/app/a.Alpha", "example.com/app/a.Renamed")
	if code != 1 || !strings.Contains(stderr, "still exists") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	withInventory(t) // the new key does not exist
	if code, _, _ := baselineIn(t, dir, "--rename", "example.com/app/a.Alpha", "example.com/app/a.Nowhere"); code != 1 {
		t.Errorf("exit %d", code)
	}
	if fileBytes(t, dir, ledgerRel) != before {
		t.Error("a refused rename changed the ledger")
	}
}

func TestUsageErrorsExitTwo(t *testing.T) {
	dir, fx := initialised(t)
	fx.ran = 0
	for name, args := range map[string][]string{
		"unknown flag":        {"--bogus"},
		"two modes":           {"--init", "--accept-regression"},
		"init and rename":     {"--init", "--rename", "a", "b"},
		"rename one key":      {"--rename", "a"},
		"rename three keys":   {"--rename", "a", "b", "c"},
		"stray argument":      {"extra"},
		"reason without mode": {"--reason", "three words here"},
		"owner without mode":  {"--owner", "maintainer"},
		"survivors kind":      {"--init", "--kind", "survivors"},
	} {
		if code, _, stderr := baselineIn(t, dir, args...); code != 2 || stderr == "" {
			t.Errorf("%s: exit %d, stderr %q", name, code, stderr)
		}
	}
	if fx.ran != 0 {
		t.Error("the layers ran for a usage error")
	}
}

func TestHelpAndTheRegistry(t *testing.T) {
	code, stdout, _ := gotopsCmd(t, "baseline", "--help")
	if code != 0 || !strings.Contains(stdout, "--accept-regression") || !strings.Contains(stdout, "--rename") {
		t.Errorf("exit %d, stdout %q", code, stdout)
	}
	if _, stdout, _ := gotopsCmd(t, "--help"); !strings.Contains(stdout, "baseline") {
		t.Errorf("baseline is not listed in help: %q", stdout)
	}
}

func TestTheLedgerIsWrittenInOneStep(t *testing.T) {
	// No temporary file is left beside the ledger.
	dir, _ := initialised(t)
	entries, err := os.ReadDir(filepath.Join(dir, ".go-tops"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("left behind: %s", e.Name())
		}
	}
}

// Ruling C3: the baseline records its build, and only a run whose build matches
// it writes the file. Any other leg refuses, naming the ledger build.

func TestAnotherLegRefusesEveryWriteAndNamesTheLedgerBuild(t *testing.T) {
	dir, fx := initialised(t)
	before := fileBytes(t, dir, ledgerRel)
	fx.build = baseline.Build{GOOS: "darwin", GOARCH: "arm64", Tags: []string{"race"}, CGOEnabled: true}
	fx.setCrap(cfn("example.com/app/a.Alpha", 25)) // a raise a matching leg could accept
	withInventory(t, "example.com/app/a.Renamed")
	fx.ran = 0
	for _, args := range [][]string{
		{},
		{"--accept-regression", "--reason", "the parser grows one arm", "--owner", "maintainer"},
		{"--rename", "example.com/app/a.Alpha", "example.com/app/a.Renamed"},
		{"--init"},
	} {
		code, _, stderr := baselineIn(t, dir, args...)
		if code != 1 {
			t.Errorf("%v: exit %d, stderr %q", args, code, stderr)
		}
		if len(args) > 0 && args[0] != "--init" && (!strings.Contains(stderr, "linux/amd64") || !strings.Contains(stderr, "darwin/arm64")) {
			t.Errorf("%v: stderr %q should name the ledger build and this run's", args, stderr)
		}
	}
	if fileBytes(t, dir, ledgerRel) != before {
		t.Error("another leg changed the ledger")
	}
	if fx.ran != 0 {
		t.Error("the feeding layers ran on a leg that may not write")
	}
}

func TestTheLedgerLegDiffersByEachPartOfTheBuild(t *testing.T) {
	dir, fx := initialised(t)
	for name, b := range map[string]baseline.Build{
		"os":   {GOOS: "darwin", GOARCH: "amd64", Tags: []string{"race"}, CGOEnabled: true},
		"arch": {GOOS: "linux", GOARCH: "arm64", Tags: []string{"race"}, CGOEnabled: true},
		"tags": {GOOS: "linux", GOARCH: "amd64", Tags: []string{"race", "integration"}, CGOEnabled: true},
		"cgo":  {GOOS: "linux", GOARCH: "amd64", Tags: []string{"race"}},
	} {
		fx.build = b
		if code, _, _ := baselineIn(t, dir); code != 1 {
			t.Errorf("%s: exit %d, want 1", name, code)
		}
	}
	fx.build = baseline.Build{GOOS: "linux", GOARCH: "amd64", Tags: []string{"race"}, CGOEnabled: true}
	if code, _, stderr := baselineIn(t, dir); code != 0 {
		t.Errorf("the matching build: exit %d: %s", code, stderr)
	}
}

func TestPlainWriteRemovesAnExemptFunctionsEntry(t *testing.T) {
	// An exempt function is scored but listed under exempt, not violations, so
	// the measure does not list it over the cap and the refresh removes its entry.
	dir, fx := initialised(t)
	for i, l := range fx.rep.Layers {
		if l.ID == "crap" {
			fx.rep.Layers[i].Details = map[string]any{
				"cap":        10.0,
				"violations": append([]map[string]any{}, cfn("example.com/app/b.Beta", 14.5)),
				"exempt":     append([]map[string]any{}, cfn("example.com/app/a.Alpha", 21)),
			}
		}
	}
	if code, _, stderr := baselineIn(t, dir); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	got := readLedgerFile(t, dir)
	if _, ok := got.Ledger.Functions["example.com/app/a.Alpha"]; ok {
		t.Error("an exempt function kept its entry")
	}
	if got.Ledger.Functions["example.com/app/b.Beta"] != 14.5 {
		t.Errorf("functions %v", got.Ledger.Functions)
	}
}

// The existence set comes from internal/crap, which reads every production file
// whatever its build tags, so a rename is checked against the real source tree.
func TestRenameIsCheckedAgainstTheRealSourceTree(t *testing.T) {
	fx := newFixture()
	fx.install(t)
	dir := repoWith(t, ratchetConfig(t), "")
	writeFile(t, dir, "a/a.go", "package a\n\nfunc New() {}\n\nfunc Same() {}\n")
	writeFile(t, dir, "a/a_windows.go", "package a\n\nfunc OnlyWindows() {}\n")
	ledger := baseline.Init(baseline.Measure{
		Build:     cmdBuild,
		Functions: map[string]float64{"example.com/app/a.Old": 21, "example.com/app/a.Same": 14, "example.com/app/a.WasWindows": 12},
	})
	writeFile(t, dir, ledgerRel, string(baseline.Marshal(ledger)))
	commitAll(t, dir)
	// Old is gone, New exists: allowed.
	if code, _, stderr := baselineIn(t, dir, "--rename", "example.com/app/a.Old", "example.com/app/a.New"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if got := readLedgerFile(t, dir).Ledger.Functions; got["example.com/app/a.New"] != 21 {
		t.Errorf("functions %v", got)
	}
	commitAll(t, dir)
	// Same still exists: refused.
	if code, _, stderr := baselineIn(t, dir, "--rename", "example.com/app/a.Same", "example.com/app/a.New2"); code != 1 || !strings.Contains(stderr, "still exists") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	// A function in a file this build does not compile still exists, so its key is
	// a valid target.
	if code, _, stderr := baselineIn(t, dir, "--rename", "example.com/app/a.WasWindows", "example.com/app/a.OnlyWindows"); code != 0 {
		t.Errorf("exit %d: %s", code, stderr)
	}
}

func TestRenameMovesAPackageKeyAgainstTheRealPackageSet(t *testing.T) {
	fx := newFixture()
	fx.install(t)
	dir := repoWith(t, ratchetConfig(t), "")
	writeFile(t, dir, "b/b.go", "package b\n\nfunc F() {}\n")
	ledger := baseline.Init(baseline.Measure{Build: cmdBuild, Packages: map[string]baseline.Package{"example.com/app/old": {StatementsPct: 50, Uncovered: 9}}})
	writeFile(t, dir, ledgerRel, string(baseline.Marshal(ledger)))
	commitAll(t, dir)
	if code, _, stderr := baselineIn(t, dir, "--rename", "example.com/app/old", "example.com/app/b"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if got := readLedgerFile(t, dir).Ledger.Packages; got["example.com/app/b"] != (baseline.Package{StatementsPct: 50, Uncovered: 9}) {
		t.Errorf("packages %v", got)
	}
}

// fakeLayer is a gate layer with a fixed outcome.
type fakeLayer struct {
	id      string
	outcome gate.Outcome
	pre     error
	needs   []string
	ran     *bool
	enforce *string
}

func (l fakeLayer) ID() string      { return l.id }
func (fakeLayer) Tier() int         { return 1 }
func (l fakeLayer) Needs() []string { return l.needs }
func (l fakeLayer) Run(_ context.Context, env *gate.Env) gate.Result {
	if l.ran != nil {
		*l.ran = true
	}
	if l.enforce != nil {
		*l.enforce = env.Config.Enforce
	}
	return gate.Result{Outcome: l.outcome}
}
func (l fakeLayer) Preflight(context.Context, *gate.Env) error { return l.pre }

func TestFeedingOnlyKeepsTheFourFeedingLayers(t *testing.T) {
	var all []gate.Layer
	for _, id := range []string{"hygiene", "tests", "coverage", "crap", "mutation", "allocs", "fuzz"} {
		all = append(all, fakeLayer{id: id, outcome: gate.Pass})
	}
	var got []string
	for _, l := range feedingOnly(all) {
		got = append(got, l.ID())
	}
	if want := "hygiene tests coverage crap"; strings.Join(got, " ") != want {
		t.Errorf("layers %v, want %s", got, want)
	}
}

func TestRunLayersReturnsTheReportAndJudgesNothing(t *testing.T) {
	dir := repoWith(t, ratchetConfig(t), "")
	layers := []gate.Layer{fakeLayer{id: "tests", outcome: gate.Pass}, fakeLayer{id: "crap", outcome: gate.Fail}}
	rep, err := runLayers(t.Context(), dir, gate.Config{Module: "example.com/app", Class: "binary-cli", Enforce: "ratchet"}, layers)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Layers) != 2 || rep.Layers[0].Outcome != "pass" || rep.Layers[1].Outcome != "fail" {
		t.Errorf("layers %+v: a failing layer must still come back as a figure, not an error", rep.Layers)
	}
}

func TestRunLayersMapsAStartFailureToItsExitCode(t *testing.T) {
	dir := repoWith(t, ratchetConfig(t), "")
	cfg := gate.Config{Module: "example.com/app", Class: "binary-cli", Enforce: "ratchet"}
	for name, c := range map[string]struct {
		pre  error
		want int
	}{
		"invalid configuration": {&gate.ConfigError{Err: errors.New("bad")}, 2},
		"lock mismatch":         {&gate.LockError{Err: errors.New("bad hash")}, 2},
		"environment":           {&gate.EnvError{Err: errors.New("no compiler")}, 3},
	} {
		_, err := runLayers(t.Context(), dir, cfg, []gate.Layer{fakeLayer{id: "tests", outcome: gate.Pass, pre: c.pre}})
		var be *baselineError
		if !errors.As(err, &be) || be.code != c.want {
			t.Errorf("%s: err %v, want exit %d", name, err, c.want)
		}
	}
}

// GO_TOPS.md §6.7: red thresholds do not stop --init. A package under the
// coverage floor makes the coverage layer fail; the CRAP layer needs coverage,
// so the gate would not reach it. The feeding run reads the figures, so it runs
// CRAP on the profile whatever the floor said.
func TestFeedingRunsCrapWhenCoverageFailsItsFloor(t *testing.T) {
	dir := repoWith(t, ratchetConfig(t), "")
	crapRan := false
	layers := feedingOnly([]gate.Layer{
		fakeLayer{id: "tests", outcome: gate.Pass},
		fakeLayer{id: "coverage", outcome: gate.Fail, needs: []string{"tests"}},
		fakeLayer{id: "crap", outcome: gate.Pass, needs: []string{"coverage"}, ran: &crapRan},
	})
	rep, err := runLayers(t.Context(), dir, gate.Config{Module: "example.com/app", Class: "binary-cli", Enforce: "ratchet"}, layers)
	if err != nil {
		t.Fatal(err)
	}
	if !crapRan {
		t.Fatalf("the CRAP layer was not reached: %+v", rep.Layers)
	}
	for _, l := range rep.Layers {
		if l.ID == "crap" && l.Outcome != "pass" {
			t.Errorf("crap outcome %s, want the layer's own", l.Outcome)
		}
	}
}

// A write changes a ledger only from a copy that matches HEAD (§12.1 check: the
// baseline is clean against HEAD). Otherwise a hand edit that raised an entry is
// lowered back to a value above the committed one, with no record.

// handEdited is a ratchet repo whose committed ledger holds BigUntested at 12,
// edited in the working tree to 20, while the code now measures 14.07: below the
// edit, above what was committed.
func handEdited(t *testing.T, cfg string) (string, *fixture) {
	t.Helper()
	fx := newFixture()
	fx.setCrap(cfn("example.com/app/a.BigUntested", 14.07))
	fx.install(t)
	dir := repoWith(t, cfg, "")
	committed := baseline.Init(baseline.Measure{Build: cmdBuild, Functions: map[string]float64{"example.com/app/a.BigUntested": 12}})
	writeFile(t, dir, ledgerRel, string(baseline.Marshal(committed)))
	commitAll(t, dir)
	edited := baseline.Init(baseline.Measure{Build: cmdBuild, Functions: map[string]float64{"example.com/app/a.BigUntested": 20}})
	writeFile(t, dir, ledgerRel, string(baseline.Marshal(edited)))
	return dir, fx
}

func TestEveryWriteRefusesALedgerThatDiffersFromHead(t *testing.T) {
	for name, cfg := range map[string]func(*testing.T) string{
		"ratchet": ratchetConfig,
		"hard":    goodConfig,
	} {
		dir, fx := handEdited(t, cfg(t))
		withInventory(t, "example.com/app/a.Renamed")
		before := fileBytes(t, dir, ledgerRel)
		for _, args := range [][]string{
			{},
			{"--accept-regression", "--reason", "the parser grows one arm", "--owner", "maintainer"},
			{"--rename", "example.com/app/a.BigUntested", "example.com/app/a.Renamed"},
		} {
			fx.ran = 0
			code, _, stderr := baselineIn(t, dir, args...)
			if code != 2 || !strings.Contains(stderr, ledgerRel) || !strings.Contains(stderr, "HEAD") {
				t.Errorf("%s %v: exit %d, stderr %q; want 2 naming %s and HEAD", name, args, code, stderr, ledgerRel)
			}
			if fx.ran != 0 {
				t.Errorf("%s %v: the layers ran for a ledger that cannot be written", name, args)
			}
		}
		if fileBytes(t, dir, ledgerRel) != before {
			t.Errorf("%s: the hand-edited ledger was rewritten", name)
		}
	}
}

func TestEveryWriteRefusesALedgerThatIsNotTracked(t *testing.T) {
	fx := newFixture()
	fx.install(t)
	dir := repoWith(t, ratchetConfig(t), "")
	writeFile(t, dir, ledgerRel, string(baseline.Marshal(baseline.Init(baseline.Measure{Build: cmdBuild}))))
	code, _, stderr := baselineIn(t, dir)
	if code != 2 || !strings.Contains(stderr, "not tracked") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

func TestACommittedLedgerIsWrittenAgain(t *testing.T) {
	dir, fx := initialised(t)
	fx.setCrap(cfn("example.com/app/a.Alpha", 15), cfn("example.com/app/b.Beta", 14.5))
	if code, _, stderr := baselineIn(t, dir); code != 0 {
		t.Fatalf("first write: exit %d: %s", code, stderr)
	}
	// The write is now a change against HEAD: the next one waits for a commit.
	if code, _, _ := baselineIn(t, dir, "--accept-regression", "--reason", "the parser grows one arm", "--owner", "maintainer"); code != 2 {
		t.Errorf("second write before a commit: exit %d, want 2", code)
	}
	commitAll(t, dir)
	fx.setCrap(cfn("example.com/app/a.Alpha", 12), cfn("example.com/app/b.Beta", 14.5))
	if code, _, stderr := baselineIn(t, dir); code != 0 {
		t.Errorf("after a commit: exit %d: %s", code, stderr)
	}
}

// The feeding run reads figures and judges nothing: it runs the layers under
// hard, so a ratchet judge added to the gate never runs for a write.
func TestFeedingRunsTheLayersUnderHard(t *testing.T) {
	dir := repoWith(t, ratchetConfig(t), "")
	var seen string
	layers := []gate.Layer{fakeLayer{id: "tests", outcome: gate.Pass, enforce: &seen}}
	if _, err := runLayers(t.Context(), dir, gate.Config{Module: "example.com/app", Class: "binary-cli", Enforce: "ratchet"}, layers); err != nil {
		t.Fatal(err)
	}
	if seen != "hard" {
		t.Errorf("the layers saw enforce %q, want hard", seen)
	}
}

// A CRAP layer that is off measured no function. A plain write from it would
// remove every function entry, so every write refuses.
func TestEveryWriteRefusesWhenAFeedingLayerDidNotRun(t *testing.T) {
	for _, outcome := range []string{"skip", "not-reached", "not-run"} {
		dir, fx := initialised(t)
		before := fileBytes(t, dir, ledgerRel)
		for i, l := range fx.rep.Layers {
			if l.ID == "crap" {
				fx.rep.Layers[i] = report.Layer{ID: "crap", Outcome: outcome, Reason: "switched off"}
			}
		}
		for _, args := range [][]string{{}, {"--accept-regression", "--reason", "the parser grows one arm", "--owner", "maintainer"}} {
			code, _, stderr := baselineIn(t, dir, args...)
			if code != 1 || !strings.Contains(stderr, "crap") {
				t.Errorf("%s %v: exit %d, stderr %q", outcome, args, code, stderr)
			}
		}
		if fileBytes(t, dir, ledgerRel) != before {
			t.Errorf("%s: the ledger changed", outcome)
		}
	}
	fx := newFixture()
	for i, l := range fx.rep.Layers {
		if l.ID == "crap" {
			fx.rep.Layers[i] = report.Layer{ID: "crap", Outcome: "skip", Reason: "off"}
		}
	}
	fx.install(t)
	dir := repoWith(t, ratchetConfig(t), "")
	if code, _, _ := baselineIn(t, dir, "--init"); code != 1 {
		t.Errorf("--init with the crap layer off: exit %d, want 1", code)
	}
}

// After code deletion the percentage of a package falls and its count does not
// rise: the ratchet does not fail it, so the refresh must not say to accept it.
func TestRefreshDoesNotAskToAcceptAPackageTheRatchetWouldPass(t *testing.T) {
	profile := func(covered, uncovered int) string {
		return fmt.Sprintf("mode: atomic\nexample.com/app/a/a.go:3.10,5.2 4 1\nexample.com/app/b/b.go:3.10,5.2 %d 1\nexample.com/app/b/b.go:6.10,8.2 %d 0\n", covered, uncovered)
	}
	fx := newFixture()
	fx.profile = profile(3, 3) // 50% with 3 uncovered
	fx.install(t)
	dir := repoWith(t, ratchetConfig(t), "")
	if code, _, stderr := baselineIn(t, dir, "--init"); code != 0 {
		t.Fatalf("init: exit %d: %s", code, stderr)
	}
	commitAll(t, dir)
	fx.profile = profile(1, 3) // tested code deleted: 25%, still 3 uncovered
	_, stdout, _ := baselineIn(t, dir)
	if strings.Contains(stdout, "--accept-regression") {
		t.Errorf("stdout %q tells the user to accept a package the ratchet passes", stdout)
	}
	fx.profile = profile(1, 6) // and untested code added: 14% with 6 uncovered
	if _, stdout, _ := baselineIn(t, dir); !strings.Contains(stdout, "--accept-regression") {
		t.Errorf("stdout %q should offer --accept-regression when the package rule would fail", stdout)
	}
}

// The module need not sit at the root of the repository. git reads "rev:path" from
// the root, so a ledger deleted from the tree of a module in a subdirectory was
// not found at HEAD, and --init was allowed to write a fresh one over the debt.
func TestDeletingTheLedgerIsNotAResetInASubdirectoryModule(t *testing.T) {
	fx := newFixture()
	fx.install(t)
	root := t.TempDir()
	mod := filepath.Join(root, "services", "api")
	for rel, data := range map[string]string{"go.mod": "module example.com/app\n\ngo 1.26\n", "go-tops.lock": goodLock(t), "go-tops.yaml": ratchetConfig(t)} {
		writeFile(t, mod, rel, data)
	}
	writeFile(t, mod, ledgerRel, string(baseline.Marshal(baseline.Init(baseline.Measure{Build: cmdBuild}))))
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "x"}} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.Remove(filepath.Join(mod, ledgerRel)); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := baselineIn(t, mod, "--init"); code != 1 || !strings.Contains(stderr, "HEAD") {
		t.Errorf("--init: exit %d, stderr %q; want 1 naming HEAD", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(mod, ledgerRel)); err == nil {
		t.Error("a ledger was written over a deleted one")
	}
}
