package initcmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
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
	judged            int
	refused           []string // ids of laws with verdict refuse, required or not
	outcome           string   // summary.outcome; "" derives it from failed
	ran               int      // layer_counts.ran; 0 means every layer in range
	sameBuild         bool     // a ratchet skip on the ledger's own build
	offUntil          string   // expires of a skipped mutation layer; "" a future day, "-" none
	survived          int      // survived_unexplained of a mutation layer that passed
	noCounts          bool     // a mutation layer that passed without its details
}

func (g gateDoc) json(t *testing.T) string {
	t.Helper()
	layers := []map[string]any{}
	for _, id := range []string{"hygiene", "tests", "coverage", "crap", "mutation", "allocs"} {
		out := "pass"
		if id == "mutation" || id == "allocs" {
			out = "skip"
		}
		if o, ok := g.layers[id]; ok {
			out = o
		}
		layer := map[string]any{"id": id, "outcome": out}
		if id == "mutation" {
			g.mutationFields(layer)
		}
		layers = append(layers, layer)
	}
	outcome := g.outcome
	if outcome == "" {
		outcome = "pass"
		if len(g.failed) > 0 {
			outcome = "fail"
		}
	}
	ran := g.ran
	if ran == 0 {
		ran = len(layers)
	}
	doc := map[string]any{
		"enforce":      g.enforce,
		"layers":       layers,
		"layer_counts": map[string]any{"enabled_in_range": len(layers), "ran": ran},
		"summary":      map[string]any{"failed": append([]string{}, g.failed...), "outcome": outcome, "exit_code": g.reportedExit(outcome), "enforced": g.enforce != "advisory"},
		"laws":         map[string]any{"required": g.required, "required_not_evaluated": g.notEval, "judged": g.judged, "results": g.results()},
	}
	switch g.ratchet {
	case "":
	case "-":
		doc["ratchet"] = map[string]any{}
	default:
		doc["ratchet"] = map[string]any{"outcome": g.ratchet, "ledger_build": linuxBuild, "run_build": g.runBuild()}
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// mutationFields adds what the mutation layer carries for its outcome (GO_TOPS.md
// section 12.3): the date of an off switch, or the counts of a run.
func (g gateDoc) mutationFields(layer map[string]any) {
	switch layer["outcome"] {
	case "skip":
		if g.offUntil != "-" {
			until := g.offUntil
			if until == "" {
				until = "2099-01-01"
			}
			layer["expires"] = until
		}
	case "pass":
		if !g.noCounts {
			layer["details"] = map[string]any{"mutants": 3, "killed": 3 - g.survived, "survived_explained": 0, "survived_unexplained": g.survived}
		}
	}
}

// reportedExit is the summary exit_code of the report (GO_TOPS.md section 12.3): 1
// for a failed run that is enforced, and 0 for every other run.
func (g gateDoc) reportedExit(outcome string) int {
	if g.enforce != "advisory" && outcome == "fail" {
		return 1
	}
	return 0
}

var linuxBuild = map[string]any{"goos": "linux", "goarch": "amd64", "tags": []string{}, "cgo_enabled": true}

// runBuild is the build of the run: the ledger's, except for a ratchet skip, which
// is the report that the two differ (ruling C3b) unless the case says otherwise.
func (g gateDoc) runBuild() map[string]any {
	if g.ratchet == "skip" && !g.sameBuild {
		return map[string]any{"goos": "darwin", "goarch": "arm64", "tags": []string{}, "cgo_enabled": true}
	}
	return linuxBuild
}

// results lists one not-evaluated result and a refuse for each refused id.
func (g gateDoc) results() []map[string]string {
	rs := []map[string]string{{"id": "G-02", "verdict": "not-evaluated"}}
	for _, id := range g.refused {
		rs = append(rs, map[string]string{"id": id, "verdict": "refuse"})
	}
	return rs
}

// c9 is a gate report of the passing state at M2/G2: the ratchet passes and the
// required laws, none evaluated, are the only failed item.
func c9() gateDoc {
	return gateDoc{enforce: "ratchet", ratchet: "pass", failed: []string{"laws"}, required: 16, notEval: 7, judged: 9}
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

// judgeCase is one report, the leg and exit code the judge runs it under, and
// whether the judge fails it.
type judgeCase struct {
	name, leg, code string
	doc             gateDoc
	fails           bool
}

// judgeCases are the verdicts ruling C9 gives. The go-tops-gate skill judges the
// same reports, and a test under the skills tag holds the two to each other.
func judgeCases() []judgeCase {
	clean := gateDoc{enforce: "ratchet", ratchet: "pass"}
	return []judgeCase{
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
		{"too many required laws still not-evaluated", "ubuntu", "1", c9().with(func(g *gateDoc) { g.notEval = 8 }), true},
		// ruling C9 allows the required laws not evaluated and nothing else: a law that
		// is refused, required or not, is not that, whatever else the report says
		{"an advisory law refused", "ubuntu", "1", c9().with(func(g *gateDoc) { g.refused = []string{"G-30"} }), true},
		{"G-41 refused", "macos", "1", c9().with(func(g *gateDoc) { g.refused = []string{"G-41"} }), true},
		{"a required law refused", "ubuntu", "1", c9().with(func(g *gateDoc) { g.refused = []string{"G-01"} }), true},
		{"a law was judged", "ubuntu", "1", c9().with(func(g *gateDoc) { g.judged = 1 }), true},
		{"a law was judged and refused", "ubuntu", "1", c9().with(func(g *gateDoc) { g.judged, g.refused = 1, []string{"G-41"} }), true},
		{"exit 0 with a refused law is the gate's verdict, not this step's", "ubuntu", "0", clean.with(func(g *gateDoc) { g.refused = []string{"G-41"} }), false},
		{"no required laws, so exit 1 is something else", "ubuntu", "1", c9().with(func(g *gateDoc) { g.required, g.notEval = 0, 0 }), true},
		// the report is read at every exit code (GO_TOPS.md section 3.3)
		{"exit 0 from a partial run", "ubuntu", "0", clean.with(func(g *gateDoc) { g.outcome = "partial" }), true},
		{"exit 0 with a layer that did not run", "ubuntu", "0", clean.with(func(g *gateDoc) { g.ran = 4 }), true},
		{"advisory exit 0 with a failing layer", "ubuntu", "0", gateDoc{enforce: "advisory", layers: map[string]string{"crap": "fail"}, failed: []string{"crap"}}, true},
		{"advisory exit 0 with the required laws alone", "ubuntu", "0", gateDoc{enforce: "advisory", failed: []string{"laws"}, required: 16, notEval: 7, judged: 9}, false},
		{"advisory exit 0 with a refused law", "ubuntu", "0", gateDoc{enforce: "advisory", failed: []string{"laws"}, required: 16, notEval: 7, judged: 9, refused: []string{"G-41"}}, true},
		{"exit 0 with a failed layer the report names", "macos", "0", clean.with(func(g *gateDoc) { g.layers = map[string]string{"tests": "fail"}; g.failed = []string{"tests"} }), true},
		// the exit code the gate recorded is the one its report says (R15)
		{"exit 0 while the report says exit 1 and fails the laws", "ubuntu", "0", c9(), true},
		{"exit 0 while the report says exit 1, on the other leg", "macos", "0", c9(), true},
		{"exit 1 while the report says exit 0", "ubuntu", "1", c9().with(func(g *gateDoc) { g.failed = nil }), true},
		// a ratchet skip is judged by build (C3b)
		{"a skip on another build", "macos", "0", clean.with(func(g *gateDoc) { g.ratchet = "skip" }), false},
		{"a skip on the ledger's own build", "macos", "0", clean.with(func(g *gateDoc) { g.ratchet = "skip"; g.sameBuild = true }), true},
		{"C9 with a skip on the ledger's own build", "macos", "1", c9().with(func(g *gateDoc) { g.ratchet = "skip"; g.sameBuild = true }), true},
		// the mutation layer (section 6.4): pass or vacuous, and off only until a day
		{"mutation pass with no unexplained survivor", "ubuntu", "1", c9().with(func(g *gateDoc) { g.layers = map[string]string{"mutation": "pass"} }), false},
		{"mutation vacuous", "ubuntu", "1", c9().with(func(g *gateDoc) { g.layers = map[string]string{"mutation": "vacuous"} }), false},
		{"mutation vacuous at exit 0", "macos", "0", clean.with(func(g *gateDoc) { g.layers = map[string]string{"mutation": "vacuous"} }), false},
		{"vacuous on another layer", "ubuntu", "1", c9().with(func(g *gateDoc) { g.layers = map[string]string{"crap": "vacuous"} }), true},
		{"mutation pass that counts an unexplained survivor", "ubuntu", "1", c9().with(func(g *gateDoc) { g.layers = map[string]string{"mutation": "pass"}; g.survived = 1 }), true},
		{"mutation pass without its counts", "ubuntu", "1", c9().with(func(g *gateDoc) { g.layers = map[string]string{"mutation": "pass"}; g.noCounts = true }), true},
		{"mutation failed on a survivor", "ubuntu", "1", c9().with(func(g *gateDoc) {
			g.layers = map[string]string{"mutation": "fail"}
			g.failed = []string{"mutation", "laws"}
		}), true},
		{"mutation off with no date", "ubuntu", "1", c9().with(func(g *gateDoc) { g.offUntil = "-" }), true},
		{"mutation off past its date", "ubuntu", "1", c9().with(func(g *gateDoc) { g.offUntil = "2000-01-01" }), true},
		{"mutation off until today", "macos", "1", c9().with(func(g *gateDoc) { g.offUntil = time.Now().UTC().Format(time.DateOnly) }), true},
		{"mutation off until a word", "ubuntu", "1", c9().with(func(g *gateDoc) { g.offUntil = "never" }), true},
		// any other exit code fails directly, whatever the report says
		{"exit 2", "ubuntu", "2", c9(), true},
		{"exit 3", "macos", "3", clean, true},
		{"exit 4", "ubuntu", "4", clean, true},
		{"an exit code that is not a number", "ubuntu", "x", clean, true},
		{"no exit code: a step before the gate failed", "ubuntu", "", clean, true},
		{"no exit code: a step before the gate failed, laws only", "macos", "", c9(), true},
	}
}

// The judging script, run against reports for each leg and exit code. Passing is
// ruling C9; everything else fails closed with an error annotation.
func TestTheJudgeAppliesC9OnEveryLegAndFailsClosed(t *testing.T) {
	needTools(t)
	_, judge := judgeSteps(t)
	for _, c := range judgeCases() {
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
		{"no law results", ptr(`{"enforce":"ratchet","ratchet":{"outcome":"pass"},"layers":[],"summary":{"failed":["laws"]},"laws":{"required":16,"required_not_evaluated":16,"judged":0}}`)},
		{"no judged count", ptr(`{"enforce":"ratchet","ratchet":{"outcome":"pass"},"layers":[],"summary":{"failed":["laws"]},"laws":{"required":16,"required_not_evaluated":16,"results":[]}}`)},
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
			// the report is read at exit 0 as well as at exit 1 (GO_TOPS.md section 3.3)
			judged(t, c.name+" at exit "+code, judge, dir, "ubuntu", code, true)
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

// A refresh that changes nothing says so in one line: the same ledger is
// uploaded. A refresh that writes the file prints nothing of its own.
func TestRefreshSaysWhenNothingChanged(t *testing.T) {
	needTools(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := newModule(t, "github.com/acme/demo", "1.26")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	steps := workflowSteps(t, dir)
	i := indexOf2(steps, func(s step) bool { return s.Name == "refresh the ledger" })
	if i < 0 {
		t.Fatal("no refresh step")
	}
	for _, c := range []struct {
		name, fake string
		line       bool
	}{
		{"nothing to lower", "exit 0", true},
		{"an entry lowered", `echo lowered >> .go-tops/baseline.json`, false},
	} {
		repo := t.TempDir()
		bin := filepath.Join(repo, "bin")
		for _, d := range []string{bin, filepath.Join(repo, ".go-tops")} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(bin, "go"), []byte("#!/bin/sh\n"+c.fake+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, ".go-tops", "baseline.json"), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "ledger"}} {
			cmd := exec.Command("git", args...)
			cmd.Dir = repo
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
		cmd := exec.Command("bash", "-e", "-c", steps[i].Run)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Errorf("%s: the step failed: %v\n%s", c.name, err, out)
		}
		if got := strings.Contains(string(out), "unchanged: same ledger uploaded"); got != c.line {
			t.Errorf("%s: prints the unchanged line = %v, want %v\n%s", c.name, got, c.line, out)
		}
	}
}

// The failure of a gate that did not give a verdict names its cause from the exit
// code and the state `baseline --status` recorded, not one hint for all of them.
func TestTheJudgeNamesTheCauseOfAGateThatGaveNoVerdict(t *testing.T) {
	needTools(t)
	_, judge := judgeSteps(t)
	if judge.Env["STATUS_STATE"] != "${{ steps.status.outputs.state }}" {
		t.Fatalf("the judge reads the recorded status state: %v", judge.Env)
	}
	for _, c := range []struct {
		name, code, state string
		want, notWant     []string
	}{
		{"a deleted ledger", "2", "deleted", []string{"deleting the ledger is not a reset", "restore"}, []string{"go-tops-baseline"}},
		{"a configuration or custody error", "2", "present", []string{"configuration", "lock"}, []string{"go-tops-baseline", "first ledger"}},
		{"no status recorded (a push)", "2", "", []string{"configuration"}, []string{"go-tops-baseline", "first ledger"}},
		{"an environment error", "3", "present", []string{"environment"}, []string{"go-tops-baseline", "first ledger"}},
		{"refused in CI", "4", "present", []string{"refused in CI"}, []string{"go-tops-baseline", "first ledger"}},
		{"a code the protocol does not define", "64", "present", []string{"64"}, []string{"go-tops-baseline"}},
	} {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, ".go-tops"), 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("bash", "-e", "-c", judge.Run)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GATE_EXIT="+c.code, "LEG=ubuntu", "STATUS_STATE="+c.state)
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Errorf("%s: the judge passed", c.name)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(string(out), w) {
				t.Errorf("%s: the message lacks %q:\n%s", c.name, w, out)
			}
		}
		for _, w := range c.notWant {
			if strings.Contains(string(out), w) {
				t.Errorf("%s: the message has %q, which is not its cause:\n%s", c.name, w, out)
			}
		}
	}
}

// A ratchet skip on the ledger leg does not send the adopter to a manual run that
// cannot start before the workflow is on the default branch.
func TestTheJudgeSaysWhereTheFirstLedgerComesFromOnASkip(t *testing.T) {
	needTools(t)
	_, judge := judgeSteps(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".go-tops"), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := c9().with(func(g *gateDoc) { g.ratchet = "skip" })
	if err := os.WriteFile(filepath.Join(dir, ".go-tops", "report.json"), []byte(doc.json(t)), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-e", "-c", judge.Run)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GATE_EXIT=1", "LEG=ubuntu", "STATUS_STATE=present")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("a skip on the ledger leg passed")
	}
	for _, w := range []string{"pull-request run", "after the workflow is on the default branch"} {
		if !strings.Contains(string(out), w) {
			t.Errorf("the message lacks %q:\n%s", w, out)
		}
	}
}
