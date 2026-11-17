package config

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Config is go-tops.yaml (GO_TOPS.md §9), or a class profile, which has the same
// shape. An optional number or boolean is a pointer, so "absent" and "zero" differ.
type Config struct {
	Protocol   string      `json:"protocol"`
	Version    string      `json:"version"`
	Module     string      `json:"module"`
	Class      string      `json:"class"`
	Enforce    string      `json:"enforce"`
	Baseline   string      `json:"baseline"`
	Advisory   *Advisory   `json:"advisory"`
	Toolchain  Toolchain   `json:"toolchain"`
	Gates      Gates       `json:"gates"`
	Layers     Layers      `json:"layers"`
	Laws       LawsConfig  `json:"laws"`
	Exemptions []Exemption `json:"exemptions"`
	Report     Report      `json:"report"`
}

// Advisory is the key that enforce: advisory requires (§6.7).
type Advisory struct {
	Reason  string `json:"reason"`
	Owner   string `json:"owner"`
	Expires string `json:"expires"`
}

// Toolchain holds the Go versions the gate matrix covers.
type Toolchain struct {
	GoFloor   string `json:"go_floor"`
	GoCurrent string `json:"go_current"`
	CGO       *bool  `json:"cgo"`
}

// Gates are the five gates of §3.1 that carry settings.
type Gates struct {
	Hygiene  Hygiene  `json:"hygiene"`
	Tests    Tests    `json:"tests"`
	Coverage Coverage `json:"coverage"`
	Crap     Crap     `json:"crap"`
	Mutation Mutation `json:"mutation"`
}

// Hygiene is gate 1.
type Hygiene struct {
	GolangciLint struct {
		Config string `json:"config"`
	} `json:"golangci_lint"`
	Govulncheck struct {
		FailOn string `json:"fail_on"`
		DB     string `json:"db"`
	} `json:"govulncheck"`
	Deps struct {
		Depguard *bool `json:"depguard"`
		Licenses struct {
			Allow []string `json:"allow"`
		} `json:"licenses"`
	} `json:"deps"`
}

// Tests is gate 2. The schema allows one value for each key.
type Tests struct {
	Race    *bool `json:"race"`
	Shuffle *bool `json:"shuffle"`
	Count   *int  `json:"count"`
}

// Coverage is gate 3.
type Coverage struct {
	StatementsFloor  *float64 `json:"statements_floor"`
	ExcludeGenerated *bool    `json:"exclude_generated"`
	Integration      struct {
		Enabled  *bool    `json:"enabled"`
		Packages []string `json:"packages"`
	} `json:"integration"`
}

// Crap is gate 4; Enabled false needs Reason and Expires.
type Crap struct {
	Enabled    *bool    `json:"enabled"`
	Reason     string   `json:"reason"`
	Expires    string   `json:"expires"`
	Cap        *float64 `json:"cap"`
	Complexity struct {
		ErrGuardDiscount *bool    `json:"err_guard_discount"`
		WrapFunctions    []string `json:"wrap_functions"`
	} `json:"complexity"`
}

// Mutation is gate 5; Enabled false needs Reason and Expires.
type Mutation struct {
	Enabled                 *bool  `json:"enabled"`
	Reason                  string `json:"reason"`
	Expires                 string `json:"expires"`
	Engine                  string `json:"engine"`
	Base                    string `json:"base"`
	UnexplainedSurvivorsMax *int   `json:"unexplained_survivors_max"`
	Ledger                  string `json:"ledger"`
}

// Layers are the class layers of §7; each may be off with a reason.
type Layers struct {
	Fuzz struct {
		Enabled *bool    `json:"enabled"`
		Reason  string   `json:"reason"`
		Targets []string `json:"targets"`
		Timed   struct {
			Duration string `json:"duration"`
		} `json:"timed"`
	} `json:"fuzz"`
	Stress struct {
		Enabled *bool  `json:"enabled"`
		Reason  string `json:"reason"`
		Count   *int   `json:"count"`
	} `json:"stress"`
	Bench struct {
		Enabled       *bool    `json:"enabled"`
		Reason        string   `json:"reason"`
		Baseline      string   `json:"baseline"`
		RegressionPct *float64 `json:"regression_pct"`
	} `json:"bench"`
}

// LawsConfig is the laws block. Required is nil when the key is absent, so a
// config that omits it inherits the profile's list.
type LawsConfig struct {
	Required  []string `json:"required"`
	AckLedger string   `json:"ack_ledger"`
}

// Exemption is one record of §6.8.
type Exemption struct {
	Path    string `json:"path"`
	Symbol  string `json:"symbol"`
	Layer   string `json:"layer"`
	Shape   string `json:"shape"`
	Reason  string `json:"reason"`
	Owner   string `json:"owner"`
	Expires string `json:"expires"`
}

// Report names where the gate writes its report.
type Report struct {
	Path string `json:"path"`
}

// Load parses one config document and validates it against the embedded schema.
// It returns the typed config when there are no issues. The error is non-nil
// only when the embedded schema cannot be compiled, never for bad input.
func Load(data []byte) (*Config, []Issue, error) {
	return load(data, "go-tops.yaml", schemaConfig)
}

func load(data []byte, file, schema string) (*Config, []Issue, error) {
	v, issues := parseYAML(data)
	if len(issues) > 0 {
		return nil, tag(issues, file), nil
	}
	found, err := validate(schema, file, v)
	if err != nil || len(found) > 0 {
		return nil, found, err
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, nil, fmt.Errorf("re-encode %s: %w", file, err)
	}
	var cfg Config
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return nil, []Issue{{File: file, Rule: "schema", Msg: "does not decode: " + err.Error()}}, nil
	}
	return &cfg, nil, nil
}

func tag(issues []Issue, file string) []Issue {
	for i := range issues {
		issues[i].File = file
	}
	return sortIssues(issues)
}
