package gotops

import (
	"os"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// The skills' script tests run on fixtures. The weekly adoption job also runs the
// skills against the real gotops, and requires the test's PASS line: a test that
// did not run, or skipped, is not a pass.
func TestTheWeeklyAdoptionJobRunsTheSkillsAgainstTheRealGotops(t *testing.T) {
	data, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	var wf struct {
		Jobs map[string]struct {
			If    string `yaml:"if"`
			Steps []struct {
				Name  string            `yaml:"name"`
				Run   string            `yaml:"run"`
				Shell string            `yaml:"shell"`
				Env   map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &wf); err != nil {
		t.Fatal(err)
	}
	job := wf.Jobs["adoption"]
	if !strings.Contains(job.If, "schedule") {
		t.Fatalf("the adoption job is not the weekly one: if %q", job.If)
	}
	nu, live := -1, -1
	for i, s := range job.Steps {
		if s.Env["VERSION"] == nuReleaseInCI && strings.Contains(s.Run, "shasum -a 256 -c") && strings.Contains(s.Run, "nushell/releases") {
			nu = i
		}
		if strings.Contains(s.Run, `-tags "skills adoption" -run TestSkillsRun`) {
			live = i
			for _, want := range []string{"set -euo pipefail", "--- PASS: TestSkillsRunTheRealGotopsOnAFixtureModule ", "exit 1"} {
				if !strings.Contains(s.Run, want) {
					t.Errorf("the live skills step lacks %q:\n%s", want, s.Run)
				}
			}
			if s.Shell != "bash" {
				t.Errorf("the live skills step pipes into tee, so it needs shell: bash for pipefail, got %q", s.Shell)
			}
		}
	}
	if live < 0 {
		t.Fatal("the adoption job does not run the live skills test")
	}
	if nu < 0 || nu > live {
		t.Errorf("the pinned Nushell must be installed, checked by its SHA-256, before the live skills test (install step %d, test step %d)", nu, live)
	}
}

// nuReleaseInCI is the Nushell release CI installs; the tagged skill tests refuse
// any other.
const nuReleaseInCI = "0.108.0"
