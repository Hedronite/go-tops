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

// §6.8: the gate prints every exemption in force, with its expiry, on every
// run; a run with none prints no such line.
func TestRenderTextPrintsEveryExemptionInForceWithItsExpiry(t *testing.T) {
	r := exampleReport(t)
	if len(r.Exemptions) == 0 {
		t.Fatal("the example no longer carries an exemption")
	}
	text := RenderText(r)
	for _, want := range []string{"exemption in force", "crap", "internal/wire/dispatch.go", "Server.handleOp", "table-switch", "2027-03-01", "platform"} {
		if !strings.Contains(text, want) {
			t.Errorf("the text lacks %q:\n%s", want, text)
		}
	}
	r.Exemptions = nil
	if text := RenderText(r); strings.Contains(text, "exemption in force") {
		t.Errorf("a run with no exemption printed one:\n%s", text)
	}
}

func TestExemptionLineIsOneLineWithItsExpiry(t *testing.T) {
	e := Exemption{Path: "a/**", Layer: "mutation", Reason: "frozen\nvendor   port", Owner: "protocol", Expires: "2027-01-31"}
	line := e.Line()
	if strings.Contains(line, "\n") || !strings.Contains(line, "2027-01-31") || !strings.Contains(line, "frozen vendor port") {
		t.Errorf("line = %q", line)
	}
	if strings.Contains(line, "symbol") || strings.Contains(line, "shape") {
		t.Errorf("an entry with no symbol or shape printed them: %q", line)
	}
}

// The ratchet line (§6.7, §12.3): the outcome and both builds, so a leg that
// skips the ratchet names the build the ledger leg runs under.
func TestRenderTextRatchetLine(t *testing.T) {
	linux := Build{GOOS: "linux", GOARCH: "amd64", Tags: []string{}, CgoEnabled: true}
	mac := Build{GOOS: "darwin", GOARCH: "arm64", Tags: []string{"integration", "x"}}
	tests := []struct {
		name string
		r    *Ratchet
		want string
	}{
		{"pass", &Ratchet{Outcome: "pass", LedgerBuild: linux, RunBuild: linux}, "  ratchet      pass  (ledger build linux/amd64 cgo on, this run linux/amd64 cgo on)\n"},
		{"fail", &Ratchet{Outcome: "fail", LedgerBuild: linux, RunBuild: linux}, "  ratchet      fail  (ledger build linux/amd64 cgo on, this run linux/amd64 cgo on)\n"},
		{"skip", &Ratchet{Outcome: "skip", LedgerBuild: linux, RunBuild: mac}, "  ratchet      skip  (ledger build linux/amd64 cgo on, this run darwin/arm64 cgo off tags=integration,x)\n"},
	}
	for _, tc := range tests {
		got := RenderText(Report{Ratchet: tc.r})
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: want %q in\n%s", tc.name, tc.want, got)
		}
	}
	if got := RenderText(Report{}); strings.Contains(got, "ratchet") {
		t.Errorf("a run with no ratchet object prints a ratchet line:\n%s", got)
	}
}

// The ratchet line sits with the layers, before the exemptions and the laws.
func TestRenderTextRatchetLineOrder(t *testing.T) {
	b := Build{GOOS: "linux", GOARCH: "amd64", Tags: []string{}}
	got := RenderText(Report{
		Layers:     []Layer{{ID: "crap", Outcome: "pass"}},
		Ratchet:    &Ratchet{Outcome: "pass", LedgerBuild: b, RunBuild: b},
		Exemptions: []Exemption{{Path: "a.go", Layer: "crap", Reason: "dispatch table", Owner: "x", Expires: "2027-01-01"}},
	})
	layer, ratchet, exemption, laws := strings.Index(got, "  crap "), strings.Index(got, "  ratchet "), strings.Index(got, "a.go"), strings.Index(got, "  laws ")
	if layer >= ratchet || ratchet >= exemption || exemption >= laws {
		t.Errorf("order: layer %d ratchet %d exemption %d laws %d\n%s", layer, ratchet, exemption, laws, got)
	}
}
