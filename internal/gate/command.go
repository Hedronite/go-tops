package gate

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Hedronite/go-tops/internal/report"
)

// CommandDeps are what `gotops gate` needs from its surroundings.
type CommandDeps struct {
	Dir string
	// LoadConfig loads, validates and merges go-tops.yaml. An error is a
	// configuration failure: exit 2.
	LoadConfig  func(dir string) (Config, error)
	Layers      []Layer // default: Layers()
	Exec        Executor
	Tools       Resolver
	CI          bool
	Now         func() time.Time
	ToolVersion string
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
	if d.Dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			stderr.Printf("gotops gate: %v\n", &EnvError{Err: err})
			return ExitEnv
		}
		d.Dir = wd
	}
	var o Options
	var asJSON bool
	fs := flag.NewFlagSet("gate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.IntVar(&o.MaxTier, "max-tier", 1, "")
	fs.BoolVar(&o.FailFast, "fail-fast", false, "")
	fs.BoolVar(&o.DryRun, "dry-run", false, "")
	fs.StringVar(&o.Base, "base", "", "")
	fs.BoolVar(&asJSON, "json", false, "")
	fs.StringVar(&o.Only, "only", "", "")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			stdout.Printf("%s", Usage)
			return ExitOK
		}
		stderr.Printf("gotops gate: %v\n%s", err, Usage)
		return ExitConfig
	}
	if fs.NArg() > 0 {
		stderr.Printf("gotops gate: unexpected argument %q\n%s", fs.Arg(0), Usage)
		return ExitConfig
	}
	if o.MaxTier < 0 || o.MaxTier > 3 {
		stderr.Printf("gotops gate: --max-tier %d: tiers are 0 to 3\n", o.MaxTier)
		return ExitConfig
	}

	cfg, err := d.LoadConfig(d.Dir)
	if err != nil {
		var ee *EnvError
		if errors.As(err, &ee) {
			stderr.Printf("gotops gate: %v\n", err)
			return ExitEnv
		}
		stderr.Printf("gotops gate: %v\n", &ConfigError{Err: err})
		return ExitConfig
	}
	res, err := Execute(ctx, cfg, o, Deps{
		Dir: d.Dir, Layers: d.Layers, Exec: d.Exec, Tools: d.Tools, CI: d.CI, Now: d.Now, ToolVersion: d.ToolVersion,
	})
	if err != nil {
		stderr.Printf("gotops gate: %v\n", err)
		return exitCodeFor(err)
	}
	if res.DryRun {
		stdout.Printf("%s", res.Text)
		return ExitOK
	}
	path := cfg.reportPath()
	if !filepath.IsAbs(path) {
		path = filepath.Join(d.Dir, path)
	}
	if err := report.Write(path, res.Report); err != nil {
		stderr.Printf("gotops gate: %v\n", &EnvError{Err: err})
		return ExitEnv
	}
	if asJSON {
		b, err := report.Marshal(res.Report)
		if err != nil {
			stderr.Printf("gotops gate: %v\n", err)
			return ExitEnv
		}
		stdout.Printf("%s", b)
	} else {
		stdout.Printf("%s", res.Text)
	}
	return res.ExitCode
}
