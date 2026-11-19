package initcmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The first ledger of an adoption (F2, ruling R6). A repository adopting Go-TOPS has
// no ledger and its workflow is not on the default branch yet, so no manual run can
// write one: on a pull request the ubuntu leg, which is the ledger leg, asks
// `gotops baseline --status` whether one is due and, when it is, writes it on its
// own build, uploads it and fails the run until it is committed.

// stepNamed finds the step whose name contains the text.
func stepNamed(t *testing.T, steps []step, text string) step {
	t.Helper()
	i := indexOf2(steps, func(s step) bool { return strings.Contains(s.Name, text) })
	if i < 0 {
		t.Fatalf("the workflow has no step named like %q", text)
	}
	return steps[i]
}

// ran lists which of the named steps run in a context.
func ran(t *testing.T, steps []step, ctx map[string]string, names ...string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, n := range names {
		out[n] = ifRunsCtx(t, stepNamed(t, steps, n).If, ctx)
	}
	return out
}

func TestStatusStepReadsTheBaseThroughTheEnvironment(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	s := stepNamed(t, workflowSteps(t, dir), "baseline --status)")
	if s.ID != "status" {
		t.Errorf("the step id is %q, want status: later steps read its output", s.ID)
	}
	if !strings.Contains(s.Run, `gotops baseline --status --base "origin/$BASE_REF"`) || strings.Contains(s.Run, "${{") {
		t.Errorf("the base ref must reach the command through the environment, not the script text:\n%s", s.Run)
	}
	if s.Env["BASE_REF"] != "${{ github.base_ref }}" {
		t.Errorf("BASE_REF = %q", s.Env["BASE_REF"])
	}
}

// Which steps run for each outcome of the status, leg and event.
func TestFirstLedgerStepsFollowTheStatus(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	steps := workflowSteps(t, dir)
	const (
		status, tell, write, upload, notCommitted = "baseline --status)", "could not tell", "write the first ledger", "upload the first ledger", "written, not committed"
		judge                                     = "judge the gate"
	)
	names := []string{status, tell, write, upload, notCommitted, judge}
	for _, c := range []struct {
		name       string
		event, leg string
		action     string
		want       []string // the steps of names that run
		gateRuns   bool
	}{
		{"a first ledger is due", "pull_request", "ubuntu", "due", []string{status, write, upload, notCommitted}, false},
		{"a ledger is present", "pull_request", "ubuntu", "proceed", []string{status, judge}, true},
		{"the status could not tell", "pull_request", "ubuntu", "stop", []string{status, tell, judge}, true},
		{"the status recorded nothing", "pull_request", "ubuntu", "", []string{status, tell, judge}, true},
		{"the macOS leg asks nothing", "pull_request", "macos", "", []string{judge}, true},
		{"the floor leg asks nothing", "pull_request", "floor", "", nil, false},
		{"a push asks nothing", "push", "ubuntu", "", []string{judge}, true},
		{"a manual run asks nothing", "workflow_dispatch", "ubuntu", "", nil, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := map[string]string{"matrix.leg": c.leg, "github.event_name": c.event, "steps.status.outputs.action": c.action}
			got := ran(t, steps, ctx, names...)
			for _, n := range names {
				want := false
				for _, w := range c.want {
					want = want || w == n
				}
				if got[n] != want {
					t.Errorf("step %q runs = %v, want %v", n, got[n], want)
				}
			}
			gateStep := steps[indexOf2(steps, func(s step) bool { return s.ID == "gate" })]
			if g := ifRunsCtx(t, gateStep.If, ctx); g != c.gateRuns {
				t.Errorf("the gate step runs = %v, want %v", g, c.gateRuns)
			}
		})
	}
}

// The status step reads the exit code and the state of the one JSON line, records
// both and the action they agree on, and never fails: the steps after it act, and
// anything but a code and a state that agree fails the run there, closed.
func TestStatusStepActsOnACodeAndAStateThatAgree(t *testing.T) {
	needTools(t)
	dir := newModule(t, "github.com/acme/demo", "1.26")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	s := stepNamed(t, workflowSteps(t, dir), "baseline --status)")
	for _, c := range []struct {
		code   int
		stdout string
		state  string
		action string
	}{
		{0, `{"state":"first-ledger-due","tree":false,"base":false}`, "first-ledger-due", "due"},
		{1, `{"state":"present","tree":true,"base":null}`, "present", "proceed"},
		{1, `{"state":"deleted","tree":false,"base":true}`, "deleted", "proceed"},
		{1, `{"state":"advisory","tree":false,"base":null}`, "advisory", "proceed"},
		{2, `{"state":"error","tree":false,"base":null,"reason":"no base"}`, "error", "stop"},
		{0, `{"state":"present"}`, "present", "stop"},                   // a code and a state that disagree
		{1, `{"state":"first-ledger-due"}`, "first-ledger-due", "stop"}, // the other way round
		{0, `{"state":"something-new"}`, "something-new", "stop"},       // an unknown state
		{0, `{"tree":false}`, "", "stop"},                               // no state
		{0, `not json`, "", "stop"},                                     // not a JSON line
		{0, ``, "", "stop"},                                             // no output
		{3, `{"state":"present"}`, "present", "stop"},                   // another exit code
	} {
		work := t.TempDir()
		bin := filepath.Join(work, "bin")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		fake := "#!/bin/sh\necho \"$@\" > \"" + filepath.Join(work, "argv") + "\"\nprintf '%s\\n' '" + c.stdout + "'\nexit " + strconv.Itoa(c.code) + "\n"
		if err := os.WriteFile(filepath.Join(bin, "go"), []byte(fake), 0o755); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(work, "output")
		cmd := exec.Command("bash", "-e", "-c", s.Run)
		cmd.Dir = work
		cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "GITHUB_OUTPUT="+out, "BASE_REF=release/1.x")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("status exit %d %s: the step failed: %v\n%s", c.code, c.stdout, err, b)
		}
		got, err := os.ReadFile(out)
		want := "code=" + strconv.Itoa(c.code) + "\nstate=" + c.state + "\naction=" + c.action
		if err != nil || strings.TrimSpace(string(got)) != want {
			t.Errorf("status exit %d %s: output %q, want %q (%v)", c.code, c.stdout, got, want, err)
		}
		argv, err := os.ReadFile(filepath.Join(work, "argv"))
		if err != nil || strings.TrimSpace(string(argv)) != "tool -modfile=tools/go.mod gotops baseline --status --base origin/release/1.x" {
			t.Errorf("the status ran as %q (%v)", argv, err)
		}
	}
}

// The steps that act on a status of 0 and on one that cannot tell.
func TestFirstLedgerStepsFailTheRunUntilTheLedgerIsCommitted(t *testing.T) {
	needTools(t)
	dir := newModule(t, "github.com/acme/demo", "1.26")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	steps := workflowSteps(t, dir)
	for _, c := range []struct{ name, text, env, want string }{
		{"not committed", "written, not committed", "", "go-tops-baseline"},
		{"cannot tell", "could not tell", "STATUS_CODE=2", "exited '2'"},
	} {
		s := stepNamed(t, steps, c.text)
		cmd := exec.Command("bash", "-e", "-c", s.Run)
		cmd.Dir = t.TempDir()
		cmd.Env = append(os.Environ(), c.env)
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "::error::") || !strings.Contains(string(out), c.want) {
			t.Errorf("%s: the step must fail with an error naming %q: %v\n%s", c.name, c.want, err, out)
		}
	}
	w := stepNamed(t, steps, "write the first ledger")
	if !strings.Contains(w.Run, "baseline --init") || strings.Contains(w.Run, "accept-regression") {
		t.Errorf("the first ledger is written with baseline --init alone:\n%s", w.Run)
	}
	u := stepNamed(t, steps, "upload the first ledger")
	if u.With["name"] != "go-tops-baseline" || u.With["path"] != ".go-tops/baseline.json" || u.With["include-hidden-files"] != true || u.With["if-no-files-found"] != "error" {
		t.Errorf("the upload must carry the ledger as go-tops-baseline, hidden files included, and fail when there is none: %v", u.With)
	}
}

// The inputs of a status step are never written into a script: a branch name is
// chosen by whoever opens the pull request.
func TestNoStepWritesAnExpressionIntoItsScript(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	for _, s := range workflowSteps(t, dir) {
		if strings.Contains(s.Run, "${{") {
			t.Errorf("step %q puts an expression in its script:\n%s", s.Name, s.Run)
		}
	}
}
