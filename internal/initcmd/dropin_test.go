package initcmd

import (
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// The drop-in workflow must work on a fresh adopter runner (SPEC-m1 B21): the
// gate finds golangci-lint in .go-tops/bin or on PATH (GO_TOPS.md section 10),
// so the workflow installs the pinned release, archive-checked, before the
// gate; and the report must reach the artifact, which hides dot-directories by
// default.

type step struct {
	ID   string            `yaml:"id"`
	Name string            `yaml:"name"`
	If   string            `yaml:"if"`
	Uses string            `yaml:"uses"`
	Env  map[string]string `yaml:"env"`
	Run  string            `yaml:"run"`
	With map[string]any    `yaml:"with"`
}

func workflowSteps(t *testing.T, dir string) []step {
	t.Helper()
	var wf struct {
		Jobs map[string]struct {
			Steps []step `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(read(t, dir, ".github/workflows/go-tops.yml")), &wf); err != nil {
		t.Fatal(err)
	}
	return wf.Jobs["gate"].Steps
}

func indexOf2(steps []step, match func(step) bool) int {
	for i, s := range steps {
		if match(s) {
			return i
		}
	}
	return -1
}

func TestWorkflowInstallsThePinnedGolangciLintBeforeTheGate(t *testing.T) {
	for _, class := range launchClasses {
		t.Run(class, func(t *testing.T) {
			dir := newModule(t, "github.com/acme/demo", "1.26")
			if code, _, errs := run(opts(dir, class)); code != ExitOK {
				t.Fatal(errs)
			}
			steps := workflowSteps(t, dir)
			install := indexOf2(steps, func(s step) bool { return strings.HasPrefix(s.Name, "install golangci-lint") })
			gate := indexOf2(steps, func(s step) bool { return strings.Contains(s.Run, "gotops gate") })
			if install < 0 || gate < 0 || install > gate {
				t.Fatalf("the install step (%d) must come before the gate (%d)", install, gate)
			}
			in := steps[install]
			for _, leg := range []string{"ubuntu", "macos", "floor"} {
				if got, want := ifRuns(t, in.If, "push", leg), leg != "floor"; got != want {
					t.Errorf("on push the %s leg installs golangci-lint = %v, want %v (the floor leg builds and tests only)", leg, got, want)
				}
			}
			lock := asMap(t, decodeYAML(t, read(t, dir, "go-tops.lock")))
			bins, ok := lock["binaries"].([]any)
			if !ok || len(bins) == 0 {
				t.Fatalf("the lock has no binaries: %v", lock)
			}
			bin := asMap(t, bins[0])
			sums := asMap(t, bin["sha256"])
			for platform, key := range map[string]string{"linux-amd64": "linux/amd64", "darwin-arm64": "darwin/arm64"} {
				want, ok := sums[key].(string)
				if !ok {
					t.Fatalf("no %s hash in the lock", key)
				}
				if !strings.Contains(in.Run, platform+")") || !strings.Contains(in.Run, "sha="+want) {
					t.Errorf("the workflow does not pin %s to the lock's archive hash %s:\n%s", platform, want, in.Run)
				}
			}
			for _, want := range []string{`shasum -a 256 -c -`, `curl --fail`, `>> "$GITHUB_PATH"`, "set -euo pipefail"} {
				if !strings.Contains(in.Run, want) {
					t.Errorf("the install step lacks %q", want)
				}
			}
			if in.Env["VERSION"] != bin["version"] {
				t.Errorf("VERSION = %q, the lock pins %v", in.Env["VERSION"], bin["version"])
			}
		})
	}
}

// The legs that install carry the platform the runner has: ubuntu-latest is
// amd64 and macos-latest is arm64.
func TestWorkflowMatrixNamesTheLintPlatformPerLeg(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	text := read(t, dir, ".github/workflows/go-tops.yml")
	for _, want := range []string{"leg: ubuntu, os: ubuntu-latest", "lint_platform: linux-amd64", "leg: macos, os: macos-latest", "lint_platform: darwin-arm64"} {
		if !strings.Contains(text, want) {
			t.Errorf("the matrix lacks %q", want)
		}
	}
	if strings.Contains(text, "leg: floor, os: ubuntu-latest, go: \"1.26\", lint_platform") {
		t.Error("the floor leg installs nothing, so it has no lint platform")
	}
}

func TestWorkflowUploadsHiddenReportFilesButNotTheBinaries(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	i := indexOf2(workflowSteps(t, dir), func(s step) bool { return strings.HasPrefix(s.Uses, "actions/upload-artifact@") })
	if i < 0 {
		t.Fatal("no upload step")
	}
	up := workflowSteps(t, dir)[i]
	if up.With["include-hidden-files"] != true {
		t.Errorf(".go-tops is a hidden directory, so the upload needs include-hidden-files: true; with = %v", up.With)
	}
	path, ok := up.With["path"].(string)
	if !ok {
		t.Fatalf("the upload path is not text: %v", up.With["path"])
	}
	lines := strings.Fields(path)
	if len(lines) != 2 || lines[0] != ".go-tops/" || lines[1] != "!.go-tops/bin/" {
		t.Errorf("the artifact is .go-tops/ without the downloaded binaries, got %q", path)
	}
	for _, c := range []struct {
		event, leg string
		want       bool
	}{{"push", "ubuntu", true}, {"pull_request", "macos", true}, {"push", "floor", false}, {"workflow_dispatch", "ubuntu", false}} {
		if got := ifRuns(t, up.If, c.event, c.leg); got != c.want {
			t.Errorf("the gate report is uploaded on %s / %s = %v, want %v (on the gate legs, never on a dispatch)", c.event, c.leg, got, c.want)
		}
	}
	if !strings.Contains(up.If, "always()") {
		t.Errorf("the report is uploaded whatever the gate did: if = %q", up.If)
	}
}

// With --offline there is no tools/go.sum, and a module with no dependencies
// has no go.sum either: setup-go's cache then finds nothing to key on. init says so.
func TestOfflineNoteExplainsTheCachePathCaveat(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	code, out, errs := run(opts(dir, "binary-cli"))
	if code != ExitOK {
		t.Fatal(errs)
	}
	for _, want := range []string{"--offline", "go -C tools mod tidy", "cache-dependency-path", "go.sum and tools/go.sum"} {
		if !strings.Contains(out, want) {
			t.Errorf("the offline note lacks %q:\n%s", want, out)
		}
	}
	// online, the tidy writes tools/go.sum, so the caveat is not printed
	o := opts(newModule(t, "github.com/acme/demo", "1.26"), "binary-cli")
	o.Offline, o.Tidy = false, func(string) error { return nil }
	if _, out, _ := run(o); strings.Contains(out, "cache-dependency-path") {
		t.Errorf("an online init must not print the offline caveat:\n%s", out)
	}
}

// ifRuns evaluates a step condition of the template for one event and leg, in
// the state of a job that has not failed (always() is true). It knows only the
// small language the template uses: matrix.leg and github.event_name compared
// with == or != to a quoted string, always(), && , || and parentheses.
func ifRuns(t *testing.T, expr, event, leg string) bool {
	t.Helper()
	return ifRunsIn(t, expr, event, leg, "")
}

// ifRunsIn is ifRuns for a dispatch with the given mode input ("" when the
// event has no inputs); inputs.mode may be compared like the other names.
func ifRunsIn(t *testing.T, expr, event, leg, mode string) bool {
	t.Helper()
	if strings.TrimSpace(expr) == "" {
		return true
	}
	toks := regexp.MustCompile(`\(|\)|&&|\|\||==|!=|always\(\)|[a-z_.]+|'[^']*'`).FindAllString(expr, -1)
	pos := 0
	var or func() bool
	atom := func() bool {
		if pos >= len(toks) {
			t.Fatalf("condition %q ends early", expr)
		}
		tok := toks[pos]
		pos++
		switch tok {
		case "(":
			v := or()
			if pos >= len(toks) || toks[pos] != ")" {
				t.Fatalf("condition %q: missing )", expr)
			}
			pos++
			return v
		case "always()":
			return true
		}
		vals := map[string]string{"matrix.leg": leg, "github.event_name": event, "inputs.mode": mode}
		got, known := vals[tok]
		if !known || pos+1 >= len(toks) {
			t.Fatalf("condition %q: cannot read %q", expr, tok)
		}
		op, lit := toks[pos], strings.Trim(toks[pos+1], "'")
		pos += 2
		switch op {
		case "==":
			return got == lit
		case "!=":
			return got != lit
		}
		t.Fatalf("condition %q: operator %q", expr, op)
		return false
	}
	and := func() bool {
		v := atom()
		for pos < len(toks) && toks[pos] == "&&" {
			pos++
			if r := atom(); !r {
				v = false
			}
		}
		return v
	}
	or = func() bool {
		v := and()
		for pos < len(toks) && toks[pos] == "||" {
			pos++
			if r := and(); r {
				v = true
			}
		}
		return v
	}
	v := or()
	if pos != len(toks) {
		t.Fatalf("condition %q: trailing %q", expr, toks[pos:])
	}
	return v
}

func workflowOn(t *testing.T, dir string) (map[string]any, int) {
	t.Helper()
	var wf struct {
		On   map[string]any `yaml:"on"`
		Jobs map[string]any `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(read(t, dir, ".github/workflows/go-tops.yml")), &wf); err != nil {
		t.Fatal(err)
	}
	return wf.On, len(wf.Jobs)
}

// stepsRunning lists the names (or, for a step with none, the action it uses)
// of the steps that run on one event and leg.
func stepsRunning(t *testing.T, steps []step, event, leg string) []string {
	t.Helper()
	return stepsRunningIn(t, steps, event, leg, "")
}

func stepsRunningIn(t *testing.T, steps []step, event, leg, mode string) []string {
	t.Helper()
	var out []string
	for _, s := range steps {
		if !ifRunsIn(t, s.If, event, leg, mode) {
			continue
		}
		name := s.Name
		if name == "" {
			name = strings.SplitN(s.Uses, "@", 2)[0]
		}
		out = append(out, name)
	}
	return out
}

// GO_TOPS.md section 16 item 26: the drop-in is one job. C3b adds the manual
// first-ledger run to it as a workflow_dispatch trigger.
func TestWorkflowIsOneJobWithADispatchTrigger(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	on, jobs := workflowOn(t, dir)
	for _, trigger := range []string{"pull_request", "push", "workflow_dispatch"} {
		if _, ok := on[trigger]; !ok {
			t.Errorf("the workflow is not triggered by %s: on = %v", trigger, on)
		}
	}
	if jobs != 1 {
		t.Errorf("the drop-in is one job, got %d", jobs)
	}
	// The dispatch takes one input, a choice of init or refresh: never free text.
	inputs := asMap(t, asMap(t, on["workflow_dispatch"])["inputs"])
	mode := asMap(t, inputs["mode"])
	opts, ok := mode["options"].([]any)
	if !ok {
		t.Fatalf("the mode input has no options: %v", mode)
	}
	if len(inputs) != 1 || mode["type"] != "choice" || len(opts) != 2 || opts[0] != "init" || opts[1] != "refresh" || mode["default"] != "init" {
		t.Errorf("the dispatch input is one choice of init or refresh, defaulting to init: %v", inputs)
	}
}

// On a manual dispatch only the ubuntu leg acts (it is the ledger leg), and it
// runs no gate step; on every other event the legs do what they did before and
// the baseline steps do not run.
func TestOnDispatchOnlyTheLedgerLegWritesTheFirstLedger(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	steps := workflowSteps(t, dir)
	has := func(got []string, want string) bool {
		for _, g := range got {
			if strings.Contains(g, want) {
				return true
			}
		}
		return false
	}
	for _, leg := range []string{"macos", "floor"} {
		if got := stepsRunning(t, steps, "workflow_dispatch", leg); len(got) != 0 {
			t.Errorf("on a dispatch the %s leg must do nothing, but runs %v", leg, got)
		}
	}
	for _, c := range []struct{ mode, runs, not string }{
		{"init", "write the first ledger", "refresh the ledger"},
		{"refresh", "refresh the ledger", "write the first ledger"},
	} {
		got := stepsRunningIn(t, steps, "workflow_dispatch", "ubuntu", c.mode)
		for _, want := range []string{"actions/checkout", "actions/setup-go", "install golangci-lint", c.runs, "go-tops-baseline"} {
			if !has(got, want) {
				t.Errorf("on a dispatch with mode %s the ubuntu leg lacks %q: runs %v", c.mode, want, got)
			}
		}
		for _, not := range []string{"gate", "ratchet", "floor", c.not} {
			if has(got, not) {
				t.Errorf("on a dispatch with mode %s the ubuntu leg runs a %q step: %v", c.mode, not, got)
			}
		}
		for _, leg := range []string{"macos", "floor"} {
			if got := stepsRunningIn(t, steps, "workflow_dispatch", leg, c.mode); len(got) != 0 {
				t.Errorf("on a dispatch with mode %s the %s leg must do nothing, but runs %v", c.mode, leg, got)
			}
		}
	}
	for _, event := range []string{"push", "pull_request"} {
		for _, leg := range []string{"ubuntu", "macos", "floor"} {
			if got := stepsRunning(t, steps, event, leg); has(got, "write the first ledger") || has(got, "refresh the ledger") || has(got, "go-tops-baseline") {
				t.Errorf("on %s the %s leg writes a ledger", event, leg)
			}
		}
		if !has(stepsRunning(t, steps, event, "ubuntu"), "the gate") && !has(stepsRunning(t, steps, event, "ubuntu"), "gate") {
			t.Errorf("on %s the ubuntu leg does not run the gate", event)
		}
		if !has(stepsRunning(t, steps, event, "floor"), "build and test on the floor") {
			t.Errorf("on %s the floor leg does not build and test", event)
		}
	}
}

// The first ledger is written on the ledger leg's build (the ubuntu runner and
// toolchain the gate leg uses), after the pinned golangci-lint is installed (the
// feeding layers read its output), and the file is uploaded to be committed.
func TestBaselineStepsRunInitAndUploadTheFile(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	steps := workflowSteps(t, dir)
	install := indexOf2(steps, func(s step) bool { return strings.HasPrefix(s.Name, "install golangci-lint") })
	initStep := indexOf2(steps, func(s step) bool { return strings.Contains(s.Run, "gotops baseline --init") })
	upload := indexOf2(steps, func(s step) bool { return s.With["name"] == "go-tops-baseline" })
	if install < 0 || initStep < install || upload < initStep {
		t.Fatalf("want install (%d), then baseline --init (%d), then upload (%d)", install, initStep, upload)
	}
	if steps[initStep].Run != "go tool -modfile=tools/go.mod gotops baseline --init" || steps[initStep].Env["GOTOOLCHAIN"] != "local" {
		t.Errorf("init step %q env %v", steps[initStep].Run, steps[initStep].Env)
	}
	// refresh is the plain lowering write, and --accept-regression is never run in CI.
	refresh := indexOf2(steps, func(s step) bool { return s.Name == "refresh the ledger" })
	if refresh < install || refresh > upload || !strings.HasPrefix(steps[refresh].Run, "go tool -modfile=tools/go.mod gotops baseline\n") || steps[refresh].Env["GOTOOLCHAIN"] != "local" {
		t.Errorf("the refresh step is the plain baseline write between the install and the upload: %d %+v", refresh, steps[refresh])
	}
	for _, s := range steps {
		if strings.Contains(s.Run, "--accept-regression") {
			t.Errorf("--accept-regression is manual, but step %q runs it", s.Name)
		}
		if strings.Contains(s.Run, "inputs") || strings.Contains(s.Run, "${{") {
			t.Errorf("step %q puts an expression in a command; inputs only pick a step: %q", s.Name, s.Run)
		}
	}
	up := steps[upload]
	if !strings.HasPrefix(up.Uses, "actions/upload-artifact@") || up.With["path"] != ".go-tops/baseline.json" || up.With["include-hidden-files"] != true || up.With["if-no-files-found"] != "error" {
		t.Errorf("the ledger is uploaded from a hidden directory, and a missing file is an error: %v", up)
	}
	// the ledger leg is the ubuntu gate leg: one toolchain for both
	gate := indexOf2(steps, func(s step) bool { return strings.Contains(s.Run, "gotops gate") })
	if steps[gate].Env["GOTOOLCHAIN"] != "local" || steps[install].Env["PLATFORM"] != "${{ matrix.lint_platform }}" {
		t.Errorf("the gate and the baseline steps share the install and the toolchain: gate %v, install %v", steps[gate].Env, steps[install].Env)
	}
}
