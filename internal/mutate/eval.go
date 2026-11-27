package mutate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Hedronite/go-tops/internal/exempt"
	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/report"
)

// ProtocolMutatorsOn lists the six mutators of §6.4.
var ProtocolMutatorsOn = []string{
	"ARITHMETIC_BASE",
	"CONDITIONALS_BOUNDARY",
	"CONDITIONALS_NEGATION",
	"INCREMENT_DECREMENT",
	"INVERT_LOGICAL",
	"INVERT_NEGATIVES",
}

// EnabledMutators returns a lookup set of the 6 protocol-enabled mutators.
func EnabledMutators() map[string]bool {
	out := make(map[string]bool, len(ProtocolMutatorsOn))
	for _, m := range ProtocolMutatorsOn {
		out[m] = true
	}
	return out
}

// MutantOutcome represents a single evaluated mutant entry mapped into protocol counts and survivors.
type MutantOutcome struct {
	Type   string `json:"type"`
	Status string `json:"status"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// Evaluation holds the protocol-mapped mutation outcome, counts, and survivor rows.
type Evaluation struct {
	Mutants             int
	Killed              int
	SurvivedExplained   int
	SurvivedUnexplained int
	TimedOut            int
	TimedOutKeys        []string
	Dropped             int
	Exempt              []string
	NotMeasured         int
	Base                string
	BaseSHA             string
	Survivors           []Survivor
	ExemptionProblems   []exempt.Problem
}

// Details renders Evaluation into the protocol 0.4.6 mutationDetails JSON map.
func (e Evaluation) Details() map[string]any {
	rows := make([]map[string]any, 0, len(e.Survivors))
	for _, s := range e.Survivors {
		rows = append(rows, survivorRow(s))
	}
	return map[string]any{
		"mutants":              e.Mutants,
		"killed":               e.Killed,
		"survived_explained":   e.SurvivedExplained,
		"survived_unexplained": e.SurvivedUnexplained,
		"max":                  0,
		"timed_out":            e.TimedOut,
		"timed_out_keys":       nonNilStrings(e.TimedOutKeys),
		"dropped":              e.Dropped,
		"exempt":               nonNilStrings(e.Exempt),
		"not_measured":         e.NotMeasured,
		"base":                 e.Base,
		"base_sha":             e.BaseSHA,
		"survivors":            rows,
	}
}

func survivorRow(s Survivor) map[string]any {
	var explained any
	if s.Explained != nil {
		explained = *s.Explained
	}
	return map[string]any{
		"key":       s.Key.String(),
		"file":      s.Key.File,
		"function":  s.Key.Function,
		"mutator":   s.Key.Mutator,
		"line_hash": s.Key.LineHash,
		"line":      s.Line,
		"status":    s.Status,
		"explained": explained,
	}
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

type evalContext struct {
	idx    *ModuleIndex
	exRes  exempt.MutationResult
	ledger Ledger
}

func prepareEvaluation(cfg gate.MutationConfig, exemptions []report.Exemption, diff DiffResult, dir string, today time.Time) (Evaluation, evalContext, error) {
	if err := validateEngine(cfg.Engine); err != nil {
		return Evaluation{}, evalContext{}, err
	}
	notMeasured, err := countNotMeasured(dir, diff.OutOfBuildFiles)
	if err != nil {
		return Evaluation{}, evalContext{}, err
	}
	idx, err := ScanModule(dir)
	if err != nil {
		return Evaluation{}, evalContext{}, err
	}
	exRes := exempt.MatchMutation(exemptions, idx.Files(), idx.Funcs(), today)
	ledger, err := loadConfiguredLedger(dir, cfg.Ledger, today)
	if err != nil {
		return Evaluation{}, evalContext{}, err
	}
	if err := checkNoLineDirectives(dir, diff.InBuildFiles); err != nil {
		return Evaluation{}, evalContext{}, err
	}
	ev := newEvaluation(diff, notMeasured, exRes.Problems)
	return ev, evalContext{idx: idx, exRes: exRes, ledger: ledger}, nil
}

// validateEngine accepts the one engine, and an empty name for it.
func validateEngine(engine string) error {
	if engine != "" && engine != "overlay" {
		return fmt.Errorf("mutation engine %q is not supported (want overlay)", engine)
	}
	return nil
}

// EvaluateOverlayEmpty evaluates a vacuous or out-of-build-only diff under the overlay engine.
func EvaluateOverlayEmpty(cfg gate.MutationConfig, exemptions []report.Exemption, diff DiffResult, dir string, today time.Time) (Evaluation, error) {
	ev, _, err := prepareEvaluation(cfg, exemptions, diff, dir, today)
	return ev, err
}

func boundWorkers(procs int) int {
	return min(max(procs, 1), 4)
}

func countNotMeasured(dir string, outFiles map[string]map[int]bool) (int, error) {
	enabled := EnabledMutators()
	total := 0
	for rel, lines := range outFiles {
		src, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return 0, fmt.Errorf("read out-of-build file %s: %w", rel, err)
		}
		n, err := CountMutantsOnLines(src, lines, enabled)
		if err != nil {
			return 0, fmt.Errorf("count mutants in %s: %w", rel, err)
		}
		total += n
	}
	return total, nil
}

func loadConfiguredLedger(dir, ledgerPath string, today time.Time) (Ledger, error) {
	if ledgerPath == "" {
		return nil, nil
	}
	slash := filepath.ToSlash(ledgerPath)
	if DefaultLedger != nil {
		return DefaultLedger(dir, slash, today)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(slash))); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return nil, fmt.Errorf("mutation ledger %q exists, but no ledger reader is registered", slash)
}

func newEvaluation(diff DiffResult, notMeasured int, problems []exempt.Problem) Evaluation {
	return Evaluation{
		TimedOutKeys:      []string{},
		Exempt:            []string{},
		NotMeasured:       notMeasured,
		Base:              diff.Base,
		BaseSHA:           diff.BaseSHA,
		Survivors:         []Survivor{},
		ExemptionProblems: problems,
	}
}

func checkNoLineDirectives(dir string, inBuild map[string]map[int]bool) error {
	for _, rel := range sortedMapKeys(inBuild) {
		src, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return fmt.Errorf("read in-build file %s: %w", rel, err)
		}
		if ln, text, ok, err := FindLineDirective(src); err != nil {
			return err
		} else if ok {
			return fmt.Errorf("line directive not allowed in %s:%d (%s)", rel, ln, text)
		}
	}
	return nil
}

func recordMutant(ev *Evaluation, rel string, m MutantOutcome, src []byte, idx *ModuleIndex, exRes exempt.MutationResult, ledger Ledger) error {
	switch m.Status {
	case "NOT VIABLE":
		ev.Dropped++
		return nil
	case "KILLED":
		ev.Mutants++
		ev.Killed++
		return nil
	case "TIMED OUT":
		ev.Mutants++
		return recordTimedOut(ev, rel, m, src, idx)
	case "LIVED", "NOT COVERED":
		ev.Mutants++
		return recordSurvivor(ev, rel, m, src, idx, exRes, ledger)
	default:
		return fmt.Errorf("unexpected mutant status %q at %s:%d:%d", m.Status, rel, m.Line, m.Column)
	}
}

func buildKey(rel string, m MutantOutcome, src []byte, idx *ModuleIndex) (SurvivorKey, error) {
	hash, err := MutatedLineHash(src, m.Line, m.Column, m.Type)
	if err != nil {
		return SurvivorKey{}, err
	}
	fn := idx.EnclosingFunc(rel, m.Line, m.Column)
	return SurvivorKey{File: rel, Function: fn, Mutator: m.Type, LineHash: hash}, nil
}

func recordTimedOut(ev *Evaluation, rel string, m MutantOutcome, src []byte, idx *ModuleIndex) error {
	key, err := buildKey(rel, m, src, idx)
	if err != nil {
		return err
	}
	ev.Killed++
	ev.TimedOut++
	ev.TimedOutKeys = append(ev.TimedOutKeys, key.String())
	return nil
}

func recordSurvivor(ev *Evaluation, rel string, m MutantOutcome, src []byte, idx *ModuleIndex, exRes exempt.MutationResult, ledger Ledger) error {
	key, err := buildKey(rel, m, src, idx)
	if err != nil {
		return err
	}
	if exRes.Covers(rel, key.Function) {
		ev.Exempt = append(ev.Exempt, key.String())
		ev.Survivors = append(ev.Survivors, Survivor{Key: key, Line: m.Line, Status: m.Status, Explained: nil})
		return nil
	}
	if ledger != nil && ledger.Explains(key) {
		srcTag := "ledger"
		ev.SurvivedExplained++
		ev.Survivors = append(ev.Survivors, Survivor{Key: key, Line: m.Line, Status: m.Status, Explained: &srcTag})
		return nil
	}
	ev.SurvivedUnexplained++
	ev.Survivors = append(ev.Survivors, Survivor{Key: key, Line: m.Line, Status: m.Status, Explained: nil})
	return nil
}

func sortEvaluation(ev *Evaluation) {
	slices.Sort(ev.TimedOutKeys)
	slices.Sort(ev.Exempt)
	slices.SortFunc(ev.Survivors, func(a, b Survivor) int {
		if a.Key.File != b.Key.File {
			return strings.Compare(a.Key.File, b.Key.File)
		}
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		if a.Key.Mutator != b.Key.Mutator {
			return strings.Compare(a.Key.Mutator, b.Key.Mutator)
		}
		return strings.Compare(a.Key.LineHash, b.Key.LineHash)
	})
}

func sortedMapKeys(m map[string]map[int]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
