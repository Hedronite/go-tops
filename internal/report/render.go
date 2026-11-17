package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	gotops "github.com/Hedronite/go-tops"
)

// Format is how `gotops report` renders a stored report (GO_TOPS.md §12.1).
type Format string

// The formats M1 renders. junit and sarif are M7-3.
const (
	FormatText Format = "text"
	FormatJSON Format = "json"
)

// ErrNotYet is returned by ParseFormat for a format the roadmap schedules
// later: the message names the milestone.
var ErrNotYet = errors.New("not available until M7")

// ParseFormat reads the --format value.
func ParseFormat(s string) (Format, error) {
	switch s {
	case "text":
		return FormatText, nil
	case "json":
		return FormatJSON, nil
	case "junit", "sarif":
		return "", fmt.Errorf("--format %s: %w (the roadmap's M7-3 slice)", s, ErrNotYet)
	}
	return "", fmt.Errorf("--format %q: want text or json (junit and sarif arrive in M7)", s)
}

// InvalidError is a report file that does not validate against
// schema/gate-report.schema.json.
type InvalidError struct {
	Path     string
	Problems []string
}

func (e *InvalidError) Error() string {
	return fmt.Sprintf("%s is not a valid gate report: %s", e.Path, strings.Join(e.Problems, "; "))
}

// Load reads a report file and validates it against the embedded schema, so a
// renderer never prints a document the schema would refuse. A missing file
// wraps fs.ErrNotExist.
func Load(path string) (Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Report{}, err
	}
	if probs := validateDocument(data); len(probs) > 0 {
		return Report{}, &InvalidError{Path: path, Problems: probs}
	}
	var r Report
	if err := json.Unmarshal(data, &r); err != nil {
		return Report{}, &InvalidError{Path: path, Problems: []string{err.Error()}}
	}
	return r, nil
}

func validateDocument(data []byte) []string {
	sch, err := compileReportSchema()
	if err != nil {
		return []string{"cannot compile the embedded report schema: " + err.Error()}
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return []string{"not JSON: " + err.Error()}
	}
	verr := sch.Validate(inst)
	if verr == nil {
		return nil
	}
	var probs []string
	for _, line := range strings.Split(verr.Error(), "\n") {
		if line = strings.TrimSpace(line); strings.HasPrefix(line, "- at '") {
			probs = append(probs, strings.TrimPrefix(line, "- at "))
		}
	}
	if len(probs) == 0 {
		probs = []string{strings.SplitN(verr.Error(), "\n", 2)[0]}
	}
	return probs
}

func compileReportSchema() (*jsonschema.Schema, error) {
	c := jsonschema.NewCompiler()
	for _, name := range []string{"core.schema.json", "go-tops.schema.json", "gate-report.schema.json"} {
		raw, err := fs.ReadFile(gotops.Files(), "schema/"+name)
		if err != nil {
			return nil, err
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		if err := c.AddResource("mem:///schema/"+name, doc); err != nil {
			return nil, err
		}
	}
	return c.Compile("mem:///schema/gate-report.schema.json")
}

// Render returns the report in the format.
func Render(r Report, f Format) ([]byte, error) {
	if f == FormatJSON {
		return Marshal(r)
	}
	return []byte(RenderText(r)), nil
}

// RenderText is the one text renderer: `gotops gate` prints it for a run and
// `gotops report` for a stored one. It shows one line per layer, the laws, the
// verdict. A partial run says PARTIAL, and an advisory run says it is not
// enforced: `NOT ENFORCED (advisory, expires <date>): outcome <OUTCOME>`
// (§3.3, §6.7).
func RenderText(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Go-TOPS gate report: %s, class %s, enforce %s, max tier %d\n", r.Module, r.Class, r.Enforce, r.MaxTier)
	for _, l := range r.Layers {
		fmt.Fprintf(&b, "  %-12s %s", l.ID, l.Outcome)
		if l.Reason != "" {
			fmt.Fprintf(&b, "  (%s)", l.Reason)
		}
		if msg := layerError(l); msg != "" && msg != l.Reason {
			fmt.Fprintf(&b, "  error: %s", msg)
		}
		b.WriteByte('\n')
	}
	if r.VulnDBTimestamp != nil {
		fmt.Fprintf(&b, "  vulnerability database as of %s; time-dependent: the same commit can change verdict when it moves\n", *r.VulnDBTimestamp)
	}
	renderLaws(&b, r.Laws)
	fmt.Fprintf(&b, "layers: %d enabled in range, %d ran\n", r.LayerCounts.EnabledInRange, r.LayerCounts.Ran)
	renderVerdict(&b, r.Summary)
	return b.String()
}

// layerError is the text of a failed layer's {error} form (§12.3).
func layerError(l Layer) string {
	if msg, ok := l.Details["error"].(string); ok {
		return msg
	}
	return ""
}

func renderLaws(b *strings.Builder, lw Laws) {
	fmt.Fprintf(b, "  laws         %d of %d judged in this release; %d apply to the class, %d required, %d required not evaluated\n",
		lw.Judged, lw.Catalog, lw.Applicable, lw.Required, lw.RequiredNotEvaluated)
	if len(lw.OutOfRange) > 0 {
		fmt.Fprintf(b, "  out of range: %s\n", strings.Join(lw.OutOfRange, " "))
	}
}

func renderVerdict(b *strings.Builder, s Summary) {
	if s.Outcome == "partial" {
		b.WriteString("PARTIAL: nothing in range failed, but a required law is out of range, so this run did not judge everything the class requires (CI refuses it)\n")
	}
	if len(s.Failed) > 0 {
		fmt.Fprintf(b, "failed: %s\n", strings.Join(s.Failed, ", "))
	}
	verdict := strings.ToUpper(s.Outcome)
	if !s.Enforced {
		fmt.Fprintf(b, "NOT ENFORCED (advisory, expires %s): outcome %s\n", s.AdvisoryExpires, verdict)
		return
	}
	fmt.Fprintf(b, "%s (exit %d)\n", verdict, s.ExitCode)
}
