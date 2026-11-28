package main

import (
	"errors"
	"flag"
	"io"
	"os"

	"github.com/Hedronite/go-tops/internal/cli"
	"github.com/Hedronite/go-tops/internal/doctor"
)

func init() {
	cli.Register(cli.Command{
		Name:    "doctor",
		Summary: "diagnose the environment: Go, the pin, a C compiler, tools, GOFLAGS, GOTOOLCHAIN, cgo",
		Run:     runDoctor,
	})
}

const doctorUsage = `Usage: gotops doctor

Checks the Go version against the language floor and the lock's pin, a C
compiler (the -race detector needs cgo), tool presence, GOFLAGS, GOTOOLCHAIN and
the cgo state. It changes nothing. Exit 3 for an environment error such as no C
compiler, 2 for a pin that does not match the lock, 1 for a missing tool.
`

func runDoctor(env cli.Env, args []string) int {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	if err := flags.Parse(args); err != nil {
		return usageError(env, "doctor", doctorUsage, err)
	}
	if flags.NArg() > 0 {
		return usageError(env, "doctor", doctorUsage, errors.New("takes no arguments, got "+flags.Arg(0)))
	}
	dir, err := os.Getwd()
	if err != nil {
		env.Stderr.Printf("gotops doctor: %v\n", err)
		return cli.ExitEnvironment
	}
	ctx, stop := signalContext()
	defer stop()
	checks := doctor.Run(ctx, doctor.Env{Dir: dir})
	for _, c := range checks {
		env.Stdout.Printf("%-5s %-14s %s\n", c.Status, c.Name, c.Detail)
	}
	return doctor.ExitCode(checks)
}
