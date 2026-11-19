package hygiene

import (
	"reflect"
	"testing"

	"github.com/Hedronite/go-tops/internal/gate"
)

// The layer folds its checks into one outcome and one reason (§3.1 gate 1). The
// ratchet re-folds the checks after it replaces one of them, and it must say what
// the layer would have said: Fold is the one function both call, and ChecksOf
// reads the checks back out of the details the layer wrote.

func foldSteps() map[string][]step {
	pass := func(name string) step { return step{name: name, outcome: gate.Pass} }
	return map[string][]step{
		"all pass":  {pass("go mod tidy -diff"), pass("go mod verify"), pass("golangci-lint run")},
		"one fails": {pass("go mod tidy -diff"), {name: "golangci-lint run", outcome: gate.Fail, message: "2 finding(s): revive 2"}, pass("govulncheck -json")},
		"two fail":  {{name: "go mod verify", outcome: gate.Fail, message: "mismatch"}, {name: "golangci-lint fmt --diff", outcome: gate.Fail, message: "a.go"}},
		"a tool is missing, another fails": {
			{name: "govulncheck -json", outcome: gate.ToolMissing, message: "govulncheck not found"},
			{name: "go mod verify", outcome: gate.Fail, message: "mismatch"},
		},
		"depguard last": {pass("golangci-lint run"), {name: "depguard enabled", outcome: gate.Fail, message: "not enabled"}},
	}
}

func TestFoldOfTheDetailsIsWhatTheLayerReports(t *testing.T) {
	for name, steps := range foldSteps() {
		h := &run{env: &gate.Env{}}
		res := h.result(steps)
		outcome, reason := Fold(ChecksOf(res.Details))
		if outcome != res.Outcome || reason != res.Reason {
			t.Errorf("%s: Fold(ChecksOf(details)) = %s %q, the layer said %s %q", name, outcome, reason, res.Outcome, res.Reason)
		}
	}
}

func TestFoldRules(t *testing.T) {
	checks := []Check{
		{Name: "a", Outcome: gate.Pass},
		{Name: "b", Outcome: gate.Fail, Message: "bad"},
		{Name: "c", Outcome: gate.ToolMissing, Message: "gone"},
	}
	outcome, reason := Fold(checks)
	if outcome != gate.ToolMissing || reason != "1 check(s) lack a tool: c: gone" {
		t.Errorf("a missing tool wins: %s %q", outcome, reason)
	}
	outcome, reason = Fold(checks[:2])
	if outcome != gate.Fail || reason != "1 of 2 checks failed: b: bad" {
		t.Errorf("a failure fails: %s %q", outcome, reason)
	}
	if outcome, reason = Fold(checks[:1]); outcome != gate.Pass || reason != "" {
		t.Errorf("all pass: %s %q", outcome, reason)
	}
	if outcome, _ = Fold(nil); outcome != gate.Pass {
		t.Errorf("no checks pass: %s", outcome)
	}
}

func TestChecksOfKeepsTheLayersOrderAndSkipsWhatIsAbsent(t *testing.T) {
	details := map[string]any{
		"lint":            map[string]any{"outcome": "fail", "message": "2 finding(s)"},
		"tidy":            map[string]any{"outcome": "pass"},
		"golangci_lint":   "v2.15.0", // not a check
		"findings":        2,         // not a check
		"vulnerabilities": "not a map",
	}
	got := ChecksOf(details)
	want := []Check{
		{Name: "go mod tidy -diff", Outcome: gate.Pass},
		{Name: "golangci-lint run", Outcome: gate.Fail, Message: "2 finding(s)"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ChecksOf = %+v, want %+v", got, want)
	}
}

// The ratchet reads details["lint"] by its shape: a map with the outcome, the
// finding count as an int, and the count by linter. It is pinned here, at the
// place that writes it.
func TestLintFiguresShape(t *testing.T) {
	res := lintResult{}
	res.Issues = append(res.Issues, lintIssue{FromLinter: "revive"}, lintIssue{FromLinter: "revive"}, lintIssue{FromLinter: "errcheck"})
	fig := lintFigures(res)
	if n, ok := fig["findings"].(int); !ok || n != 3 {
		t.Errorf("findings %v (%T), want the int 3", fig["findings"], fig["findings"])
	}
	by, ok := fig["by_linter"].(map[string]any)
	if !ok || by["revive"] != 2 || by["errcheck"] != 1 {
		t.Errorf("by_linter %v (%T)", fig["by_linter"], fig["by_linter"])
	}
	h := &run{env: &gate.Env{}}
	h.note("lint", fig)
	entry, ok := h.result([]step{{name: "golangci-lint run", outcome: gate.Fail, message: "3 finding(s)"}}).Details["lint"].(map[string]any)
	if !ok || entry["outcome"] != "fail" || entry["findings"] != 3 {
		t.Errorf("details[lint] = %v (%T)", entry, entry)
	}
}
