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
	"slices"
	"strings"

	"github.com/Hedronite/go-tops/internal/baseline"
	"github.com/Hedronite/go-tops/internal/cli"
	"github.com/Hedronite/go-tops/internal/config"
	"github.com/Hedronite/go-tops/internal/crap"
	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/gobuild"
	"github.com/Hedronite/go-tops/internal/lawpass"
	"github.com/Hedronite/go-tops/internal/layers/tests"
	"github.com/Hedronite/go-tops/internal/report"
)

// testLawPass is set by baseline tests to stub the law lint pass.
var testLawPass func(context.Context, *gate.Env) (lawpass.LintResult, error)

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
       gotops baseline --status [--base REF]

The only command that writes .go-tops/baseline.json (GO_TOPS.md §6.7).
  --init                 measure and write the first ledger: the feeding layers run and
                         judge nothing. Needs green tests and a non-empty output from
                         each layer; red thresholds do not stop it. Refused when a
                         ledger exists.
  (no flag)              lower entries and remove paid ones. Never adds or raises one.
                         A regressions record follows its entry down: its "to" is lowered
                         with the entry, and the record goes when the entry is back at
                         its earlier value.
  --accept-regression    also raise or add the entries the run measures above the
                         ledger, and lower judged_laws, each recorded under
                         regressions with --reason (three words or more) and --owner.
                         The PR carries the protocol-change label.
  --rename OLD NEW       rewrite one function or package key and never a value. Refused
                         unless OLD has vanished from the code and NEW exists.
  --status               write nothing: print one JSON line saying whether a first ledger is
                         due, which is when neither the diff base (--base, default
                         origin/main) nor the tree has one. Exit 0 when it is due, 1 when
                         it is not (present, deleted, or not-ratchet: only enforce: ratchet reads a
                         ledger), 2 when the base cannot be
                         read (a shallow clone, an unknown ref, a copy that does not
                         parse) or the configuration is invalid.
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
	// runBuild is the build the run measures (ruling C3).
	runBuild = currentBuild
	// scanInventory is the existence set of `--rename`: every function identity
	// and package in any production .go file, read by internal/crap without a
	// test run or a build.
	scanInventory = func(dir string) (baseline.Inventory, error) {
		e, err := crap.Exist(dir)
		if err != nil {
			return nil, err
		}
		return baseline.KeySets{Functions: e.Keys, Packages: e.Packages}, nil
	}
)

type baselineArgs struct {
	init, accept, rename bool
	status               bool
	reason, owner, base  string
	oldKey, newKey       string
}

// newArgSet registers the flags of the command on a.
func newArgSet(a *baselineArgs, kind *string) *flag.FlagSet {
	set := flag.NewFlagSet("baseline", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.BoolVar(&a.init, "init", false, "")
	set.BoolVar(&a.accept, "accept-regression", false, "")
	set.BoolVar(&a.rename, "rename", false, "")
	set.BoolVar(&a.status, "status", false, "")
	set.StringVar(&a.base, "base", "", "")
	set.StringVar(&a.reason, "reason", "", "")
	set.StringVar(&a.owner, "owner", "", "")
	set.StringVar(kind, "kind", "", "")
	return set
}

// argRule says what is wrong with a set of arguments: nil when nothing is.
type argRule func(a baselineArgs, kind string, rest []string) error

// argRules are the rules of the command line, in the order they are checked.
var argRules = []argRule{ruleKind, ruleOneMode, ruleRenameKeys, ruleNoStrays, ruleAcceptFlagsOnly, ruleBaseOnly, ruleAcceptFlags}

func ruleKind(_ baselineArgs, kind string, _ []string) error {
	if kind != "" {
		return fmt.Errorf("--kind %s: the survivors kind is recorded from a tier 2 run, which lands at M6", kind)
	}
	return nil
}

func ruleOneMode(a baselineArgs, _ string, _ []string) error {
	modes := 0
	for _, on := range []bool{a.init, a.accept, a.rename, a.status} {
		if on {
			modes++
		}
	}
	if modes > 1 {
		return errors.New("--init, --accept-regression, --rename and --status are separate modes; give one")
	}
	return nil
}

func ruleRenameKeys(a baselineArgs, _ string, rest []string) error {
	if a.rename && len(rest) != 2 {
		return errors.New("--rename takes the old key and the new key")
	}
	return nil
}

func ruleNoStrays(a baselineArgs, _ string, rest []string) error {
	if !a.rename && len(rest) > 0 {
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	return nil
}

func ruleAcceptFlagsOnly(a baselineArgs, _ string, _ []string) error {
	if !a.accept && (a.reason != "" || a.owner != "") {
		return errors.New("--reason and --owner belong to --accept-regression")
	}
	return nil
}

func ruleBaseOnly(a baselineArgs, _ string, _ []string) error {
	if !a.status && a.base != "" {
		return errors.New("--base belongs to --status")
	}
	return nil
}

// ruleAcceptFlags is the ledger's own rule for a reason and an owner, so the
// command and the write cannot disagree.
func ruleAcceptFlags(a baselineArgs, _ string, _ []string) error {
	if a.accept {
		return baseline.CheckAcceptArgs(a.reason, a.owner)
	}
	return nil
}

func parseBaselineArgs(args []string) (baselineArgs, error) {
	var a baselineArgs
	var kind string
	set := newArgSet(&a, &kind)
	if err := set.Parse(args); err != nil {
		return a, err
	}
	rest := set.Args()
	for _, rule := range argRules {
		if err := rule(a, kind, rest); err != nil {
			return a, err
		}
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
	ctx, stop := signalContext()
	defer stop()
	if a.status {
		return runStatus(ctx, env, a)
	}
	if err := baselineMain(ctx, env, a); err != nil {
		var be *baselineError
		if !errors.As(err, &be) {
			be = bfail(baselineEnv, "%v", err)
		}
		env.Stderr.Printf("gotops baseline: %s\n", be.msg)
		return be.code
	}
	return 0
}

// baselineRun is one invocation that has found its configuration and its
// ledger and is on the ledger leg.
type baselineRun struct {
	env            cli.Env
	a              baselineArgs
	dir, rel, path string
	acks           string
	cfg            gate.Config
	old            baseline.File
	build          baseline.Build
}

func baselineMain(ctx context.Context, env cli.Env, a baselineArgs) error {
	r, err := openBaseline(ctx, env, a)
	if err != nil {
		return err
	}
	if a.rename {
		return r.renameKey()
	}
	m, err := measure(ctx, r.dir, r.cfg, r.acks, r.build)
	if err != nil {
		return err
	}
	switch {
	case a.init:
		return r.initLedger(m)
	case a.accept:
		return r.acceptRegressions(m)
	}
	return r.lower(m)
}

// openBaseline loads the configuration, refuses what advisory refuses, finds the
// ledger the mode needs, and checks that this run is on its leg.
func openBaseline(ctx context.Context, env cli.Env, a baselineArgs) (*baselineRun, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, bfail(baselineEnv, "%v", err)
	}
	cfg, acks, rel, err := loadBaselineConfig(dir)
	if err != nil {
		return nil, err
	}
	if cfg.Enforce == "advisory" {
		return nil, bfail(baselineRefused, "refused under enforce: advisory: the baseline cannot be written (GO_TOPS.md §6.7)")
	}
	r := &baselineRun{env: env, a: a, dir: dir, rel: rel, path: filepath.Join(dir, filepath.FromSlash(rel)), acks: acks, cfg: cfg}
	if err := r.findLedger(); err != nil {
		return nil, err
	}
	if !a.init {
		if err := r.requireClean(); err != nil {
			return nil, err
		}
	}
	if err := r.checkLeg(ctx); err != nil {
		return nil, err
	}
	return r, nil
}

// findLedger reads the ledger a plain write, an accept or a rename changes, and
// refuses --init when one exists: in the tree or at HEAD, because deleting the
// file is not a reset.
func (r *baselineRun) findLedger() error {
	old, exists, err := readLedger(r.path, r.rel)
	switch {
	case r.a.init && (exists || trackedAtHead(r.dir, r.rel)):
		return bfail(baselineRefused, "%s exists, in the tree or at HEAD: --init writes a first ledger only, and deleting the file is not a reset (GO_TOPS.md §6.7). A later write is `gotops baseline`", r.rel)
	case !r.a.init && !exists:
		return bfail(baselineCustody, "%s does not exist: under ratchet a missing baseline is a custody failure. The first ledger is `gotops baseline --init`", r.rel)
	case err != nil && !r.a.init:
		return err
	}
	r.old = old
	return nil
}

// requireClean refuses a write when the ledger in the working tree is not the
// copy at HEAD, whatever the enforcement mode: a hand edit would otherwise be
// lowered back to a value above the committed one, with no record.
func (r *baselineRun) requireClean() error {
	why := ledgerProblem(r.dir, r.rel)
	if why == "" {
		return nil
	}
	return bfail(baselineCustody, "%s %s: a write changes a ledger only from the copy at HEAD, so a hand edit is never lowered or raised silently. Commit or revert it first (GO_TOPS.md §6.7)", r.rel, why)
}

// ledgerProblem says why the ledger is not the copy at HEAD, or "" when it is
// tracked and unchanged, or when the directory is not a git repository (check
// settles that under ratchet). It is the rule `check` applies to the baseline.
func ledgerProblem(dir, rel string) string {
	if gitExit(dir, "rev-parse", "--is-inside-work-tree") != 0 {
		return ""
	}
	for _, s := range []struct {
		problem string
		args    []string
	}{
		{"cannot be clean: the repository has no commit", []string{"rev-parse", "--verify", "--quiet", "HEAD"}},
		{"is not tracked by git", []string{"ls-files", "--error-unmatch", "--", rel}},
		{"differs from HEAD", []string{"diff", "--quiet", "HEAD", "--", rel}},
	} {
		if gitExit(dir, s.args...) != 0 {
			return s.problem
		}
	}
	return ""
}

// gitExit runs git in dir and returns its exit code, or -1 when it did not run.
func gitExit(dir string, args ...string) int {
	err := exec.Command("git", append([]string{"-C", dir}, args...)...).Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		return exit.ExitCode()
	}
	return -1
}

// checkLeg reads this run's build and refuses a write from any run but the ledger
// leg: the one whose build the ledger records (ruling C3). --init records it.
func (r *baselineRun) checkLeg(ctx context.Context) error {
	build, err := runBuild(ctx, r.dir)
	if err != nil {
		return bfail(baselineEnv, "read the build: %v", err)
	}
	r.build = build
	if !r.a.init && !build.Equal(r.old.Build) {
		return bfail(baselineRefused, "this run's build (%s) is not the ledger build (%s): the baseline is written only on the ledger leg (GO_TOPS.md §6.7)", baseline.DescribeBuild(build), baseline.DescribeBuild(r.old.Build))
	}
	return nil
}

func (r *baselineRun) initLedger(m baseline.Measure) error {
	next := baseline.Init(m)
	if err := writeLedger(r.path, baseline.Marshal(next)); err != nil {
		return err
	}
	r.env.Stdout.Printf("%s written: %s\nCommit it with go-tops.yaml; `gotops check` and `gotops gate` then read it as the first ledger.\n", r.rel, describe(next))
	return nil
}

func (r *baselineRun) acceptRegressions(m baseline.Measure) error {
	next, err := baseline.Accept(r.old, m, r.a.reason, r.a.owner)
	if err != nil {
		return refusal(err)
	}
	if err := writeLedger(r.path, baseline.Marshal(next)); err != nil {
		return err
	}
	r.env.Stdout.Printf("%s written: %s\n%d regression(s) recorded by this write under owner %s (%d record(s) in the file). The PR carries the protocol-change label.\n",
		r.rel, describe(next), len(baseline.Raised(r.old, next)), r.a.owner, len(next.Regressions))
	return nil
}

// lower is the plain write: it lowers and removes, and says what it left alone.
func (r *baselineRun) lower(m baseline.Measure) error {
	next := baseline.Lower(r.old, m)
	if err := r.writeIfChanged(next); err != nil {
		return err
	}
	if n := len(baseline.NeedsAccept(baseline.Raised(next, baseline.Init(m)))); n > 0 {
		r.env.Stdout.Printf("%d measured value(s) are above the ledger or absent from it and were left as they are. `gotops baseline --accept-regression --reason TEXT --owner HANDLE` records them.\n", n)
	}
	return nil
}

func (r *baselineRun) writeIfChanged(next baseline.File) error {
	after := baseline.Marshal(next)
	if string(baseline.Marshal(r.old)) == string(after) {
		r.env.Stdout.Printf("%s unchanged: nothing to lower.\n", r.rel)
		return nil
	}
	if err := writeLedger(r.path, after); err != nil {
		return err
	}
	r.env.Stdout.Printf("%s written: %s\n", r.rel, describe(next))
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

func (r *baselineRun) renameKey() error {
	inv, err := scanInventory(r.dir)
	if err != nil {
		return bfail(baselineEnv, "read the source tree: %v", err)
	}
	next, err := baseline.Rename(r.old, r.a.oldKey, r.a.newKey, inv)
	if err != nil {
		return refusal(err)
	}
	if err := writeLedger(r.path, baseline.Marshal(next)); err != nil {
		return err
	}
	r.env.Stdout.Printf("%s written: %s renamed to %s; its value is unchanged. Review the diff in the PR.\n", r.rel, r.a.oldKey, r.a.newKey)
	return nil
}

// measure runs the feeding layers, which judge nothing here, and reads what
// they measured. A run that measured nothing it can record is a refusal.
func measure(ctx context.Context, dir string, cfg gate.Config, acks string, build baseline.Build) (baseline.Measure, error) {
	rep, err := runFeeding(ctx, dir, cfg)
	if err != nil {
		return baseline.Measure{}, err
	}
	m, err := baseline.Collect(baseline.Feeds{Report: rep, Config: cfg, Dir: dir, Acks: acks, Build: build})
	if err != nil {
		return baseline.Measure{}, refusal(err)
	}
	return m, nil
}

// currentBuild is the build of the run: the tests layer's build with the GOOS and
// GOARCH the go command resolves (gobuild.Resolve), because a ledger that left
// them empty would let every platform write it.
func currentBuild(_ context.Context, dir string) (baseline.Build, error) {
	b, err := gobuild.Resolve(dir, tests.Build)
	if err != nil {
		return baseline.Build{}, err
	}
	b.Tags = slices.Clone(b.Tags)
	return b, nil
}

// runFeedingLayers runs hygiene, tests, coverage and CRAP as `gate` would and
// returns the report. Nothing is judged: the caller reads the figures, never the
// outcome of the run.
func runFeedingLayers(ctx context.Context, dir string, cfg gate.Config) (report.Report, error) {
	return runLayers(ctx, dir, cfg, feedingOnly(gate.Layers()))
}

// feedingOnly keeps the layers that feed the ledger (§6.7). The CRAP layer
// loses its need of a passing coverage layer: a package under the floor makes
// coverage fail, red thresholds do not stop the ledger, and CRAP reads the same
// profile either way.
func feedingOnly(all []gate.Layer) []gate.Layer {
	var out []gate.Layer
	for _, l := range all {
		switch l.ID() {
		case "hygiene", "tests", "coverage":
			out = append(out, l)
		case crapLayerID:
			out = append(out, floorFree{l})
		}
	}
	return out
}

// crapLayerID is the CRAP layer's name in the runner.
const crapLayerID = "crap"

// floorFree is a layer that does not wait for coverage to pass.
type floorFree struct{ gate.Layer }

// Needs is the layer's own, less the coverage layer.
func (l floorFree) Needs() []string {
	return slices.DeleteFunc(slices.Clone(l.Layer.Needs()), func(n string) bool { return n == tests.CoverageID })
}

// runLayers runs layers through the gate's runner and returns its report. A run
// that cannot start is the exit code the gate gives it.
func runLayers(ctx context.Context, dir string, cfg gate.Config, layers []gate.Layer) (report.Report, error) {
	// The feeding layers read figures and judge nothing, so they run under hard:
	// the ratchet judgment, once it is wired into the gate, never runs for a write.
	cfg.Enforce = "hard"
	deps := gate.Deps{Dir: dir, Layers: layers, ToolVersion: toolVersion()}
	if testLawPass != nil {
		deps.LawPass = testLawPass
	}
	res, err := gate.Execute(ctx, cfg, gate.Options{MaxTier: 1}, deps)
	if err != nil {
		return report.Report{}, startError(err)
	}
	return res.Report, nil
}

// startError is the exit code of a run that did not start: the code the runner
// gave it (2, 3 or 4), 2 for an invalid configuration or a lock mismatch, and 3
// for anything else.
func startError(err error) *baselineError {
	var ee *gate.ExitError
	var ce *gate.ConfigError
	var le *gate.LockError
	switch {
	case errors.As(err, &ee):
		return bfail(ee.Code, "%v", err)
	case errors.As(err, &ce), errors.As(err, &le):
		return bfail(baselineCustody, "%v", err)
	}
	return bfail(baselineEnv, "%v", err)
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

// trackedAtHead says whether git has the ledger at HEAD. The path is read from
// the module directory ("./"), not from the root of the repository, which the
// module need not be. A ledger deleted from
// the working tree is still the repo's ledger, so --init does not stand in for
// it. A directory git cannot read has none.
func trackedAtHead(dir, rel string) bool {
	return exec.Command("git", "-C", dir, "cat-file", "-e", "HEAD:./"+rel).Run() == nil
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

// countSummary returns the total count across all buckets and the number of
// distinct files in c, formatted as "0" or "<sum> in <files> file(s)".
func countSummary(c baseline.Counts) string {
	sum := 0
	files := map[string]bool{}
	for _, byFile := range c {
		for file, k := range byFile {
			sum += k
			files[file] = true
		}
	}
	if len(files) == 0 {
		return "0"
	}
	unit := "files"
	if len(files) == 1 {
		unit = "file"
	}
	return fmt.Sprintf("%d in %d %s", sum, len(files), unit)
}

// describe is one line saying what a ledger holds.
func describe(f baseline.File) string {
	l := f.Ledger
	return fmt.Sprintf("functions %d, packages %d, findings %s, nolint %s, acks %s, exemptions %d, judged_laws %d",
		len(l.Functions), len(l.Packages), countSummary(l.Findings), countSummary(l.Nolint), countSummary(l.Ack), l.Exemptions, f.JudgedLaws)
}
