package main

import (
	"github.com/Hedronite/go-tops/internal/cli"
	"github.com/Hedronite/go-tops/internal/config"
)

// exitConfig is code 2 of GO_TOPS.md §12.2: the configuration or its custody is
// invalid.
const exitConfig = 2

func init() {
	cli.Register(cli.Command{
		Name:    "check",
		Summary: "validate go-tops.yaml and go-tops.lock against their schemas and the semantic rules",
		Run:     runCheck,
	})
}

func runCheck(env cli.Env, args []string) int {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		env.Stdout.Printf("Usage: gotops check\n\nReads go-tops.yaml, go-tops.lock and the ledger files in the current directory.\nExit 0: valid. 2: invalid, one line per issue on standard error. 3: the environment failed.\n")
		return 0
	}
	if len(args) > 0 {
		env.Stderr.Printf("gotops check: takes no arguments, got %q\n", args[0])
		return cli.ExitUsage
	}
	ctx, stop := signalContext()
	defer stop()
	res, err := config.Check(ctx, ".", config.Options{})
	if err != nil {
		env.Stderr.Printf("gotops check: %v\n", err)
		return cli.ExitEnvironment
	}
	if len(res.Issues) > 0 {
		for _, is := range res.Issues {
			env.Stderr.Printf("%s\n", is)
		}
		return exitConfig
	}
	env.Stdout.Printf("go-tops.yaml: ok (class %s)\n", res.Config.Class)
	return 0
}
