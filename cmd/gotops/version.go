package main

import (
	gotops "github.com/Hedronite/go-tops"
	"github.com/Hedronite/go-tops/internal/cli"
)

// packageVersion is the gotops package version, a separate plane from the
// protocol version (GO_TOPS.md §2). A release sets it at link time:
// -ldflags "-X main.packageVersion=<version>".
var packageVersion = "0.0.0-dev"

func init() {
	cli.Register(cli.Command{
		Name:    "version",
		Summary: "print the protocol and package versions",
		Run:     runVersion,
	})
}

func runVersion(env cli.Env, args []string) int {
	if len(args) > 0 {
		env.Stderr.Printf("gotops version: takes no arguments, got %q\n", args[0])
		return cli.ExitUsage
	}
	protocol, err := gotops.Protocol()
	if err != nil {
		env.Stderr.Printf("gotops version: %v\n", err)
		return cli.ExitEnvironment
	}
	env.Stdout.Printf("protocol %s\npackage %s\n", protocol, packageVersion)
	return 0
}
