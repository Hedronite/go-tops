package main

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"

	gotops "github.com/Hedronite/go-tops"
	"github.com/Hedronite/go-tops/internal/cli"
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

// TEMPORARY bridge until G2's loader lands: a small read of go-tops.yaml and the
// embedded profile of its class. No schema validation and no semantic checks:
// those are `check`.
func loadGateConfig(dir string) (gate.Config, error) {
	b, err := os.ReadFile(filepath.Join(dir, "go-tops.yaml"))
	if err != nil {
		return gate.Config{}, err
	}
	var f rawConfig
	if err := yaml.Unmarshal(b, &f); err != nil {
		return gate.Config{}, err
	}
	var p rawConfig
	if pb, err := fs.ReadFile(gotops.Files(), "profiles/"+f.Class+".yaml"); err == nil {
		if err := yaml.Unmarshal(pb, &p); err != nil {
			return gate.Config{}, err
		}
	}
	c := gate.Config{Module: f.Module, Class: f.Class, Enforce: f.Enforce, Layers: map[string]gate.LayerConfig{}, Exemptions: f.Exemptions}
	if f.Advisory != nil {
		c.AdvisoryExpires = f.Advisory.Expires
	}
	c.RequiredLaws = append(append(c.RequiredLaws, p.Laws.Required...), f.Laws.Required...)
	for id, lc := range map[string]*rawLayer{
		"crap": f.Gates.Crap, "mutation": f.Gates.Mutation, "fuzz": f.Layers.Fuzz, "stress": f.Layers.Stress, "bench": f.Layers.Bench,
	} {
		if lc != nil && lc.Enabled != nil && !*lc.Enabled {
			c.Layers[id] = gate.LayerConfig{Reason: lc.Reason, Expires: lc.Expires}
		}
	}
	return c, nil
}

type rawLayer struct {
	Enabled *bool  `yaml:"enabled"`
	Reason  string `yaml:"reason"`
	Expires string `yaml:"expires"`
}

type rawConfig struct {
	Module   string `yaml:"module"`
	Class    string `yaml:"class"`
	Enforce  string `yaml:"enforce"`
	Advisory *struct {
		Expires string `yaml:"expires"`
	} `yaml:"advisory"`
	Gates struct {
		Crap     *rawLayer `yaml:"crap"`
		Mutation *rawLayer `yaml:"mutation"`
	} `yaml:"gates"`
	Layers struct {
		Fuzz   *rawLayer `yaml:"fuzz"`
		Stress *rawLayer `yaml:"stress"`
		Bench  *rawLayer `yaml:"bench"`
	} `yaml:"layers"`
	Laws struct {
		Required []string `yaml:"required"`
	} `yaml:"laws"`
	Exemptions []report.Exemption `yaml:"exemptions"`
}
