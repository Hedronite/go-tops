package main

import (
	"errors"
	"flag"
	"io"
	"io/fs"
	"path/filepath"

	"github.com/Hedronite/go-tops/internal/cli"
	"github.com/Hedronite/go-tops/internal/report"
)

func init() {
	cli.Register(cli.Command{
		Name:    "report",
		Summary: "render the last report: --format text or json",
		Run:     runReport,
	})
}

const reportUsage = `Usage: gotops report [--format text|json] [--path <file>]

Renders the report of the last gate run (default .go-tops/report.json).
Formats: text (default) and json. junit and sarif arrive in M7.
`

func runReport(env cli.Env, args []string) int {
	flags := flag.NewFlagSet("report", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	format := flags.String("format", "text", "")
	path := flags.String("path", "", "")
	if err := flags.Parse(args); err != nil {
		return usageError(env, "report", reportUsage, err)
	}
	if flags.NArg() > 0 {
		return usageError(env, "report", reportUsage, errors.New("takes no arguments, got "+flags.Arg(0)))
	}
	f, err := report.ParseFormat(*format)
	if err != nil {
		env.Stderr.Printf("gotops report: %v\n", err)
		return cli.ExitUsage
	}
	return renderReport(env, reportPath(*path), f)
}

func reportPath(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return filepath.FromSlash(report.DefaultPath)
}

func renderReport(env cli.Env, path string, f report.Format) int {
	r, err := report.Load(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		env.Stderr.Printf("gotops report: no report at %s: run gotops gate first\n", path)
		return cli.ExitUsage
	case err != nil:
		env.Stderr.Printf("gotops report: %v\n", err)
		return cli.ExitUsage
	}
	out, err := report.Render(r, f)
	if err != nil {
		env.Stderr.Printf("gotops report: %v\n", err)
		return cli.ExitEnvironment
	}
	env.Stdout.Printf("%s", out)
	return 0
}

// usageError prints a flag or argument problem with the command's usage and
// returns the usage exit code. A --help is not an error.
func usageError(env cli.Env, name, usage string, err error) int {
	if errors.Is(err, flag.ErrHelp) {
		env.Stdout.Printf("%s", usage)
		return 0
	}
	env.Stderr.Printf("gotops %s: %v\n\n%s", name, err, usage)
	return cli.ExitUsage
}
