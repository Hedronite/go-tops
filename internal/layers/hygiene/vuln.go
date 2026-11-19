package hygiene

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

// vulnReport is what the gate keeps of a govulncheck run.
type vulnReport struct {
	dbLastModified string
	reachable      []string // OSV IDs with a finding in a function the code calls
	other          int      // OSV IDs found only at package or module level
	fixed          map[string]string
}

// extras are the figures the vulnerabilities entry adds to its outcome. The
// check is time-dependent: the same commit can change verdict when the
// database moves (§3.1).
func (v *vulnReport) extras(failOn string) map[string]any {
	if failOn == "" {
		failOn = "reachable"
	}
	r := make([]any, 0, len(v.reachable))
	for _, id := range v.reachable {
		e := map[string]any{"osv": id}
		if f := v.fixed[id]; f != "" {
			e["fixed_version"] = f
		}
		r = append(r, e)
	}
	return map[string]any{"db_last_modified": v.dbLastModified, "time_dependent": true, "fail_on": failOn, "reachable": r, "other": v.other}
}

// message is one object of govulncheck's -json stream.
type message struct {
	Config  *vulnConfig  `json:"config"`
	Finding *vulnFinding `json:"finding"`
}

// vulnConfig is the config message: the database the scan read.
type vulnConfig struct {
	DBLastModified string `json:"db_last_modified"`
}

// vulnFinding is one finding: an OSV entry, the version that fixes it, and the
// trace that reaches it. A trace that starts in a function is one the code calls.
type vulnFinding struct {
	OSV          string `json:"osv"`
	FixedVersion string `json:"fixed_version"`
	Trace        []struct {
		Function string `json:"function"`
	} `json:"trace"`
}

// parseVulns reads govulncheck's -json stream. The stream must hold a config
// message with the database timestamp: a run that printed nothing, or no
// config, judged nothing (§3.4 rule 5).
func parseVulns(stream []byte) (*vulnReport, error) {
	if len(bytes.TrimSpace(stream)) == 0 {
		return nil, fmt.Errorf("no output: nothing was scanned")
	}
	s := &vulnScan{v: &vulnReport{fixed: map[string]string{}}, reach: map[string]bool{}, seen: map[string]bool{}}
	if err := s.read(stream); err != nil {
		return nil, err
	}
	return s.finish()
}

// vulnScan holds what the messages of one stream add up to.
type vulnScan struct {
	v     *vulnReport
	reach map[string]bool // OSV IDs with a finding in a function the code calls
	seen  map[string]bool // every OSV ID found
}

// read decodes the stream message by message.
func (s *vulnScan) read(stream []byte) error {
	dec := json.NewDecoder(bytes.NewReader(stream))
	for {
		var m message
		err := dec.Decode(&m)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("output is not govulncheck JSON: %w", err)
		}
		s.config(m.Config)
		s.finding(m.Finding)
	}
}

func (s *vulnScan) config(c *vulnConfig) {
	if c != nil && c.DBLastModified != "" {
		s.v.dbLastModified = c.DBLastModified
	}
}

func (s *vulnScan) finding(f *vulnFinding) {
	if f == nil {
		return
	}
	s.seen[f.OSV] = true
	if len(f.Trace) > 0 && f.Trace[0].Function != "" {
		s.reach[f.OSV] = true
		s.v.fixed[f.OSV] = f.FixedVersion
	}
}

// finish checks that the database timestamp was recorded and is one, and lists
// what was found.
func (s *vulnScan) finish() (*vulnReport, error) {
	v := s.v
	if v.dbLastModified == "" {
		return nil, fmt.Errorf("no config message with db_last_modified: the database timestamp cannot be recorded")
	}
	if _, err := time.Parse(time.RFC3339, v.dbLastModified); err != nil {
		return nil, fmt.Errorf("db_last_modified %q is not a timestamp", v.dbLastModified)
	}
	for id := range s.reach {
		v.reachable = append(v.reachable, id)
	}
	sort.Strings(v.reachable)
	v.other = len(s.seen) - len(s.reach)
	return v, nil
}

// vulns: `govulncheck -json ./...`, with `-db` when go-tops.yaml names a
// mirrored database. govulncheck exits 0 with -json whatever it finds, so the
// gate judges the parsed findings: with fail_on reachable, a finding in a
// function the code calls fails; a vulnerable package that is only imported,
// or a module that is only required, is reported and does not (§3.1).
func (h *run) vulns() step {
	const name = "govulncheck -json"
	argv, err := h.tool(govulncheck)
	if err != nil {
		return missing(name, err)
	}
	args := append(append([]string{}, argv...), "-json")
	if db := h.env.Config.Hygiene.VulnDB; db != "" {
		args = append(args, "-db", db)
	}
	out, err := h.exec(append(args, "./...")...)
	if err != nil {
		return cantStart(name, err)
	}
	if out.ExitCode != 0 {
		return failed(name, "exit %d: %s", out.ExitCode, head(string(out.Stderr), 3))
	}
	v, err := parseVulns(out.Stdout)
	if err != nil {
		return failed(name, "%v", err)
	}
	h.vuln = v
	h.note("vulnerabilities", v.extras(h.env.Config.Hygiene.FailOn))
	if len(v.reachable) == 0 {
		return passed(name)
	}
	parts := make([]string, 0, len(v.reachable))
	for _, id := range v.reachable {
		if f := v.fixed[id]; f != "" {
			id += " (fixed in " + f + ")"
		}
		parts = append(parts, id)
	}
	return failed(name, "%d reachable vulnerabilit(ies): %s", len(v.reachable), strings.Join(parts, ", "))
}
