package initcmd

import (
	"os"
	"os/exec"
	"path/filepath"
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
			if in.If != "matrix.leg != 'floor'" {
				t.Errorf("the floor leg builds and tests only, so it installs nothing; if = %q", in.If)
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
	if up.If != "always() && matrix.leg != 'floor'" {
		t.Errorf("the report is uploaded whatever the gate did, on the gate legs: if = %q", up.If)
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

type workflowJob struct {
	If     string `yaml:"if"`
	RunsOn string `yaml:"runs-on"`
	Steps  []step `yaml:"steps"`
}

func workflowJobs(t *testing.T, dir string) (map[string]workflowJob, map[string]any) {
	t.Helper()
	var wf struct {
		On   map[string]any         `yaml:"on"`
		Jobs map[string]workflowJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(read(t, dir, ".github/workflows/go-tops.yml")), &wf); err != nil {
		t.Fatal(err)
	}
	return wf.Jobs, wf.On
}

// C3b: the gate job does not run when the workflow is dispatched by hand, and
// the baseline job runs only then.
func TestWorkflowHasAManualBaselineJob(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	jobs, on := workflowJobs(t, dir)
	for _, trigger := range []string{"pull_request", "push", "workflow_dispatch"} {
		if _, ok := on[trigger]; !ok {
			t.Errorf("the workflow is not triggered by %s: on = %v", trigger, on)
		}
	}
	if jobs["gate"].If != "github.event_name != 'workflow_dispatch'" {
		t.Errorf("the gate job must not run on a manual dispatch: if = %q", jobs["gate"].If)
	}
	b := jobs["baseline"]
	if b.If != "github.event_name == 'workflow_dispatch'" || b.RunsOn != "ubuntu-latest" {
		t.Errorf("the baseline job runs only on a manual dispatch, on the ledger leg's runner: if %q, runs-on %q", b.If, b.RunsOn)
	}
}

// The first ledger is written on the ledger leg's build: the same runner and
// toolchain as the ubuntu gate leg, with the pinned golangci-lint installed
// first (the feeding layers read its saved output), and the file is uploaded
// to be committed.
func TestBaselineJobRunsInitOnTheLedgerLegAndUploadsTheFile(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	jobs, _ := workflowJobs(t, dir)
	steps := jobs["baseline"].Steps
	install := indexOf2(steps, func(s step) bool { return strings.HasPrefix(s.Name, "install golangci-lint") })
	initStep := indexOf2(steps, func(s step) bool { return strings.Contains(s.Run, "gotops baseline --init") })
	upload := indexOf2(steps, func(s step) bool { return strings.HasPrefix(s.Uses, "actions/upload-artifact@") })
	if install < 0 || initStep < install || upload < initStep {
		t.Fatalf("want install (%d), then baseline --init (%d), then upload (%d)", install, initStep, upload)
	}
	if !strings.Contains(steps[install].Env["PLATFORM"], "linux-amd64") || !strings.Contains(steps[initStep].Run, "go tool -modfile=tools/go.mod gotops baseline --init") {
		t.Errorf("install %v, init %q", steps[install].Env, steps[initStep].Run)
	}
	up := steps[upload]
	if up.With["path"] != ".go-tops/baseline.json" || up.With["include-hidden-files"] != true || up.With["name"] != "go-tops-baseline" || up.With["if-no-files-found"] != "error" {
		t.Errorf("the ledger is uploaded as go-tops-baseline from a hidden directory, and a missing file is an error: %v", up.With)
	}
	gate := workflowSteps(t, dir)
	g := indexOf2(gate, func(s step) bool { return strings.Contains(s.Run, "gotops gate") })
	if g < 0 || !strings.Contains(gate[g].Env["GOTOOLCHAIN"], "local") {
		t.Fatal("the gate step is gone")
	}
	setup := func(steps []step) string {
		i := indexOf2(steps, func(s step) bool { return strings.HasPrefix(s.Uses, "actions/setup-go@") })
		if i < 0 {
			t.Fatal("no setup-go step")
		}
		v, _ := steps[i].With["go-version"].(string)
		return v
	}
	if want := "${{ matrix.go }}"; setup(gate) != want || setup(steps) == "" || strings.Contains(setup(steps), "matrix") {
		t.Errorf("the baseline job pins the toolchain the ubuntu leg uses, not a matrix value: %q", setup(steps))
	}
}

// reportWith writes a gate report with the given enforce mode and ratchet
// outcome ("" for no ratchet object) and returns the directory.
func reportWith(t *testing.T, enforce, outcome string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".go-tops"), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := `{"enforce": "` + enforce + `"`
	if outcome != "" {
		doc += `, "ratchet": {"outcome": "` + outcome + `"}`
	}
	doc += "}"
	if err := os.WriteFile(filepath.Join(dir, ".go-tops", "report.json"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// C3b: the ubuntu leg is the ledger leg, and CI fails when it reports a ratchet
// outcome of skip (the recorded build matches no leg, so nothing is ever judged).
// The assertion is run here against reports.
func TestRatchetAssertionFailsWhenTheLedgerLegSkips(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is not installed; the runners have it")
	}
	dir := newModule(t, "github.com/acme/demo", "1.26")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	steps := workflowSteps(t, dir)
	gate := indexOf2(steps, func(s step) bool { return strings.Contains(s.Run, "gotops gate") })
	i := indexOf2(steps, func(s step) bool { return strings.Contains(s.Name, "ratchet") })
	if i < 0 || i < gate {
		t.Fatalf("the ratchet assertion (%d) must come after the gate (%d)", i, gate)
	}
	if steps[i].If != "matrix.leg == 'ubuntu'" {
		t.Errorf("the assertion is the ubuntu leg's: if = %q", steps[i].If)
	}
	for _, c := range []struct {
		enforce, outcome string
		fails            bool
	}{
		{"ratchet", "pass", false},
		{"ratchet", "fail", false}, // the gate step already failed the job; not this step's business
		{"ratchet", "skip", true},
		{"ratchet", "", true}, // a missing judgment is not a pass
		{"hard", "", false},
		{"advisory", "", false},
	} {
		cmd := exec.Command("bash", "-c", steps[i].Run)
		cmd.Dir = reportWith(t, c.enforce, c.outcome)
		out, err := cmd.CombinedOutput()
		if (err != nil) != c.fails {
			t.Errorf("enforce %s, ratchet %q: failed = %v, want %v\n%s", c.enforce, c.outcome, err != nil, c.fails, out)
		}
		if c.fails && !strings.Contains(string(out), "::error::") {
			t.Errorf("enforce %s, ratchet %q: no error annotation\n%s", c.enforce, c.outcome, out)
		}
	}
}
