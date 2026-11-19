package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Hedronite/go-tops/internal/baseline"
	"github.com/Hedronite/go-tops/internal/cli"
	"github.com/Hedronite/go-tops/internal/config"
	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/report"
)

func init() {
	cli.Register(cli.Command{
		Name:    "baseline",
		Summary: "write .go-tops/baseline.json: --init, a plain lowering write, --accept-regression or --rename",
		Run:     runBaseline,
	})
}

const baselineUsage = `usage: gotops baseline --init
       gotops baseline
       gotops baseline --accept-regression --reason TEXT --owner HANDLE
       gotops baseline --rename OLD NEW

The only command that writes .go-tops/baseline.json (GO_TOPS.md §6.7).
  --init                 measure and write the first ledger: the feeding layers run and
                         judge nothing. Needs green tests and a non-empty output from
                         each layer; red thresholds do not stop it. Refused when a
                         ledger exists.
  (no flag)              lower entries and remove paid ones. Never adds or raises one.
  --accept-regression    also raise or add the entries the run measures above the
                         ledger, and lower judged_laws, each recorded under
                         regressions with --reason (three words or more) and --owner.
                         The PR carries the protocol-change label.
  --rename OLD NEW       rewrite one function or package key and never a value. Refused
                         unless OLD has vanished from the code and NEW exists.
Every write is refused under enforce: advisory.
Exit codes:
  0  written, or nothing to change
  1  refused: red tests, an empty feeding output, a ledger exists (--init), nothing
     to accept, a rename the rules refuse, or enforce: advisory
  2  usage error, invalid configuration, or a missing or invalid ledger
  3  environment error
`

// Exit codes of the command (§12.2): a refusal is 1, a usage error or an invalid
// custody is 2, an environment error is 3.
const (
	baselineRefused = 1
	baselineCustody = 2
	baselineEnv     = 3
)

// baselineError ends the command with a message and an exit code.
type baselineError struct {
	code int
	msg  string
}

func (e *baselineError) Error() string { return e.msg }

func bfail(code int, format string, a ...any) *baselineError {
	return &baselineError{code: code, msg: fmt.Sprintf(format, a...)}
}

// Seams. The defaults run the feeding layers and read the source tree; a test
// replaces them, so the command is exercised without a test run or a linter.
var (
	runFeeding = runFeedingLayers
	// scoredFunctions is the set of functions the CRAP layer scored (ruling C2).
	// STAND-IN until internal/crap exports its per-function list: every function
	// found in the source is taken as scored.
	scoredFunctions = func(dir string, cfg gate.Config) (map[string]bool, error) {
		inv, err := baseline.ScanInventory(dir, cfg.Module)
		if err != nil {
			return nil, err
		}
		return inv.Functions(), nil
	}
	scanInventory = func(dir, module string) (baseline.Inventory, error) { return baseline.ScanInventory(dir, module) }
)

type baselineArgs struct {
	init, accept, rename bool
	reason, owner        string
	oldKey, newKey       string
}

func parseBaselineArgs(args []string) (baselineArgs, error) {
	var a baselineArgs
	var kind string
	set := flag.NewFlagSet("baseline", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.BoolVar(&a.init, "init", false, "")
	set.BoolVar(&a.accept, "accept-regression", false, "")
	set.BoolVar(&a.rename, "rename", false, "")
	set.StringVar(&a.reason, "reason", "", "")
	set.StringVar(&a.owner, "owner", "", "")
	set.StringVar(&kind, "kind", "", "")
	if err := set.Parse(args); err != nil {
		return a, err
	}
	modes := 0
	for _, on := range []bool{a.init, a.accept, a.rename} {
		if on {
			modes++
		}
	}
	rest := set.Args()
	switch {
	case kind != "":
		return a, fmt.Errorf("--kind %s: the survivors kind is recorded from a tier 2 run, which lands with mutation at M4", kind)
	case modes > 1:
		return a, errors.New("--init, --accept-regression and --rename are separate modes; give one")
	case a.rename && len(rest) != 2:
		return a, errors.New("--rename takes the old key and the new key")
	case !a.rename && len(rest) > 0:
		return a, fmt.Errorf("unexpected argument %q", rest[0])
	case !a.accept && (a.reason != "" || a.owner != ""):
		return a, errors.New("--reason and --owner belong to --accept-regression")
	case a.accept && len(strings.Fields(a.reason)) < 3:
		return a, errors.New("--accept-regression needs --reason with at least three words saying why the debt grows")
	case a.accept && (strings.TrimSpace(a.owner) == "" || len(strings.Fields(a.owner)) != 1):
		return a, errors.New("--accept-regression needs --owner with one handle, a person or a team")
	}
	if a.rename {
		a.oldKey, a.newKey = rest[0], rest[1]
	}
	return a, nil
}

func runBaseline(env cli.Env, args []string) int {
	a, err := parseBaselineArgs(args)
	if errors.Is(err, flag.ErrHelp) {
		env.Stdout.Printf("%s", baselineUsage)
		return 0
	}
	if err != nil {
		env.Stderr.Printf("gotops baseline: %v\n\n%s", err, baselineUsage)
		return baselineCustody
	}
	if err := baselineMain(context.Background(), env, a); err != nil {
		var be *baselineError
		if !errors.As(err, &be) {
			be = bfail(baselineEnv, "%v", err)
		}
		env.Stderr.Printf("gotops baseline: %s\n", be.msg)
		return be.code
	}
	return 0
}

func baselineMain(ctx context.Context, env cli.Env, a baselineArgs) error {
	dir, err := os.Getwd()
	if err != nil {
		return bfail(baselineEnv, "%v", err)
	}
	cfg, acks, rel, err := loadBaselineConfig(dir)
	if err != nil {
		return err
	}
	if cfg.Enforce == "advisory" {
		return bfail(baselineRefused, "refused under enforce: advisory: the baseline cannot be written (GO_TOPS.md §6.7)")
	}
	path := filepath.Join(dir, filepath.FromSlash(rel))
	old, exists, err := readLedger(path, rel)
	switch {
	case a.init && (exists || trackedAtHead(dir, rel)):
		return bfail(baselineRefused, "%s exists, in the tree or at HEAD: --init writes a first ledger only, and deleting the file is not a reset (GO_TOPS.md §6.7). A later write is `gotops baseline`", rel)
	case !a.init && !exists:
		return bfail(baselineCustody, "%s does not exist: under ratchet a missing baseline is a custody failure. The first ledger is `gotops baseline --init`", rel)
	case err != nil && !a.init:
		return err
	}
	if a.rename {
		return renameKey(env, dir, cfg, path, rel, old, a)
	}
	m, err := measure(ctx, dir, cfg, acks)
	if err != nil {
		return err
	}
	inv, err := scanInventory(dir, cfg.Module)
	if err != nil {
		return bfail(baselineEnv, "read the source tree: %v", err)
	}
	switch {
	case a.init:
		next := baseline.Init(m)
		if err := writeLedger(path, baseline.Marshal(next)); err != nil {
			return err
		}
		env.Stdout.Printf("%s written: %s\nCommit it with go-tops.yaml; `gotops check` and `gotops gate` then read it as the first ledger.\n", rel, describe(next))
		return nil
	case a.accept:
		next, err := baseline.Accept(old, m, a.reason, a.owner, inv)
		if err != nil {
			return refusal(err)
		}
		if err := writeLedger(path, baseline.Marshal(next)); err != nil {
			return err
		}
		env.Stdout.Printf("%s written: %s\n%d regression(s) recorded under owner %s. The PR carries the protocol-change label.\n", rel, describe(next), len(next.Regressions), a.owner)
		return nil
	}
	next := baseline.Lower(old, m, inv)
	before, after := baseline.Marshal(old), baseline.Marshal(next)
	if string(before) == string(after) {
		env.Stdout.Printf("%s unchanged: nothing to lower.\n", rel)
	} else {
		if err := writeLedger(path, after); err != nil {
			return err
		}
		env.Stdout.Printf("%s written: %s\n", rel, describe(next))
	}
	if n := len(baseline.Raised(next, baseline.Init(m))); n > 0 {
		env.Stdout.Printf("%d measured value(s) are above the ledger or absent from it and were left as they are. `gotops baseline --accept-regression --reason TEXT --owner HANDLE` records them.\n", n)
	}
	return nil
}

// refusal turns an error of the baseline package into the command's: a
// refusal is exit 1, anything else is an environment error.
func refusal(err error) error {
	var ref *baseline.RefusalError
	if errors.As(err, &ref) {
		return bfail(baselineRefused, "%s", ref.Msg)
	}
	return bfail(baselineEnv, "%v", err)
}

func renameKey(env cli.Env, dir string, cfg gate.Config, path, rel string, old baseline.File, a baselineArgs) error {
	inv, err := scanInventory(dir, cfg.Module)
	if err != nil {
		return bfail(baselineEnv, "read the source tree: %v", err)
	}
	next, err := baseline.Rename(old, a.oldKey, a.newKey, inv)
	if err != nil {
		return refusal(err)
	}
	if err := writeLedger(path, baseline.Marshal(next)); err != nil {
		return err
	}
	env.Stdout.Printf("%s written: %s renamed to %s; its value is unchanged. Review the diff in the PR.\n", rel, a.oldKey, a.newKey)
	return nil
}

// measure runs the feeding layers, which judge nothing here, and reads what
// they measured. A run that measured nothing it can record is a refusal.
func measure(ctx context.Context, dir string, cfg gate.Config, acks string) (baseline.Measure, error) {
	rep, err := runFeeding(ctx, dir, cfg)
	if err != nil {
		return baseline.Measure{}, err
	}
	scored, err := scoredFunctions(dir, cfg)
	if err != nil {
		return baseline.Measure{}, bfail(baselineEnv, "read the source tree: %v", err)
	}
	m, err := baseline.Collect(baseline.Feeds{Report: rep, Config: cfg, Dir: dir, Acks: acks, Scored: scored})
	if err != nil {
		return baseline.Measure{}, refusal(err)
	}
	return m, nil
}

// runFeedingLayers runs hygiene, tests, coverage and CRAP as `gate` would and
// returns the report. Nothing is judged: the caller reads the figures, never the
// outcome of the run.
func runFeedingLayers(ctx context.Context, dir string, cfg gate.Config) (report.Report, error) {
	var layers []gate.Layer
	for _, l := range gate.Layers() {
		switch l.ID() {
		case "hygiene", "tests", "coverage", "crap":
			layers = append(layers, l)
		}
	}
	res, err := gate.Execute(ctx, cfg, gate.Options{MaxTier: 1}, gate.Deps{Dir: dir, Layers: layers, ToolVersion: packageVersion})
	if err != nil {
		var ee *gate.ExitError
		var ce *gate.ConfigError
		var le *gate.LockError
		switch {
		case errors.As(err, &ee):
			return report.Report{}, bfail(ee.Code, "%v", err)
		case errors.As(err, &ce), errors.As(err, &le):
			return report.Report{}, bfail(baselineCustody, "%v", err)
		}
		return report.Report{}, bfail(baselineEnv, "%v", err)
	}
	return res.Report, nil
}

// loadBaselineConfig is the loading of `gate` (check, then the class defaults)
// minus the rules about the ledger file itself: whether it exists and is clean
// against HEAD is what this command is here to settle. A ledger that does not
// parse or is not valid stays an issue. It returns the gate's configuration,
// the module-relative path of the acks file and of the ledger.
func loadBaselineConfig(dir string) (gate.Config, string, string, error) {
	res, err := config.Check(dir, config.Options{})
	if err != nil {
		return gate.Config{}, "", "", bfail(baselineEnv, "%v", err)
	}
	var lines []string
	for _, is := range res.Issues {
		if is.Rule == "baseline" {
			continue
		}
		lines = append(lines, is.String())
	}
	if len(lines) > 0 {
		return gate.Config{}, "", "", bfail(baselineCustody, "the configuration is invalid:\n%s", strings.Join(lines, "\n"))
	}
	eff, err := config.Effective(res.Config)
	if err != nil {
		return gate.Config{}, "", "", bfail(baselineEnv, "%v", err)
	}
	cfg, err := toGateConfig(eff)
	if err != nil {
		return gate.Config{}, "", "", bfail(baselineEnv, "%v", err)
	}
	rel := ".go-tops/baseline.json"
	if res.Config.Baseline != "" {
		rel = res.Config.Baseline
	}
	return cfg, eff.Laws.AckLedger, rel, nil
}

// trackedAtHead says whether git has the ledger at HEAD. A ledger deleted from
// the working tree is still the repo's ledger, so --init does not stand in for
// it. A directory git cannot read has none.
func trackedAtHead(dir, rel string) bool {
	return exec.Command("git", "-C", dir, "cat-file", "-e", "HEAD:"+rel).Run() == nil
}

// readLedger reads the ledger. A file that is there but is not a ledger is a
// custody failure: it is never overwritten from a guess.
func readLedger(path, rel string) (baseline.File, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return baseline.File{}, false, nil
	}
	if err != nil {
		return baseline.File{}, false, bfail(baselineEnv, "%v", err)
	}
	f, err := baseline.Parse(data)
	if err != nil {
		return baseline.File{}, true, bfail(baselineCustody, "%s is not a valid ledger: %v", rel, err)
	}
	return f, true, nil
}

// writeLedger replaces the file in one step, so a crash never leaves half a
// ledger.
func writeLedger(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return bfail(baselineEnv, "%v", err)
	}
	tmp, err := os.CreateTemp(dir, ".baseline-*.tmp")
	if err != nil {
		return bfail(baselineEnv, "%v", err)
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(name, 0o644)
	}
	if werr == nil {
		werr = os.Rename(name, path)
	}
	if werr != nil {
		if rerr := os.Remove(name); rerr != nil {
			werr = errors.Join(werr, rerr)
		}
		return bfail(baselineEnv, "%v", werr)
	}
	return nil
}

// describe is one line saying what a ledger holds.
func describe(f baseline.File) string {
	n := func(c baseline.Counts) int {
		total := 0
		for _, files := range c {
			total += len(files)
		}
		return total
	}
	l := f.Ledger
	return fmt.Sprintf("functions %d, packages %d, findings %d, nolint %d, acks %d, exemptions %d, judged_laws %d",
		len(l.Functions), len(l.Packages), n(l.Findings), n(l.Nolint), n(l.Ack), l.Exemptions, f.JudgedLaws)
}
