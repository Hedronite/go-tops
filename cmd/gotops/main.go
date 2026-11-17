// Command gotops is the Go-TOPS gate (GO_TOPS.md §12). Each subcommand lives in
// its own file in this directory and registers itself with internal/cli from an
// init function, so this file does not change when a command lands.
package main

import (
	"os"

	"github.com/Hedronite/go-tops/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr))
}
