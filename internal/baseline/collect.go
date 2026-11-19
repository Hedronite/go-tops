package baseline

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Hedronite/go-tops/internal/cover"
	"github.com/Hedronite/go-tops/internal/exempt"
	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/layers/hygiene"
	"github.com/Hedronite/go-tops/internal/layers/tests"
	"github.com/Hedronite/go-tops/internal/report"
)

// crapID is the CRAP layer's name in the report (§3.1).
const crapID = "crap"

// Feeds is what the feeding layers (tests, coverage, CRAP, hygiene) left
// behind: the report of a run that judged nothing, the profile and the lint
// output it wrote under Dir, and the configuration it ran on.
type Feeds struct {
	Report report.Report
	Config gate.Config
	Dir    string
	// Acks is the module-relative path of .go-tops/acks.yaml; "" for none.
	Acks string
	// Build is the build this run measured (ruling C3).
	Build Build
	// Now is the clock for the exemptions in force today (§6.8); nil is the real
	// one.
	Now func() time.Time
}

// Collect measures a run for the ledger (§6.7). It refuses, with a
// *RefusalError, when the run measured nothing it can record: the tests are not
// green, or a feeding layer's output is missing or empty. Red thresholds do
// not refuse. A ledger is what was measured, not what passed.
func Collect(f Feeds) (Measure, error) {
	var m Measure
	if err := f.requireTests(); err != nil {
		return m, err
	}
	pkgs, err := f.packages()
	if err != nil {
		return m, err
	}
	m.Packages = pkgs
	if m.Findings, err = f.findings(); err != nil {
		return m, err
	}
	if m.Functions, err = f.functions(); err != nil {
		return m, err
	}
	if m.Nolint, m.Ack, err = scanSuppressions(f.Dir, f.Acks); err != nil {
		return m, err
	}
	m.Exemptions = exempt.Count(f.Config.Exemptions, f.today())
	m.JudgedLaws = f.Report.Laws.ApplicableJudged
	m.Build = f.Build
	return m, nil
}

// today is the day the run measures: the clock, or the real one.
func (f Feeds) today() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

func (f Feeds) layer(id string) (report.Layer, bool) {
	for _, l := range f.Report.Layers {
		if l.ID == id {
			return l, true
		}
	}
	return report.Layer{}, false
}

// ran is a layer that produced a result of its own: pass or fail. Not-run,
// not-reached, tool-missing and skip measured nothing.
func ran(l report.Layer) bool {
	return l.Outcome == string(gate.Pass) || l.Outcome == string(gate.Fail)
}

func why(l report.Layer) string {
	if l.Reason == "" {
		return ""
	}
	return ": " + l.Reason
}

func (f Feeds) requireTests() error {
	l, ok := f.layer(tests.TestsID)
	switch {
	case !ok:
		return refuse("the tests layer is not in the run: a ledger is written from a green test run")
	case l.Outcome != string(gate.Pass):
		return refuse("the tests layer is %s%s: a ledger is written from a green test run", l.Outcome, why(l))
	}
	return nil
}

// ranLayer is the report entry of a layer that produced a result of its own. A
// layer that is missing, did not run, was not reached or lacks its tool
// measured nothing for this run.
func (f Feeds) ranLayer(id string) (report.Layer, error) {
	l, ok := f.layer(id)
	switch {
	case !ok:
		return l, refuse("the %s layer is not in the run", id)
	case !ran(l):
		return l, refuse("the %s layer is %s%s: it measured nothing for this run", id, l.Outcome, why(l))
	}
	return l, nil
}

// readFeed reads a file a feeding layer wrote. A file that is missing or empty
// is a refusal: what a layer leaves out, the ledger cannot record.
func (f Feeds) readFeed(rel, what, missing string) ([]byte, error) {
	raw, err := os.ReadFile(filepath.Join(f.Dir, filepath.FromSlash(rel)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, refuse("the %s %s does not exist: %s", what, rel, missing)
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, refuse("the %s %s is empty", what, rel)
	}
	return raw, nil
}

// packages reads the coverage profile and lists the packages under the floor.
func (f Feeds) packages() (map[string]Package, error) {
	l, err := f.ranLayer(tests.CoverageID)
	if err != nil {
		return nil, err
	}
	if msg, ok := l.Details["error"].(string); ok && msg != "" {
		return nil, refuse("the coverage layer measured nothing: %s", msg)
	}
	sum, err := f.summarize()
	if err != nil {
		return nil, err
	}
	return underFloor(sum, f.Config.Coverage.StatementsFloor), nil
}

// summarize reads the coverage profile of the test run and totals it per
// package. A profile with no block of a production file measured nothing.
func (f Feeds) summarize() (cover.Summary, error) {
	raw, err := f.readFeed(tests.ProfilePath, "coverage profile", "the test run wrote none")
	if err != nil {
		return cover.Summary{}, err
	}
	prof, err := cover.Parse(bytes.NewReader(raw))
	if err != nil {
		return cover.Summary{}, refuse("%s: %v", tests.ProfilePath, err)
	}
	sum, err := prof.Summarize(cover.Options{Module: f.Config.Module, Dir: f.Dir, ExcludeGenerated: f.Config.Coverage.ExcludeGenerated})
	if errors.Is(err, cover.ErrNoBlocks) {
		return cover.Summary{}, refuse("the coverage profile holds no block of a production file")
	}
	return sum, err
}

// underFloor lists the packages of a summary under the coverage floor, each
// with its percentage and uncovered-statement count.
func underFloor(sum cover.Summary, floor float64) map[string]Package {
	var out map[string]Package
	for _, p := range sum.Below(floor) {
		if out == nil {
			out = map[string]Package{}
		}
		out[p.Path] = Package{StatementsPct: p.Pct(), Uncovered: p.Uncovered()}
	}
	return out
}

// lintIssue is the part of a golangci-lint finding the ledger reads.
type lintIssue struct {
	FromLinter string
	Pos        struct{ Filename string }
}

// findings reads the adopter-style lint output the hygiene layer saved and
// counts the findings per linter and file. No law maps a finding before M5, so
// every finding is a `findings` entry.
func (f Feeds) findings() (Counts, error) {
	if _, err := f.ranLayer(hygiene.ID); err != nil {
		return nil, err
	}
	issues, err := f.lintIssues()
	if err != nil {
		return nil, err
	}
	var out Counts
	for _, is := range issues {
		if out == nil {
			out = Counts{}
		}
		if out[is.FromLinter] == nil {
			out[is.FromLinter] = map[string]int{}
		}
		out[is.FromLinter][f.moduleRel(is.Pos.Filename)]++
	}
	return out, nil
}

// lintIssues reads the findings of the saved lint output.
func (f Feeds) lintIssues() ([]lintIssue, error) {
	raw, err := f.readFeed(hygiene.LintJSON, "lint output", "the lint run saved none")
	if err != nil {
		return nil, err
	}
	var doc struct{ Issues *[]lintIssue }
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, refuse("%s is not JSON: %v", hygiene.LintJSON, err)
	}
	if doc.Issues == nil {
		return nil, refuse("%s is not a golangci-lint result: it has no Issues key", hygiene.LintJSON)
	}
	return *doc.Issues, nil
}

// moduleRel makes a file name from the linter a module-relative slash path.
func (f Feeds) moduleRel(name string) string {
	if name == "" {
		return "(no file)"
	}
	if filepath.IsAbs(name) {
		if rel, err := filepath.Rel(f.Dir, name); err == nil && !strings.HasPrefix(rel, "..") {
			name = rel
		}
	}
	return path.Clean(filepath.ToSlash(name))
}

// crapRow is the part of a crapDetails row the ledger reads. Crap is nil for a
// function the run does not measure (§6.2).
type crapRow struct {
	Func string
	Crap *float64
}

// functions lists the functions the CRAP layer scored over the cap. Functions
// under a crap exemption are listed apart in the report and are not debt: the
// exemption count carries them. A function the run does not measure has no
// score, so it gets no entry (ruling C3).
func (f Feeds) functions() (map[string]float64, error) {
	l, ok := f.layer(crapID)
	if ok && l.Outcome == string(gate.Skip) {
		return nil, nil // switched off in go-tops.yaml with a reason and an expiry
	}
	if _, err := f.ranLayer(crapID); err != nil {
		return nil, err
	}
	rows, err := crapRows(l)
	if err != nil {
		return nil, err
	}
	return scoresOf(rows)
}

// crapRows reads the violations of a CRAP layer's details. Details without a
// cap and a violations list, or with an error, are no CRAP report.
func crapRows(l report.Layer) ([]crapRow, error) {
	raw, err := json.Marshal(l.Details)
	if err != nil {
		return nil, refuse("the crap layer's details cannot be read: %v", err)
	}
	var d struct {
		Error      string
		Cap        *float64
		Violations *[]crapRow
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, refuse("the crap layer's details cannot be read: %v", err)
	}
	if d.Error != "" {
		return nil, refuse("the crap layer measured nothing: %s", d.Error)
	}
	if d.Cap == nil || d.Violations == nil {
		return nil, refuse("the crap layer's details have no cap or no violations list: no CRAP report was produced")
	}
	return *d.Violations, nil
}

// scoresOf turns the rows into the scores of the functions the run measured.
func scoresOf(rows []crapRow) (map[string]float64, error) {
	var out map[string]float64
	for _, r := range rows {
		if err := r.check(); err != nil {
			return nil, err
		}
		if r.Crap == nil {
			continue // not measured on this run: no score, so no entry
		}
		if out == nil {
			out = map[string]float64{}
		}
		if _, dup := out[r.Func]; dup {
			return nil, refuse("the crap layer listed two functions with the identity %s", r.Func)
		}
		out[r.Func] = *r.Crap
	}
	return out, nil
}

// check refuses a row with no identity or a score that is not a number.
func (r crapRow) check() error {
	switch {
	case r.Func == "":
		return refuse("the crap layer listed a function with no identity")
	case r.Crap != nil && (math.IsNaN(*r.Crap) || math.IsInf(*r.Crap, 0) || *r.Crap < 0):
		return refuse("the crap layer gave %s the score %v", r.Func, *r.Crap)
	}
	return nil
}
