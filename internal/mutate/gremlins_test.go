package mutate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/report"
)

type fakeLedger map[string]bool

func (f fakeLedger) Explains(k SurvivorKey) bool {
	return f[k.String()]
}

func TestParseGremlinsOutputAndFailures(t *testing.T) {
	// G0 real output structure check.
	g0Fixture := `{
  "go_module": "github.com/Hedronite/go-tops",
  "files": [
    {
      "file_name": "internal/calc/calc.go",
      "mutations": [
        {"type": "CONDITIONALS_BOUNDARY", "status": "KILLED", "line": 3, "column": 11},
        {"type": "INVERT_LOGICAL", "status": "SKIPPED", "line": 10, "column": 14}
      ]
    }
  ],
  "test_efficacy": 100,
  "mutations_coverage": 100,
  "mutants_total": 1,
  "mutants_killed": 1,
  "mutants_lived": 0,
  "mutants_not_viable": 0,
  "mutants_not_covered": 0,
  "elapsed_time": 0.42,
  "mutator_statistics": {"conditionals_boundary": 1, "invert_logical": 1}
}`
	rep, err := ParseGremlinsOutput([]byte(g0Fixture))
	if err != nil {
		t.Fatalf("valid G0 fixture failed: %v", err)
	}
	if rep.MutantsKilled != 1 || len(rep.Files) != 1 {
		t.Fatalf("unexpected parsed report: %+v", rep)
	}

	badCases := []struct {
		name string
		raw  string
	}{
		{name: "empty", raw: "   "},
		{name: "truncated", raw: `{"go_module": "x", "files": [`},
		{name: "unknown_status", raw: `{"files": [{"file_name": "a.go", "mutations": [{"type": "ARITHMETIC_BASE", "status": "SURVIVED", "line": 1, "column": 1}]}]}`},
		{name: "unknown_field", raw: `{"unexpected_extra": 1}`},
	}
	for _, tc := range badCases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseGremlinsOutput([]byte(tc.raw)); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

func TestHandleMissingJSONContracts(t *testing.T) {
	// E5 pass condition: exit 0 WITH "No results to report."
	if _, noMut, err := handleMissingJSON(gate.Output{ExitCode: 0, Stdout: []byte("No results to report.\n")}); err != nil || !noMut {
		t.Fatalf("expected E5 no-mutants pass, got noMut=%v err=%v", noMut, err)
	}
	// SIGTERM condition: exit 0 WITHOUT "No results to report." MUST fail closed.
	if _, _, err := handleMissingJSON(gate.Output{ExitCode: 0, Stdout: []byte("interrupted\n")}); err == nil {
		t.Fatal("expected fail closed on exit 0 missing JSON without No results to report.")
	}
	// Non-zero exit MUST fail closed.
	if _, _, err := handleMissingJSON(gate.Output{ExitCode: 2, Stderr: []byte("panic")}); err == nil {
		t.Fatal("expected fail closed on exit 2 missing JSON")
	}
}

func TestEvaluateStatusesLedgerAndExemptions(t *testing.T) {
	dir := t.TempDir()
	src := `package p

func check(a, b int) bool {
	if a < b {
		return a == b
	}
	return b > 0
}

func skippedExempt(x int) bool {
	return x < 5
}

func expiredExempt(x int) bool {
	return x < 9
}

var varExempt = func(x int) bool {
	return x < 3
}
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/p\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "p.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "p_linux.go"), []byte("package p\nfunc lin(a int) bool { return a < 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hLived, err := MutatedLineHash([]byte(src), 4, 7, "CONDITIONALS_BOUNDARY")
	if err != nil {
		t.Fatal(err)
	}
	hTimed, err := MutatedLineHash([]byte(src), 5, 12, "CONDITIONALS_NEGATION")
	if err != nil {
		t.Fatal(err)
	}
	hExempt, err := MutatedLineHash([]byte(src), 11, 11, "CONDITIONALS_BOUNDARY")
	if err != nil {
		t.Fatal(err)
	}
	hVarExempt, err := MutatedLineHash([]byte(src), 19, 11, "CONDITIONALS_BOUNDARY")
	if err != nil {
		t.Fatal(err)
	}
	keyLived := SurvivorKey{File: "p.go", Function: "check", Mutator: "CONDITIONALS_BOUNDARY", LineHash: hLived}
	keyTimed := SurvivorKey{File: "p.go", Function: "check", Mutator: "CONDITIONALS_NEGATION", LineHash: hTimed}
	keyExempt := SurvivorKey{File: "p.go", Function: "skippedExempt", Mutator: "CONDITIONALS_BOUNDARY", LineHash: hExempt}
	keyVarExempt := SurvivorKey{File: "p.go", Function: "varExempt", Mutator: "CONDITIONALS_BOUNDARY", LineHash: hVarExempt}

	jsonOut := `{
  "files": [{
    "file_name": "p.go",
    "mutations": [
      {"type": "CONDITIONALS_BOUNDARY", "status": "SKIPPED", "line": 99, "column": 1},
      {"type": "CONDITIONALS_BOUNDARY", "status": "KILLED", "line": 4, "column": 7},
      {"type": "CONDITIONALS_BOUNDARY", "status": "LIVED", "line": 4, "column": 7},
      {"type": "CONDITIONALS_NEGATION", "status": "TIMED OUT", "line": 5, "column": 12},
      {"type": "CONDITIONALS_BOUNDARY", "status": "NOT COVERED", "line": 7, "column": 11},
      {"type": "ARITHMETIC_BASE", "status": "NOT VIABLE", "line": 7, "column": 11},
      {"type": "CONDITIONALS_BOUNDARY", "status": "LIVED", "line": 11, "column": 11},
      {"type": "CONDITIONALS_BOUNDARY", "status": "NOT COVERED", "line": 15, "column": 11},
      {"type": "CONDITIONALS_BOUNDARY", "status": "NOT COVERED", "line": 19, "column": 11},
      {"type": "CONDITIONALS_BOUNDARY", "status": "LIVED", "line": 99, "column": 1}
    ]
  }]
}`
	diff := DiffResult{
		Base:    "origin/main",
		BaseSHA: "abc123",
		ProductionFiles: map[string]map[int]bool{
			"p.go":       {4: true, 5: true, 7: true, 11: true, 15: true},
			"p_linux.go": {2: true},
		},
		InBuildFiles:    map[string]map[int]bool{"p.go": {4: true, 5: true, 7: true, 11: true, 15: true, 19: true}},
		OutOfBuildFiles: map[string]map[int]bool{"p_linux.go": {2: true}},
	}

	today := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	exemptions := []report.Exemption{
		{Path: "p.go", Symbol: "skippedExempt", Layer: "mutation", Reason: "ok", Owner: "@a", Expires: "2027-01-01"},
		{Path: "p.go", Symbol: "varExempt", Layer: "mutation", Reason: "var fn", Owner: "@a", Expires: "2027-01-01"},
		{Path: "p.go", Symbol: "expiredExempt", Layer: "mutation", Reason: "old", Owner: "@a", Expires: "2026-01-01"},
	}

	// DefaultLedger nil + absent file treats survivors as unexplained (fail-closed); existing file fails closed immediately.
	oldDefault := DefaultLedger
	DefaultLedger = nil
	t.Cleanup(func() { DefaultLedger = oldDefault })

	cfg := gate.MutationConfig{Engine: "gremlins", Ledger: ".go-tops/survivors.yaml"}
	evNoFile, err := Evaluate(cfg, exemptions, []byte(jsonOut), false, diff, dir, today)
	if err != nil || evNoFile.SurvivedExplained != 0 || evNoFile.SurvivedUnexplained != 3 {
		t.Fatalf("absent ledger with nil DefaultLedger: ev=%+v err=%v", evNoFile, err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".go-tops"), 0o755); err != nil {
		t.Fatal(err)
	}
	ledgerFile := filepath.Join(dir, ".go-tops", "survivors.yaml")
	if err := os.WriteFile(ledgerFile, []byte("survivors: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Evaluate(cfg, exemptions, []byte(jsonOut), false, diff, dir, today); err == nil {
		t.Fatal("expected fail closed when DefaultLedger is nil and ledger file exists")
	}
	if err := os.Remove(ledgerFile); err != nil {
		t.Fatal(err)
	}

	// Loader error propagates.
	DefaultLedger = func(gotDir, gotPath string, _ time.Time) (Ledger, error) {
		if gotPath != ".go-tops/survivors.yaml" {
			t.Fatalf("ledgerPath = %q, want slash-separated", gotPath)
		}
		return nil, errors.New("corrupt survivor ledger")
	}
	if _, err := Evaluate(cfg, exemptions, []byte(jsonOut), false, diff, dir, today); err == nil || !strings.Contains(err.Error(), "corrupt survivor ledger") {
		t.Fatalf("expected loader error, got %v", err)
	}

	// Full accounting test with working Ledger explaining keyLived.
	DefaultLedger = func(_, _ string, _ time.Time) (Ledger, error) {
		return fakeLedger{keyLived.String(): true}, nil
	}
	ev, err := Evaluate(cfg, exemptions, []byte(jsonOut), false, diff, dir, today)
	if err != nil {
		t.Fatal(err)
	}
	// Line 99 filtered out; NOT VIABLE dropped outside Mutants (O1) => 7 counted mutants (1 killed + 1 timed + 1 ledger + 2 exempt + 2 unexplained):
	if ev.Mutants != 7 || ev.Killed != 2 || ev.TimedOut != 1 || ev.Dropped != 1 ||
		ev.SurvivedExplained != 1 || ev.SurvivedUnexplained != 2 || ev.NotMeasured != 2 || len(ev.ExemptionProblems) != 0 {
		t.Fatalf("unexpected counts: %+v", ev)
	}
	if !slices.Equal(ev.TimedOutKeys, []string{keyTimed.String()}) {
		t.Fatalf("TimedOutKeys = %v, want [%s]", ev.TimedOutKeys, keyTimed.String())
	}
	wantExempt := []string{keyExempt.String(), keyVarExempt.String()}
	slices.Sort(wantExempt)
	if !slices.Equal(ev.Exempt, wantExempt) {
		t.Fatalf("Exempt = %v, want %v", ev.Exempt, wantExempt)
	}

	details := ev.Details()
	rows, ok := details["survivors"].([]map[string]any)
	if !ok {
		t.Fatalf("unexpected survivors type: %T", details["survivors"])
	}
	if len(rows) != 5 {
		t.Fatalf("expected 5 survivor rows (1 explained, 2 exempt, 2 unexplained), got %d", len(rows))
	}
	for _, row := range rows {
		val, hasExplained := row["explained"]
		if !hasExplained {
			t.Fatalf("row missing 'explained' key: %+v", row)
		}
		if row["key"] == keyLived.String() && val != "ledger" {
			t.Fatalf("ledger survivor explained = %v, want 'ledger'", val)
		}
		if row["key"] == keyExempt.String() && val != nil {
			t.Fatalf("exempt survivor explained = %v, want nil", val)
		}
	}

	// RUNNABLE and SKIPPED on a changed line must fail closed (B2).
	for _, badStatus := range []string{"RUNNABLE", "SKIPPED"} {
		badJSON := `{"files": [{"file_name": "p.go", "mutations": [{"type": "CONDITIONALS_BOUNDARY", "status": "` + badStatus + `", "line": 4, "column": 7}]}]}`
		if _, err := Evaluate(gate.MutationConfig{}, nil, []byte(badJSON), false, diff, dir, today); err == nil {
			t.Fatalf("expected fail on %s mutant on changed line", badStatus)
		}
	}
}

func TestNeedsIntegration(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "cmd/tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "internal/calc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmd/tool/main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "internal/calc/calc.go"), []byte("package calc\nfunc F() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	needInternal, err := NeedsIntegration(ctx, gate.OS{}, dir, map[string]map[int]bool{"internal/calc/calc.go": {2: true}})
	if err != nil || needInternal {
		t.Fatalf("internal/calc NeedsIntegration = %v, err = %v; want false", needInternal, err)
	}
	needMain, err := NeedsIntegration(ctx, gate.OS{}, dir, map[string]map[int]bool{"cmd/tool/main.go": {2: true}})
	if err != nil || !needMain {
		t.Fatalf("cmd/tool NeedsIntegration = %v, err = %v; want true", needMain, err)
	}

	// B5: space in module directory path must still detect package main.
	spaceDir := filepath.Join(t.TempDir(), "mod with space")
	if err := os.MkdirAll(filepath.Join(spaceDir, "cmd/tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(spaceDir, "go.mod"), []byte("module example.com/app\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(spaceDir, "cmd/tool/main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	needSpace, err := NeedsIntegration(ctx, gate.OS{}, spaceDir, map[string]map[int]bool{"cmd/tool/main.go": {2: true}})
	if err != nil || !needSpace {
		t.Fatalf("spaceDir NeedsIntegration = %v, err = %v; want true", needSpace, err)
	}
}
