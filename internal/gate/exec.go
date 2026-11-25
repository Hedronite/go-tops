package gate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Cmd is one argv step. It is run by exec, never through a shell: no element
// of Argv is parsed or expanded.
type Cmd struct {
	Dir      string
	Argv     []string
	Env      []string // extra KEY=VALUE entries added to the process environment
	StripEnv []string // prefixes of environment keys removed before Env is applied
	Stdin    io.Reader
}

// Output is what a finished step produced. A non-zero exit is an Output, not
// an error: the layer judges it.
type Output struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Executor runs argv steps. The error is non-nil only when the step could not
// run at all: the program was not found, or the context ended.
type Executor interface {
	Run(ctx context.Context, c Cmd) (Output, error)
}

// ErrNotFound is returned by an Executor when the program does not exist.
var ErrNotFound = errors.New("program not found")

// OS runs steps with os/exec.
type OS struct{}

// Run starts Argv[0] with the rest as its arguments.
func (OS) Run(ctx context.Context, c Cmd) (Output, error) {
	if len(c.Argv) == 0 {
		return Output{}, errors.New("gate: empty argv")
	}
	cmd := exec.CommandContext(ctx, c.Argv[0], c.Argv[1:]...)
	cmd.Dir = c.Dir
	if len(c.StripEnv) > 0 || len(c.Env) > 0 {
		cmd.Env = buildEnv(cmd.Environ(), c.StripEnv, c.Env)
	}
	cmd.Stdin = c.Stdin
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	out := Output{Stdout: so.Bytes(), Stderr: se.Bytes()}
	var ee *exec.ExitError
	switch {
	case err == nil:
		return out, nil
	case errors.As(err, &ee):
		out.ExitCode = ee.ExitCode()
		return out, nil
	case errors.Is(err, exec.ErrNotFound):
		return out, fmt.Errorf("%w: %s", ErrNotFound, c.Argv[0])
	}
	return out, err
}

func buildEnv(base, strip, extra []string) []string {
	if len(strip) == 0 {
		return append(base, extra...)
	}
	out := make([]string, 0, len(base)+len(extra))
	for _, kv := range base {
		if hasAnyPrefix(kv, strip) {
			continue
		}
		out = append(out, kv)
	}
	return append(out, extra...)
}

func hasAnyPrefix(kv string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(kv, p) {
			return true
		}
	}
	return false
}
