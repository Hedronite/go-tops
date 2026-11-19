package crap

import (
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/report"
)

// The oracle is GO_TOPS.md §6.8: an exemption takes covered functions out of
// the cap check, in the open and for a limited time. They are still scored and
// listed under crapDetails.exempt; an entry that matches nothing fails, naming
// the entry; a table-switch entry holds only while the function is a table.

const far = "2999-01-01" // an expiry the run date never reaches

func exemption(mod func(*report.Exemption)) report.Exemption {
	e := report.Exemption{Path: "m.go", Layer: "crap", Reason: "dispatch table, one call per arm", Owner: "platform", Expires: far}
	if mod != nil {
		mod(&e)
	}
	return e
}

// heavy is a function of complexity 11, fully covered: it fails the cap of 10 at
// every coverage. Its span is lines 3 to 35 of a file that starts with the
// package clause.
func heavyEnv(t *testing.T, es ...report.Exemption) *gate.Env {
	t.Helper()
	env := fixture(t, map[string]string{"m.go": "package m\n\n" + ifs("Heavy", 10) + "\n" + ifs("Light", 2)},
		line("m.go", 3, 1, 35, 2, 40, 1),
		line("m.go", 37, 1, 44, 2, 10, 1))
	env.Config.Exemptions = es
	return env
}

func rows(t *testing.T, r gate.Result, key string) []map[string]any {
	t.Helper()
	list, ok := r.Details[key].([]any)
	if !ok {
		if r.Details[key] == nil {
			return nil
		}
		t.Fatalf("%s is %T", key, r.Details[key])
	}
	var out []map[string]any
	for _, v := range list {
		row, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("a row of %s is %T", key, v)
		}
		out = append(out, row)
	}
	return out
}

func TestExemptFunctionIsScoredListedAndNotJudged(t *testing.T) {
	r := run(heavyEnv(t, exemption(func(e *report.Exemption) { e.Symbol = "Heavy" })))
	if r.Outcome != gate.Pass {
		t.Fatalf("%s: %s", r.Outcome, r.Reason)
	}
	if v := rows(t, r, "violations"); len(v) != 0 {
		t.Errorf("an exempt function was judged against the cap: %v", v)
	}
	ex := rows(t, r, "exempt")
	if len(ex) != 1 || ex[0]["func"] != module+".Heavy" || ex[0]["cc"] != 11 || ex[0]["crap"] != 11.0 || ex[0]["cov_pct"] != 100.0 {
		t.Fatalf("exempt rows %v: want Heavy, scored (comp 11, 100%% covered, CRAP 11)", ex)
	}
	if err := validate(t, r); err != nil {
		t.Errorf("details do not validate: %v", err)
	}
}

func TestExemptionLeavesOtherFunctionsToTheCap(t *testing.T) {
	src := "package m\n\n" + ifs("A", 10) + "\n" + ifs("B", 10)
	env := fixture(t, map[string]string{"m.go": src},
		line("m.go", 3, 1, 35, 2, 40, 1),
		line("m.go", 37, 1, 69, 2, 40, 1))
	env.Config.Exemptions = []report.Exemption{exemption(func(e *report.Exemption) { e.Symbol = "A" })}
	r := run(env)
	v, ex := rows(t, r, "violations"), rows(t, r, "exempt")
	if r.Outcome != gate.Fail || len(v) != 1 || v[0]["func"] != module+".B" || len(ex) != 1 || ex[0]["func"] != module+".A" {
		t.Fatalf("%s: violations %v, exempt %v; want B judged and A exempt", r.Outcome, v, ex)
	}
}

// §6.8: a path that matches no production file, or a symbol that names no
// function in the files it matches, fails, with the entry named. Nothing over
// the cap is needed for that.
func TestUnmatchedExemptionFailsNamingTheEntry(t *testing.T) {
	for _, c := range []struct {
		name string
		mod  func(*report.Exemption)
		want []string
	}{
		{"path matches nothing", func(e *report.Exemption) { e.Path = "gone/**" }, []string{"gone/**", "no production file"}},
		{"symbol names nothing", func(e *report.Exemption) { e.Symbol = "Gone" }, []string{"Gone", "m.go", "names no function"}},
	} {
		env := fixture(t, map[string]string{"m.go": "package m\n\n" + ifs("Light", 2)}, line("m.go", 3, 1, 10, 2, 10, 1))
		env.Config.Exemptions = []report.Exemption{exemption(c.mod)}
		r := run(env)
		if r.Outcome != gate.Fail {
			t.Errorf("%s: %s, want fail", c.name, r.Outcome)
		}
		for _, w := range c.want {
			if !strings.Contains(r.Reason, w) {
				t.Errorf("%s: reason %q lacks %q", c.name, r.Reason, w)
			}
		}
		if err := validate(t, r); err != nil {
			t.Errorf("%s: details do not validate: %v", c.name, err)
		}
	}
}

// An entry past its date exempts nothing (tier 0 fails it by name), and an
// entry of the mutation layer is M4's: neither takes a function off the cap.
func TestExpiredAndMutationEntriesExemptNothing(t *testing.T) {
	for _, mod := range []func(*report.Exemption){
		func(e *report.Exemption) { e.Symbol, e.Expires = "Heavy", "2020-01-01" },
		func(e *report.Exemption) { e.Symbol, e.Layer = "Heavy", "mutation" },
	} {
		r := run(heavyEnv(t, exemption(mod)))
		if v := rows(t, r, "violations"); r.Outcome != gate.Fail || len(v) != 1 {
			t.Errorf("%s, violations %v: Heavy must answer to the cap", r.Outcome, v)
		}
		if ex := rows(t, r, "exempt"); len(ex) != 0 {
			t.Errorf("exempt rows %v", ex)
		}
	}
}

// table builds a dispatch function of n arms and a default: complexity n+1.
// With nested set, its last arm makes two calls, which §6.8 says is no longer
// a table. Its span is lines 3 to 2n+8.
func table(name string, n int, nested bool) string {
	var b strings.Builder
	b.WriteString("func " + name + "(op int) int {\n\tswitch op {\n")
	for i := 0; i < n; i++ {
		ret := "return " + itoa(i)
		if nested && i == n-1 {
			ret = "return one(one(op))"
		}
		b.WriteString("\tcase " + itoa(i) + ":\n\t\t" + ret + "\n")
	}
	b.WriteString("\tdefault:\n\t\treturn -1\n\t}\n}\n")
	return b.String()
}

func tableEnv(t *testing.T, n int, nested bool) *gate.Env {
	t.Helper()
	src := "package m\n\nfunc one(a int) int { return a }\n\n" + table("Dispatch", n, nested)
	end := 5 + 2*n + 5
	env := fixture(t, map[string]string{"m.go": src},
		line("m.go", 3, 1, 3, 30, 1, 1),
		line("m.go", 5, 1, end, 2, 3*n, 1))
	env.Config.Exemptions = []report.Exemption{exemption(func(e *report.Exemption) { e.Symbol, e.Shape = "Dispatch", "table-switch" })}
	return env
}

// §6.8: a table-shaped switch of complexity 13 takes the exemption, and an arm
// that grows a second call takes the function back to the cap.
func TestTableSwitchExemptionHoldsOnlyWhileTheFunctionIsATable(t *testing.T) {
	r := run(tableEnv(t, 12, false))
	if ex := rows(t, r, "exempt"); r.Outcome != gate.Pass || len(ex) != 1 || ex[0]["cc"] != 13 {
		t.Fatalf("a table: %s %q exempt %v; want pass with Dispatch (comp 13) exempt", r.Outcome, r.Reason, ex)
	}
	r = run(tableEnv(t, 12, true))
	v, ex := rows(t, r, "violations"), rows(t, r, "exempt")
	if r.Outcome != gate.Fail || len(v) != 1 || v[0]["func"] != module+".Dispatch" || len(ex) != 0 {
		t.Fatalf("not a table: %s, violations %v, exempt %v; want Dispatch judged against the cap", r.Outcome, v, ex)
	}
	if !strings.Contains(r.Reason, "Dispatch") || !strings.Contains(r.Reason, "table") {
		t.Errorf("reason %q should say the function is no longer a table", r.Reason)
	}
}

// A function that lost its shape and is under the cap passes, and the text
// still says the exemption no longer holds.
func TestLostShapeIsSaidEvenUnderTheCap(t *testing.T) {
	r := run(tableEnv(t, 5, true))
	if r.Outcome != gate.Pass || !strings.Contains(r.Reason, "Dispatch") || !strings.Contains(r.Reason, "table") {
		t.Fatalf("%s: %q", r.Outcome, r.Reason)
	}
}

// §6.8 and ruling C2: a function in a file the run does not compile is covered
// like any other, and its row has null cov_pct and crap.
func TestNotMeasuredFunctionCanBeExempt(t *testing.T) {
	files := map[string]string{
		"m.go":         "package m\n\n" + ifs("Light", 2),
		"x_windows.go": "package m\n\n" + ifs("Win", 11),
	}
	env := fixture(t, files, line("m.go", 3, 1, 10, 2, 10, 1))
	r := run(env)
	if v := rows(t, r, "violations"); r.Outcome != gate.Fail || len(v) != 1 {
		t.Fatalf("without an exemption: %s, violations %v; want Win judged by its complexity", r.Outcome, v)
	}
	env.Config.Exemptions = []report.Exemption{exemption(func(e *report.Exemption) { e.Path, e.Symbol = "x_windows.go", "Win" })}
	r = run(env)
	ex := rows(t, r, "exempt")
	if r.Outcome != gate.Pass || len(ex) != 1 || ex[0]["func"] != module+".Win" || ex[0]["cov_pct"] != nil || ex[0]["crap"] != nil {
		t.Fatalf("%s %q, exempt %v; want Win exempt with null cov_pct and crap", r.Outcome, r.Reason, ex)
	}
	if r.Details["not_measured"] != 1 {
		t.Errorf("not_measured = %v", r.Details["not_measured"])
	}
	if err := validate(t, r); err != nil {
		t.Errorf("details do not validate: %v", err)
	}
}

// With no exemption the report says so: an absent list and an empty one are
// the same, and the schema accepts either.
func TestNoExemptionLeavesNoExemptRows(t *testing.T) {
	r := run(heavyEnv(t))
	if ex := rows(t, r, "exempt"); len(ex) != 0 {
		t.Errorf("exempt rows %v", ex)
	}
	if err := validate(t, r); err != nil {
		t.Errorf("details do not validate: %v", err)
	}
}
