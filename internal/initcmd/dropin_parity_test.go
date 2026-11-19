//go:build skills

package initcmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The go-tops-gate skill and the drop-in's judge step both apply ruling C9, one
// to a local run and one in CI. They must reach the same verdict on the same
// report: a skill that passes what CI fails, or the reverse, sends an agent the
// wrong way. This test runs the judge cases of dropin_judge_test.go through both.
// It needs Nushell and jq and fails without them.

const parityFake = `#!/bin/sh
mkdir -p .go-tops
cp "$FAKE_REPORT" .go-tops/report.json
exit "$FAKE_EXIT"
`

// skillJudges runs the gate skill against the stand-in gate and returns its ok.
func skillJudges(t *testing.T, report []byte, code string) bool {
	t.Helper()
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake.sh")
	in := filepath.Join(dir, "in.json")
	for p, content := range map[string][]byte{fake: []byte(parityFake), in: report} {
		if err := os.WriteFile(p, content, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	work := filepath.Join(dir, "work")
	if err := os.Mkdir(work, 0o755); err != nil {
		t.Fatal(err)
	}
	args := []string{filepath.Join("..", "..", ".agents", "skills", "go-tops-gate", "go-tops-gate.nu"), "--gotops", "sh " + fake}
	abs, err := filepath.Abs(args[0])
	if err != nil {
		t.Fatal(err)
	}
	args[0] = abs
	cmd := exec.Command("nu", args...)
	cmd.Dir = work
	cmd.Env = append(os.Environ(), "FAKE_REPORT="+in, "FAKE_EXIT="+code)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("nu go-tops-gate: %v\n%s", err, out)
	}
	var got struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the skill printed no JSON object: %v\n%s", err, out)
	}
	return got.OK
}

func TestTheGateSkillAndTheDropInJudgeAgreeOnEveryCase(t *testing.T) {
	for _, tool := range []string{"nu", "jq", "bash"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("%s is required under -tags skills: %v", tool, err)
		}
	}
	_, judge := judgeSteps(t)
	compared := 0
	for _, c := range judgeCases() {
		// The skill sees the exit code of a process: a blank or non-numeric
		// recorded code is the drop-in's own case, a step that never ran.
		if n, err := strconv.Atoi(c.code); err != nil || n < 0 {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, ".go-tops"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, ".go-tops", "report.json"), []byte(c.doc.json(t)), 0o644); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", "-e", "-c", judge.Run)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GATE_EXIT="+c.code, "LEG="+c.leg)
			_, err := cmd.CombinedOutput()
			dropInPasses := err == nil
			if dropInPasses == c.fails {
				t.Fatalf("the drop-in judge's verdict (passes = %v) is not the case's (fails = %v)", dropInPasses, c.fails)
			}
			skill := skillJudges(t, []byte(c.doc.json(t)), c.code)
			if c.leg == "ubuntu" && c.doc.ratchet == "skip" && !c.doc.sameBuild {
				// The drop-in knows its ubuntu leg is the ledger leg, so it requires
				// pass there. The skill does not know the leg: it allows a skip on a
				// build other than the ledger's, as C3b does, and CI judges the rest.
				if dropInPasses || !skill {
					t.Errorf("a ratchet skip on another build: drop-in passes = %v, skill ok = %v; want false and true", dropInPasses, skill)
				}
				return
			}
			if skill != dropInPasses {
				t.Errorf("the gate skill gives ok = %v, the drop-in judge passes = %v (%s leg, exit %s)", skill, dropInPasses, c.leg, c.code)
			}
		})
		compared++
	}
	if compared < 20 {
		t.Fatalf("only %d cases were compared", compared)
	}
	if strings.TrimSpace(judge.Run) == "" {
		t.Fatal("the judge step has no script")
	}
}
