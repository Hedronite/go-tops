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

	"github.com/Hedronite/go-tops/internal/cover"
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
	// TODO(G2): stand-in. Replace with the live exemption count internal/exempt
	// exposes (records in force today, §6.8) when G2 merges. The configuration's
	// records are all unexpired here, because `check` rejects an expired one.
	m.Exemptions = len(f.Config.Exemptions)
	m.JudgedLaws = f.Report.Laws.ApplicableJudged
	m.Build = f.Build
	return m, nil
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

// packages reads the coverage profile and lists the packages under the floor.
func (f Feeds) packages() (map[string]Package, error) {
	l, ok := f.layer(tests.CoverageID)
	switch {
	case !ok:
		return nil, refuse("the coverage layer is not in the run")
	case !ran(l):
		return nil, refuse("the coverage layer is %s%s: nothing was measured", l.Outcome, why(l))
	}
	if msg, ok := l.Details["error"].(string); ok && msg != "" {
		return nil, refuse("the coverage layer measured nothing: %s", msg)
	}
	raw, err := os.ReadFile(filepath.Join(f.Dir, filepath.FromSlash(tests.ProfilePath)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, refuse("the coverage profile %s does not exist: the test run wrote none", tests.ProfilePath)
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, refuse("the coverage profile %s is empty", tests.ProfilePath)
	}
	prof, err := cover.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, refuse("%s: %v", tests.ProfilePath, err)
	}
	c := f.Config
	sum, err := prof.Summarize(cover.Options{Module: c.Module, Dir: f.Dir, ExcludeGenerated: c.Coverage.ExcludeGenerated})
	if errors.Is(err, cover.ErrNoBlocks) {
		return nil, refuse("the coverage profile holds no block of a production file")
	}
	if err != nil {
		return nil, err
	}
	var out map[string]Package
	for _, p := range sum.Below(c.Coverage.StatementsFloor) {
		if out == nil {
			out = map[string]Package{}
		}
		out[p.Path] = Package{StatementsPct: p.Pct(), Uncovered: p.Uncovered()}
	}
	return out, nil
}

// findings reads the adopter-style lint output the hygiene layer saved and
// counts the findings per linter and file. No law maps a finding before M5, so
// every finding is a `findings` entry.
func (f Feeds) findings() (Counts, error) {
	l, ok := f.layer(hygiene.ID)
	switch {
	case !ok:
		return nil, refuse("the hygiene layer is not in the run")
	case !ran(l):
		return nil, refuse("the hygiene layer is %s%s: its lint output is not this run's", l.Outcome, why(l))
	}
	raw, err := os.ReadFile(filepath.Join(f.Dir, filepath.FromSlash(hygiene.LintJSON)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, refuse("the lint output %s does not exist: the lint run saved none", hygiene.LintJSON)
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, refuse("the lint output %s is empty", hygiene.LintJSON)
	}
	var doc struct {
		Issues *[]struct {
			FromLinter string
			Pos        struct{ Filename string }
		}
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, refuse("%s is not JSON: %v", hygiene.LintJSON, err)
	}
	if doc.Issues == nil {
		return nil, refuse("%s is not a golangci-lint result: it has no Issues key", hygiene.LintJSON)
	}
	var out Counts
	for _, is := range *doc.Issues {
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

// functions lists the functions the CRAP layer scored over the cap. Functions
// under a crap exemption are listed apart in the report and are not debt: the
// exemption count carries them.
func (f Feeds) functions() (map[string]float64, error) {
	l, ok := f.layer(crapID)
	switch {
	case !ok:
		return nil, refuse("the crap layer is not in the run")
	case l.Outcome == string(gate.Skip):
		return nil, nil // switched off in go-tops.yaml with a reason and an expiry
	case !ran(l):
		return nil, refuse("the crap layer is %s%s: no CRAP report was produced", l.Outcome, why(l))
	}
	raw, err := json.Marshal(l.Details)
	if err != nil {
		return nil, refuse("the crap layer's details cannot be read: %v", err)
	}
	var d struct {
		Error      string
		Cap        *float64
		Violations *[]struct {
			Func string
			Crap *float64 // null for a function the run does not measure (§6.2)
		}
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
	var out map[string]float64
	for _, v := range *d.Violations {
		switch {
		case v.Func == "":
			return nil, refuse("the crap layer listed a function with no identity")
		case v.Crap == nil:
			continue // not measured on this run: no score, so no entry
		case math.IsNaN(*v.Crap) || math.IsInf(*v.Crap, 0) || *v.Crap < 0:
			return nil, refuse("the crap layer gave %s the score %v", v.Func, *v.Crap)
		}
		if out == nil {
			out = map[string]float64{}
		}
		if _, dup := out[v.Func]; dup {
			return nil, refuse("the crap layer listed two functions with the identity %s", v.Func)
		}
		out[v.Func] = *v.Crap
	}
	return out, nil
}
