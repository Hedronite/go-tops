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
	g, l := e.Gates, e.Layers
	if g.Crap.Cap == nil || g.Coverage.StatementsFloor == nil || g.Crap.Enabled == nil || g.Mutation.Enabled == nil ||
		l.Fuzz.Enabled == nil || l.Stress.Enabled == nil || l.Bench.Enabled == nil {
		return gate.Config{}, fmt.Errorf("the configuration is not effective: a key the gate reads is absent")
	}
	c := gate.Config{
		Module:       e.Module,
		Class:        e.Class,
		Enforce:      e.Enforce,
		Base:         g.Mutation.Base,
		ReportPath:   e.Report.Path,
		RequiredLaws: e.Laws.Required,
		Layers: map[string]gate.LayerConfig{
			"crap":     {Enabled: *g.Crap.Enabled, Reason: g.Crap.Reason, Expires: g.Crap.Expires},
			"mutation": {Enabled: *g.Mutation.Enabled, Reason: g.Mutation.Reason, Expires: g.Mutation.Expires},
			"fuzz":     {Enabled: *l.Fuzz.Enabled, Reason: l.Fuzz.Reason},
			"stress":   {Enabled: *l.Stress.Enabled, Reason: l.Stress.Reason},
			"bench":    {Enabled: *l.Bench.Enabled, Reason: l.Bench.Reason},
		},
		Hygiene: gate.HygieneConfig{
			LintConfig: g.Hygiene.GolangciLint.Config,
			VulnDB:     g.Hygiene.Govulncheck.DB,
			FailOn:     g.Hygiene.Govulncheck.FailOn,
			Depguard:   g.Hygiene.Deps.Depguard != nil && *g.Hygiene.Deps.Depguard,
			Licenses:   g.Hygiene.Deps.Licenses.Allow,
		},
		Coverage: gate.CoverageConfig{
			StatementsFloor:     *g.Coverage.StatementsFloor,
			ExcludeGenerated:    g.Coverage.ExcludeGenerated != nil && *g.Coverage.ExcludeGenerated,
			IntegrationEnabled:  g.Coverage.Integration.Enabled != nil && *g.Coverage.Integration.Enabled,
			IntegrationPackages: g.Coverage.Integration.Packages,
		},
		Crap: gate.CrapConfig{
			Cap:              *g.Crap.Cap,
			ErrGuardDiscount: g.Crap.Complexity.ErrGuardDiscount != nil && *g.Crap.Complexity.ErrGuardDiscount,
			WrapFunctions:    g.Crap.Complexity.WrapFunctions,
		},
	}
	if e.Advisory != nil {
		c.AdvisoryExpires = e.Advisory.Expires
	}
	for _, x := range e.Exemptions {
		c.Exemptions = append(c.Exemptions, report.Exemption{
			Path: x.Path, Symbol: x.Symbol, Layer: x.Layer, Shape: x.Shape, Reason: x.Reason, Owner: x.Owner, Expires: x.Expires,
		})
	}
	return c, nil
}
