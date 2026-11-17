package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// Defaults for the keys no profile states. They are the values the protocol
// gives when a config is silent: §3.1 for the test run, §6.1 for generated
// files, §6.4 for mutation, §9 for the paths.
const (
	defaultLintConfig = ".golangci.yml"
	defaultFailOn     = "reachable"
	defaultEngine     = "gremlins"
	defaultBase       = "origin/main"
	defaultReport     = ".go-tops/report.json"
	defaultEnforce    = "ratchet" // adopters default to ratchet (§6.7)
)

// defaultLicenses is the allow list of the example in §9, used when go-tops.yaml
// states none.
var defaultLicenses = []string{"MIT", "Apache-2.0", "BSD-3-Clause", "BSD-2-Clause", "ISC"}

// Effective returns cfg with the defaults of its class merged in, so that no
// key the gate reads is absent. A key cfg sets wins over the profile's, except
// that the CRAP cap is never above, and the coverage floor never below, the
// class column of §5.2 (LimitsFor), and `laws.required` is the profile's laws
// plus cfg's (§3.3). A class with no profile (the nine of §5.1 that have
// none) takes the cap and floor of its column, or 8 and 90 when it has none.
//
// Effective does not validate: Check does, and Effective is meant for a config
// that passed it. It never changes cfg.
func Effective(cfg *Config) (*Config, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config: Effective of no config")
	}
	out, err := clone(cfg)
	if err != nil {
		return nil, err
	}
	prof := &Config{}
	switch p, err := ProfileFor(cfg.Class); {
	case err == nil:
		prof = p
	case !errors.Is(err, ErrNoProfile):
		return nil, err
	}
	lim := LimitsFor(cfg.Class)

	if out.Enforce == "" {
		out.Enforce = defaultEnforce
	}
	out.Baseline = firstString(out.Baseline, prof.Baseline, defaultBaseline)
	out.Report.Path = firstString(out.Report.Path, prof.Report.Path, defaultReport)

	g, pg := &out.Gates, &prof.Gates
	g.Hygiene.GolangciLint.Config = firstString(g.Hygiene.GolangciLint.Config, pg.Hygiene.GolangciLint.Config, defaultLintConfig)
	g.Hygiene.Govulncheck.FailOn = firstString(g.Hygiene.Govulncheck.FailOn, pg.Hygiene.Govulncheck.FailOn, defaultFailOn)
	g.Hygiene.Govulncheck.DB = firstString(g.Hygiene.Govulncheck.DB, pg.Hygiene.Govulncheck.DB, "")
	g.Hygiene.Deps.Depguard = pick(g.Hygiene.Deps.Depguard, pg.Hygiene.Deps.Depguard, true)
	g.Hygiene.Deps.Licenses.Allow = firstList(g.Hygiene.Deps.Licenses.Allow, pg.Hygiene.Deps.Licenses.Allow, defaultLicenses)

	g.Tests.Race = pick(g.Tests.Race, pg.Tests.Race, true)
	g.Tests.Shuffle = pick(g.Tests.Shuffle, pg.Tests.Shuffle, true)
	g.Tests.Count = pick(g.Tests.Count, pg.Tests.Count, 1)

	// The floor and the cap come from the class column: a profile states the
	// same number, and a class with no profile still has one.
	g.Coverage.StatementsFloor = ptr(max(*pick(g.Coverage.StatementsFloor, pg.Coverage.StatementsFloor, lim.Floor), lim.Floor))
	g.Coverage.ExcludeGenerated = pick(g.Coverage.ExcludeGenerated, pg.Coverage.ExcludeGenerated, true)
	g.Coverage.Integration.Enabled = pick(g.Coverage.Integration.Enabled, pg.Coverage.Integration.Enabled, false)
	g.Coverage.Integration.Packages = firstList(g.Coverage.Integration.Packages, pg.Coverage.Integration.Packages, nil)

	g.Crap.Enabled = pick(g.Crap.Enabled, pg.Crap.Enabled, true)
	g.Crap.Cap = ptr(min(*pick(g.Crap.Cap, pg.Crap.Cap, lim.Cap), lim.Cap))
	g.Crap.Complexity.ErrGuardDiscount = pick(g.Crap.Complexity.ErrGuardDiscount, pg.Crap.Complexity.ErrGuardDiscount, true)
	g.Crap.Complexity.WrapFunctions = firstList(g.Crap.Complexity.WrapFunctions, pg.Crap.Complexity.WrapFunctions, nil)

	g.Mutation.Enabled = pick(g.Mutation.Enabled, pg.Mutation.Enabled, true)
	g.Mutation.Engine = firstString(g.Mutation.Engine, pg.Mutation.Engine, defaultEngine)
	g.Mutation.Base = firstString(g.Mutation.Base, pg.Mutation.Base, defaultBase)
	g.Mutation.UnexplainedSurvivorsMax = pick(g.Mutation.UnexplainedSurvivorsMax, pg.Mutation.UnexplainedSurvivorsMax, 0)
	g.Mutation.Ledger = firstString(g.Mutation.Ledger, pg.Mutation.Ledger, defaultSurvivors)

	l, pl := &out.Layers, &prof.Layers
	l.Fuzz.Enabled, l.Fuzz.Reason = layerSwitch(l.Fuzz.Enabled, l.Fuzz.Reason, pl.Fuzz.Enabled, pl.Fuzz.Reason)
	l.Fuzz.Targets = firstList(l.Fuzz.Targets, pl.Fuzz.Targets, nil)
	l.Fuzz.Timed.Duration = firstString(l.Fuzz.Timed.Duration, pl.Fuzz.Timed.Duration, "")
	l.Stress.Enabled, l.Stress.Reason = layerSwitch(l.Stress.Enabled, l.Stress.Reason, pl.Stress.Enabled, pl.Stress.Reason)
	l.Stress.Count = pickOrNil(l.Stress.Count, pl.Stress.Count)
	l.Bench.Enabled, l.Bench.Reason = layerSwitch(l.Bench.Enabled, l.Bench.Reason, pl.Bench.Enabled, pl.Bench.Reason)
	l.Bench.Baseline = firstString(l.Bench.Baseline, pl.Bench.Baseline, "")
	l.Bench.RegressionPct = tighter(l.Bench.RegressionPct, pl.Bench.RegressionPct)

	out.Laws.Required = union(prof.Laws.Required, cfg.Laws.Required)
	out.Laws.AckLedger = firstString(out.Laws.AckLedger, prof.Laws.AckLedger, defaultAcks)
	return out, nil
}

// clone copies a config through its JSON form, which every field has.
func clone(c *Config) (*Config, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("config: copy: %w", err)
	}
	var out Config
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("config: copy: %w", err)
	}
	return &out, nil
}

func ptr[T any](v T) *T { return &v }

// pick returns cur when set, else the profile's value, else def.
func pick[T any](cur, prof *T, def T) *T {
	switch {
	case cur != nil:
		return cur
	case prof != nil:
		return prof
	}
	return &def
}

// pickOrNil is pick for a key with no default.
func pickOrNil[T any](cur, prof *T) *T {
	if cur != nil {
		return cur
	}
	return prof
}

// tighter returns the smaller of two thresholds where both are set.
func tighter(cur, prof *float64) *float64 {
	switch {
	case cur != nil && prof != nil:
		return ptr(min(*cur, *prof))
	case cur != nil:
		return cur
	}
	return prof
}

func firstString(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstList(vals ...[]string) []string {
	for _, v := range vals {
		if len(v) > 0 {
			return slices.Clone(v)
		}
	}
	return nil
}

// layerSwitch resolves one `enabled`/`reason` pair: the config's own when it
// sets `enabled`, else the profile's (a profile that turns a layer off says
// why), else on.
func layerSwitch(enabled *bool, reason string, profEnabled *bool, profReason string) (*bool, string) {
	switch {
	case enabled != nil:
		return enabled, reason
	case profEnabled != nil:
		return profEnabled, firstString(reason, profReason)
	}
	return ptr(true), reason
}

// union is the profile's list followed by what cfg adds, without repeats.
func union(profile, cfg []string) []string {
	out := slices.Clone(profile)
	for _, id := range cfg {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}
