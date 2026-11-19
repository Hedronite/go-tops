package ratchet_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/Hedronite/go-tops/internal/cover"
	"github.com/Hedronite/go-tops/internal/crap"
	"github.com/Hedronite/go-tops/internal/gobuild"
	"github.com/Hedronite/go-tops/internal/ratchet"
)

var (
	linux  = gobuild.Build{GOOS: "linux", GOARCH: "amd64", CGOEnabled: true}
	darwin = gobuild.Build{GOOS: "darwin", GOARCH: "arm64", CGOEnabled: true}
)

// overScores are the eight functions over the cap of 10 in the §6.7 first
// adoption walk-through: a repo of 100 production functions, 8 over the cap.
var overScores = []float64{21.0, 15.5, 14.2, 13.0, 12.5, 11.8, 11.2, 10.4}

func key(i int) string { return fmt.Sprintf("example.com/m/p.F%03d", i) }

// legacy is what a run measures on the legacy repo of the walk-through: 100
// production functions, the first 8 over the cap, a package under the coverage
// floor, three //nolint in two files, no exemption.
func legacy() ratchet.Measure {
	var fs []crap.Function
	for i := 0; i < 100; i++ {
		score := 2 + float64(i%8)
		if i < len(overScores) {
			score = overScores[i]
		}
		fs = append(fs, crap.Function{Key: key(i), File: "p/p.go", Comp: int(math.Ceil(score)), Cov: 0.5, CRAP: score})
	}
	return ratchet.Measure{
		Cap: 10, Floor: 90,
		Functions: fs,
		// Packages a (95%) and b (92%) meet the floor, so the baseline measures
		// neither; low is under it.
		Packages:   map[string]ratchet.Package{"example.com/m/low": {StatementsPct: 80, Uncovered: 20}},
		Nolint:     ratchet.Counts{"errcheck": {"a.go": 2}, "gosec": {"b.go": 1}},
		JudgedLaws: 5,
	}
}

// initLedger is what `baseline --init` writes from a measurement: what was
// measured, not what passed.
func initLedger(m ratchet.Measure, build gobuild.Build) *ratchet.Ledger {
	l := &ratchet.Ledger{
		Build: build, Functions: map[string]float64{}, Packages: map[string]ratchet.Package{},
		Findings: m.Findings, Laws: m.Laws, Nolint: m.Nolint, Ack: m.Ack,
		Exemptions: m.Exemptions, JudgedLaws: m.JudgedLaws,
	}
	for _, f := range m.Functions {
		if f.CRAP > m.Cap {
			l.Functions[f.Key] = f.CRAP
		}
	}
	for path, p := range m.Packages {
		l.Packages[path] = p
	}
	return l
}

// input is a run on the ledger leg against ledger, with base as the diff base.
func input(m ratchet.Measure, l *ratchet.Ledger, base ratchet.Base) ratchet.Input {
	return ratchet.Input{Ledger: l, Base: base, Run: l.Build, M: m}
}

var firstLedger = ratchet.Base{}

func based(l *ratchet.Ledger) ratchet.Base { return ratchet.Base{Ledger: l} }

func clone(l *ratchet.Ledger) *ratchet.Ledger {
	c := *l
	c.Functions = map[string]float64{}
	for k, v := range l.Functions {
		c.Functions[k] = v
	}
	c.Packages = map[string]ratchet.Package{}
	for k, v := range l.Packages {
		c.Packages[k] = v
	}
	c.Regressions = append([]ratchet.Regression(nil), l.Regressions...)
	return &c
}

func setScore(m ratchet.Measure, k string, score float64) ratchet.Measure {
	fs := append([]crap.Function(nil), m.Functions...)
	for i := range fs {
		if fs[i].Key == k {
			fs[i].CRAP = score
		}
	}
	m.Functions = fs
	return m
}

// setPackage is the measure after a package has statements statements of which
// covered run: a package at or over the floor is not measured, one under it is, with
// its percentage and uncovered count.
func setPackage(m ratchet.Measure, path string, statements, covered int) ratchet.Measure {
	ps := map[string]ratchet.Package{}
	for k, v := range m.Packages {
		ps[k] = v
	}
	pkg := cover.Package{Path: path, Statements: statements, Covered: covered}
	if pkg.Pct() >= m.Floor {
		delete(ps, path)
	} else {
		ps[path] = ratchet.Package{StatementsPct: pkg.Pct(), Uncovered: pkg.Uncovered()}
	}
	m.Packages = ps
	return m
}

func wantOutcome(t *testing.T, j ratchet.Judgment, want ratchet.Outcome) {
	t.Helper()
	if j.Outcome != want {
		t.Fatalf("outcome %s, want %s; failures %v", j.Outcome, want, j.Failures)
	}
}

func wantFailure(t *testing.T, j ratchet.Judgment, kind, key string) {
	t.Helper()
	for _, f := range j.Failures {
		if f.Kind == kind && (key == "" || f.Key == key) {
			return
		}
	}
	t.Errorf("no %s failure for %q among %v", kind, key, j.Failures)
}

func ptr(f float64) *float64 { return &f }

func notMeasured(key string, comp int) crap.NotMeasured {
	return crap.NotMeasured{Key: key, Comp: comp}
}

func overFunction(key string, score float64) crap.Function {
	return crap.Function{Key: key, File: "p/p.go", Comp: int(math.Ceil(score)), Cov: 0.5, CRAP: score}
}

// ledgerBuildFiles and ledgerBuildPackages are what the ledger build compiles in the
// legacy repo: every file and package its measures name.
var (
	ledgerBuildFiles    = map[string]bool{"a.go": true, "b.go": true, "x.go": true, "y.go": true, "z.go": true}
	ledgerBuildPackages = map[string]bool{"example.com/m/a": true, "example.com/m/b": true, "example.com/m/low": true}
)

// otherLegInput is a run on darwin against a ledger recorded on linux, given what the
// ledger build compiles: the functions in compiled, and the files and packages of
// the legacy repo.
func otherLegInput(l *ratchet.Ledger, base ratchet.Base, m ratchet.Measure, compiled map[string]bool) ratchet.Input {
	return ratchet.Input{
		Ledger: l, Base: base, Run: darwin, M: m,
		LedgerCompiled: compiled, LedgerFiles: ledgerBuildFiles, LedgerPackages: ledgerBuildPackages,
	}
}

// defaultPaths is the configuration of a module that names no baseline or acks path.
func defaultPaths(string) (ratchet.Paths, error) {
	return ratchet.Paths{Ledger: ratchet.LedgerPath}, nil
}

// without is the measure of a repo where the function with the key is covered by an
// exemption: the CRAP layer leaves it out of the violations it lists, so the baseline
// measures nothing for it.
func without(m ratchet.Measure, k string) ratchet.Measure {
	var fs []crap.Function
	for _, f := range m.Functions {
		if f.Key != k {
			fs = append(fs, f)
		}
	}
	m.Functions = fs
	return m
}

// num reads a number out of layer details whatever form it took.
func num(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case float64:
		return n
	}
	return -1
}
