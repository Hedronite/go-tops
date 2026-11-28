package laws

import (
	"testing"

	"github.com/Hedronite/go-tops/internal/lawpass"
)

func TestRefusalCountsMergesLintAndAnalyzers(t *testing.T) {
	ev := Evidence{
		LintPassOK:  true,
		AnalyzersOK: true,
		Lint: lawpass.LintResult{
			Issues: []lawpass.LintIssue{{FromLinter: "errcheck", Pos: struct {
				Filename string
				Line     int
			}{Filename: "a.go"}}},
		},
		AnalyzersByFile: map[string]map[string]int{
			"G-12": {"a.go": 2},
		},
	}
	rel := func(string) string { return "a.go" }
	got := RefusalCounts(ev, rel)
	if got["G-12"]["a.go"] != 3 {
		t.Fatalf("got %#v", got)
	}
}

func TestRefusalCountsEmptyInputs(t *testing.T) {
	if RefusalCounts(Evidence{Lint: lawpass.LintResult{}}, func(string) string { return "" }) != nil {
		t.Fatal("want nil")
	}
}

func TestMergeCountsAnalyzerOnly(t *testing.T) {
	got := mergeCounts(nil, map[string]map[string]int{"G-11": {"b.go": 2}})
	if got["G-11"]["b.go"] != 2 {
		t.Fatalf("got %#v", got)
	}
}

func TestMergeCountsReturnsNilWhenBothEmpty(t *testing.T) {
	if mergeCounts(map[string]map[string]int{}, map[string]map[string]int{}) != nil {
		t.Fatal("want nil")
	}
}
