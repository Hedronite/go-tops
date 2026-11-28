package mutate

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Hedronite/go-tops/internal/exempt"
	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/report"
)

type fakeLedger map[string]bool

func (f fakeLedger) Explains(k SurvivorKey) bool {
	return f[k.String()]
}

const accountingSource = `package p

func check(a, b int) bool {
	if a < b {
		return a == b
	}
	return b > 0 + 0
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

// accounted records one outcome of every kind over accountingSource.
func accounted(t *testing.T, dir string, cfg gate.MutationConfig, today time.Time) (Evaluation, error) {
	t.Helper()
	diff := DiffResult{
		Base:            "origin/main",
		BaseSHA:         "abc123",
		OutOfBuildFiles: map[string]map[int]bool{"p_linux.go": {2: true}},
	}
	exemptions := []report.Exemption{
		{Path: "p.go", Symbol: "skippedExempt", Layer: "mutation", Reason: "ok", Owner: "@a", Expires: "2027-01-01"},
		{Path: "p.go", Symbol: "varExempt", Layer: "mutation", Reason: "var fn", Owner: "@a", Expires: "2027-01-01"},
		{Path: "p.go", Symbol: "expiredExempt", Layer: "mutation", Reason: "old", Owner: "@a", Expires: "2026-01-01"},
	}
	ev, ec, err := prepareEvaluation(cfg, exemptions, diff, dir, today)
	if err != nil {
		return Evaluation{}, err
	}
	for _, m := range []MutantOutcome{
		{Type: "CONDITIONALS_NEGATION", Status: "KILLED", Line: 4, Column: 7},
		{Type: "CONDITIONALS_BOUNDARY", Status: "LIVED", Line: 4, Column: 7},
		{Type: "CONDITIONALS_NEGATION", Status: "TIMED OUT", Line: 5, Column: 12},
		{Type: "CONDITIONALS_BOUNDARY", Status: "NOT COVERED", Line: 7, Column: 11},
		{Type: "CONDITIONALS_NEGATION", Status: "KILLED", Line: 7, Column: 11},
		{Type: "ARITHMETIC_BASE", Status: "NOT VIABLE", Line: 7, Column: 15},
		{Type: "CONDITIONALS_BOUNDARY", Status: "LIVED", Line: 11, Column: 11},
		{Type: "CONDITIONALS_NEGATION", Status: "KILLED", Line: 11, Column: 11},
		{Type: "CONDITIONALS_BOUNDARY", Status: "NOT COVERED", Line: 15, Column: 11},
		{Type: "CONDITIONALS_NEGATION", Status: "KILLED", Line: 15, Column: 11},
		{Type: "CONDITIONALS_BOUNDARY", Status: "NOT COVERED", Line: 19, Column: 11},
		{Type: "CONDITIONALS_NEGATION", Status: "KILLED", Line: 19, Column: 11},
	} {
		if err := recordMutant(&ev, "p.go", m, []byte(accountingSource), ec.idx, ec.exRes, ec.ledger); err != nil {
			return Evaluation{}, err
		}
	}
	sortEvaluation(&ev)
	return ev, nil
}

func accountingKey(t *testing.T, function string, line, col int, mutator string) SurvivorKey {
	t.Helper()
	hash, err := MutatedLineHash([]byte(accountingSource), line, col, mutator)
	if err != nil {
		t.Fatal(err)
	}
	return SurvivorKey{File: "p.go", Function: function, Mutator: mutator, LineHash: hash}
}

func TestEvaluationStatusesLedgerAndExemptions(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"go.mod":     "module example.com/p\n\ngo 1.26\n",
		"p.go":       accountingSource,
		"p_linux.go": "package p\nfunc lin(a int) bool { return a < 1 }\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	keyLived := accountingKey(t, "check", 4, 7, "CONDITIONALS_BOUNDARY")
	keyTimed := accountingKey(t, "check", 5, 12, "CONDITIONALS_NEGATION")
	keyExempt := accountingKey(t, "skippedExempt", 11, 11, "CONDITIONALS_BOUNDARY")
	keyVarExempt := accountingKey(t, "varExempt", 19, 11, "CONDITIONALS_BOUNDARY")
	today := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	cfg := gate.MutationConfig{Engine: "overlay", Ledger: ".go-tops/survivors.yaml"}

	oldDefault := DefaultLedger
	DefaultLedger = nil
	t.Cleanup(func() { DefaultLedger = oldDefault })

	// No reader and no ledger file: every survivor is unexplained.
	bare, err := accounted(t, dir, cfg, today)
	if err != nil || bare.SurvivedExplained != 0 || bare.SurvivedUnexplained != 3 {
		t.Fatalf("absent ledger with nil DefaultLedger: ev=%+v err=%v", bare, err)
	}
	// A ledger file with no reader fails closed.
	if err := os.MkdirAll(filepath.Join(dir, ".go-tops"), 0o750); err != nil {
		t.Fatal(err)
	}
	ledgerFile := filepath.Join(dir, ".go-tops", "survivors.yaml")
	if err := os.WriteFile(ledgerFile, []byte("survivors: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := accounted(t, dir, cfg, today); err == nil {
		t.Fatal("expected fail closed when DefaultLedger is nil and ledger file exists")
	}
	if err := os.Remove(ledgerFile); err != nil {
		t.Fatal(err)
	}
	errCorruptLedger := errors.New("corrupt survivor ledger")
	DefaultLedger = func(_, gotPath string, _ time.Time) (Ledger, error) {
		if gotPath != ".go-tops/survivors.yaml" {
			t.Fatalf("ledgerPath = %q, want slash-separated", gotPath)
		}
		return nil, errCorruptLedger
	}
	if _, err := accounted(t, dir, cfg, today); !errors.Is(err, errCorruptLedger) {
		t.Fatalf("expected loader error, got %v", err)
	}

	DefaultLedger = func(_, _ string, _ time.Time) (Ledger, error) {
		return fakeLedger{keyLived.String(): true}, nil
	}
	ev, err := accounted(t, dir, cfg, today)
	if err != nil {
		t.Fatal(err)
	}
	// NOT VIABLE is outside Mutants: 11 = 5 killed + 1 timed out + 1 ledger + 2 exempt + 2 unexplained.
	if ev.Mutants != 11 || ev.Killed != 6 || ev.TimedOut != 1 || ev.Dropped != 1 ||
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
	rows, ok := ev.Details()["survivors"].([]map[string]any)
	if !ok || len(rows) != 5 {
		t.Fatalf("expected 5 survivor rows (1 explained, 2 exempt, 2 unexplained), got %v", rows)
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
}

func TestEvalHelpersAndBoundaries(t *testing.T) {
	for _, tc := range []struct {
		in, want int
	}{
		{0, 1},
		{1, 1},
		{2, 2},
		{4, 4},
		{8, 4},
	} {
		if got := boundWorkers(tc.in); got != tc.want {
			t.Errorf("boundWorkers(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}

	if len(nonNilStrings(nil)) != 0 || !slices.Equal(nonNilStrings([]string{"a"}), []string{"a"}) {
		t.Error("nonNilStrings mismatch")
	}

	ev := Evaluation{
		TimedOutKeys: []string{"b", "a"},
		Exempt:       []string{"z", "m"},
		Survivors: []Survivor{
			{Key: SurvivorKey{File: "b.go", Mutator: "X", LineHash: "2"}, Line: 10},
			{Key: SurvivorKey{File: "a.go", Mutator: "B", LineHash: "2"}, Line: 5},
			{Key: SurvivorKey{File: "a.go", Mutator: "A", LineHash: "2"}, Line: 5},
			{Key: SurvivorKey{File: "a.go", Mutator: "A", LineHash: "1"}, Line: 5},
			{Key: SurvivorKey{File: "a.go", Mutator: "A", LineHash: "1"}, Line: 3},
		},
	}
	sortEvaluation(&ev)
	if !slices.Equal(ev.TimedOutKeys, []string{"a", "b"}) || !slices.Equal(ev.Exempt, []string{"m", "z"}) {
		t.Fatalf("unsorted keys: %+v", ev)
	}
	if ev.Survivors[0].Line != 3 || ev.Survivors[1].Key.LineHash != "1" || ev.Survivors[2].Key.LineHash != "2" || ev.Survivors[3].Key.Mutator != "B" || ev.Survivors[4].Key.File != "b.go" {
		t.Fatalf("unexpected survivor sort order: %+v", ev.Survivors)
	}

	det := ev.Details()
	rows, ok := det["survivors"].([]map[string]any)
	if !ok || det["max"] != 0 || len(rows) != 5 {
		t.Fatalf("unexpected Details: %+v", det)
	}

	tag := "ledger"
	row := survivorRow(Survivor{Key: SurvivorKey{File: "a.go", Function: "F", Mutator: "M", LineHash: "H"}, Line: 1, Status: "LIVED", Explained: &tag})
	if row["explained"] != "ledger" {
		t.Fatalf("expected explained=ledger, got %v", row)
	}
	rowNil := survivorRow(Survivor{Key: SurvivorKey{File: "a.go"}, Line: 1, Status: "LIVED"})
	if rowNil["explained"] != nil {
		t.Fatalf("expected explained=nil, got %v", rowNil)
	}

	// EvaluateOverlayEmpty + OutOfBuildFiles countNotMeasured:
	dir := t.TempDir()
	oob := filepath.Join(dir, "oob.go")
	if err := os.WriteFile(oob, []byte("//go:build ignore\npackage p\nfunc F() int { return 1 + 2 }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/e\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	diff := DiffResult{Base: "main", BaseSHA: "abc", OutOfBuildFiles: map[string]map[int]bool{"oob.go": {3: true}}}
	emptyEv, err := EvaluateOverlayEmpty(gate.MutationConfig{Engine: "overlay"}, nil, diff, dir, time.Now())
	if err != nil || emptyEv.NotMeasured != 1 {
		t.Fatalf("EvaluateOverlayEmpty got ev=%+v err=%v", emptyEv, err)
	}
	if _, err := countNotMeasured(dir, map[string]map[int]bool{"missing.go": {1: true}}); err == nil {
		t.Error("expected missing out-of-build file error")
	}
	badSyntax := filepath.Join(dir, "bad.go")
	if err := os.WriteFile(badSyntax, []byte("not go code"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := countNotMeasured(dir, map[string]map[int]bool{"bad.go": {1: true}}); err == nil {
		t.Error("expected bad out-of-build parse error")
	}

	// loadConfiguredLedger missing vs directory stat:
	orig := DefaultLedger
	DefaultLedger = nil
	t.Cleanup(func() { DefaultLedger = orig })
	if l, err := loadConfiguredLedger(dir, "does-not-exist.yaml", time.Now()); err != nil || l != nil {
		t.Fatalf("missing ledger got (%v,%v)", l, err)
	}
	if _, err := loadConfiguredLedger(dir, "oob.go", time.Now()); !errors.Is(err, ErrNoLedgerReader) {
		t.Fatalf("existing ledger without reader got %v", err)
	}

	// recordMutant error/non-viable paths:
	src := []byte("package p\nfunc F() int { return 1 + 2 }\n")
	idx, err := ScanModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range []string{"SKIPPED", "RUNNABLE", "WEIRD"} {
		if err := recordMutant(&Evaluation{}, "oob.go", MutantOutcome{Type: "ARITHMETIC_BASE", Status: st, Line: 3, Column: 25}, src, idx, exempt.MutationResult{}, nil); err == nil {
			t.Errorf("expected error for status %s", st)
		}
	}
	var nv Evaluation
	if err := recordMutant(&nv, "oob.go", MutantOutcome{Type: "ARITHMETIC_BASE", Status: "NOT VIABLE", Line: 3, Column: 25}, src, idx, exempt.MutationResult{}, nil); err != nil || nv.Dropped != 1 {
		t.Fatalf("NOT VIABLE got nv=%+v err=%v", nv, err)
	}
}

func TestE28OutOfBuildBinaryAndUnaryMinusNotMeasured(t *testing.T) {
	dir := t.TempDir()
	const rel = "e28_linux.go"
	src := "//go:build linux\n\npackage p\n\nfunc F(a, b, x int) int {\n\ty := a - b\n\treturn -x\n}\n"
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/e28\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	diff := DiffResult{
		Base: "main", BaseSHA: "abc",
		OutOfBuildFiles: map[string]map[int]bool{rel: {6: true, 7: true}},
	}
	ev, err := EvaluateOverlayEmpty(gate.MutationConfig{Engine: "overlay"}, nil, diff, dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if ev.NotMeasured != 2 {
		t.Fatalf("NotMeasured = %d, want 2", ev.NotMeasured)
	}
}
