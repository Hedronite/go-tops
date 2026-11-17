package config

import (
	"encoding/json"
	"slices"
	"testing"
)

// The oracle is GO_TOPS.md §5.1 to §5.3 (the class column, the profiles, a
// class with no column takes 8 and 90), §9 (the keys) and §12.1 (a config may
// tighten a profile and never loosen it): after Effective, no key the gate
// reads is absent, and an absent cap or floor never reads as "none".

var launch = []string{"binary-cli", "binary-service", "library-core"}

// classes the schema permits that have no profile; tui-app and
// concurrency-runtime have a column in §5.2, the others have none.
var profileless = []string{"tui-app", "concurrency-runtime", "library-util", "parser-codec", "unsafe-kernel", "cgo-bridge", "codegen", "embedded-tinygo", "workspace-root"}

func eff(t *testing.T, cfg *Config) *Config {
	t.Helper()
	e, err := Effective(cfg)
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	return e
}

// resolved lists every key of the gate that must hold a value after Effective,
// each as a name and whether it is set.
func resolved(e *Config) map[string]bool {
	g, l := e.Gates, e.Layers
	return map[string]bool{
		"enforce":                     e.Enforce != "",
		"baseline":                    e.Baseline != "",
		"report.path":                 e.Report.Path != "",
		"gates.hygiene.golangci_lint": g.Hygiene.GolangciLint.Config != "",
		"gates.hygiene.fail_on":       g.Hygiene.Govulncheck.FailOn != "",
		"gates.hygiene.depguard":      g.Hygiene.Deps.Depguard != nil,
		"gates.hygiene.licenses":      len(g.Hygiene.Deps.Licenses.Allow) > 0,
		"gates.tests.race":            g.Tests.Race != nil,
		"gates.tests.shuffle":         g.Tests.Shuffle != nil,
		"gates.tests.count":           g.Tests.Count != nil,
		"gates.coverage.floor":        g.Coverage.StatementsFloor != nil,
		"gates.coverage.generated":    g.Coverage.ExcludeGenerated != nil,
		"gates.coverage.integration":  g.Coverage.Integration.Enabled != nil,
		"gates.crap.enabled":          g.Crap.Enabled != nil,
		"gates.crap.cap":              g.Crap.Cap != nil,
		"gates.crap.err_guard":        g.Crap.Complexity.ErrGuardDiscount != nil,
		"gates.mutation.enabled":      g.Mutation.Enabled != nil,
		"gates.mutation.engine":       g.Mutation.Engine != "",
		"gates.mutation.base":         g.Mutation.Base != "",
		"gates.mutation.survivors":    g.Mutation.UnexplainedSurvivorsMax != nil,
		"gates.mutation.ledger":       g.Mutation.Ledger != "",
		"layers.fuzz.enabled":         l.Fuzz.Enabled != nil,
		"layers.stress.enabled":       l.Stress.Enabled != nil,
		"layers.bench.enabled":        l.Bench.Enabled != nil,
		"laws.ack_ledger":             e.Laws.AckLedger != "",
	}
}

// §5.2: every class gets its cap and floor, and nothing reads as none.
func TestEffectiveCapAndFloorByClass(t *testing.T) {
	cases := map[string]struct{ cap, floor float64 }{
		"binary-cli":          {10, 70},
		"binary-service":      {10, 75},
		"library-core":        {8, 85},
		"tui-app":             {8, 65},
		"concurrency-runtime": {8, 90},
		// no column: the strictest cap and the strictest floor
		"library-util": {8, 90}, "parser-codec": {8, 90}, "unsafe-kernel": {8, 90}, "cgo-bridge": {8, 90},
		"codegen": {8, 90}, "embedded-tinygo": {8, 90}, "workspace-root": {8, 90},
	}
	for class, want := range cases {
		e := eff(t, &Config{Class: class})
		if got := *e.Gates.Crap.Cap; got != want.cap {
			t.Errorf("%s: cap %v, want %v", class, got, want.cap)
		}
		if got := *e.Gates.Coverage.StatementsFloor; got != want.floor {
			t.Errorf("%s: floor %v, want %v", class, got, want.floor)
		}
	}
	if len(cases) != len(launch)+len(profileless) {
		t.Fatalf("the table covers %d classes, the schema has %d", len(cases), len(launch)+len(profileless))
	}
}

// Every gate key resolves for each launch class and for a class with no column.
func TestEveryGateKeyResolves(t *testing.T) {
	for _, class := range append(slices.Clone(launch), profileless...) {
		e := eff(t, &Config{Class: class})
		for key, ok := range resolved(e) {
			if !ok {
				t.Errorf("%s: %s is absent after Effective", class, key)
			}
		}
		// a launch class has the 16 laws of its profile; any other class has none of its own
		if has, want := len(e.Laws.Required), slices.Contains(launch, class); (has > 0) != want {
			t.Errorf("%s: %d required laws", class, has)
		}
	}
}

// The values the protocol fixes: the test run of §3.1, the survivors rule of §6.4.
func TestEffectiveFixedValues(t *testing.T) {
	for _, class := range launch {
		e := eff(t, &Config{Class: class})
		g := e.Gates
		if !*g.Tests.Race || !*g.Tests.Shuffle || *g.Tests.Count != 1 {
			t.Errorf("%s: tests %v %v %v, want true true 1", class, *g.Tests.Race, *g.Tests.Shuffle, *g.Tests.Count)
		}
		if *g.Mutation.UnexplainedSurvivorsMax != 0 {
			t.Errorf("%s: unexplained_survivors_max %d, want 0", class, *g.Mutation.UnexplainedSurvivorsMax)
		}
		if g.Hygiene.Govulncheck.FailOn != "reachable" || !*g.Crap.Complexity.ErrGuardDiscount || !*g.Coverage.ExcludeGenerated {
			t.Errorf("%s: %+v", class, g)
		}
		if e.Enforce != "ratchet" {
			t.Errorf("%s: enforce %q, adopters default to ratchet (§6.7)", class, e.Enforce)
		}
	}
}

// A profile that turns a layer off says why, and Effective keeps both.
func TestEffectiveKeepsTheProfilesOffSwitch(t *testing.T) {
	e := eff(t, &Config{Class: "binary-cli"})
	if *e.Layers.Bench.Enabled || e.Layers.Bench.Reason == "" {
		t.Fatalf("binary-cli bench: enabled %v reason %q, the profile turns it off with a reason", *e.Layers.Bench.Enabled, e.Layers.Bench.Reason)
	}
	e = eff(t, &Config{Class: "binary-service"})
	if !*e.Layers.Bench.Enabled || e.Layers.Bench.RegressionPct == nil || *e.Layers.Bench.RegressionPct != 5 {
		t.Fatalf("binary-service bench: %+v", e.Layers.Bench)
	}
	// the config's own switch wins
	off := false
	cfg := &Config{Class: "binary-service"}
	cfg.Layers.Bench.Enabled, cfg.Layers.Bench.Reason = &off, "no benchmarks in this service"
	e = eff(t, cfg)
	if *e.Layers.Bench.Enabled || e.Layers.Bench.Reason != "no benchmarks in this service" {
		t.Fatalf("%+v", e.Layers.Bench)
	}
}

// A config may tighten a profile and never loosen it (§12.1): Effective takes
// the stricter, so a config that slipped past check cannot loosen the gate.
func TestEffectiveTightensAndNeverLoosens(t *testing.T) {
	cfg := &Config{Class: "binary-cli"}
	cfg.Gates.Crap.Cap, cfg.Gates.Coverage.StatementsFloor = ptr(6.0), ptr(80.0)
	e := eff(t, cfg)
	if *e.Gates.Crap.Cap != 6 || *e.Gates.Coverage.StatementsFloor != 80 {
		t.Errorf("tighter values lost: cap %v floor %v", *e.Gates.Crap.Cap, *e.Gates.Coverage.StatementsFloor)
	}
	cfg.Gates.Crap.Cap, cfg.Gates.Coverage.StatementsFloor = ptr(12.0), ptr(50.0)
	e = eff(t, cfg)
	if *e.Gates.Crap.Cap != 10 || *e.Gates.Coverage.StatementsFloor != 70 {
		t.Errorf("looser values got through: cap %v floor %v, want 10 and 70", *e.Gates.Crap.Cap, *e.Gates.Coverage.StatementsFloor)
	}
	// a class with no profile cannot be loosened either
	cfg = &Config{Class: "parser-codec"}
	cfg.Gates.Crap.Cap, cfg.Gates.Coverage.StatementsFloor = ptr(12.0), ptr(50.0)
	e = eff(t, cfg)
	if *e.Gates.Crap.Cap != 8 || *e.Gates.Coverage.StatementsFloor != 90 {
		t.Errorf("parser-codec: cap %v floor %v, want 8 and 90", *e.Gates.Crap.Cap, *e.Gates.Coverage.StatementsFloor)
	}
	// the bench threshold only tightens
	cfg = &Config{Class: "binary-service"}
	cfg.Layers.Bench.RegressionPct = ptr(9.0)
	if got := *eff(t, cfg).Layers.Bench.RegressionPct; got != 5 {
		t.Errorf("regression_pct %v, want the profile's 5", got)
	}
	cfg.Layers.Bench.RegressionPct = ptr(2.0)
	if got := *eff(t, cfg).Layers.Bench.RegressionPct; got != 2 {
		t.Errorf("regression_pct %v, want the tighter 2", got)
	}
}

// §3.3: go-tops.yaml may add laws and never remove one: the profile's laws
// come first, then the config's additions.
func TestEffectiveLawsRequiredIsAUnion(t *testing.T) {
	prof, err := ProfileFor("binary-cli")
	if err != nil {
		t.Fatal(err)
	}
	e := eff(t, &Config{Class: "binary-cli"})
	if !slices.Equal(e.Laws.Required, prof.Laws.Required) {
		t.Errorf("no key: %v, want the profile's %v", e.Laws.Required, prof.Laws.Required)
	}
	cfg := &Config{Class: "binary-cli"}
	cfg.Laws.Required = []string{"G-41", "G-02"} // one addition, one the profile has
	e = eff(t, cfg)
	if len(e.Laws.Required) != len(prof.Laws.Required)+1 || e.Laws.Required[len(e.Laws.Required)-1] != "G-41" {
		t.Errorf("%v", e.Laws.Required)
	}
	// a config that lists fewer cannot remove: the profile's laws stay
	cfg.Laws.Required = []string{"G-41"}
	for _, id := range prof.Laws.Required {
		if !slices.Contains(eff(t, cfg).Laws.Required, id) {
			t.Errorf("%s was removed", id)
		}
	}
}

// A class with no profile has no required laws of its own; the config states them.
func TestEffectiveForAClassWithNoProfile(t *testing.T) {
	cfg := &Config{Class: "parser-codec"}
	cfg.Laws.Required = []string{"G-02"}
	e := eff(t, cfg)
	if !slices.Equal(e.Laws.Required, []string{"G-02"}) {
		t.Errorf("%v", e.Laws.Required)
	}
}

func TestEffectiveKeepsWhatTheConfigSays(t *testing.T) {
	cfg := &Config{Class: "binary-cli", Enforce: "hard", Baseline: "ci/base.json"}
	cfg.Report.Path = "out/r.json"
	cfg.Gates.Hygiene.Deps.Licenses.Allow = []string{"MIT"}
	cfg.Gates.Hygiene.Govulncheck.DB = "file:///mirror"
	cfg.Gates.Mutation.Base = "origin/release"
	e := eff(t, cfg)
	if e.Enforce != "hard" || e.Baseline != "ci/base.json" || e.Report.Path != "out/r.json" {
		t.Errorf("%+v", e)
	}
	if !slices.Equal(e.Gates.Hygiene.Deps.Licenses.Allow, []string{"MIT"}) || e.Gates.Hygiene.Govulncheck.DB != "file:///mirror" || e.Gates.Mutation.Base != "origin/release" {
		t.Errorf("%+v", e.Gates)
	}
}

// Effective never changes its argument, and the copy shares nothing with it.
func TestEffectiveDoesNotTouchItsInput(t *testing.T) {
	cfg := &Config{Class: "binary-cli"}
	cfg.Laws.Required = []string{"G-41"}
	cfg.Gates.Hygiene.Deps.Licenses.Allow = []string{"MIT"}
	e := eff(t, cfg)
	if cfg.Gates.Crap.Cap != nil || cfg.Enforce != "" || cfg.Gates.Tests.Race != nil || len(cfg.Laws.Required) != 1 {
		t.Fatalf("the input changed: %+v", cfg)
	}
	e.Gates.Hygiene.Deps.Licenses.Allow[0] = "X"
	e.Laws.Required[0] = "X"
	if cfg.Gates.Hygiene.Deps.Licenses.Allow[0] != "MIT" || cfg.Laws.Required[0] != "G-41" {
		t.Fatal("the copy shares a slice with the input")
	}
}

func TestEffectiveRefusesNil(t *testing.T) {
	if _, err := Effective(nil); err == nil {
		t.Fatal("Effective(nil) succeeded")
	}
}

// Effective is idempotent: a second pass changes nothing.
func TestEffectiveIsIdempotent(t *testing.T) {
	for _, class := range append(slices.Clone(launch), "parser-codec") {
		once := eff(t, &Config{Class: class})
		a, err := json.Marshal(once)
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(eff(t, once))
		if err != nil {
			t.Fatal(err)
		}
		if string(a) != string(b) {
			t.Errorf("%s: a second Effective changed the config:\n%s\n%s", class, a, b)
		}
	}
}
