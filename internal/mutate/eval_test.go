package mutate

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hedronite/go-tops/internal/exempt"
	"github.com/Hedronite/go-tops/internal/gate"
)

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
	if _, err := loadConfiguredLedger(dir, "oob.go", time.Now()); err == nil || !strings.Contains(err.Error(), "no ledger reader") {
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
