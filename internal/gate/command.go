package gate

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Hedronite/go-tops/internal/lawpass"
	"github.com/Hedronite/go-tops/internal/laws"
	"github.com/Hedronite/go-tops/internal/report"
)

// CommandDeps are what `gotops gate` needs from its surroundings.
type CommandDeps struct {
	ScanAcks func(dir, ackPath string) (laws.FileCounts, error)
	Dir      string
	// LoadConfig loads, validates and merges go-tops.yaml. An error is a
	// configuration failure: exit 2.
	LoadConfig  func(ctx context.Context, dir string) (Config, error)
	Layers      []Layer // default: Layers()
	Exec        Executor
	Tools       Resolver
	CI          bool
	Now         func() time.Time
	ToolVersion string
	LawPass     func(context.Context, *Env) (lawpass.LintResult, error)
	LawsRun     func(context.Context, string, string) (laws.RunnerResult, error)
}

// Usage is the text of `gotops gate --help`.
const Usage = `usage: gotops gate [--max-tier N] [--fail-fast] [--dry-run] [--base REF] [--json] [--only LAYER]

Runs every enabled layer at or below --max-tier (default 1), in order, each
judged, laws last. Writes .go-tops/report.json. Exit codes:
  0  every layer in range is pass, skip or vacuous and no law fails (or the
     run is partial, or enforce is advisory)
  1  a layer failed, did not run, was not reached or lacks a tool; a law is
     refused; a required law in range is not evaluated
  2  configuration or tool lock invalid, or a usage error
  3  environment error (unreadable repository, no C compiler)
  4  refused when CI=true: --dry-run, or a range that would end partial
`

// Printer is where the command writes. cli.Out is one: it remembers its first
// write error, which the registry turns into an environment error.
type Printer interface {
	Printf(format string, a ...any)
}

// Main runs `gotops gate` and returns its exit code.
func Main(ctx context.Context, args []string, d CommandDeps, stdout, stderr Printer) int {
	dir, err := workingDir(d.Dir)
	if err != nil {
		stderr.Printf("gotops gate: %v\n", &EnvError{Err: err})
		return ExitEnv
	}
	d.Dir = dir
	o, asJSON, code, ok := parseOptions(args, stdout, stderr)
	if !ok {
		return code
	}
	cfg, code, ok := loadConfig(ctx, d, stderr)
	if !ok {
		return code
	}
	res, err := Execute(ctx, cfg, o, Deps{
		Dir: d.Dir, Layers: d.Layers, Exec: d.Exec, Tools: d.Tools, CI: d.CI, Now: d.Now,
		ToolVersion: d.ToolVersion, LawPass: d.LawPass, LawsRun: d.LawsRun, ScanAcks: d.ScanAcks,
	})
	if err != nil {
		stderr.Printf("gotops gate: %v\n", err)
		return exitCodeFor(err)
	}
	return present(res, cfg, d.Dir, asJSON, stdout, stderr)
}

// workingDir is dir, or the current directory when none was given.
func workingDir(dir string) (string, error) {
	if dir != "" {
		return dir, nil
	}
	return os.Getwd()
}

// parseOptions reads the flags of `gotops gate`. ok is false when the command is
// over: --help printed, or a usage error printed, with the exit code to return.
func parseOptions(args []string, stdout, stderr Printer) (o Options, asJSON bool, code int, ok bool) {
	fs := flag.NewFlagSet("gate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.IntVar(&o.MaxTier, "max-tier", 1, "")
	fs.BoolVar(&o.FailFast, "fail-fast", false, "")
	fs.BoolVar(&o.DryRun, "dry-run", false, "")
	fs.StringVar(&o.Base, "base", "", "")
	fs.BoolVar(&asJSON, "json", false, "")
	fs.StringVar(&o.Only, "only", "", "")
	err := fs.Parse(args)
	switch {
	case errors.Is(err, flag.ErrHelp):
		stdout.Printf("%s", Usage)
		return o, false, ExitOK, false
	case err != nil:
		stderr.Printf("gotops gate: %v\n%s", err, Usage)
		return o, false, ExitConfig, false
	case fs.NArg() > 0:
		stderr.Printf("gotops gate: unexpected argument %q\n%s", fs.Arg(0), Usage)
		return o, false, ExitConfig, false
	case o.MaxTier < 0 || o.MaxTier > 3:
		stderr.Printf("gotops gate: --max-tier %d: tiers are 0 to 3\n", o.MaxTier)
		return o, false, ExitConfig, false
	}
	return o, asJSON, ExitOK, true
}

// loadConfig loads the configuration the run judges. ok is false when it cannot
// be loaded, with the exit code: an environment error is 3, an invalid
// configuration is 2.
func loadConfig(ctx context.Context, d CommandDeps, stderr Printer) (cfg Config, code int, ok bool) {
	cfg, err := d.LoadConfig(ctx, d.Dir)
	if err == nil {
		return cfg, ExitOK, true
	}
	var ee *EnvError
	if errors.As(err, &ee) {
		stderr.Printf("gotops gate: %v\n", err)
		return cfg, ExitEnv, false
	}
	stderr.Printf("gotops gate: %v\n", &ConfigError{Err: err})
	return cfg, ExitConfig, false
}

// present prints a dry run's plan, or writes the report and prints it as text or
// JSON, and returns the run's exit code.
func present(res *Run, cfg Config, dir string, asJSON bool, stdout, stderr Printer) int {
	if res.DryRun {
		stdout.Printf("%s", res.Text)
		return ExitOK
	}
	path := cfg.reportPath()
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	if err := report.Write(path, res.Report); err != nil {
		stderr.Printf("gotops gate: %v\n", &EnvError{Err: err})
		return ExitEnv
	}
	if !asJSON {
		stdout.Printf("%s", res.Text)
		return res.ExitCode
	}
	b, err := report.Marshal(res.Report)
	if err != nil {
		stderr.Printf("gotops gate: %v\n", err)
		return ExitEnv
	}
	stdout.Printf("%s", b)
	return res.ExitCode
}
