package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	embedfs "io/fs"
	"os"
	"regexp"
	"strings"

	gotops "github.com/Hedronite/go-tops"
	"github.com/Hedronite/go-tops/internal/cli"
	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/laws"
)

func init() {
	cli.Register(cli.Command{
		Name:    "laws",
		Summary: "list, explain or run the laws layer (§12.1)",
		Run:     runLaws,
	})
}

var lawIDPattern = regexp.MustCompile(`^G-\d\d$`)

const lawsUsage = `Usage: gotops laws list
       gotops laws explain G-NN
       gotops laws run [--max-tier N] [--fail-fast] [--base REF] [--json]

list prints every catalog row. explain prints one law file. run executes the
laws layer alone, as gotops gate --only laws.
`

func runLaws(env cli.Env, args []string) int {
	if err := laws.ErrCatalog(); err != nil {
		env.Stderr.Printf("gotops laws: malformed catalog: %v\n", err)
		return 2
	}
	if len(args) == 0 {
		return usageError(env, "laws", lawsUsage, errors.New("missing subcommand"))
	}
	switch args[0] {
	case "list":
		return lawsList(env, args[1:])
	case "explain":
		return lawsExplain(env, args[1:])
	case "run":
		return lawsRun(env, args[1:])
	default:
		return usageError(env, "laws", lawsUsage, fmt.Errorf("unknown subcommand %q", args[0]))
	}
}

func lawsList(env cli.Env, args []string) int {
	flags := flag.NewFlagSet("laws list", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		return usageError(env, "laws list", "Usage: gotops laws list\n", errors.New("unexpected arguments"))
	}
	env.Stdout.Printf("ID\tTITLE\tSEV\tREADER\tSTATUS\tV0.1\tLIVE\n")
	for _, l := range laws.Catalog() {
		live := "no"
		if laws.Live(l.ID) {
			live = "yes"
		}
		v01 := " "
		if l.V01 {
			v01 = "yes"
		}
		title := strings.ReplaceAll(l.Title, "\t", " ")
		env.Stdout.Printf("%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			l.ID, title, l.Severity, l.Reader, l.Status, v01, live)
	}
	return 0
}

func lawsExplain(env cli.Env, args []string) int {
	flags := flag.NewFlagSet("laws explain", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 1 {
		return usageError(env, "laws explain", "Usage: gotops laws explain G-NN\n", errors.New("want exactly one law id"))
	}
	id := flags.Arg(0)
	if !lawIDPattern.MatchString(id) {
		return usageError(env, "laws explain", "Usage: gotops laws explain G-NN\n", fmt.Errorf("%q is not a law id", id))
	}
	if _, ok := laws.Lookup(id); !ok {
		env.Stderr.Printf("gotops laws explain: %s is not in the catalog\n", id)
		return 1
	}
	path := "laws/" + id + ".yaml"
	data, err := embedfs.ReadFile(gotops.Files(), path)
	if err != nil {
		env.Stderr.Printf("gotops laws explain: %v\n", err)
		return 2
	}
	env.Stdout.Printf("%s", string(data))
	return 0
}

func lawsRun(env cli.Env, args []string) int {
	ctx, stop := signalContext()
	defer stop()
	gateArgs := append([]string{"--only", "laws"}, args...)
	return gate.Main(ctx, gateArgs, gate.CommandDeps{
		LoadConfig:  loadGateConfig,
		CI:          os.Getenv("CI") == "true",
		ToolVersion: toolVersion(),
	}, env.Stdout, env.Stderr)
}
