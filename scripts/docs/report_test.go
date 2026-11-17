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

// Gate 1 reports one typed object per check (section 12.3): the figures, or the
// {error} form of a layer that measured nothing.
func TestReportHygieneDetails(t *testing.T) {
	raw, err := os.ReadFile(reportExample)
	if err != nil {
		t.Fatal(err)
	}
	ex := string(raw)
	if err := validateReport(t, ex); err != nil {
		t.Fatalf("the example must validate: %v", err)
	}
	cases := []struct{ name, old, new string }{
		{"tidy-fails-without-message", `"tidy": { "outcome": "pass" }`, `"tidy": { "outcome": "fail" }`},
		{"tool-missing-without-message", `"verify": { "outcome": "pass" }`, `"verify": { "outcome": "tool-missing" }`},
		{"check-outcome-skip", `"tidy": { "outcome": "pass" }`, `"tidy": { "outcome": "skip" }`},
		{"check-unknown-key", `"tidy": { "outcome": "pass" }`, `"tidy": { "outcome": "pass", "bogus_key": 1 }`},
		{"toolchain-takes-only-its-keys", `"go_version": "go1.27.4"`, `"go_version": "go1.27.4", "files": []`},
		{"check-missing", "                   \"verify\": { \"outcome\": \"pass\" },\n", ""},
		{"unknown-hygiene-key", "\"findings\": 0,\n                   \"tidy\"", "\"findings\": 0, \"bogus_key\": 1,\n                   \"tidy\""},
		{"bad-linter-version", `"golangci_lint": "v2.15.0"`, `"golangci_lint": "2.15"`},
		{"negative-findings", "\"findings\": 0,\n                   \"tidy\"", "\"findings\": -1,\n                   \"tidy\""},
		{"vuln-not-time-dependent", `"time_dependent": true`, `"time_dependent": false`},
		{"vuln-pass-without-timestamp", `"db_last_modified": "2026-11-17T09:12:44Z", `, ``},
		{"vuln-fail-on-other", `"fail_on": "reachable"`, `"fail_on": "other"`},
		{"vuln-reachable-needs-an-id", `"reachable": []`, `"reachable": [ {} ]`},
		{"by-linter-zero", `"by_linter": {}`, `"by_linter": { "gocritic": 0 }`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := strings.Replace(ex, c.old, c.new, 1)
			if doc == ex && c.old != c.new {
				t.Fatalf("the example lacks %q", c.old)
			}
			if err := validateReport(t, doc); err == nil {
				t.Fatal("the planted report validated")
			}
		})
	}
	// depguard is optional: present only when deps.depguard is true (section 9)
	withGuard := func(body string) string {
		return strings.Replace(ex, `"licenses": {`, `"depguard": `+body+`, "licenses": {`, 1)
	}
	if err := validateReport(t, withGuard(`{ "outcome": "pass" }`)); err != nil {
		t.Errorf("a passing depguard check must validate: %v", err)
	}
	if err := validateReport(t, withGuard(`{ "outcome": "fail", "message": "a package imports a denied module" }`)); err != nil {
		t.Errorf("a failing depguard check with a message must validate: %v", err)
	}
	for name, body := range map[string]string{
		"depguard-unknown-key":      `{ "outcome": "pass", "bogus_key": 1 }`,
		"depguard-fail-no-message":  `{ "outcome": "fail" }`,
		"depguard-outcome-skip":     `{ "outcome": "skip" }`,
		"depguard-takes-no-details": `{ "outcome": "pass", "files": [] }`,
	} {
		if err := validateReport(t, withGuard(body)); err == nil {
			t.Errorf("%s validated", name)
		}
	}
	// a failing check carries a message, and a failed layer may carry {error}
	good := strings.Replace(ex, `"tidy": { "outcome": "pass" }`, `"tidy": { "outcome": "fail", "message": "go.mod needs tidying" }`, 1)
	if err := validateReport(t, good); err != nil {
		t.Errorf("a failed check with a message must validate: %v", err)
	}
	hyg := ex[strings.Index(ex, `"id": "hygiene"`):strings.Index(ex, `"id": "tests"`)]
	errLayer := `"id": "hygiene", "tier": 1, "outcome": "fail",
      "details": { "error": "cannot read the toolchain pin" }
    },
    {
      `
	if err := validateReport(t, strings.Replace(ex, hyg, errLayer, 1)); err != nil {
		t.Errorf("hygiene fail with {error} must validate: %v", err)
	}
	if err := validateReport(t, strings.Replace(ex, hyg, strings.Replace(errLayer, `"outcome": "fail"`, `"outcome": "pass"`, 1), 1)); err == nil {
		t.Error("hygiene pass with {error} validated")
	}
	noDetails := `"id": "hygiene", "tier": 1, "outcome": "pass"
    },
    {
      `
	if err := validateReport(t, strings.Replace(ex, hyg, noDetails, 1)); err == nil {
		t.Error("a hygiene layer that ran with no details validated")
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
