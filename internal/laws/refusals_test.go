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
