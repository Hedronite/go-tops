package crap

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	gotops "github.com/Hedronite/go-tops"
	"github.com/Hedronite/go-tops/internal/crap"
	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/layers/tests"
	"github.com/Hedronite/go-tops/internal/report"
)

const module = "example.com/m"

func fixture(t *testing.T, files map[string]string, profile ...string) *gate.Env {
	t.Helper()
	dir := t.TempDir()
	put := func(name, content string) {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	put("go.mod", "module "+module+"\n\ngo 1.26\n")
	for name, src := range files {
		put(name, src)
	}
	if profile != nil {
		put(tests.ProfilePath, "mode: atomic\n"+strings.Join(profile, "\n")+"\n")
	}
	return &gate.Env{Dir: dir, Config: gate.Config{
		Module: module,
		Crap:   gate.CrapConfig{Cap: 10, ErrGuardDiscount: true},
	}}
}

func line(file string, sl, sc, el, ec, stmts, count int) string {
	return module + "/" + file + ":" + itoa(sl) + "." + itoa(sc) + "," + itoa(el) + "." + itoa(ec) + " " + itoa(stmts) + " " + itoa(count)
}

func itoa(n int) string { return strings.TrimSpace(strings.Repeat(" ", 0) + fmtInt(n)) }

func fmtInt(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return fmtInt(n/10) + string(rune('0'+n%10))
}

// ifs is a function Name with n plain ifs: comp n+1, five lines of statements
// at the end of its body.
func ifs(name string, n int) string {
	return "func " + name + "(a bool) int {\n" + strings.Repeat("\tif a {\n\t\treturn 1\n\t}\n", n) + "\treturn 0\n}\n"
}

func run(env *gate.Env) gate.Result {
	return Layer{}.Run(context.Background(), env)
}

func validate(t *testing.T, r gate.Result) error {
	t.Helper()
	rep := report.Report{
		Protocol: report.Protocol, ToolVersion: "x", Module: "m", Class: "binary-cli", Enforce: "hard",
		Git: report.Git{Head: "a", Base: "b"}, MaxTier: 1, StartedAt: "2026-11-17T18:00:00Z",
		Layers:  []report.Layer{{ID: "crap", Tier: 1, Outcome: string(r.Outcome), Reason: r.Reason, Details: r.Details}},
		Summary: report.Summary{Outcome: "fail", ExitCode: 1, Enforced: true},
	}
	doc, err := report.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	for _, n := range []string{"core.schema.json", "go-tops.schema.json", "gate-report.schema.json"} {
		b, err := fs.ReadFile(gotops.Files(), "schema/"+n)
		if err != nil {
			t.Fatal(err)
		}
		v, err := jsonschema.UnmarshalJSON(strings.NewReader(string(b)))
		if err != nil {
			t.Fatal(err)
		}
		if err := c.AddResource("mem:///schema/"+n, v); err != nil {
			t.Fatal(err)
		}
	}
	sch, err := c.Compile("mem:///schema/gate-report.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	inst, err := jsonschema.UnmarshalJSON(strings.NewReader(string(doc)))
	if err != nil {
		t.Fatal(err)
	}
	return sch.Validate(inst)
}

func TestLayerIdentity(t *testing.T) {
	l := Layer{}
	if l.ID() != "crap" || l.Tier() != 1 || strings.Join(l.Needs(), ",") != "coverage" {
		t.Errorf("layer %s tier %d needs %v, want crap, tier 1, coverage", l.ID(), l.Tier(), l.Needs())
	}
}

// §6.2 under hard: a function over the cap fails the layer, and the report
// names it. Comp 11 fails at every coverage level, so it is fully covered here.
func TestOverTheCapFails(t *testing.T) {
	src := "package m\n\n" + ifs("Heavy", 10) + "\n" + ifs("Light", 2)
	env := fixture(t, map[string]string{"m.go": src},
		line("m.go", 3, 1, 35, 2, 40, 1), // Heavy: all covered
		line("m.go", 37, 1, 44, 2, 10, 1),
	)
	r := run(env)
	if r.Outcome != gate.Fail || !strings.Contains(r.Reason, module+".Heavy") {
		t.Fatalf("%s: %s", r.Outcome, r.Reason)
	}
	if err := validate(t, r); err != nil {
		t.Errorf("the failed layer's details do not validate: %v", err)
	}
}

func TestUnderTheCapPasses(t *testing.T) {
	env := fixture(t, map[string]string{"m.go": "package m\n\n" + ifs("Light", 2)},
		line("m.go", 3, 1, 10, 2, 10, 1))
	r := run(env)
	if r.Outcome != gate.Pass || r.Reason != "" {
		t.Fatalf("%s: %s", r.Outcome, r.Reason)
	}
	if err := validate(t, r); err != nil {
		t.Errorf("the passing layer's details do not validate: %v", err)
	}
}

// The layer's violations are the scorer's list filtered by CRAP > cap, one for
// one, in the scorer's order: the layer scores nothing itself (ruling C1).
func TestViolationsAreTheScorerListFilteredByTheCap(t *testing.T) {
	// Three functions near the cap: under it, at it only when covered fully,
	// and over it.
	src := "package m\n\n" + ifs("A", 9) + "\n" + ifs("B", 9) + "\n" + ifs("C", 12)
	// A: comp 10, fully covered (CRAP 10: meets). B: comp 10, one statement
	// uncovered (just over 10). C: comp 13, covered.
	env := fixture(t, map[string]string{"m.go": src},
		line("m.go", 3, 1, 32, 2, 40, 1),
		line("m.go", 34, 1, 50, 2, 39, 1),
		line("m.go", 51, 1, 63, 2, 1, 0),
		line("m.go", 65, 1, 103, 2, 44, 1),
	)
	rep, err := crap.Score(env.Dir, filepath.Join(env.Dir, filepath.FromSlash(tests.ProfilePath)), crap.Options{
		ErrGuardDiscount: true, Build: tests.Build,
	})
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, f := range rep.Measured {
		if f.CRAP > 10 {
			want = append(want, f.Key)
		}
	}
	r := run(env)
	list, ok := r.Details["violations"].([]any)
	if !ok {
		t.Fatalf("violations is %T", r.Details["violations"])
	}
	var got []string
	for _, v := range list {
		entry, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("a violation is %T", v)
		}
		name, ok := entry["func"].(string)
		if !ok {
			t.Fatalf("a violation's func is %T", entry["func"])
		}
		got = append(got, name)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("violations %v, want the scorer list over the cap %v", got, want)
	}
	if len(want) != 2 || r.Outcome != gate.Fail {
		t.Errorf("fixture: %d over the cap, outcome %s; want B and C over", len(want), r.Outcome)
	}
}

// Ruling C2: a function in a file the run's build does not compile is reported
// by count, not silently passed and not scored as uncovered.
func TestNotMeasuredIsReportedByCount(t *testing.T) {
	env := fixture(t, map[string]string{
		"m.go":      "package m\n\n" + ifs("Light", 2),
		"tagged.go": "//go:build integration\n\npackage m\n\n" + ifs("Tagged", 3),
	}, line("m.go", 3, 1, 10, 2, 10, 1))
	r := run(env)
	if r.Outcome != gate.Pass || r.Reason != "1 production function(s) not measured on this build" {
		t.Fatalf("%s: %q", r.Outcome, r.Reason)
	}
	if err := validate(t, r); err != nil {
		t.Errorf("details do not validate: %v", err)
	}
}

// Ruling C2b: a not-measured function above the cap fails like any function
// over it: at full coverage its score is its comp.
func TestNotMeasuredOverTheCapFails(t *testing.T) {
	env := fixture(t, map[string]string{
		"m.go":      "package m\n\n" + ifs("Light", 2),
		"tagged.go": "//go:build integration\n\npackage m\n\n" + ifs("Tagged", 11),
	}, line("m.go", 3, 1, 10, 2, 10, 1))
	r := run(env)
	if r.Outcome != gate.Fail || !strings.Contains(r.Reason, module+".Tagged") || !strings.Contains(r.Reason, "not measured") {
		t.Fatalf("%s: %s", r.Outcome, r.Reason)
	}
	if err := validate(t, r); err != nil {
		t.Errorf("details do not validate: %v", err)
	}
}

func TestNoProductionBlockFails(t *testing.T) {
	env := fixture(t, map[string]string{"m.go": "package m\n\n" + ifs("Light", 2)},
		line("other.go", 1, 1, 2, 2, 1, 1))
	r := run(env)
	if r.Outcome != gate.Fail || !strings.Contains(r.Reason, "no block of a production file") {
		t.Fatalf("%s: %s", r.Outcome, r.Reason)
	}
	if err := validate(t, r); err != nil {
		t.Errorf("the failure's details do not validate: %v", err)
	}
}

func TestMissingProfileFails(t *testing.T) {
	env := fixture(t, map[string]string{"m.go": "package m\n\n" + ifs("Light", 2)})
	r := run(env)
	if r.Outcome != gate.Fail || r.Details["error"] == nil {
		t.Fatalf("%s: %s %v", r.Outcome, r.Reason, r.Details)
	}
}
