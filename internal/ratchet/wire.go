package ratchet

import (
	"context"
	"fmt"
	"path/filepath"
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

// Measurer measures what the run has to compare with the ledger, from the layers
// the run just ran, in the build it ran under.
type Measurer func(ctx context.Context, env *gate.Env, run gobuild.Build, layers []report.Layer) (Measure, error)

// NewJudge returns the gate's ratchet hook: it reads the ledger, measures the
// run, judges it, and gives the layers the thresholds the ledger replaces.
func NewJudge(measure Measurer) gate.RatchetJudge {
	return func(ctx context.Context, env *gate.Env, layers []report.Layer) (*gate.RatchetVerdict, error) {
		run, err := gobuild.Resolve(env.Dir, tests.Build)
		if err != nil {
			return nil, err
		}
		in := Input{Run: run}
		ledger, readErr := readLedgerFile(env.Dir)
		if readErr != nil {
			return unreadable(layers, run, readErr), nil
		}
		in.Ledger = ledger
		if in.Ledger != nil {
			// Every leg reads the base copy: the ledger leg judges against it, and the
			// others only say when the build moved. Only the ledger leg fails on it.
			in.Base = ReadBase(env.Dir, env.Base)
		}
		if in.Ledger != nil && !run.Equal(in.Ledger.Build) {
			in.LedgerCompiled, err = crap.Compiled(env.Dir, in.Ledger.Build)
			if err != nil {
				return nil, fmt.Errorf("find what the ledger build compiles: %w", err)
			}
		}
		if in.M, err = measure(ctx, env, run, layers); err != nil {
			// Nothing was measured, so nothing can be judged: a run whose tests or
			// profile failed gets a failed ratchet, never a guess and never an
			// environment fault that hides the layers' own report.
			return refused(layers, run, in.Ledger, fmt.Errorf("the run could not be measured: %w", err)), nil
		}
		j := Judge(in)
		rep := j.Report()
		return &gate.RatchetVerdict{Report: &rep, Layers: Apply(layers, j, in.Ledger), Failed: j.Failed(), Notes: Notes(j)}, nil
	}
}

// unreadable is the verdict when the baseline file exists and cannot be read: a
// file the judge cannot trust fails the run, and nothing is guessed from it.
func unreadable(layers []report.Layer, run gobuild.Build, err error) *gate.RatchetVerdict {
	return refused(layers, run, nil, fmt.Errorf("baseline: %w", err))
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
// measure and the exemptions that match nothing. acks gives the module-relative
// path of the acks file of the configuration, or "".
func CollectMeasure(acks func(dir string) (string, error)) Measurer {
	return func(_ context.Context, env *gate.Env, run gobuild.Build, layers []report.Layer) (Measure, error) {
		path, err := acks(env.Dir)
		if err != nil {
			return Measure{}, err
		}
		bm, err := baseline.Collect(baseline.Feeds{
			Report: report.Report{Layers: layers}, Config: env.Config, Dir: env.Dir, Acks: path, Build: run,
		})
		if err != nil {
			return Measure{}, err
		}
		m := fromBaseline(bm, env.Config)
		m.NotMeasured = notMeasuredRows(layers)
		if len(env.Config.Exemptions) > 0 {
			if m.Problems, err = exemptionProblems(env, run); err != nil {
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

// number is v as an int, or 0 when it is not one.
func number(v any) int {
	n, ok := v.(int)
	if !ok {
		return 0
	}
	return n
}

// notMeasuredRows reads the rows of the CRAP layer's violations that carry no
// score: the functions the run does not measure and whose complexity is over the
// cap (§6.2). The baseline records none of them, and the judge fails each.
func notMeasuredRows(layers []report.Layer) []crap.NotMeasured {
	var out []crap.NotMeasured
	for _, l := range layers {
		if l.ID != "crap" {
			continue
		}
		rows, ok := l.Details["violations"].([]any)
		if !ok {
			return nil
		}
		for _, r := range rows {
			row, ok := r.(map[string]any)
			if !ok || row["crap"] != nil {
				continue
			}
			if key := text(row["func"]); key != "" {
				out = append(out, crap.NotMeasured{Key: key, Comp: number(row["cc"])})
			}
		}
	}
	return out
}

// exemptionProblems applies the exemptions in force to every production function,
// as the CRAP layer does, and returns the entries that match nothing. It scores the
// module again for the function list, so it runs only when the configuration has
// exemptions.
func exemptionProblems(env *gate.Env, run gobuild.Build) ([]Failure, error) {
	rep, err := crap.Score(env.Dir, filepath.Join(env.Dir, filepath.FromSlash(tests.ProfilePath)), crap.Options{
		ErrGuardDiscount: env.Config.Crap.ErrGuardDiscount, WrapFunctions: env.Config.Crap.WrapFunctions, Build: run,
	})
	if err != nil {
		return nil, err
	}
	funcs := make([]exempt.Func, 0, len(rep.Measured)+len(rep.NotMeasured))
	for _, f := range rep.Measured {
		funcs = append(funcs, exempt.Func{Key: f.Key, File: f.File, Recv: f.Recv, Name: f.Name, Table: f.Table})
	}
	for _, f := range rep.NotMeasured {
		funcs = append(funcs, exempt.Func{Key: f.Key, File: f.File, Recv: f.Recv, Name: f.Name, Table: f.Table})
	}
	var problems []Failure
	for _, p := range exempt.Match(env.Config.Exemptions, funcs, time.Now()).Problems {
		problems = append(problems, Failure{Kind: ProblemKind, Key: p.Entry.Path, Why: p.Message})
	}
	return problems, nil
}
