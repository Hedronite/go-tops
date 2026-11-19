package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The oracle is GO_TOPS.md §6.2 and §6.3. testdata/weak-test holds Grade, whose
// complexity is 9 (1, two `||`/`&&` pairs, two `if`s and four `case`s), under
// the cap of 10, and a test that calls it once: 4 of its 10 statements run, so
// cov = 0.4 and CRAP = 9² × 0.6³ + 9 = 26.496. Shipping has complexity 8 and a
// test that reaches every branch, so its CRAP is 8. The gate must fail `crap`,
// name Grade, and leave Shipping out.
func TestGateFailsCRAPOnTheWeakTestFixture(t *testing.T) {
	dir := gitModule(t, goodConfig(t))
	for _, f := range []string{"weak.go", "weak_test.go"} {
		writeFile(t, dir, f, repoFile(t, "testdata/weak-test/"+f))
	}
	code, stdout, stderr := gotopsEnv(t, dir, nil, "gate", "--only", "crap")
	if code != 1 {
		t.Fatalf("exit %d, want 1 (a function over the cap fails the gate)\n%s\n%s", code, stdout, stderr)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".go-tops", "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rep struct {
		Layers []struct {
			ID      string `json:"id"`
			Outcome string `json:"outcome"`
			Details struct {
				Cap        float64 `json:"cap"`
				Violations []struct {
					Func   string  `json:"func"`
					File   string  `json:"file"`
					CC     int     `json:"cc"`
					CovPct float64 `json:"cov_pct"`
					CRAP   float64 `json:"crap"`
				} `json:"violations"`
			} `json:"details"`
		} `json:"layers"`
	}
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatal(err)
	}
	outcome := map[string]string{}
	for _, l := range rep.Layers {
		outcome[l.ID] = l.Outcome
		if l.ID != "crap" {
			continue
		}
		if l.Details.Cap != 10 || len(l.Details.Violations) != 1 {
			t.Fatalf("crap details %+v: want cap 10 and one violation", l.Details)
		}
		v := l.Details.Violations[0]
		if !strings.HasSuffix(v.Func, "Grade") || v.File != "weak.go" || v.CC != 9 ||
			math.Abs(v.CovPct-40) > 0.5 || math.Abs(v.CRAP-26.496) > 0.05 {
			t.Errorf("violation %+v, want Grade in weak.go, cc 9, 40%% covered, CRAP 26.5", v)
		}
	}
	if outcome["tests"] != "pass" || outcome["coverage"] != "pass" || outcome["crap"] != "fail" {
		t.Errorf("outcomes %v: want tests and coverage pass and crap fail", outcome)
	}
	if !strings.Contains(stdout, "Grade") || strings.Contains(stdout, "Shipping") {
		t.Errorf("the text output must name Grade and not Shipping:\n%s", stdout)
	}
}
