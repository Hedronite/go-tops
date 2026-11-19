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

// The drop-in judges the gate the way ruling C9 defines passing at M2: until the
// required laws are judged (M5) the gate exits 1 on them alone, and that is a
// pass; the gate step records the exit code, and a step after it judges it on
// every gate leg. These tests run the two steps' scripts for real.

// judgeSteps returns the gate step and the judging step of a rendered drop-in.
func judgeSteps(t *testing.T) (gate, judge step) {
	t.Helper()
	dir := newModule(t, "github.com/acme/demo", "1.26")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	steps := workflowSteps(t, dir)
	g := indexOf2(steps, func(s step) bool { return s.ID == "gate" })
	j := indexOf2(steps, func(s step) bool { return strings.Contains(s.Name, "judge the gate") })
	if g < 0 || j < g {
		t.Fatalf("the gate step (%d) must come before the step that judges it (%d)", g, j)
	}
	return steps[g], steps[j]
}

func needTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"jq", "bash"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed; the runners have it", tool)
		}
	}
}

// Which events and legs run each step: the gate and the judge run on the two gate
// legs of a push or a pull request, and the judge runs even when a step before it
// failed (always()).
func TestTheGateIsRecordedAndJudgedOnEveryGateLeg(t *testing.T) {
	gate, judge := judgeSteps(t)
	if !strings.HasPrefix(judge.If, "always()") {
		t.Errorf("the judge must run after a failed step: if %q", judge.If)
	}
	for _, event := range []string{"push", "pull_request", "workflow_dispatch"} {
		for _, leg := range []string{"ubuntu", "macos", "floor"} {
			want := event != "workflow_dispatch" && leg != "floor"
			for name, s := range map[string]step{"gate": gate, "judge": judge} {
				if got := ifRuns(t, s.If, event, leg); got != want {
					t.Errorf("the %s step runs on %s / %s = %v, want %v", name, event, leg, got, want)
				}
			}
		}
	}
	if judge.Env["GATE_EXIT"] != "${{ steps.gate.outputs.code }}" || judge.Env["LEG"] != "${{ matrix.leg }}" {
		t.Errorf("the judge reads the recorded exit code and the leg: %v", judge.Env)
	}
}

// The gate step records whatever the gate exits with and itself exits 0.
func TestTheGateStepRecordsItsExitCodeWithoutFailing(t *testing.T) {
	needTools(t)
	gate, _ := judgeSteps(t)
	for _, code := range []int{0, 1, 2, 3, 4} {
		dir := t.TempDir()
		bin := filepath.Join(dir, "bin")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		fake := "#!/bin/sh\nexit " + strconv.Itoa(code) + "\n"
		if err := os.WriteFile(filepath.Join(bin, "go"), []byte(fake), 0o755); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(dir, "output")
		cmd := exec.Command("bash", "-e", "-c", gate.Run)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "GITHUB_OUTPUT="+out)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("gate exit %d: the step failed: %v\n%s", code, err, b)
		}
		got, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(string(got)) != "code="+strconv.Itoa(code) {
			t.Errorf("gate exit %d: output %q", code, got)
		}
	}
}

// gateDoc is a gate report: the enforce mode, the ratchet outcome ("" for no
// ratchet object, "-" for an object without an outcome), the layers' outcomes,
// the failed items and the required laws.
type gateDoc struct {
	enforce, ratchet  string
	layers            map[string]string
	failed            []string
	required, notEval int
}

func (g gateDoc) json(t *testing.T) string {
	t.Helper()
	layers := []map[string]string{}
	for _, id := range []string{"hygiene", "tests", "coverage", "crap", "mutation", "allocs"} {
		out := "pass"
		if id == "mutation" || id == "allocs" {
			out = "skip"
		}
		if o, ok := g.layers[id]; ok {
			out = o
		}
		layers = append(layers, map[string]string{"id": id, "outcome": out})
	}
	doc := map[string]any{
		"enforce": g.enforce,
		"layers":  layers,
		"summary": map[string]any{"failed": append([]string{}, g.failed...)},
		"laws":    map[string]any{"required": g.required, "required_not_evaluated": g.notEval},
	}
	switch g.ratchet {
	case "":
	case "-":
		doc["ratchet"] = map[string]any{}
	default:
		doc["ratchet"] = map[string]any{"outcome": g.ratchet}
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// c9 is a gate report of the passing state at M2: the ratchet passes and the
// required laws, none evaluated, are the only failed item.
func c9() gateDoc {
	return gateDoc{enforce: "ratchet", ratchet: "pass", failed: []string{"laws"}, required: 16, notEval: 16}
}

func (g gateDoc) with(f func(*gateDoc)) gateDoc {
	if g.layers != nil {
		m := map[string]string{}
		for k, v := range g.layers {
			m[k] = v
		}
		g.layers = m
	}
	f(&g)
	return g
}

// The judging script, run against reports for each leg and exit code. Passing is
// ruling C9; everything else fails closed with an error annotation.
func TestTheJudgeAppliesC9OnEveryLegAndFailsClosed(t *testing.T) {
	needTools(t)
	_, judge := judgeSteps(t)
	clean := gateDoc{enforce: "ratchet", ratchet: "pass"}
	for _, c := range []struct {
		name, leg, code string
		doc             gateDoc
		fails           bool
	}{
		// exit 0 passes, and the ledger leg must have judged the ratchet
		{"exit 0 on the ledger leg", "ubuntu", "0", clean, false},
		{"exit 0 on another leg, ratchet pass", "macos", "0", clean, false},
		{"exit 0 on another leg, ratchet skip", "macos", "0", clean.with(func(g *gateDoc) { g.ratchet = "skip" }), false},
		{"exit 0, no ratchet under advisory", "ubuntu", "0", gateDoc{enforce: "advisory"}, false},
		{"exit 0 on the ledger leg, ratchet skip", "ubuntu", "0", clean.with(func(g *gateDoc) { g.ratchet = "skip" }), true},
		{"exit 0 on the ledger leg, no ratchet object", "ubuntu", "0", clean.with(func(g *gateDoc) { g.ratchet = "" }), true},
		{"exit 0 on the ledger leg, ratchet object without an outcome", "ubuntu", "0", clean.with(func(g *gateDoc) { g.ratchet = "-" }), true},
		{"exit 0 on the ledger leg, ratchet fail", "ubuntu", "0", clean.with(func(g *gateDoc) { g.ratchet = "fail" }), true},
		{"exit 0 on another leg, ratchet fail", "macos", "0", clean.with(func(g *gateDoc) { g.ratchet = "fail" }), true},
		// exit 1 passes on the required laws alone
		{"C9 on the ledger leg", "ubuntu", "1", c9(), false},
		{"C9 on another leg, ratchet pass", "macos", "1", c9(), false},
		{"C9 on another leg, ratchet skip", "macos", "1", c9().with(func(g *gateDoc) { g.ratchet = "skip" }), false},
		{"C9 on the ledger leg, ratchet skip", "ubuntu", "1", c9().with(func(g *gateDoc) { g.ratchet = "skip" }), true},
		{"C9 on the ledger leg, ratchet fail", "ubuntu", "1", c9().with(func(g *gateDoc) { g.ratchet = "fail"; g.failed = []string{"ratchet", "laws"} }), true},
		{"a layer failed with the laws", "ubuntu", "1", c9().with(func(g *gateDoc) {
			g.layers = map[string]string{"crap": "fail"}
			g.failed = []string{"crap", "laws"}
		}), true},
		{"a layer failed and is not listed", "ubuntu", "1", c9().with(func(g *gateDoc) { g.layers = map[string]string{"tests": "fail"} }), true},
		{"a layer errored", "ubuntu", "1", c9().with(func(g *gateDoc) { g.layers = map[string]string{"hygiene": "error"} }), true},
		{"a failed item besides the laws", "macos", "1", c9().with(func(g *gateDoc) { g.failed = []string{"coverage", "laws"} }), true},
		{"nothing failed but the gate exited 1", "ubuntu", "1", c9().with(func(g *gateDoc) { g.failed = nil }), true},
		{"a required law was evaluated", "ubuntu", "1", c9().with(func(g *gateDoc) { g.notEval = 15 }), true},
		{"no required laws, so exit 1 is something else", "ubuntu", "1", c9().with(func(g *gateDoc) { g.required, g.notEval = 0, 0 }), true},
		// any other exit code fails directly, whatever the report says
		{"exit 2", "ubuntu", "2", c9(), true},
		{"exit 3", "macos", "3", clean, true},
		{"exit 4", "ubuntu", "4", clean, true},
		{"an exit code that is not a number", "ubuntu", "x", clean, true},
		{"no exit code: a step before the gate failed", "ubuntu", "", clean, true},
		{"no exit code: a step before the gate failed, laws only", "macos", "", c9(), true},
	} {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, ".go-tops"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".go-tops", "report.json"), []byte(c.doc.json(t)), 0o644); err != nil {
			t.Fatal(err)
		}
		judged(t, c.name, judge, dir, c.leg, c.code, c.fails)
	}
}

// A missing report and a report that is not a gate report fail closed, which a
// test of [ "$(jq ...)" = x ] never does: jq fails inside the test and set -e is
// not triggered there.
func TestTheJudgeFailsClosedOnAMissingOrBrokenReport(t *testing.T) {
	needTools(t)
	_, judge := judgeSteps(t)
	good := c9().json(t)
	for _, c := range []struct {
		name  string
		write *string // nil: no report file
	}{
		{"no report", nil},
		{"an empty report", ptr("")},
		{"a report that is not JSON", ptr("the gate crashed\n")},
		{"a truncated report", ptr(good[:len(good)/2])},
		{"JSON null", ptr("null")},
		{"a JSON array", ptr("[]")},
		{"an object with no enforce mode", ptr("{}")},
		{"no layers", ptr(`{"enforce":"ratchet","ratchet":{"outcome":"pass"},"summary":{"failed":["laws"]},"laws":{"required":16,"required_not_evaluated":16}}`)},
		{"no laws object", ptr(`{"enforce":"ratchet","ratchet":{"outcome":"pass"},"layers":[],"summary":{"failed":["laws"]}}`)},
	} {
		for _, code := range []string{"0", "1"} {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, ".go-tops"), 0o755); err != nil {
				t.Fatal(err)
			}
			if c.write != nil {
				if err := os.WriteFile(filepath.Join(dir, ".go-tops", "report.json"), []byte(*c.write), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			// exit 0 needs only a readable report; "no layers" and "no laws" matter at exit 1
			fails := true
			if code == "0" && (c.name == "no layers" || c.name == "no laws object") {
				fails = false
			}
			judged(t, c.name+" at exit "+code, judge, dir, "ubuntu", code, fails)
		}
	}
}

func ptr(s string) *string { return &s }

// judged runs the judging step's script in dir and checks that it fails, with an
// error annotation, exactly when it should.
func judged(t *testing.T, name string, judge step, dir, leg, code string, fails bool) {
	t.Helper()
	cmd := exec.Command("bash", "-e", "-c", judge.Run)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GATE_EXIT="+code, "LEG="+leg)
	out, err := cmd.CombinedOutput()
	if (err != nil) != fails {
		t.Errorf("%s (%s leg, exit %q): failed = %v, want %v\n%s", name, leg, code, err != nil, fails, out)
		return
	}
	if fails && !strings.Contains(string(out), "::error::") {
		t.Errorf("%s: it failed without an error annotation\n%s", name, out)
	}
}
