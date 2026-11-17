// Package doctor is `gotops doctor` (GO_TOPS.md §12.1): it diagnoses the
// environment the gate runs in and changes nothing.
package doctor

import (
	"fmt"
	"go/version"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Hedronite/go-tops/internal/lock"
)

// FloorGo is the language floor of the protocol (§2), as a go/version name.
const FloorGo = "go1.26"

// Status is the verdict on one check.
type Status string

// The statuses. Fail carries the exit code the problem maps to in §12.2.
const (
	OK   Status = "ok"
	Warn Status = "warn"
	Fail Status = "fail"
)

// Check is one diagnosis. Exit is the code a Fail maps to: 1 for a missing
// tool, 2 for a pin that does not match the lock, 3 for an environment error.
type Check struct {
	Name   string
	Status Status
	Detail string
	Exit   int
}

// Env is what doctor reads: the repository, the environment and the commands.
type Env struct {
	Dir      string
	Getenv   func(string) string
	Run      lock.Runner
	LookPath func(string) (string, error)
}

func (e Env) withDefaults() Env {
	if e.Getenv == nil {
		e.Getenv = os.Getenv
	}
	if e.Run == nil {
		e.Run = func(dir string, argv ...string) (string, error) {
			cmd := exec.Command(argv[0], argv[1:]...)
			cmd.Dir = dir
			out, err := cmd.Output()
			return string(out), err
		}
	}
	if e.LookPath == nil {
		e.LookPath = exec.LookPath
	}
	return e
}

// Run performs every check and returns them in order.
func Run(e Env) []Check {
	e = e.withDefaults()
	var l *lock.Lock
	if got, err := lock.LoadDir(e.Dir); err == nil {
		l = got
	}
	out := []Check{goVersion(e, l), cCompiler(e), cgoState(e)}
	out = append(out, tools(e, l)...)
	return append(out, goflags(e), gotoolchain(e))
}

// ExitCode maps the checks to an exit code: 2 for a pin mismatch, else 3 for
// an environment error, else 1 for a missing tool, else 0. Warnings never fail.
func ExitCode(cs []Check) int {
	for _, code := range []int{2, 3, 1} {
		for _, c := range cs {
			if c.Status == Fail && c.Exit == code {
				return code
			}
		}
	}
	return 0
}

func goVersion(e Env, l *lock.Lock) Check {
	out, err := e.Run(e.Dir, "go", "env", "GOVERSION")
	if err != nil {
		return Check{"go", Fail, "go is not usable: " + err.Error(), 3}
	}
	got := strings.TrimSpace(out)
	release, _, _ := strings.Cut(got, " ") // GOVERSION may carry experiments: "go1.27.4 X:boringcrypto"
	if !version.IsValid(release) {
		return Check{"go", Fail, fmt.Sprintf("go env GOVERSION says %q, not a Go version", got), 3}
	}
	if version.Compare(version.Lang(release), FloorGo) < 0 {
		return Check{"go", Fail, fmt.Sprintf("%s is below the language floor %s (§2)", got, FloorGo), 3}
	}
	switch {
	case l == nil:
		return Check{"go", Warn, got + ": no valid go-tops.lock here, so the pin is not checked", 0}
	case got != l.Toolchain:
		return Check{"go", Fail, fmt.Sprintf("%s is running, the lock pins %s (a different patch release passes silently under GOTOOLCHAIN=local)", got, l.Toolchain), 2}
	}
	return Check{"go", OK, got + ", the pin and above the floor " + FloorGo, 0}
}

// cCompiler finds the C compiler cgo needs: the -race detector, and so every
// test run of the gate, requires cgo.
func cCompiler(e Env) Check {
	cc := strings.TrimSpace(e.Getenv("CC"))
	if cc == "" {
		if out, err := e.Run(e.Dir, "go", "env", "CC"); err == nil {
			cc = strings.TrimSpace(out)
		}
	}
	if cc == "" {
		cc = "cc"
	}
	prog := strings.Fields(cc)
	if len(prog) == 0 {
		return Check{"c-compiler", Fail, "CC is blank: cgo needs a C compiler for -race", 3}
	}
	path, err := e.LookPath(prog[0])
	if err != nil {
		return Check{"c-compiler", Fail, fmt.Sprintf("no C compiler: %s is not on PATH; the gate runs tests with CGO_ENABLED=1 and -race", prog[0]), 3}
	}
	return Check{"c-compiler", OK, path, 0}
}

func cgoState(e Env) Check {
	out, err := e.Run(e.Dir, "go", "env", "CGO_ENABLED")
	if err != nil {
		return Check{"cgo", Warn, "cannot read CGO_ENABLED: " + err.Error(), 0}
	}
	switch got := strings.TrimSpace(out); got {
	case "1":
		return Check{"cgo", OK, "CGO_ENABLED=1", 0}
	default:
		return Check{"cgo", Warn, "CGO_ENABLED=" + got + " here; the gate sets CGO_ENABLED=1 for its test run, a build of your own needs it for -race", 0}
	}
}

// tools checks that each pinned tool can be found: a module tool through the
// Go tool command, a downloaded binary in .go-tops/bin or on PATH. Whether it
// is the pinned version is `gotops lock --verify`.
func tools(e Env, l *lock.Lock) []Check {
	var out []Check
	if l != nil {
		for _, b := range l.Binaries {
			out = append(out, binaryCheck(e, b.Name))
		}
	}
	data, err := os.ReadFile(filepath.Join(e.Dir, "tools", "go.mod"))
	if err != nil {
		return out
	}
	mods, err := lock.ParseToolsMod(data)
	if err != nil {
		return append(out, Check{"tools/go.mod", Fail, err.Error(), 2})
	}
	for _, t := range mods {
		out = append(out, moduleCheck(e, t))
	}
	return out
}

func binaryCheck(e Env, name string) Check {
	if p, ok := lock.BinaryPath(lock.VerifyOptions{Dir: e.Dir, LookPath: e.LookPath}, name); ok {
		return Check{name, OK, p, 0}
	}
	return Check{name, Fail, "neither in .go-tops/bin nor on PATH (tool-missing)", 1}
}

func moduleCheck(e Env, t lock.ModuleTool) Check {
	if _, err := e.Run(e.Dir, "go", "tool", "-modfile=tools/go.mod", "-n", t.Name); err != nil {
		return Check{t.Name, Fail, "not a tool of tools/go.mod, or its modules are missing (tool-missing): " + err.Error(), 1}
	}
	return Check{t.Name, OK, t.Module + " " + t.Version + " (tools/go.mod)", 0}
}

// goflags warns about GOFLAGS entries that change what the gate measures: the
// gate sets its own flags, and these edit go.mod or the test run.
func goflags(e Env) Check {
	flags := strings.TrimSpace(e.Getenv("GOFLAGS"))
	if flags == "" {
		return Check{"GOFLAGS", OK, "unset", 0}
	}
	var risky []string
	for _, f := range strings.Fields(flags) {
		for _, p := range []string{"-mod=", "-modfile=", "-tags", "-race", "-cover", "-count", "-shuffle", "-ldflags", "-gcflags"} {
			if strings.HasPrefix(f, p) {
				risky = append(risky, f)
			}
		}
	}
	if len(risky) > 0 {
		return Check{"GOFLAGS", Warn, "sets " + strings.Join(risky, " ") + ": it can change what the gate builds or measures", 0}
	}
	return Check{"GOFLAGS", OK, flags, 0}
}

func gotoolchain(e Env) Check {
	switch v := strings.TrimSpace(e.Getenv("GOTOOLCHAIN")); v {
	case "local":
		return Check{"GOTOOLCHAIN", OK, "local: the go command uses the toolchain it ships with, and the gate checks it against the pin", 0}
	case "":
		return Check{"GOTOOLCHAIN", Warn, "unset: the go command may download another toolchain; the gate checks GOVERSION against the pin either way", 0}
	default:
		return Check{"GOTOOLCHAIN", Warn, v + ": the go command may switch toolchain; the gate checks GOVERSION against the pin", 0}
	}
}
