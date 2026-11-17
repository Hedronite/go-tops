package tests

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/gate"
)

// scripted is a gate.Executor that answers by command prefix.
type scripted struct {
	t     *testing.T
	calls []gate.Cmd
	on    map[string]func(gate.Cmd) (gate.Output, error)
}

func (s *scripted) Run(_ context.Context, c gate.Cmd) (gate.Output, error) {
	s.calls = append(s.calls, c)
	key := strings.Join(c.Argv, " ")
	for prefix, h := range s.on {
		if strings.HasPrefix(key, prefix) {
			return h(c)
		}
	}
	s.t.Fatalf("unexpected command %s", key)
	return gate.Output{}, nil
}

func (s *scripted) goTests() int {
	n := 0
	for _, c := range s.calls {
		if len(c.Argv) > 1 && c.Argv[0] == "go" && c.Argv[1] == "test" {
			n++
		}
	}
	return n
}

func newEnv(t *testing.T, goTest func(gate.Cmd) (gate.Output, error)) (*gate.Env, *scripted) {
	t.Helper()
	s := &scripted{t: t, on: map[string]func(gate.Cmd) (gate.Output, error){
		"go test":      goTest,
		"go env CC":    func(gate.Cmd) (gate.Output, error) { return gate.Output{Stdout: []byte("cc\n")}, nil },
		"cc --version": func(gate.Cmd) (gate.Output, error) { return gate.Output{}, nil },
	}}
	return &gate.Env{Dir: t.TempDir(), Exec: s}, s
}

func goodStream() gate.Output {
	return gate.Output{Stdout: stream(cat(
		[]ev{seed("m/a", "42")}, pass("m/a", "TestA"), pass("m/a", "TestB"),
		[]ev{{Action: "run", Package: "m/a", Test: "TestS"}, {Action: "skip", Package: "m/a", Test: "TestS"}, {Action: "pass", Package: "m/a"}},
	)...)}
}

func answer(o gate.Output) func(gate.Cmd) (gate.Output, error) {
	return func(gate.Cmd) (gate.Output, error) { return o, nil }
}

// G5a: one run, parsed natively; zero tests fails; skips and the seed recorded.
func TestPassRecordsFiguresAndTheSeed(t *testing.T) {
	env, _ := newEnv(t, answer(goodStream()))
	r := Tests{}.Run(context.Background(), env)
	if r.Outcome != gate.Pass {
		t.Fatalf("%s: %s", r.Outcome, r.Reason)
	}
	d := r.Details
	if d["packages"] != 1 || d["tests"] != 3 || d["skipped"] != 1 || d["race"] != true || d["shuffle_seed"] != int64(42) {
		t.Fatalf("details %v", d)
	}
}

// The command is exactly the one of §3.1, with cgo on, run once.
func TestTheCommandIsTheOneOfSection31(t *testing.T) {
	env, s := newEnv(t, answer(goodStream()))
	Tests{}.Run(context.Background(), env)
	if s.goTests() != 1 {
		t.Fatalf("%d go test invocations, want 1", s.goTests())
	}
	var c gate.Cmd
	for _, x := range s.calls {
		if x.Argv[1] == "test" {
			c = x
		}
	}
	want := "go test -json -race -shuffle=on -count=1 -coverprofile=.go-tops/cover.out -coverpkg=./... ./..."
	if got := strings.Join(c.Argv, " "); got != want {
		t.Errorf("argv\n got %s\nwant %s", got, want)
	}
	if len(c.Env) != 1 || c.Env[0] != "CGO_ENABLED=1" {
		t.Errorf("env %v, want CGO_ENABLED=1: -race needs cgo", c.Env)
	}
}

func TestFailingTestFails(t *testing.T) {
	out := gate.Output{ExitCode: 1, Stdout: stream(cat(
		[]ev{seed("m/c", "7")},
		[]ev{{Action: "run", Package: "m/c", Test: "TestC"}, {Action: "fail", Package: "m/c", Test: "TestC"}, {Action: "fail", Package: "m/c"}},
	)...)}
	env, _ := newEnv(t, answer(out))
	r := Tests{}.Run(context.Background(), env)
	if r.Outcome != gate.Fail || !strings.Contains(r.Reason, "m/c.TestC") {
		t.Fatalf("%s: %s", r.Outcome, r.Reason)
	}
	if r.Details["shuffle_seed"] != int64(7) {
		t.Errorf("a failed run still records the seed, to replay it: %v", r.Details)
	}
}

// G5a: zero tests fails, and a suite that is all skips ran nothing.
func TestZeroTestsFails(t *testing.T) {
	zero := gate.Output{Stdout: stream(ev{Action: "output", Package: "m/b", Output: "?   \tm/b\t[no test files]\n"}, ev{Action: "skip", Package: "m/b"})}
	allSkipped := gate.Output{Stdout: stream(cat(
		[]ev{seed("m/a", "3")},
		[]ev{{Action: "run", Package: "m/a", Test: "TestS"}, {Action: "skip", Package: "m/a", Test: "TestS"}, {Action: "pass", Package: "m/a"}},
	)...)}
	for name, o := range map[string]gate.Output{"no test files": zero, "all skipped": allSkipped} {
		env, _ := newEnv(t, answer(o))
		r := Tests{}.Run(context.Background(), env)
		if r.Outcome != gate.Fail || !strings.Contains(r.Reason, "no test ran") {
			t.Errorf("%s: %s: %s", name, r.Outcome, r.Reason)
		}
	}
}

// G5d: a race is a failure, even if every test reported pass.
func TestRaceFails(t *testing.T) {
	out := gate.Output{ExitCode: 66, Stdout: stream(cat(
		[]ev{seed("m/e", "9")}, pass("m/e", "TestRace"),
		[]ev{{Action: "output", Package: "m/e", Output: "WARNING: DATA RACE\n"}, {Action: "fail", Package: "m/e"}},
	)...)}
	env, _ := newEnv(t, answer(out))
	r := Tests{}.Run(context.Background(), env)
	if r.Outcome != gate.Fail || !strings.Contains(r.Reason, "data race") {
		t.Fatalf("%s: %s", r.Outcome, r.Reason)
	}
}

func TestBuildFailureFails(t *testing.T) {
	out := gate.Output{ExitCode: 1, Stdout: stream(
		ev{Action: "build-output", ImportPath: "m/d", Output: "d/d.go:3: undefined: x\n"},
		ev{Action: "build-fail", ImportPath: "m/d"},
		ev{Action: "fail", Package: "m/d", FailedBuild: "m/d"},
	)}
	env, _ := newEnv(t, answer(out))
	r := Tests{}.Run(context.Background(), env)
	if r.Outcome != gate.Fail || !strings.Contains(r.Reason, "undefined: x") {
		t.Fatalf("%s: %s", r.Outcome, r.Reason)
	}
	if _, ok := r.Details["error"]; !ok {
		t.Errorf("no seed, so the details are the {error} form: %v", r.Details)
	}
}

// A run with no shuffle line was not shuffled.
func TestNoShuffleSeedFails(t *testing.T) {
	out := gate.Output{Stdout: stream(cat(pass("m/a", "TestA"), []ev{{Action: "pass", Package: "m/a"}})...)}
	env, _ := newEnv(t, answer(out))
	r := Tests{}.Run(context.Background(), env)
	if r.Outcome != gate.Fail || !strings.Contains(r.Reason, "shuffle") {
		t.Fatalf("%s: %s", r.Outcome, r.Reason)
	}
}

// §3.4 rule 5: no output, or output that is not the JSON stream, is a failure,
// and so is a bad exit code with nothing else to blame.
func TestEmptyOrBrokenOutputFails(t *testing.T) {
	cases := map[string]gate.Output{
		"empty":            {},
		"plain text":       {Stdout: []byte("FAIL example.com/m [setup failed]\n"), ExitCode: 1},
		"exit with no why": {ExitCode: 2, Stdout: goodStream().Stdout, Stderr: []byte("go: downloading")},
	}
	for name, o := range cases {
		env, _ := newEnv(t, answer(o))
		r := Tests{}.Run(context.Background(), env)
		if r.Outcome != gate.Fail {
			t.Errorf("%s: %s", name, r.Outcome)
		}
		if r.Reason == "" {
			t.Errorf("%s: no reason", name)
		}
	}
}

// A stale profile must not stand in for this run's: the layer clears it.
func TestStaleProfileIsCleared(t *testing.T) {
	var dir string
	env, _ := newEnv(t, func(c gate.Cmd) (gate.Output, error) {
		if _, err := os.Stat(filepath.Join(dir, ".go-tops", "cover.out")); err == nil {
			t.Error("the previous profile was still there when go test ran")
		}
		return goodStream(), nil
	})
	dir = env.Dir
	if err := os.MkdirAll(filepath.Join(dir, ".go-tops"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".go-tops", "cover.out"), []byte("mode: atomic\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	Tests{}.Run(context.Background(), env)
}

func TestGoNotFoundIsToolMissing(t *testing.T) {
	env, _ := newEnv(t, func(gate.Cmd) (gate.Output, error) { return gate.Output{}, gate.ErrNotFound })
	if r := (Tests{}).Run(context.Background(), env); r.Outcome != gate.ToolMissing {
		t.Fatalf("%s", r.Outcome)
	}
}

// ---- preflight: no C compiler is exit 3 ----

func TestPreflightWithoutACCompilerIsAnEnvironmentError(t *testing.T) {
	env, s := newEnv(t, answer(goodStream()))
	s.on["cc --version"] = func(gate.Cmd) (gate.Output, error) { return gate.Output{}, gate.ErrNotFound }
	err := Tests{}.Preflight(context.Background(), env)
	var ee *gate.EnvError
	if !errors.As(err, &ee) {
		t.Fatalf("err = %v, want a *gate.EnvError (exit 3)", err)
	}
	if err := (Tests{}).Preflight(context.Background(), mustEnv(t, s)); err == nil {
		// the other env shares the broken cc; both must fail
		t.Fatal("expected failure")
	}
}

func mustEnv(t *testing.T, s *scripted) *gate.Env {
	t.Helper()
	return &gate.Env{Dir: t.TempDir(), Exec: s}
}

func TestPreflightWithACompilerPasses(t *testing.T) {
	env, _ := newEnv(t, answer(goodStream()))
	if err := (Tests{}).Preflight(context.Background(), env); err != nil {
		t.Fatal(err)
	}
}

func TestPreflightUsesTheCompilerGoNames(t *testing.T) {
	env, s := newEnv(t, answer(goodStream()))
	s.on["go env CC"] = func(gate.Cmd) (gate.Output, error) { return gate.Output{Stdout: []byte("/nonexistent/cc -m64\n")}, nil }
	s.on["/nonexistent/cc"] = func(gate.Cmd) (gate.Output, error) { return gate.Output{}, gate.ErrNotFound }
	var ee *gate.EnvError
	if err := (Tests{}).Preflight(context.Background(), env); !errors.As(err, &ee) {
		t.Fatalf("err = %v", err)
	}
}

func TestLayerIdentities(t *testing.T) {
	if (Tests{}).ID() != "tests" || (Tests{}).Tier() != 1 || len((Tests{}).Needs()) != 0 {
		t.Error("tests: tier 1, needs nothing (§3.5)")
	}
	if (Coverage{}).ID() != "coverage" || (Coverage{}).Tier() != 1 || strings.Join((Coverage{}).Needs(), ",") != "tests" {
		t.Error("coverage: tier 1, needs tests (§3.5)")
	}
	reg := map[string]bool{}
	for _, l := range gate.Layers() {
		switch l.(type) {
		case Tests, Coverage:
			reg[l.ID()] = true
		}
	}
	if !reg["tests"] || !reg["coverage"] {
		t.Errorf("importing the package did not register the real layers: %v", reg)
	}
}

// §3.1: one run feeds gates 2 and 3: through the runner, with both layers,
// `go test` is invoked once.
func TestOneRunFeedsBothLayers(t *testing.T) {
	env, s := newEnv(t, func(c gate.Cmd) (gate.Output, error) {
		// the real go test writes the profile; the fake does too
		p := filepath.Join(c.Dir, ".go-tops", "cover.out")
		if err := os.WriteFile(p, []byte("mode: atomic\nm/a/a.go:3.1,4.2 2 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return goodStream(), nil
	})
	s.on["git rev-parse HEAD"] = func(gate.Cmd) (gate.Output, error) { return gate.Output{Stdout: []byte("abc\n")}, nil }
	s.on["git status"] = func(gate.Cmd) (gate.Output, error) { return gate.Output{}, nil }
	cfg := gate.Config{Module: "m", Class: "binary-cli", Enforce: "hard", Coverage: gate.CoverageConfig{StatementsFloor: 70}}
	run, err := gate.Execute(context.Background(), cfg, gate.Options{MaxTier: 1, Only: "coverage"}, gate.Deps{Dir: env.Dir, Exec: s, Layers: []gate.Layer{Tests{}, Coverage{}}})
	if err != nil {
		t.Fatal(err)
	}
	if s.goTests() != 1 {
		t.Fatalf("%d go test invocations, want 1", s.goTests())
	}
	got := map[string]string{}
	for _, l := range run.Report.Layers {
		got[l.ID] = l.Outcome
	}
	if got["tests"] != "pass" || got["coverage"] != "pass" {
		t.Fatalf("layers %v\n%s", got, run.Text)
	}
}
