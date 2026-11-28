package gate

import "github.com/Hedronite/go-tops/internal/report"

// LayerConfig is one layer's switch in go-tops.yaml (§9): enabled: false needs
// a reason, and for crap and mutation an expires.
type LayerConfig struct {
	Enabled bool
	Reason  string
	Expires string
}

// Config is the resolved view of go-tops.yaml the runner needs: the class
// profile merged with the repo's file. Loading, validating and merging it is
// the job of `check`; the runner takes the result.
type Config struct {
	Module  string
	Class   string
	Enforce string // hard | ratchet | advisory (§6.7)
	// AdvisoryExpires is the advisory key's expires date, set under advisory.
	AdvisoryExpires string
	// Base is the merge base the diff-scoped layers read; default origin/main.
	Base string
	// ReportPath is where the report goes; default .go-tops/report.json.
	ReportPath string

	// Layers holds the switch of each layer by ID. A layer with no entry is enabled.
	Layers map[string]LayerConfig

	// RequiredLaws is laws.required: the laws whose not-evaluated fails the gate.
	RequiredLaws []string
	// AckLedger is laws.ack_ledger: .go-tops/acks.yaml by default.
	AckLedger  string
	Exemptions []report.Exemption

	Hygiene  HygieneConfig
	Coverage CoverageConfig
	Crap     CrapConfig
	Mutation MutationConfig
}

// HygieneConfig is gates.hygiene.
type HygieneConfig struct {
	LintConfig string   // golangci_lint.config
	VulnDB     string   // govulncheck.db, the -db URL or path
	FailOn     string   // govulncheck.fail_on: reachable
	Depguard   bool     // deps.depguard
	Licenses   []string // deps.licenses.allow
}

// CoverageConfig is gates.coverage.
type CoverageConfig struct {
	StatementsFloor     float64
	ExcludeGenerated    bool
	IntegrationEnabled  bool
	IntegrationPackages []string
}

// MutationConfig is gates.mutation (§6.4).
type MutationConfig struct {
	Engine         string
	UnexplainedMax int
	Ledger         string
}

// CrapConfig is gates.crap.
type CrapConfig struct {
	Cap              float64
	ErrGuardDiscount bool
	WrapFunctions    []string
}

// layer returns the switch for a layer, enabled when the config says nothing.
// The allocation-budget layer (§7) has no key of its own: it follows
// layers.bench, so a profile that does not gate benchmarks skips it too.
func (c Config) layer(id string) LayerConfig {
	if lc, ok := c.Layers[id]; ok {
		return lc
	}
	if id == "allocs" {
		if lc, ok := c.Layers["bench"]; ok {
			return lc
		}
	}
	return LayerConfig{Enabled: true}
}

// BaseRef is the diff base of a run: the --base option, else the configured base,
// else origin/main. The gate and `baseline --status` read the same one.
func (c Config) BaseRef(opt string) string { return c.base(opt) }

func (c Config) base(opt string) string {
	switch {
	case opt != "":
		return opt
	case c.Base != "":
		return c.Base
	}
	return "origin/main"
}

func (c Config) reportPath() string {
	if c.ReportPath != "" {
		return c.ReportPath
	}
	return report.DefaultPath
}
