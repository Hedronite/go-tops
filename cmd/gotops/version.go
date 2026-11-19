package main

import (
	"os"
	"runtime/debug"
	"strings"

	gotops "github.com/Hedronite/go-tops"
	"github.com/Hedronite/go-tops/internal/cli"
	"github.com/Hedronite/go-tops/internal/lock"
)

// packageVersion is the gotops package version, a separate plane from the
// protocol version (GO_TOPS.md §2). A release sets it at link time:
// -ldflags "-X main.packageVersion=<version>".
var packageVersion = devVersion

// devVersion is the version of a build that links none in and carries no module version.
const devVersion = "0.0.0-dev"

// readBuildInfo is the build info of the running binary; a test replaces it.
var readBuildInfo = debug.ReadBuildInfo

// toolVersion is the version of this gotops: the one a release links in, else the
// module version of the build, which is a pseudo-version for a gotops an adopter
// runs through `go tool` before the kit has a tag, else 0.0.0-dev.
func toolVersion() string {
	if packageVersion != devVersion {
		return packageVersion
	}
	if info, ok := readBuildInfo(); ok {
		if v := strings.TrimPrefix(info.Main.Version, "v"); v != "" && v != "(devel)" {
			return v
		}
	}
	return packageVersion
}

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
	env.Stdout.Printf("protocol %s\npackage %s\n", protocol, toolVersion())
	printPins(env)
	return 0
}

// printPins lists the toolchain and the tools pinned in go-tops.lock and
// tools/go.mod of the working directory (GO_TOPS.md §12.1). Outside a repository
// that pins nothing, it prints nothing.
func printPins(env cli.Env) {
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	for _, p := range lock.Pins(dir) {
		if p.Name == "toolchain" {
			env.Stdout.Printf("toolchain %s (%s)\n", p.Version, p.Source)
			continue
		}
		env.Stdout.Printf("tool %s %s (%s)\n", p.Name, p.Version, p.Source)
	}
}
