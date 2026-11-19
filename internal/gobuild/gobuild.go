// Package gobuild defines a build of Go code once: the GOOS, GOARCH, build tags
// and cgo setting that decide which files compile. The tests layer runs the
// suite under one, and the CRAP layer loads the module under the same one, so
// that the files it scores are the files whose coverage the profile holds
// (GO_TOPS.md §6.2, ruling C2). The baseline records one (§6.7, ruling C3).
package gobuild

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
)

// Build is a named build. Its zero value is the build the go command chooses
// from its own environment, with cgo off.
type Build struct {
	// GOOS and GOARCH name the platform; empty leaves them to the go command.
	GOOS   string `json:"goos"`
	GOARCH string `json:"goarch"`
	// Tags are the build tags, sorted and without the race tag, which the test
	// run adds by itself.
	Tags       []string `json:"tags"`
	CGOEnabled bool     `json:"cgo_enabled"`
}

// RaceTag is the tag `go test -race` sets.
const RaceTag = "race"

// Env is the environment the gate's test run takes for the build, added to the
// process environment. GOFLAGS is always set, so that a -mod, -skip, -short,
// -run or -tags inherited from the process or from `go env -w` never reaches
// the run: the go command applies an inherited GOFLAGS, and an empty value does
// not override a `go env -w` setting, but a non-empty one does. The one flag
// the gate passes anyway, -count=1, is the value.
func (b Build) Env() []string {
	env := []string{"CGO_ENABLED=" + cgo(b.CGOEnabled)}
	if b.GOOS != "" {
		env = append(env, "GOOS="+b.GOOS)
	}
	if b.GOARCH != "" {
		env = append(env, "GOARCH="+b.GOARCH)
	}
	flags := "-count=1"
	if len(b.Tags) > 0 {
		flags += " -tags=" + strings.Join(b.Tags, ",")
	}
	return append(env, "GOFLAGS="+flags)
}

// LoadEnv is the environment for loading the packages the test run compiles:
// Env plus the race tag, because the run is `go test -race` and that sets the
// tag. Loading without it flips every `//go:build race` and `//go:build !race`
// file.
func (b Build) LoadEnv() []string {
	if !slices.Contains(b.Tags, RaceTag) {
		b.Tags = append(slices.Clone(b.Tags), RaceTag)
	}
	return b.Env()
}

// Equal reports whether two builds compile the same files: the same platform and
// cgo setting, and the same set of tags, in any order and with any repeat. The
// baseline records a build and the ratchet compares it with the run's.
func (b Build) Equal(o Build) bool {
	return b.GOOS == o.GOOS && b.GOARCH == o.GOARCH && b.CGOEnabled == o.CGOEnabled && slices.Equal(tagSet(b.Tags), tagSet(o.Tags))
}

func tagSet(tags []string) []string {
	set := slices.Clone(tags)
	slices.Sort(set)
	return slices.Compact(set)
}

// Platform returns b on another GOOS and GOARCH. Cgo is off: a cross build
// has no C toolchain to count on.
func (b Build) Platform(goos, goarch string) Build {
	b.GOOS, b.GOARCH, b.CGOEnabled = goos, goarch, false
	return b
}

func cgo(on bool) string {
	if on {
		return "1"
	}
	return "0"
}

// Resolve fills in the GOOS and GOARCH that b leaves empty from the go
// command's own environment (`go env GOOS GOARCH`, run in dir under b's
// environment), and keeps the tags and the cgo setting as given. The test run's
// build leaves the platform to the go command, so a build recorded as it stands
// would name no platform and match every one; the CRAP layer, the baseline and
// the ratchet all call Resolve, so the build of a run is defined here and
// nowhere else.
func Resolve(dir string, b Build) (Build, error) {
	if b.GOOS != "" && b.GOARCH != "" {
		return b, nil
	}
	cmd := exec.Command("go", "env", "GOOS", "GOARCH")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), b.Env()...)
	out, err := cmd.Output()
	if err != nil {
		return Build{}, fmt.Errorf("go env GOOS GOARCH: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		return Build{}, fmt.Errorf("go env GOOS GOARCH printed %q, want two values", strings.TrimSpace(string(out)))
	}
	if b.GOOS == "" {
		b.GOOS = fields[0]
	}
	if b.GOARCH == "" {
		b.GOARCH = fields[1]
	}
	return b, nil
}
