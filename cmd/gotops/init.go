package main

import (
	"errors"
	"flag"
	"io"
	"os"
	"strings"

	gotops "github.com/Hedronite/go-tops"
	"github.com/Hedronite/go-tops/internal/cli"
	"github.com/Hedronite/go-tops/internal/initcmd"
)

func init() {
	cli.Register(cli.Command{
		Name:    "init",
		Summary: "write the Go-TOPS drop-in files into this module",
		Run:     runInit,
	})
}

const initUsage = `Usage: gotops init --class <class> [--force] [--offline] [--owner <handle>]

Writes go-tops.yaml, go-tops.lock, tools/go.mod, .golangci.yml, the CI workflow,
a CODEOWNERS fragment, justfile and GO-TOPS.md, and the gotops block of AGENTS.md,
into the module in the current directory. It runs no gate and writes no baseline.

Flags:
  --class    binary-cli, binary-service or library-core (required)
  --force    overwrite files that already exist
  --offline  do not run go mod tidy for tools/go.sum
  --owner    the CODEOWNERS owner (default: the owner of a github.com module path)
`

func runInit(env cli.Env, args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	class := fs.String("class", "", "")
	force := fs.Bool("force", false, "")
	offline := fs.Bool("offline", false, "")
	owner := fs.String("owner", "", "")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			env.Stdout.Printf("%s", initUsage)
			return 0
		}
		env.Stderr.Printf("gotops init: %v\n\n%s", err, initUsage)
		return cli.ExitUsage
	}
	if fs.NArg() > 0 || *class == "" {
		env.Stderr.Printf("gotops init: --class is required and init takes no arguments, got %q\n\n%s", fs.Args(), initUsage)
		return cli.ExitUsage
	}
	dir, err := os.Getwd()
	if err != nil {
		env.Stderr.Printf("gotops init: %v\n", err)
		return cli.ExitEnvironment
	}
	ctx, stop := signalContext()
	defer stop()
	return initcmd.Run(ctx, initcmd.Options{
		Class: *class, Dir: dir, Owner: *owner, Force: *force, Offline: *offline,
		Assets: gotops.Files(), KitVersion: kitVersion(),
	}, env.Stdout, env.Stderr)
}

// kitVersion is the package version as a module version: a release sets
// packageVersion at link time, a development build leaves "0.0.0-dev".
func kitVersion() string {
	if strings.HasPrefix(packageVersion, "v") {
		return packageVersion
	}
	return "v" + packageVersion
}
