package initcmd

import (
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
