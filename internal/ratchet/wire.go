package ratchet

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Hedronite/go-tops/internal/baseline"
	"github.com/Hedronite/go-tops/internal/crap"
	"github.com/Hedronite/go-tops/internal/exempt"
	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/gobuild"
	"github.com/Hedronite/go-tops/internal/layers/tests"
	"github.com/Hedronite/go-tops/internal/report"
)

// Paths are the two files the configuration names, relative to the module root:
// the baseline (`baseline:` in go-tops.yaml, default LedgerPath) and the acks
// file. `check` and `baseline` read the same keys.
type Paths struct {
	Ledger string
	Acks   string
}

// Inputs is what a measurer reads about the run: the build, the layers the run
// ran, the configured paths and the day the run measures. The day is read once, so
// that every part of a run counts the exemptions in force on the same day.
type Inputs struct {
	Build  gobuild.Build
	Layers []report.Layer
	Paths  Paths
	Now    time.Time
}

// Measurer measures what the run has to compare with the ledger.
type Measurer func(ctx context.Context, env *gate.Env, in Inputs) (Measure, error)

// NewJudge returns the gate's ratchet hook: it reads the ledger at the configured
// path, measures the run, judges it, and gives the layers the thresholds the
// ledger replaces. paths gives the configured paths of the module in dir.
func NewJudge(measure Measurer, paths func(dir string) (Paths, error)) gate.RatchetJudge {
	return func(ctx context.Context, env *gate.Env, layers []report.Layer) (*gate.RatchetVerdict, error) {
		if !holdsFeeds(layers) {
			return partialVerdict(env, layers, paths)
		}
		run, err := gobuild.Resolve(env.Dir, tests.Build)
		if err != nil {
			return nil, err
		}
		ps, err := paths(env.Dir)
		if err != nil {
			return refused(layers, run, nil, fmt.Errorf("the configuration: %w", err)), nil
		}
		in, err := judgeInput(env, run, ps)
		if err != nil {
			return refused(layers, run, in.Ledger, err), nil
		}
		if in.M, err = measure(ctx, env, Inputs{Build: run, Layers: layers, Paths: ps, Now: time.Now()}); err != nil {
			// Nothing was measured, so nothing can be judged: a run whose tests or
			// profile failed gets a failed ratchet, never a guess and never an
			// environment fault that hides the layers' own report.
			return refused(layers, run, in.Ledger, fmt.Errorf("the run could not be measured: %w", err)), nil
		}
		j := Judge(in)
		rep, applied := j.Report(), Apply(layers, j, in.Ledger)
		return &gate.RatchetVerdict{Report: &rep, Layers: applied, Failed: j.Outcome == Fail || uncarried(j, applied), Notes: Notes(j)}, nil
	}
}

// feedingLayers are the layers the ledger is measured from (§6.7).
var feedingLayers = []string{"hygiene", "tests", "coverage", "crap"}

// holdsFeeds reports whether the run's range holds every feeding layer. A partial run
// (`--only crap`, `--only tests`, `--max-tier 0`) holds fewer.
func holdsFeeds(layers []report.Layer) bool {
	for _, id := range feedingLayers {
		if !slices.ContainsFunc(layers, func(l report.Layer) bool { return l.ID == id }) {
			return false
		}
	}
	return true
}

// partialVerdict is the verdict of a partial run (ruling C3e): no ratchet object, and
// the layers as ApplyPartial gives them. The run ends partial as it would without a
// ledger. A configuration or a ledger that cannot be read leaves nothing with an entry,
// so everything is judged at hard.
func partialVerdict(env *gate.Env, layers []report.Layer, paths func(dir string) (Paths, error)) (*gate.RatchetVerdict, error) {
	p := Partial{Cap: env.Config.Crap.Cap}
	if ps, err := paths(env.Dir); err == nil {
		if ledger, err := readLedgerFile(env.Dir, ps.Ledger); err == nil {
			p.Ledger = ledger
		}
	}
	if len(env.Config.Exemptions) > 0 {
		var err error
		if p.Problems, err = exemptionProblems(env, time.Now()); err != nil {
			return nil, err
		}
	}
	feeds := baseline.Feeds{Report: report.Report{Layers: layers}, Config: env.Config, Dir: env.Dir}
	p.Packages = readable(feeds.Packages())
	p.Findings = readable(feeds.Findings())
	return &gate.RatchetVerdict{Layers: ApplyPartial(layers, p)}, nil
}

// readable is what a partial range read, or nothing when it could not read it: a
// layer out of the range, or an output that is missing, leaves the layer as it ran.
func readable[T any](v T, err error) T {
	if err != nil {
		var none T
		return none
	}
	return v
}

// carrier is the layer that reports a held failure of a kind on a leg that is not
// the ledger leg.
var carrier = map[string]string{
	"functions": "crap", "not measured": "crap", ProblemKind: "crap",
	"findings": "hygiene", "suppressions.nolint": "hygiene", "suppressions.ack": "hygiene", "laws": "hygiene",
	"packages": "coverage",
}

// uncarried reports whether a failure held by a leg that is not the ledger leg is
// not carried by a failing layer, so that the run fails on the ratchet itself.
func uncarried(j Judgment, layers []report.Layer) bool {
	for _, f := range j.HeldToCap {
		id := carrier[f.Kind]
		if !slices.ContainsFunc(layers, func(l report.Layer) bool { return l.ID == id && l.Outcome == "fail" }) {
			return true
		}
	}
	return false
}

// judgeInput reads the ledger and its base copy, and on a leg that is not the ledger
// leg what the ledger build compiles. A ledger that cannot be read is an error: the
// judge trusts nothing it cannot read.
func judgeInput(env *gate.Env, run gobuild.Build, ps Paths) (Input, error) {
	in := Input{Run: run}
	ledger, err := readLedgerFile(env.Dir, ps.Ledger)
	if err != nil {
		return in, fmt.Errorf("baseline: %w", err)
	}
	if in.Ledger = ledger; ledger == nil {
		return in, nil
	}
	// Every leg reads the base copy: the ledger leg judges against it, and the
	// others only say when the build moved. Only the ledger leg fails on it.
	in.Base = ReadBase(env.Dir, env.Base, ps.Ledger)
	if !run.Equal(ledger.Build) {
		if err := ledgerSources(&in, env.Dir); err != nil {
			return in, fmt.Errorf("find what the ledger build compiles: %w", err)
		}
	}
	return in, nil
}

// ledgerSources finds what the ledger build compiles by loading and selecting with
// the recorded build: its functions, its files and its packages.
func ledgerSources(in *Input, dir string) error {
	var err error
	if in.LedgerCompiled, err = crap.Compiled(dir, in.Ledger.Build); err != nil {
		return err
	}
	sel, err := crap.Select(dir, in.Ledger.Build)
	if err != nil {
		return err
	}
	in.LedgerFiles, in.LedgerPackages = sel.Files, sel.Packages
	return nil
}

// refused is a failed ratchet that judged nothing, with the layers as they ran.
func refused(layers []report.Layer, run gobuild.Build, ledger *Ledger, err error) *gate.RatchetVerdict {
	ledgerBuild := run
	if ledger != nil {
		ledgerBuild = ledger.Build
	}
	rep := report.Ratchet{Outcome: string(Fail), LedgerBuild: reportBuild(ledgerBuild), RunBuild: reportBuild(run)}
	return &gate.RatchetVerdict{Report: &rep, Layers: layers, Failed: true, Notes: []string{fmt.Sprintf("ratchet: %v\n", err)}}
}

// CollectMeasure is the measurer of the gate: what the baseline package measures
// for the ledger (internal/baseline.Collect, the measurer `baseline --init` writes
// the first ledger from), so that the run and the ledger can never measure
// differently, plus what only a judgment needs: the functions the run does not
// measure and the exemptions that match nothing.
func CollectMeasure() Measurer {
	return func(_ context.Context, env *gate.Env, in Inputs) (Measure, error) {
		bm, err := baseline.Collect(baseline.Feeds{
			Report: report.Report{Layers: in.Layers}, Config: env.Config, Dir: env.Dir,
			Acks: in.Paths.Acks, Build: in.Build, Now: func() time.Time { return in.Now },
		})
		if err != nil {
			return Measure{}, err
		}
		m := fromBaseline(bm, env.Config)
		m.NotMeasured = notMeasuredRows(in.Layers)
		if len(env.Config.Exemptions) > 0 {
			if m.Problems, err = exemptionProblems(env, in.Now); err != nil {
				return Measure{}, err
			}
		}
		return m, nil
	}
}

// fromBaseline is what the baseline package measured, as the judge reads it:
// every function over the cap and every package under the floor, with the counts.
func fromBaseline(bm baseline.Measure, cfg gate.Config) Measure {
	m := Measure{
		Cap: cfg.Crap.Cap, Floor: cfg.Coverage.StatementsFloor,
		Packages: bm.Packages, Findings: bm.Findings, Nolint: bm.Nolint, Ack: bm.Ack,
		Exemptions: bm.Exemptions, JudgedLaws: bm.JudgedLaws,
	}
	for key, score := range bm.Functions {
		m.Functions = append(m.Functions, crap.Function{Key: key, CRAP: score})
	}
	slices.SortFunc(m.Functions, func(a, b crap.Function) int { return strings.Compare(a.Key, b.Key) })
	return m
}

// notMeasuredRows reads the rows of the CRAP layer's violations that carry no
// score: the functions the run does not measure and whose complexity is over the
// cap (§6.2). The baseline records none of them, and the judge fails each. The
// layers are plain (see details.go): a row's crap is null, not absent.
func notMeasuredRows(layers []report.Layer) []crap.NotMeasured {
	var out []crap.NotMeasured
	for _, l := range plain(layers) {
		if l.ID != "crap" {
			continue
		}
		for _, row := range listOf(l.Details["violations"]) {
			if row["crap"] != nil {
				continue
			}
			if key := text(row["func"]); key != "" {
				out = append(out, crap.NotMeasured{Key: key, Comp: number(row["cc"])})
			}
		}
	}
	return out
}

// exemptionProblems applies the exemptions in force on day now to every production
// function the source declares, whether or not the run's build compiles it, as the
// CRAP layer does, and returns the entries that match nothing. The layer reports
// them in its reason and in no field a reader could use, and the report schema has
// none to add, so the match is made again here: from the source alone, with no
// profile and no scoring.
func exemptionProblems(env *gate.Env, now time.Time) ([]Failure, error) {
	ex, err := crap.Exist(env.Dir)
	if err != nil {
		return nil, err
	}
	funcs := make([]exempt.Func, len(ex.Funcs))
	for i, f := range ex.Funcs {
		funcs[i] = exempt.Func{Key: f.Key, File: f.File, Recv: f.Recv, Name: f.Name, Table: f.Table}
	}
	var problems []Failure
	for _, p := range exempt.Match(env.Config.Exemptions, funcs, now).Problems {
		problems = append(problems, Failure{Kind: ProblemKind, Key: p.Entry.Path, Why: p.Message})
	}
	return problems, nil
}
