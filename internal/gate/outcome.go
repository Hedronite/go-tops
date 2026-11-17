// Package gate is the runner behind `gotops gate` (GO_TOPS.md §3.1 to §3.5,
// §12.2): it plans a range of layers, runs each through os/exec argv steps,
// judges the laws last, writes .go-tops/report.json and decides the exit code.
package gate

import "fmt"

// Outcome is what one layer did in one run (§3.3). The values are the
// outcome enum of schema/core.schema.json.
type Outcome string

// The seven layer outcomes.
const (
	Pass        Outcome = "pass"
	Fail        Outcome = "fail"
	Skip        Outcome = "skip"
	NotRun      Outcome = "not-run"
	NotReached  Outcome = "not-reached"
	Vacuous     Outcome = "vacuous"
	ToolMissing Outcome = "tool-missing"
)

// RunOutcome is the outcome of a whole run (§3.3), in the report summary.
type RunOutcome string

// The three run outcomes.
const (
	RunPass    RunOutcome = "pass"
	RunFail    RunOutcome = "fail"
	RunPartial RunOutcome = "partial"
)

// Outcomes lists every layer outcome.
func Outcomes() []Outcome {
	return []Outcome{Pass, Fail, Skip, NotRun, NotReached, Vacuous, ToolMissing}
}

// valid reports whether o is one of the seven.
func (o Outcome) valid() bool {
	for _, v := range Outcomes() {
		if o == v {
			return true
		}
	}
	return false
}

// fails reports whether the outcome fails the gate (§3.3 table).
func (o Outcome) fails() bool {
	switch o {
	case Fail, NotRun, NotReached, ToolMissing:
		return true
	}
	return false
}

// ran reports whether the layer ran: pass, fail or vacuous (§3.5).
func (o Outcome) ran() bool { return o == Pass || o == Fail || o == Vacuous }

// Result is what a layer returns from Run.
type Result struct {
	Outcome Outcome
	// Reason explains a skip, a not-reached or a tool-missing. A skip needs one.
	Reason string
	// Details are the layer's figures. The report schema types them for tests,
	// coverage, crap and mutation, and requires them when the layer ran.
	Details map[string]any
}

// NoCommand is the result of a layer that is enabled and in range but has no
// command mapped (§3.3): it fails the gate.
func NoCommand(layer string) Result {
	return Result{Outcome: NotRun, Reason: fmt.Sprintf("the %s layer has no command mapped yet", layer)}
}

// MissingTool is the result of a layer whose mapped tool could not be found
// (§3.3, §12.2): tool-missing, which fails the gate with exit 1.
func MissingTool(name string, err error) Result {
	return Result{Outcome: ToolMissing, Reason: fmt.Sprintf("tool %s not found: %v", name, err)}
}
