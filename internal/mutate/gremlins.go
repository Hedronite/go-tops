package mutate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Hedronite/go-tops/internal/exempt"
	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/gobuild"
	"github.com/Hedronite/go-tops/internal/report"
)

// ProtocolMutatorsOn lists the 6 mutators enabled under protocol 0.4.5 (M4-R10).
var ProtocolMutatorsOn = []string{
	"ARITHMETIC_BASE",
	"CONDITIONALS_BOUNDARY",
	"CONDITIONALS_NEGATION",
	"INCREMENT_DECREMENT",
	"INVERT_LOGICAL",
	"INVERT_NEGATIVES",
}

// ProtocolMutatorsOff lists the 5 mutators explicitly disabled under protocol 0.4.5.
var ProtocolMutatorsOff = []string{
	"INVERT_ASSIGNMENTS",
	"INVERT_BITWISE",
	"INVERT_BWASSIGN",
	"INVERT_LOOPCTRL",
	"REMOVE_SELF_ASSIGNMENTS",
}

// EnabledMutators returns a lookup set of the 6 protocol-enabled mutators.
func EnabledMutators() map[string]bool {
	out := make(map[string]bool, len(ProtocolMutatorsOn))
	for _, m := range ProtocolMutatorsOn {
		out[m] = true
	}
	return out
}

// GremlinsReport mirrors the machine-readable JSON file written by gremlins v0.6.0.
type GremlinsReport struct {
	GoModule          string         `json:"go_module"`
	Files             []GremlinsFile `json:"files"`
	TestEfficacy      float64        `json:"test_efficacy"`
	MutationsCoverage float64        `json:"mutations_coverage"`
	MutantsTotal      int            `json:"mutants_total"`
	MutantsKilled     int            `json:"mutants_killed"`
	MutantsLived      int            `json:"mutants_lived"`
	MutantsNotViable  int            `json:"mutants_not_viable"`
	MutantsNotCovered int            `json:"mutants_not_covered"`
	ElapsedTime       float64        `json:"elapsed_time"`
	MutatorStatistics map[string]int `json:"mutator_statistics"`
}

// GremlinsFile represents one file entry in GremlinsReport.
type GremlinsFile struct {
	Filename  string             `json:"file_name"`
	Mutations []GremlinsMutation `json:"mutations"`
}

// GremlinsMutation represents a single mutant entry reported by gremlins.
type GremlinsMutation struct {
	Type   string `json:"type"`
	Status string `json:"status"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// ParseGremlinsOutput strictly decodes a gremlins v0.6.0 JSON report.
func ParseGremlinsOutput(data []byte) (GremlinsReport, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return GremlinsReport{}, fmt.Errorf("empty gremlins output")
	}
	var rep GremlinsReport
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rep); err != nil {
		return GremlinsReport{}, fmt.Errorf("parse gremlins output: %w", err)
	}
	if err := ensureEOF(dec); err != nil {
		return GremlinsReport{}, err
	}
	if err := validateStatuses(rep.Files); err != nil {
		return GremlinsReport{}, err
	}
	return rep, nil
}

func ensureEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("trailing data in gremlins output")
	}
	return nil
}

func validateStatuses(files []GremlinsFile) error {
	for _, f := range files {
		for _, m := range f.Mutations {
			if !isKnownGremlinsStatus(m.Status) {
				return fmt.Errorf("unknown mutant status %q at %s:%d:%d", m.Status, f.Filename, m.Line, m.Column)
			}
		}
	}
	return nil
}

func isKnownGremlinsStatus(status string) bool {
	switch status {
	case "KILLED", "LIVED", "NOT COVERED", "TIMED OUT", "NOT VIABLE", "SKIPPED", "RUNNABLE":
		return true
	default:
		return false
	}
}

// NeedsIntegration checks whether any in-build changed file lives in a package whose declared name
// differs from the last import-path element (e.g. package main in cmd/gotops, E18/Q1).
func NeedsIntegration(ctx context.Context, ex gate.Executor, dir string, inBuild map[string]map[int]bool) (bool, error) {
	_, integ, err := PartitionByIntegration(ctx, ex, dir, inBuild)
	if err != nil {
		return false, err
	}
	return len(integ) > 0, nil
}

// PartitionByIntegration splits in-build changed files into normal packages and mismatched-name packages requiring --integration.
func PartitionByIntegration(ctx context.Context, ex gate.Executor, dir string, inBuild map[string]map[int]bool) (normal, integ map[string]map[int]bool, err error) {
	normal = map[string]map[int]bool{}
	integ = map[string]map[int]bool{}
	if len(inBuild) == 0 {
		return normal, integ, nil
	}
	res, err := ex.Run(ctx, gate.Cmd{Dir: dir, Argv: []string{"go", "list", "-f", "{{.Dir}} {{.ImportPath}} {{.Name}}", "./..."}})
	if err != nil || res.ExitCode != 0 {
		return nil, nil, fmt.Errorf("list packages for mutation integration check: %s", strings.TrimSpace(string(res.Stderr)))
	}
	byRelDir := parsePkgList(resolvedPath(dir), string(res.Stdout))
	for rel, lines := range inBuild {
		if byRelDir[filepath.ToSlash(filepath.Dir(rel))] {
			integ[rel] = lines
		} else {
			normal[rel] = lines
		}
	}
	return normal, integ, nil
}

func parsePkgList(absDir, out string) map[string]bool {
	mismatched := map[string]bool{}
	for _, line := range nonEmptyLines(out) {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		rel, err := filepath.Rel(absDir, resolvedPath(fields[0]))
		if err != nil {
			continue
		}
		if fields[2] != path.Base(fields[1]) {
			mismatched[filepath.ToSlash(rel)] = true
		}
	}
	return mismatched
}

// RunGremlins invokes gremlins in env.Dir for one target file subset and returns JSON bytes or noMutants=true.
func RunGremlins(ctx context.Context, env gate.Env, diff DiffResult, build gobuild.Build, integration bool) (data []byte, noMutants bool, err error) {
	return runGremlinsSubset(ctx, env, diff, diff.InBuildFiles, false, build, integration)
}

func runGremlinsSubset(ctx context.Context, env gate.Env, diff DiffResult, keep map[string]map[int]bool, forceExclude bool, build gobuild.Build, integration bool) (data []byte, noMutants bool, err error) {
	prefix, err := env.Tools.Resolve(ctx, "gremlins")
	if err != nil {
		return nil, false, err
	}
	cfgDir, cfgFile, err := writeEmptyConfig()
	if err != nil {
		return nil, false, err
	}
	defer func() {
		if cerr := os.RemoveAll(cfgDir); cerr != nil && err == nil {
			err = cerr
		}
	}()
	return execGremlins(ctx, env, prefix, cfgFile, diff, keep, forceExclude, build, integration)
}

// RunGremlinsBatches partitions in-build files so only mismatched-name packages run --integration.
func RunGremlinsBatches(ctx context.Context, env gate.Env, diff DiffResult, build gobuild.Build) ([][]byte, error) {
	normal, integ, err := PartitionByIntegration(ctx, env.Exec, env.Dir, diff.InBuildFiles)
	if err != nil {
		return nil, err
	}
	forceExclude := len(normal) > 0 && len(integ) > 0
	var reports [][]byte
	for _, batch := range []struct {
		files map[string]map[int]bool
		integ bool
	}{{normal, false}, {integ, true}} {
		if len(batch.files) == 0 {
			continue
		}
		data, noMut, err := runGremlinsSubset(ctx, env, diff, batch.files, forceExclude, build, batch.integ)
		if err != nil {
			return nil, err
		}
		if !noMut {
			reports = append(reports, data)
		}
	}
	return reports, nil
}

func writeEmptyConfig() (string, string, error) {
	dir, err := os.MkdirTemp("", "gotops-gremlins-*")
	if err != nil {
		return "", "", err
	}
	cfgPath := filepath.Join(dir, "empty.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o644); err != nil {
		return "", "", errors.Join(err, os.RemoveAll(dir))
	}
	return dir, cfgPath, nil
}

func execGremlins(ctx context.Context, env gate.Env, prefix []string, cfgFile string, diff DiffResult, keep map[string]map[int]bool, forceExclude bool, build gobuild.Build, integration bool) ([]byte, bool, error) {
	outPath := filepath.Join(env.Dir, ".go-tops", "gremlins.json")
	if err := os.Remove(outPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}
	useDiff, useExclude, err := scopeModes(ctx, env.Exec, env.Dir, diff, keep, forceExclude)
	if err != nil {
		return nil, false, err
	}
	argv := buildGremlinsArgv(env.Dir, prefix, cfgFile, outPath, diff, keep, build, integration, useDiff, useExclude)
	res, err := env.Exec.Run(ctx, gate.Cmd{
		Dir:      env.Dir,
		Argv:     argv,
		Env:      gremlinsEnv(build),
		StripEnv: []string{"GREMLINS_"},
	})
	return readGremlinsResult(res, err, outPath)
}

func scopeModes(ctx context.Context, ex gate.Executor, dir string, diff DiffResult, keep map[string]map[int]bool, forceExclude bool) (useDiff, useExclude bool, err error) {
	if diff.ModPrefix != "." {
		return false, true, nil
	}
	untracked, err := listUntrackedGo(ctx, ex, dir)
	if err != nil {
		return false, false, err
	}
	hasUntracked := false
	for _, rel := range untracked {
		if len(keep[filepath.ToSlash(rel)]) > 0 {
			hasUntracked = true
			break
		}
	}
	// Untracked files are invisible to git diff, so only fall back to whole-file exclusion when keep holds one.
	return !hasUntracked, forceExclude || hasUntracked, nil
}

func buildGremlinsArgv(dir string, prefix []string, cfgFile, outPath string, diff DiffResult, keep map[string]map[int]bool, build gobuild.Build, integration, useDiff, useExclude bool) []string {
	argv := append(slices.Clone(prefix), "--config="+cfgFile, "unleash", "--output="+outPath)
	if len(build.Tags) > 0 {
		argv = append(argv, "--tags="+strings.Join(build.Tags, ","))
	}
	argv = append(argv, "--workers="+strconv.Itoa(boundWorkers(runtime.GOMAXPROCS(0))), "--timeout-coefficient=5")
	argv = appendMutatorFlags(argv)
	if integration {
		argv = append(argv, "--integration")
	}
	return appendScopeFlags(dir, argv, diff, keep, useDiff, useExclude)
}

func boundWorkers(procs int) int {
	if procs < 1 {
		return 1
	}
	if procs > 4 {
		return 4
	}
	return procs
}

func appendMutatorFlags(argv []string) []string {
	for _, m := range ProtocolMutatorsOn {
		argv = append(argv, mutatorFlag(m, true))
	}
	for _, m := range ProtocolMutatorsOff {
		argv = append(argv, mutatorFlag(m, false))
	}
	return argv
}

func mutatorFlag(name string, on bool) string {
	kebab := strings.ToLower(strings.ReplaceAll(name, "_", "-"))
	return "--" + kebab + "=" + strconv.FormatBool(on)
}

func appendScopeFlags(dir string, argv []string, diff DiffResult, keep map[string]map[int]bool, useDiff, useExclude bool) []string {
	if useDiff {
		argv = append(argv, "--diff="+diff.BaseSHA)
	}
	if useExclude {
		if pattern := excludePatternFor(dir, keep); pattern != "" {
			argv = append(argv, "--exclude-files="+pattern)
		}
	}
	return argv
}

func excludePatternFor(dir string, keep map[string]map[int]bool) string {
	idx, err := ScanModule(dir)
	if err != nil {
		return ""
	}
	var quoted []string
	for _, rel := range idx.Files() {
		if len(keep[rel]) == 0 {
			quoted = append(quoted, regexp.QuoteMeta(rel))
		}
	}
	if len(quoted) == 0 {
		return ""
	}
	return "^(" + strings.Join(quoted, "|") + ")$"
}

func gremlinsEnv(build gobuild.Build) []string {
	cgo := "0"
	if build.CGOEnabled {
		cgo = "1"
	}
	env := []string{
		"CGO_ENABLED=" + cgo,
		"GOFLAGS=-count=1",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=diff.context",
		"GIT_CONFIG_VALUE_0=0",
	}
	if build.GOOS != "" {
		env = append(env, "GOOS="+build.GOOS)
	}
	if build.GOARCH != "" {
		env = append(env, "GOARCH="+build.GOARCH)
	}
	return env
}

func readGremlinsResult(res gate.Output, runErr error, outPath string) ([]byte, bool, error) {
	if runErr != nil {
		return nil, false, fmt.Errorf("gremlins execution failed: %w", runErr)
	}
	data, err := os.ReadFile(outPath)
	if errors.Is(err, os.ErrNotExist) {
		return handleMissingJSON(res)
	}
	if err != nil {
		return nil, false, fmt.Errorf("read gremlins output %s: %w", outPath, err)
	}
	if res.ExitCode != 0 {
		return nil, false, fmt.Errorf("gremlins exited %d: %s", res.ExitCode, strings.TrimSpace(string(res.Stderr)))
	}
	return data, false, nil
}

func handleMissingJSON(res gate.Output) ([]byte, bool, error) {
	if res.ExitCode == 0 && strings.Contains(string(res.Stdout), "No results to report.") {
		return nil, true, nil
	}
	return nil, false, fmt.Errorf("gremlins produced no JSON output (exit %d): stdout=%q stderr=%q",
		res.ExitCode, strings.TrimSpace(string(res.Stdout)), strings.TrimSpace(string(res.Stderr)))
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

// Details renders Evaluation into the protocol 0.4.5 mutationDetails JSON map.
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

// Evaluate validates one gremlins output, applies exemptions and ledger matching, and returns the mutation Evaluation.
func Evaluate(cfg gate.MutationConfig, exemptions []report.Exemption, gremlinsJSON []byte, noMutants bool, diff DiffResult, dir string, today time.Time) (Evaluation, error) {
	if noMutants {
		return EvaluateBatches(cfg, exemptions, nil, diff, dir, today)
	}
	return EvaluateBatches(cfg, exemptions, [][]byte{gremlinsJSON}, diff, dir, today)
}

// EvaluateBatches merges one or more gremlins JSON payloads, applies exemptions and ledger matching, and returns Evaluation.
func EvaluateBatches(cfg gate.MutationConfig, exemptions []report.Exemption, reports [][]byte, diff DiffResult, dir string, today time.Time) (Evaluation, error) {
	if cfg.Engine != "" && cfg.Engine != "gremlins" {
		return Evaluation{}, fmt.Errorf("mutation engine %q is not supported in M4 (only gremlins)", cfg.Engine)
	}
	notMeasured, err := countNotMeasured(dir, diff.OutOfBuildFiles)
	if err != nil {
		return Evaluation{}, err
	}
	idx, err := ScanModule(dir)
	if err != nil {
		return Evaluation{}, err
	}
	exRes := exempt.MatchMutation(exemptions, idx.Files(), idx.Funcs(), today)
	ledger, err := loadConfiguredLedger(dir, cfg.Ledger, today)
	if err != nil {
		return Evaluation{}, err
	}
	ev := newEvaluation(diff, notMeasured, exRes.Problems)
	for _, raw := range reports {
		ev, err = applyReport(ev, raw, diff, idx, exRes, ledger, dir)
		if err != nil {
			return Evaluation{}, err
		}
	}
	return ev, nil
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

func applyReport(ev Evaluation, data []byte, diff DiffResult, idx *ModuleIndex, exRes exempt.MutationResult, ledger Ledger, dir string) (Evaluation, error) {
	rep, err := ParseGremlinsOutput(data)
	if err != nil {
		return Evaluation{}, err
	}
	cache := map[string][]byte{}
	for _, gf := range rep.Files {
		if err := evalFile(&ev, gf, diff, idx, exRes, ledger, dir, cache); err != nil {
			return Evaluation{}, err
		}
	}
	sortEvaluation(&ev)
	return ev, nil
}

func evalFile(ev *Evaluation, gf GremlinsFile, diff DiffResult, idx *ModuleIndex, exRes exempt.MutationResult, ledger Ledger, dir string, cache map[string][]byte) error {
	rel := filepath.ToSlash(gf.Filename)
	lines := diff.InBuildFiles[rel]
	if len(lines) == 0 {
		return nil
	}
	src, err := readCachedFile(dir, rel, cache)
	if err != nil {
		return err
	}
	for _, m := range gf.Mutations {
		if !lines[m.Line] || m.Status == "SKIPPED" {
			continue
		}
		if err := recordMutant(ev, rel, m, src, idx, exRes, ledger); err != nil {
			return err
		}
	}
	return nil
}

func readCachedFile(dir, rel string, cache map[string][]byte) ([]byte, error) {
	if src, ok := cache[rel]; ok {
		return src, nil
	}
	src, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		return nil, err
	}
	cache[rel] = src
	return src, nil
}

func recordMutant(ev *Evaluation, rel string, m GremlinsMutation, src []byte, idx *ModuleIndex, exRes exempt.MutationResult, ledger Ledger) error {
	if m.Status == "RUNNABLE" {
		return fmt.Errorf("gremlins left mutant RUNNABLE at %s:%d:%d", rel, m.Line, m.Column)
	}
	ev.Mutants++
	switch m.Status {
	case "KILLED":
		ev.Killed++
		return nil
	case "NOT VIABLE":
		ev.Dropped++
		return nil
	case "TIMED OUT":
		return recordTimedOut(ev, rel, m, src, idx)
	case "LIVED", "NOT COVERED":
		return recordSurvivor(ev, rel, m, src, idx, exRes, ledger)
	default:
		return fmt.Errorf("unexpected mutant status %q at %s:%d:%d", m.Status, rel, m.Line, m.Column)
	}
}

func buildKey(rel string, m GremlinsMutation, src []byte, idx *ModuleIndex) (SurvivorKey, error) {
	hash, err := MutatedLineHash(src, m.Line, m.Column, m.Type)
	if err != nil {
		return SurvivorKey{}, err
	}
	fn := idx.EnclosingFunc(rel, m.Line, m.Column)
	return SurvivorKey{File: rel, Function: fn, Mutator: m.Type, LineHash: hash}, nil
}

func recordTimedOut(ev *Evaluation, rel string, m GremlinsMutation, src []byte, idx *ModuleIndex) error {
	key, err := buildKey(rel, m, src, idx)
	if err != nil {
		return err
	}
	ev.Killed++
	ev.TimedOut++
	ev.TimedOutKeys = append(ev.TimedOutKeys, key.String())
	return nil
}

func recordSurvivor(ev *Evaluation, rel string, m GremlinsMutation, src []byte, idx *ModuleIndex, exRes exempt.MutationResult, ledger Ledger) error {
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
