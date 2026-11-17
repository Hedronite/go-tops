// Package report holds the gate report of GO_TOPS.md §6.6 and §12.3 and
// writes it. The shape is schema/gate-report.schema.json; tests validate every
// report this package and the runner produce against that embedded schema.
package report

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Protocol is the protocol version the report states.
const Protocol = "0.4.1"

// DefaultPath is where the report goes unless go-tops.yaml says otherwise.
const DefaultPath = ".go-tops/report.json"

// Report is .go-tops/report.json. Slices that the schema requires are never
// nil when written: Write turns a nil slice into an empty array.
type Report struct {
	Protocol        string      `json:"protocol"`
	ToolVersion     string      `json:"tool_version"`
	Module          string      `json:"module"`
	Class           string      `json:"class"`
	Enforce         string      `json:"enforce"`
	Git             Git         `json:"git"`
	MaxTier         int         `json:"max_tier"`
	StartedAt       string      `json:"started_at"`
	DurationMS      int64       `json:"duration_ms"`
	VulnDBTimestamp *string     `json:"vuln_db_timestamp"`
	LayerCounts     LayerCounts `json:"layer_counts"`
	Exemptions      []Exemption `json:"exemptions"`
	Layers          []Layer     `json:"layers"`
	Laws            Laws        `json:"laws"`
	Summary         Summary     `json:"summary"`
}

// Git is the state of the repository the run judged.
type Git struct {
	Head  string `json:"head"`
	Base  string `json:"base"`
	Dirty bool   `json:"dirty"`
}

// LayerCounts are the two numbers §3.5 judges by: the run fails when they differ.
type LayerCounts struct {
	EnabledInRange int `json:"enabled_in_range"`
	Ran            int `json:"ran"`
}

// Exemption is one record of §6.8, in force on the day of the run.
type Exemption struct {
	Path    string `json:"path"`
	Symbol  string `json:"symbol,omitempty"`
	Layer   string `json:"layer"`
	Shape   string `json:"shape,omitempty"`
	Reason  string `json:"reason"`
	Owner   string `json:"owner"`
	Expires string `json:"expires"`
}

// Layer is one layer in range with its outcome (§3.3).
type Layer struct {
	ID      string         `json:"id"`
	Tier    int            `json:"tier"`
	Outcome string         `json:"outcome"`
	Reason  string         `json:"reason,omitempty"`
	Details map[string]any `json:"details,omitempty"`
	Expires string         `json:"expires,omitempty"`
}

// Laws are the counts of §12.3 and one result per applicable law.
type Laws struct {
	Catalog              int         `json:"catalog"`
	Judged               int         `json:"judged"`
	NotEvaluated         int         `json:"not_evaluated"`
	Applicable           int         `json:"applicable"`
	ApplicableJudged     int         `json:"applicable_judged"`
	Required             int         `json:"required"`
	RequiredNotEvaluated int         `json:"required_not_evaluated"`
	NotEvaluatedByReason ByReason    `json:"not_evaluated_by_reason"`
	OutOfRange           []string    `json:"out_of_range"`
	Results              []LawResult `json:"results"`
}

// ByReason counts not-evaluated laws by reason code.
type ByReason struct {
	NoReader     int `json:"no_reader"`
	ConfigOff    int `json:"config_off"`
	InputMissing int `json:"input_missing"`
}

// LawResult is one law's verdict.
type LawResult struct {
	ID      string `json:"id"`
	Verdict string `json:"verdict"`
	Reason  string `json:"reason,omitempty"`
	Reader  string `json:"reader"`
}

// Summary is the run's verdict. Outcome is always the honest one the layers
// and laws earn (pass, fail or partial). ExitCode is what the process exits
// with: 0 or 1. Under enforce: advisory a run that starts exits 0 whatever the
// outcome, Enforced is false and AdvisoryExpires says when that ends (§6.7).
type Summary struct {
	Outcome         string   `json:"outcome"`
	ExitCode        int      `json:"exit_code"`
	Failed          []string `json:"failed"`
	Enforced        bool     `json:"enforced"`
	AdvisoryExpires string   `json:"advisory_expires,omitempty"`
}

// Marshal renders the report as indented JSON with a trailing newline.
func Marshal(r Report) ([]byte, error) {
	if r.Exemptions == nil {
		r.Exemptions = []Exemption{}
	}
	if r.Layers == nil {
		r.Layers = []Layer{}
	}
	if r.Laws.OutOfRange == nil {
		r.Laws.OutOfRange = []string{}
	}
	if r.Laws.Results == nil {
		r.Laws.Results = []LawResult{}
	}
	if r.Summary.Failed == nil {
		r.Summary.Failed = []string{}
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("report: marshal: %w", err)
	}
	return append(b, '\n'), nil
}

// Write marshals the report and writes it to path, creating the directory.
// The file appears whole or not at all: it is written beside the target and
// renamed into place.
func Write(path string, r Report) error {
	b, err := Marshal(r)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("report: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".report-*.json")
	if err != nil {
		return fmt.Errorf("report: %w", err)
	}
	name := tmp.Name()
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		return removeAfter(name, errors.Join(werr, cerr), "write")
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return removeAfter(name, err, "chmod")
	}
	if err := os.Rename(name, path); err != nil {
		return removeAfter(name, err, "rename")
	}
	return nil
}

// removeAfter reports err and, when the temp file cannot be removed either,
// that too.
func removeAfter(name string, err error, step string) error {
	if rerr := os.Remove(name); rerr != nil {
		err = errors.Join(err, rerr)
	}
	return fmt.Errorf("report: %s: %w", step, err)
}
