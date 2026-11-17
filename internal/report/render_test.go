package report

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The oracle is GO_TOPS.md §12.1 (report renders the last report; junit and
// sarif exit 2 naming M7), §3.3 (a partial run says PARTIAL) and §6.7 (an
// advisory run prints NOT ENFORCED (advisory, expires <date>): outcome
// <OUTCOME>). The protocol's own example is the sample.

const examplePath = "../../config/gate-report.example.json"

func exampleReport(t *testing.T) Report {
	t.Helper()
	r, err := Load(examplePath)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestParseFormat(t *testing.T) {
	for in, want := range map[string]Format{"text": FormatText, "json": FormatJSON} {
		if got, err := ParseFormat(in); err != nil || got != want {
			t.Errorf("ParseFormat(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"junit", "sarif"} {
		_, err := ParseFormat(in)
		if !errors.Is(err, ErrNotYet) || !strings.Contains(err.Error(), "M7") {
			t.Errorf("ParseFormat(%q) = %v, want an error that names M7", in, err)
		}
	}
	if _, err := ParseFormat("xml"); err == nil || errors.Is(err, ErrNotYet) {
		t.Errorf("an unknown format is a plain error: %v", err)
	}
}

func TestLoadReadsTheProtocolExample(t *testing.T) {
	r := exampleReport(t)
	if r.Module == "" || len(r.Layers) == 0 || r.Summary.Outcome != "fail" || !r.Summary.Enforced {
		t.Fatalf("the example was not read whole: %+v", r.Summary)
	}
}

func TestLoadRefusesWhatTheSchemaRefuses(t *testing.T) {
	raw, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"unknown-key":     strings.Replace(string(raw), `"enforced": true`, `"enforced": true, "bogus_key": 1`, 1),
		"bad-outcome":     strings.Replace(string(raw), `"outcome": "fail", "exit_code": 1`, `"outcome": "maybe", "exit_code": 1`, 1),
		"exit-vs-outcome": strings.Replace(string(raw), `"exit_code": 1`, `"exit_code": 0`, 1),
		"not-json":        "{",
		"empty":           "",
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "report.json")
			if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path)
			var inv *InvalidError
			if !errors.As(err, &inv) || len(inv.Problems) == 0 {
				t.Fatalf("want an InvalidError with problems, got %v", err)
			}
		})
	}
}

func TestLoadMissingFileIsNotExist(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "none.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("want fs.ErrNotExist, got %v", err)
	}
}

func TestRenderJSONIsTheReportWrittenByTheGate(t *testing.T) {
	r := exampleReport(t)
	got, err := Render(r, FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	want, err := Marshal(r)
	if err != nil || string(got) != string(want) {
		t.Fatalf("json render differs from Marshal: %v", err)
	}
	var back Report
	if err := json.Unmarshal(got, &back); err != nil || back.Summary.Outcome != r.Summary.Outcome || back.Summary.ExitCode != r.Summary.ExitCode || back.Summary.Enforced != r.Summary.Enforced {
		t.Fatalf("the json does not read back: %v", err)
	}
}

func TestRenderTextShowsEveryLayerAndTheVerdict(t *testing.T) {
	r := exampleReport(t)
	text := RenderText(r)
	for _, l := range r.Layers {
		if !strings.Contains(text, l.ID+" ") || !strings.Contains(text, l.Outcome) {
			t.Errorf("text lacks layer %s %s:\n%s", l.ID, l.Outcome, text)
		}
	}
	for _, want := range []string{r.Module, "failed: crap", "FAIL (exit 1)", "16 of 43 judged"} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "NOT ENFORCED") || strings.Contains(text, "PARTIAL") {
		t.Errorf("an enforced failing run must say neither NOT ENFORCED nor PARTIAL:\n%s", text)
	}
}

func TestRenderTextAdvisoryRunSaysNotEnforced(t *testing.T) {
	r := exampleReport(t)
	r.Enforce = "advisory"
	r.Summary = Summary{Outcome: "fail", ExitCode: 0, Failed: []string{"crap"}, Enforced: false, AdvisoryExpires: "2027-03-31"}
	text := RenderText(r)
	want := "NOT ENFORCED (advisory, expires 2027-03-31): outcome FAIL\n"
	if !strings.HasSuffix(text, want) {
		t.Fatalf("the last line must be %q:\n%s", want, text)
	}
	if strings.Contains(text, "(exit") {
		t.Errorf("an unenforced run states no exit verdict:\n%s", text)
	}
}

func TestRenderTextPartialSaysPartial(t *testing.T) {
	r := exampleReport(t)
	r.Summary = Summary{Outcome: "partial", ExitCode: 0, Failed: []string{}, Enforced: true}
	r.Laws.OutOfRange = []string{"G-02", "G-07"}
	text := RenderText(r)
	for _, want := range []string{"PARTIAL:", "CI refuses it", "out of range: G-02 G-07", "PARTIAL (exit 0)"} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
}

func TestRenderTextShowsAFailedLayersError(t *testing.T) {
	r := exampleReport(t)
	r.Layers[0] = Layer{ID: "tests", Tier: 1, Outcome: "fail", Details: map[string]any{"error": "go test printed nothing"}}
	if text := RenderText(r); !strings.Contains(text, "tests") || !strings.Contains(text, "error: go test printed nothing") {
		t.Errorf("the {error} form must reach the text:\n%s", text)
	}
}

func TestRenderTextSaysSkipReasonsAndTheVulnDatabase(t *testing.T) {
	r := exampleReport(t)
	ts := "2026-11-17T12:00:00Z"
	r.VulnDBTimestamp = &ts
	r.Layers = append(r.Layers, Layer{ID: "mutation", Tier: 1, Outcome: "skip", Reason: "lands at M4", Expires: "2027-03-01"})
	text := RenderText(r)
	for _, want := range []string{"skip  (lands at M4)", "vulnerability database as of " + ts, "time-dependent"} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
}

// The {error} of a failed layer is shown unless the reason already says it:
// no layer prints the same message twice.
func TestRenderTextPrintsTheErrorOnlyWhenItDiffersFromTheReason(t *testing.T) {
	r := exampleReport(t)
	r.Layers[0] = Layer{ID: "tests", Tier: 1, Outcome: "fail", Reason: "go test printed nothing", Details: map[string]any{"error": "go test printed nothing"}}
	text := RenderText(r)
	if n := strings.Count(text, "go test printed nothing"); n != 1 {
		t.Errorf("the message appears %d times, want once:\n%s", n, text)
	}
	if strings.Contains(text, "error: go test printed nothing") {
		t.Errorf("an error equal to the reason is not repeated:\n%s", text)
	}
	r.Layers[0] = Layer{ID: "tests", Tier: 1, Outcome: "fail", Reason: "the run failed", Details: map[string]any{"error": "go test printed nothing"}}
	if text := RenderText(r); !strings.Contains(text, "(the run failed)") || !strings.Contains(text, "error: go test printed nothing") {
		t.Errorf("a different error is shown beside the reason:\n%s", text)
	}
}
