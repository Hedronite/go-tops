package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// The report schema ties the summary to the enforcement mode (section 6.7):
// under advisory a run that starts exits 0 and says it was not enforced, and
// the outcome stays what the run would have been under hard.
const reportExample = "../../config/gate-report.example.json"

func validateReport(t *testing.T, doc string) error {
	t.Helper()
	r := &repo{Root: "../.."}
	sch, err := r.schema("schema/gate-report.schema.json", map[string]*jsonschema.Schema{})
	if err != nil {
		t.Fatal(err)
	}
	inst, err := decodeInstance("report.json", []byte(doc))
	if err != nil {
		return err
	}
	return sch.Validate(inst)
}

const (
	enforcedSummary = `"summary": { "outcome": "fail", "exit_code": 1, "failed": ["crap"], "enforced": true }`
	advisorySummary = `"summary": { "outcome": "fail", "exit_code": 0, "failed": ["crap"], "enforced": false, "advisory_expires": "2027-03-31" }`
)

func TestReportExampleValidates(t *testing.T) {
	raw, err := os.ReadFile(reportExample)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateReport(t, string(raw)); err != nil {
		t.Fatalf("the example must validate: %v", err)
	}
}

// When the run is enforced the exit code follows the outcome: fail is 1, and
// pass and partial are 0 (section 3.3, 12.2).
func TestReportExitCodeFollowsOutcome(t *testing.T) {
	raw, err := os.ReadFile(reportExample)
	if err != nil {
		t.Fatal(err)
	}
	ex := string(raw)
	with := func(outcome string, code int) string {
		s := strings.Replace(ex, `"outcome": "fail", "exit_code": 1`, fmt.Sprintf(`"outcome": %q, "exit_code": %d`, outcome, code), 1)
		if s == ex && (outcome != "fail" || code != 1) {
			t.Fatal("the example no longer has the summary this test edits")
		}
		return s
	}
	for _, c := range []struct {
		outcome string
		code    int
		valid   bool
	}{
		{"fail", 1, true},
		{"pass", 0, true},
		{"partial", 0, true},
		{"fail", 0, false},
		{"pass", 1, false},
		{"partial", 1, false},
	} {
		err := validateReport(t, with(c.outcome, c.code))
		if c.valid && err != nil {
			t.Errorf("%s with exit %d must validate: %v", c.outcome, c.code, err)
		}
		if !c.valid && err == nil {
			t.Errorf("%s with exit %d validated", c.outcome, c.code)
		}
	}
}

// A failed tests, coverage, CRAP or mutation layer carries its figures or
// {error}: empty or unparseable output is a fail with nothing to measure.
func TestReportLayerErrorForm(t *testing.T) {
	raw, err := os.ReadFile(reportExample)
	if err != nil {
		t.Fatal(err)
	}
	ex := string(raw)
	const crapLayer = `"id": "crap", "tier": 1, "outcome": "fail",`
	start := strings.Index(ex, crapLayer)
	if start < 0 {
		t.Fatal("the example no longer has the crap layer this test edits")
	}
	// replace the crap layer details object (up to its closing brace line) with an error form
	end := strings.Index(ex[start:], "\n    },\n    {\n      \"id\": \"mutation\"")
	if end < 0 {
		t.Fatal("the example no longer has the layout this test edits")
	}
	with := func(layer string) string { return ex[:start] + layer + ex[start+end:] }
	failed := `"id": "crap", "tier": 1, "outcome": "fail",
      "details": { "error": "crap report was empty" }`
	if err := validateReport(t, with(failed)); err != nil {
		t.Fatalf("a failed layer with the error form must validate: %v", err)
	}
	cases := []struct{ name, layer string }{
		{"error-with-pass", strings.Replace(failed, `"outcome": "fail"`, `"outcome": "pass"`, 1)},
		{"error-with-not-run", strings.Replace(failed, `"outcome": "fail"`, `"outcome": "not-run"`, 1)},
		{"error-empty", strings.Replace(failed, "crap report was empty", "", 1)},
		{"error-blank", strings.Replace(failed, "crap report was empty", "  ", 1)},
		{"error-and-figures", strings.Replace(failed, `{ "error"`, `{ "cap": 10, "violations": [], "error"`, 1)},
		{"error-unknown-key", strings.Replace(failed, `{ "error"`, `{ "bogus_key": 1, "error"`, 1)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := validateReport(t, with(c.layer)); err == nil {
				t.Fatal("the planted layer validated")
			}
		})
	}
	// every one of the four layers takes the form
	for _, id := range []string{"tests", "coverage", "mutation"} {
		if err := validateReport(t, with(strings.Replace(failed, `"id": "crap"`, `"id": "`+id+`"`, 1))); err != nil {
			t.Errorf("%s with the error form must validate: %v", id, err)
		}
	}
}

func TestReportAdvisoryRun(t *testing.T) {
	raw, err := os.ReadFile(reportExample)
	if err != nil {
		t.Fatal(err)
	}
	ex := string(raw)
	if !strings.Contains(ex, enforcedSummary) || !strings.Contains(ex, `"enforce": "ratchet"`) {
		t.Fatal("the example no longer has the summary and mode this test edits")
	}
	advisory := strings.Replace(ex, `"enforce": "ratchet"`, `"enforce": "advisory"`, 1)
	good := strings.Replace(advisory, enforcedSummary, advisorySummary, 1)
	if err := validateReport(t, good); err != nil {
		t.Fatalf("a failing run under advisory exits 0, keeps outcome fail and must validate: %v", err)
	}
	cases := []struct{ name, doc string }{
		{"advisory-still-exits-1", strings.Replace(good, `"exit_code": 0`, `"exit_code": 1`, 1)},
		{"advisory-without-expiry", strings.Replace(good, `, "advisory_expires": "2027-03-31"`, "", 1)},
		{"advisory-with-bad-date", strings.Replace(good, "2027-03-31", "next spring", 1)},
		{"advisory-marked-enforced", strings.Replace(advisory, enforcedSummary, strings.Replace(advisorySummary, `"enforced": false`, `"enforced": true`, 1), 1)},
		{"advisory-with-enforced-summary", advisory},
		{"ratchet-marked-not-enforced", strings.Replace(ex, enforcedSummary, advisorySummary, 1)},
		{"enforced-with-expiry", strings.Replace(ex, `"enforced": true`, `"enforced": true, "advisory_expires": "2027-03-31"`, 1)},
		{"enforced-missing", strings.Replace(ex, `, "enforced": true`, "", 1)},
		{"unknown-summary-key", strings.Replace(ex, `"enforced": true`, `"enforced": true, "bogus_key": 1`, 1)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := validateReport(t, c.doc); err == nil {
				t.Fatal("the planted report validated")
			}
		})
	}
}
