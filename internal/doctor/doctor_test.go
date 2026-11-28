package doctor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The oracle is GO_TOPS.md §12.1 (doctor checks the Go version against the
// floor, compared in Go's order, and the pin; a C compiler; tool presence;
// GOFLAGS; GOTOOLCHAIN; cgo state) and §12.2 (a missing tool is 1, a pin that
// does not match is 2, an environment error such as no C compiler is 3).

func env(dir string, vars map[string]string, goVersion string) Env {
	return Env{
		Dir:    dir,
		Getenv: func(k string) string { return vars[k] },
		Run: func(_ context.Context, _ string, argv ...string) (string, error) {
			switch strings.Join(argv, " ") {
			case "go env GOVERSION":
				return goVersion + "\n", nil
			case "go env CC":
				return "cc\n", nil
			case "go env CGO_ENABLED":
				return "1\n", nil
			}
			return "", errors.New("unexpected: " + strings.Join(argv, " "))
		},
		LookPath: func(name string) (string, error) {
			if name == "cc" || name == "gcc" || name == "golangci-lint" {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("not found")
		},
	}
}

func get(cs []Check, name string) Check {
	for _, c := range cs {
		if c.Name == name {
			return c
		}
	}
	return Check{Name: name + " (absent)"}
}

func TestHealthyEnvironmentExitsZero(t *testing.T) {
	cs := Run(context.Background(), env(t.TempDir(), map[string]string{"GOTOOLCHAIN": "local"}, "go1.27.4"))
	if code := ExitCode(cs); code != 0 {
		t.Fatalf("exit %d: %+v", code, cs)
	}
	for _, name := range []string{"go", "c-compiler", "cgo", "GOFLAGS", "GOTOOLCHAIN"} {
		if c := get(cs, name); c.Status == Fail || strings.Contains(c.Name, "absent") {
			t.Errorf("%+v", c)
		}
	}
}

// A C compiler that is not there is exit 3, whatever else is wrong with the tools.
func TestNoCCompilerExitsThree(t *testing.T) {
	cs := Run(context.Background(), env(t.TempDir(), map[string]string{"CC": "/nonexistent"}, "go1.27.4"))
	c := get(cs, "c-compiler")
	if c.Status != Fail || c.Exit != 3 || ExitCode(cs) != 3 || !strings.Contains(c.Detail, "/nonexistent") {
		t.Fatalf("exit %d, %+v", ExitCode(cs), c)
	}
}

func TestCCFromGoEnvWhenTheVariableIsUnset(t *testing.T) {
	e := env(t.TempDir(), nil, "go1.27.4")
	e.LookPath = func(string) (string, error) { return "", errors.New("none") }
	if c := get(Run(context.Background(), e), "c-compiler"); c.Status != Fail || !strings.Contains(c.Detail, "cc") {
		t.Fatalf("%+v", c)
	}
}

func TestCCFromGoEnvWhenUnsetAndOnlyThatCompilerIsOnPath(t *testing.T) {
	e := env(t.TempDir(), nil, "go1.27.4")
	base := e.Run
	e.Run = func(ctx context.Context, d string, argv ...string) (string, error) {
		if strings.Join(argv, " ") == "go env CC" {
			return "clang\n", nil
		}
		return base(ctx, d, argv...)
	}
	e.LookPath = func(name string) (string, error) {
		if name == "clang" {
			return "/usr/bin/clang", nil
		}
		return "", errors.New("not found")
	}
	c := get(Run(context.Background(), e), "c-compiler")
	if c.Status != OK || c.Detail != "/usr/bin/clang" {
		t.Fatalf("%+v", c)
	}
}

// The floor is a language version, compared in Go's own order: go1.26.4 and
// go1.26 both meet go1.26, go1.25.9 does not, and a release candidate or a
// toolchain suffix is read as the language it carries.
func TestGoVersionAgainstTheFloor(t *testing.T) {
	cases := map[string]bool{"go1.26": true, "go1.26.4": true, "go1.27.4": true, "go1.26rc1": true, "go1.25.9": false, "go1.24": false, "go1.27.4 X:nocoverageredesign": true}
	for v, ok := range cases {
		t.Run(v, func(t *testing.T) {
			c := get(Run(context.Background(), env(t.TempDir(), nil, v)), "go")
			if ok != (c.Status != Fail) {
				t.Fatalf("%s: %+v", v, c)
			}
			if !ok && (c.Exit != 3 || !strings.Contains(c.Detail, "floor")) {
				t.Errorf("below the floor is an environment error, exit 3: %+v", c)
			}
		})
	}
	if c := get(Run(context.Background(), env(t.TempDir(), nil, "not a version")), "go"); c.Status != Fail || c.Exit != 3 {
		t.Errorf("garbage GOVERSION: %+v", c)
	}
}

// The pin is the exact release: a different patch release is exit 2.
func TestPinMismatchExitsTwo(t *testing.T) {
	dir := t.TempDir()
	lockFor(t, dir, "go1.27.4", pinnedBinary)
	binaryAt(t, dir, pinnedBinary)
	cs := Run(context.Background(), env(dir, nil, "go1.27.3"))
	c := get(cs, "go")
	if c.Status != Fail || c.Exit != 2 || ExitCode(cs) != 2 || !strings.Contains(c.Detail, "go1.27.4") {
		t.Fatalf("exit %d, %+v", ExitCode(cs), c)
	}
	if code := ExitCode(Run(context.Background(), env(dir, nil, "go1.27.4"))); code != 0 {
		t.Errorf("the pinned release must pass, exit %d", code)
	}
}

func TestNoLockIsAWarningNotAFailure(t *testing.T) {
	c := get(Run(context.Background(), env(t.TempDir(), nil, "go1.27.4")), "go")
	if c.Status != Warn || !strings.Contains(c.Detail, "go-tops.lock") {
		t.Fatalf("%+v", c)
	}
}

func TestDownloadedBinaryMissingIsToolMissingExitOne(t *testing.T) {
	dir := t.TempDir()
	lockFor(t, dir, "go1.27.4", "x")
	e := env(dir, nil, "go1.27.4")
	e.LookPath = func(name string) (string, error) {
		if name == "cc" {
			return "/usr/bin/cc", nil
		}
		return "", errors.New("not found")
	}
	cs := Run(context.Background(), e)
	if c := get(cs, "golangci-lint"); c.Status != Fail || c.Exit != 1 || ExitCode(cs) != 1 {
		t.Fatalf("exit %d, %+v", ExitCode(cs), c)
	}
	// found in .go-tops/bin
	if err := os.MkdirAll(filepath.Join(dir, ".go-tops", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".go-tops", "bin", "golangci-lint"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if code := ExitCode(Run(context.Background(), e)); code != 0 {
		t.Errorf("a binary in .go-tops/bin is present, exit %d", code)
	}
}

func TestModuleToolsAreCheckedWithGoTool(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	mod := "module example.com/m/tools\n\ngo 1.26\n\ntool golang.org/x/vuln/cmd/govulncheck\n\nrequire golang.org/x/vuln v1.9.0\n"
	if err := os.WriteFile(filepath.Join(dir, "tools", "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	e := env(dir, nil, "go1.27.4")
	base := e.Run
	var asked []string
	e.Run = func(ctx context.Context, d string, argv ...string) (string, error) {
		if argv[1] == "tool" {
			asked = append(asked, strings.Join(argv, " "))
			return "", errors.New("no such tool")
		}
		return base(ctx, d, argv...)
	}
	cs := Run(context.Background(), e)
	if c := get(cs, "govulncheck"); c.Status != Fail || c.Exit != 1 || ExitCode(cs) != 1 {
		t.Fatalf("exit %d, %+v", ExitCode(cs), c)
	}
	if len(asked) != 1 || asked[0] != "go tool -modfile=tools/go.mod -n govulncheck" {
		t.Errorf("asked %v", asked)
	}
}

func TestGOFLAGSWarnsOnFlagsThatChangeWhatIsMeasured(t *testing.T) {
	cases := map[string]Status{"": OK, "-v": OK, "-mod=mod": Warn, "-race -count=3": Warn, "-tags=integration": Warn, "-modfile=x.mod": Warn}
	for flags, want := range cases {
		c := get(Run(context.Background(), env(t.TempDir(), map[string]string{"GOFLAGS": flags}, "go1.27.4")), "GOFLAGS")
		if c.Status != want {
			t.Errorf("GOFLAGS=%q: %+v, want %s", flags, c, want)
		}
	}
	for flags := range cases {
		if code := ExitCode(Run(context.Background(), env(t.TempDir(), map[string]string{"GOFLAGS": flags}, "go1.27.4"))); code != 0 {
			t.Errorf("GOFLAGS=%q: a warning must not fail, exit %d", flags, code)
		}
	}
}

func TestGOTOOLCHAINAndCgoStateAreReported(t *testing.T) {
	if c := get(Run(context.Background(), env(t.TempDir(), map[string]string{"GOTOOLCHAIN": "local"}, "go1.27.4")), "GOTOOLCHAIN"); c.Status != OK {
		t.Errorf("%+v", c)
	}
	for _, v := range []string{"", "auto", "go1.28.0+auto"} {
		if c := get(Run(context.Background(), env(t.TempDir(), map[string]string{"GOTOOLCHAIN": v}, "go1.27.4")), "GOTOOLCHAIN"); c.Status != Warn {
			t.Errorf("GOTOOLCHAIN=%q: %+v", v, c)
		}
	}
	e := env(t.TempDir(), nil, "go1.27.4")
	base := e.Run
	e.Run = func(ctx context.Context, d string, argv ...string) (string, error) {
		if strings.Join(argv, " ") == "go env CGO_ENABLED" {
			return "0\n", nil
		}
		return base(ctx, d, argv...)
	}
	c := get(Run(context.Background(), e), "cgo")
	if c.Status != Warn || !strings.Contains(c.Detail, "CGO_ENABLED=0") {
		t.Errorf("%+v", c)
	}
}

// When a pin mismatch and an environment error are both present, the pin
// mismatch wins: the lock is the custody check, and it is the more specific.
func TestExitCodePriority(t *testing.T) {
	cs := []Check{{Status: Fail, Exit: 1}, {Status: Fail, Exit: 3}, {Status: Fail, Exit: 2}, {Status: Warn, Exit: 0}}
	if ExitCode(cs) != 2 {
		t.Errorf("want 2, got %d", ExitCode(cs))
	}
	if ExitCode(cs[:2]) != 3 || ExitCode(cs[:1]) != 1 || ExitCode(cs[3:]) != 0 || ExitCode(nil) != 0 {
		t.Error("3 outranks 1, a warning is 0, no checks is 0")
	}
}
