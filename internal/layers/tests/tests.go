// Package tests holds gates 2 and 3 of GO_TOPS.md §3.1: the tests layer, which
// runs the suite once, and the coverage layer, which reads the profile of that
// run. Gate 4 (CRAP) reads the same profile later.
//
// Importing the package registers both layers with the runner.
package tests

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Hedronite/go-tops/internal/gate"
)

// IDs of the two layers.
const (
	TestsID    = "tests"
	CoverageID = "coverage"
)

// ProfilePath is where the test run writes its coverage profile (§3.1).
const ProfilePath = ".go-tops/cover.out"

func init() {
	gate.Register(Tests{})
	gate.Register(Coverage{})
}

// Command is the one test run that feeds gates 2, 3 and 4 (§3.1). -race needs
// cgo, so the run sets CGO_ENABLED=1; with -race the cover mode is atomic.
var Command = []string{
	"go", "test", "-json", "-race", "-shuffle=on", "-count=1",
	"-coverprofile=" + ProfilePath, "-coverpkg=./...", "./...",
}

// Env is added to the environment of the test run.
var Env = []string{"CGO_ENABLED=1"}

// Tests is gate 2.
type Tests struct{}

// ID implements gate.Layer.
func (Tests) ID() string { return TestsID }

// Tier implements gate.Layer.
func (Tests) Tier() int { return 1 }

// Needs implements gate.Layer: the tests need no other layer (§3.5).
func (Tests) Needs() []string { return nil }

// Preflight finds, before any layer runs, that -race cannot run here: there is
// no C compiler (§12.2 code 3). It asks the go command which compiler it would
// use, which honors CC, and starts it.
func (Tests) Preflight(ctx context.Context, env *gate.Env) error {
	out, err := env.Exec.Run(ctx, gate.Cmd{Dir: env.Dir, Argv: []string{"go", "env", "CC"}})
	if err != nil {
		return &gate.EnvError{Err: fmt.Errorf("go env CC: %w", err)}
	}
	cc := strings.Fields(string(out.Stdout))
	if out.ExitCode != 0 || len(cc) == 0 {
		return &gate.EnvError{Err: errors.New("go env CC printed no C compiler; -race needs cgo")}
	}
	if _, err := env.Exec.Run(ctx, gate.Cmd{Dir: env.Dir, Argv: append(cc, "--version")}); err != nil {
		return &gate.EnvError{Err: fmt.Errorf("no C compiler (%s); the test run uses -race, which needs cgo: %w", cc[0], err)}
	}
	return nil
}

// Run implements gate.Layer.
func (Tests) Run(ctx context.Context, env *gate.Env) gate.Result {
	prof := filepath.Join(env.Dir, filepath.FromSlash(ProfilePath))
	if err := os.MkdirAll(filepath.Dir(prof), 0o755); err != nil {
		return failErr("cannot create %s: %v", filepath.Dir(ProfilePath), err)
	}
	// A profile left by an earlier run must not stand in for this run's.
	if err := os.Remove(prof); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return failErr("cannot clear the previous profile: %v", err)
	}
	out, err := env.Exec.Run(ctx, gate.Cmd{Dir: env.Dir, Argv: Command, Env: Env})
	if err != nil {
		if errors.Is(err, gate.ErrNotFound) {
			return gate.MissingTool("go", err)
		}
		return failErr("go test did not run: %v", err)
	}
	res, perr := Parse(out.Stdout)
	if perr != nil {
		return failErr("%v (exit %d: %s)", perr, out.ExitCode, firstLines(string(out.Stderr), 2))
	}
	return judge(res, out)
}

// judge turns a parsed run into the layer's result (§3.1 gate 2): all pass, no
// race, at least one test ran, and the shuffle seed recorded.
func judge(r *Result, out gate.Output) gate.Result {
	var problems []string
	if len(r.BuildErrors) > 0 || len(r.FailedPackages) > 0 {
		var parts []string
		if len(r.BuildErrors) > 0 {
			parts = append(parts, "build failed: "+joinMax(r.BuildErrors, 3))
		}
		if len(r.FailedPackages) > 0 && len(r.BuildErrors) == 0 {
			parts = append(parts, "package(s) failed outside any test: "+joinMax(r.FailedPackages, 3))
		}
		problems = append(problems, parts...)
	}
	if len(r.FailedTests) > 0 {
		problems = append(problems, fmt.Sprintf("%d test(s) failed: %s", len(r.FailedTests), joinMax(r.FailedTests, 5)))
	}
	if len(r.Races) > 0 {
		problems = append(problems, "data race detected in "+joinMax(r.Races, 3))
	}
	if r.Ran() == 0 && len(problems) == 0 {
		problems = append(problems, fmt.Sprintf("no test ran (%d skipped): a green run that measured nothing is a failure", r.Skipped))
	}
	if out.ExitCode != 0 && len(problems) == 0 {
		problems = append(problems, fmt.Sprintf("go test exited %d: %s", out.ExitCode, firstLines(string(out.Stderr), 3)))
	}
	if !r.HasSeed && r.Ran() > 0 {
		problems = append(problems, "no -test.shuffle line in the output: the run was not shuffled")
	}
	if len(problems) == 0 {
		return gate.Result{Outcome: gate.Pass, Details: figures(r)}
	}
	msg := strings.Join(problems, "; ")
	if r.HasSeed {
		return gate.Result{Outcome: gate.Fail, Reason: msg, Details: figures(r)}
	}
	return gate.Result{Outcome: gate.Fail, Reason: msg, Details: map[string]any{"error": msg}}
}

// figures are the typed details of the tests layer (schema/gate-report.schema.json).
func figures(r *Result) map[string]any {
	return map[string]any{
		"packages": r.Packages, "tests": r.Tests, "race": true, "skipped": r.Skipped, "shuffle_seed": r.Seed,
	}
}

func failErr(format string, a ...any) gate.Result {
	msg := fmt.Sprintf(format, a...)
	return gate.Result{Outcome: gate.Fail, Reason: msg, Details: map[string]any{"error": msg}}
}

func firstLines(text string, n int) string {
	var out []string
	for _, ln := range strings.Split(text, "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			out = append(out, ln)
			if len(out) == n {
				break
			}
		}
	}
	return strings.Join(out, " | ")
}

// joinMax lists up to n items and counts the rest.
func joinMax(items []string, n int) string {
	if len(items) <= n {
		return strings.Join(items, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(items[:n], ", "), len(items)-n)
}
