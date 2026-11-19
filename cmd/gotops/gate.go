package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Hedronite/go-tops/internal/cli"
	"github.com/Hedronite/go-tops/internal/config"
	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/report"
)

func init() {
	cli.Register(cli.Command{
		Name:    "gate",
		Summary: "run the gate: every enabled layer in range, laws last; writes .go-tops/report.json",
		Run:     runGate,
	})
}

func runGate(env cli.Env, args []string) int {
	return gate.Main(context.Background(), args, gate.CommandDeps{
		LoadConfig:  loadGateConfig,
		CI:          os.Getenv("CI") == "true",
		ToolVersion: packageVersion,
	}, env.Stdout, env.Stderr)
}

// loadGateConfig is `check` followed by the defaults of the class: the gate
// runs on the effective configuration, so no cap or floor is ever absent.
// Issues from check are a configuration failure (exit 2); a repository or file
// that cannot be read is an environment failure (exit 3).
func loadGateConfig(dir string) (gate.Config, error) {
	res, err := config.Check(dir, config.Options{})
	if err != nil {
		return gate.Config{}, &gate.EnvError{Err: err}
	}
	if len(res.Issues) > 0 {
		lines := make([]string, len(res.Issues))
		for i, is := range res.Issues {
			lines[i] = is.String()
		}
		return gate.Config{}, errors.New("\n" + strings.Join(lines, "\n"))
	}
	eff, err := config.Effective(res.Config)
	if err != nil {
		return gate.Config{}, &gate.EnvError{Err: err}
	}
	return toGateConfig(eff)
}

// toGateConfig turns an effective configuration into what the runner reads. It
// fails when a key the runner needs is absent, which Effective rules out.
func toGateConfig(e *config.Config) (gate.Config, error) {
	if err := requireEffective(e); err != nil {
		return gate.Config{}, err
	}
	c := gate.Config{
		Module:       e.Module,
		Class:        e.Class,
		Enforce:      e.Enforce,
		Base:         e.Gates.Mutation.Base,
		ReportPath:   e.Report.Path,
		RequiredLaws: e.Laws.Required,
		Layers:       layerSwitches(e.Gates, e.Layers),
		Hygiene:      hygieneOf(e.Gates.Hygiene),
		Coverage:     coverageOf(e.Gates.Coverage),
		Crap:         crapOf(e.Gates.Crap),
		Exemptions:   exemptionsOf(e.Exemptions),
	}
	if e.Advisory != nil {
		c.AdvisoryExpires = e.Advisory.Expires
	}
	return c, nil
}

// requireEffective checks that every key the gate reads has a value: the class
// defaults fill them all, so an absent one is a configuration that did not go
// through Effective.
func requireEffective(e *config.Config) error {
	g, l := e.Gates, e.Layers
	if g.Crap.Cap == nil || g.Coverage.StatementsFloor == nil || g.Crap.Enabled == nil || g.Mutation.Enabled == nil ||
		l.Fuzz.Enabled == nil || l.Stress.Enabled == nil || l.Bench.Enabled == nil {
		return fmt.Errorf("the configuration is not effective: a key the gate reads is absent")
	}
	return nil
}

// layerSwitches is the switch of each layer that has one in go-tops.yaml.
func layerSwitches(g config.Gates, l config.Layers) map[string]gate.LayerConfig {
	return map[string]gate.LayerConfig{
		"crap":     {Enabled: *g.Crap.Enabled, Reason: g.Crap.Reason, Expires: g.Crap.Expires},
		"mutation": {Enabled: *g.Mutation.Enabled, Reason: g.Mutation.Reason, Expires: g.Mutation.Expires},
		"fuzz":     {Enabled: *l.Fuzz.Enabled, Reason: l.Fuzz.Reason},
		"stress":   {Enabled: *l.Stress.Enabled, Reason: l.Stress.Reason},
		"bench":    {Enabled: *l.Bench.Enabled, Reason: l.Bench.Reason},
	}
}

func hygieneOf(h config.Hygiene) gate.HygieneConfig {
	return gate.HygieneConfig{
		LintConfig: h.GolangciLint.Config,
		VulnDB:     h.Govulncheck.DB,
		FailOn:     h.Govulncheck.FailOn,
		Depguard:   h.Deps.Depguard != nil && *h.Deps.Depguard,
		Licenses:   h.Deps.Licenses.Allow,
	}
}

func coverageOf(c config.Coverage) gate.CoverageConfig {
	return gate.CoverageConfig{
		StatementsFloor:     *c.StatementsFloor,
		ExcludeGenerated:    c.ExcludeGenerated != nil && *c.ExcludeGenerated,
		IntegrationEnabled:  c.Integration.Enabled != nil && *c.Integration.Enabled,
		IntegrationPackages: c.Integration.Packages,
	}
}

func crapOf(c config.Crap) gate.CrapConfig {
	return gate.CrapConfig{
		Cap:              *c.Cap,
		ErrGuardDiscount: c.Complexity.ErrGuardDiscount != nil && *c.Complexity.ErrGuardDiscount,
		WrapFunctions:    c.Complexity.WrapFunctions,
	}
}

func exemptionsOf(in []config.Exemption) []report.Exemption {
	var out []report.Exemption
	for _, x := range in {
		out = append(out, report.Exemption{
			Path: x.Path, Symbol: x.Symbol, Layer: x.Layer, Shape: x.Shape, Reason: x.Reason, Owner: x.Owner, Expires: x.Expires,
		})
	}
	return out
}
