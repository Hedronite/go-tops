package calibration

import (
	"strings"
	"testing"
)

// The contract is GO_TOPS.md §6.2: a function is over the cap when its CRAP is
// above it, compared unrounded, so a score equal to the cap is not over.
func TestOverCapIsStrictAndUnrounded(t *testing.T) {
	fs := []scored{{CRAP: 9.999999}, {CRAP: 10}, {CRAP: 10.0000001}, {CRAP: 40}}
	for _, c := range []struct {
		cap  float64
		want int
	}{{6, 4}, {8, 4}, {10, 2}, {40, 0}} {
		if got := overCap(fs, c.cap); got != c.want {
			t.Errorf("overCap(%v) = %d, want %d", c.cap, got, c.want)
		}
	}
}

// A function that is not measured has a complexity and no coverage: it is over
// the cap exactly when its complexity is, which is how the layer judges it
// (§6.2) and why it counts in the repository's over-cap figure.
func TestNotMeasuredIsOverByComplexityAlone(t *testing.T) {
	ns := []unmeasured{{Comp: 5}, {Comp: 10}, {Comp: 11}, {Comp: 30}}
	r := repoResult{Funcs: []scored{{CRAP: 40, Comp: 12}, {CRAP: 2, Comp: 2}}, NotMeasured: ns}
	for _, c := range []struct {
		limit        float64
		notMeasured  int
		over, byComp int
	}{{6, 3, 4, 4}, {10, 2, 3, 3}, {11, 1, 2, 2}, {30, 0, 1, 0}} {
		if got := overNotMeasured(ns, c.limit); got != c.notMeasured {
			t.Errorf("overNotMeasured(%v) = %d, want %d", c.limit, got, c.notMeasured)
		}
		if got := r.over(c.limit); got != c.over {
			t.Errorf("over(%v) = %d, want %d (measured over the cap plus not measured over it)", c.limit, got, c.over)
		}
		if got := r.overByComplexity(c.limit); got != c.byComp {
			t.Errorf("overByComplexity(%v) = %d, want %d", c.limit, got, c.byComp)
		}
	}
	if r.functions() != 6 {
		t.Errorf("functions() = %d, want every function, measured or not", r.functions())
	}
}

// A function whose complexity is above the cap fails at every coverage (§6.2):
// one at the cap does not, because full coverage brings it to the cap.
func TestOverByComplexityIsStrict(t *testing.T) {
	fs := []scored{{Comp: 5}, {Comp: 10}, {Comp: 11}, {Comp: 30}}
	for _, c := range []struct {
		limit float64
		want  int
	}{{6, 3}, {10, 2}, {11, 1}, {30, 0}} {
		if got := overByComplexity(fs, c.limit); got != c.want {
			t.Errorf("overByComplexity(%v) = %d, want %d", c.limit, got, c.want)
		}
	}
}

// Each bin holds the scores above the previous edge and up to its own, so the
// bins at 6, 8 and 10 line up with the "over the cap" counts.
func TestHistogramEdgesLineUpWithTheCaps(t *testing.T) {
	scores := []float64{1, 4, 4.5, 6, 6.01, 8, 9, 10, 10.01, 15, 15.5, 30, 31}
	var fs []scored
	for _, s := range scores {
		fs = append(fs, scored{CRAP: s})
	}
	got := histogram(fs)
	// <=4: 1,4  <=6: 4.5,6  <=8: 6.01,8  <=10: 9,10  <=15: 10.01,15  <=30: 15.5,30  >30: 31
	want := []int{2, 2, 2, 2, 2, 2, 1}
	if len(got) != len(want) {
		t.Fatalf("histogram = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("histogram = %v, want %v", got, want)
			break
		}
	}
	sum := 0
	for _, n := range got {
		sum += n
	}
	if sum != len(fs) {
		t.Errorf("the bins hold %d functions of %d", sum, len(fs))
	}
}

// The top scorers come highest first, ties by key, and the list never holds
// more than were asked for.
func TestTopN(t *testing.T) {
	fs := []scored{{Key: "b", CRAP: 5}, {Key: "a", CRAP: 5}, {Key: "c", CRAP: 9}, {Key: "d", CRAP: 1}}
	got := topN(fs, 3)
	if len(got) != 3 || got[0].Key != "c" || got[1].Key != "a" || got[2].Key != "b" {
		t.Errorf("topN = %+v", got)
	}
	if len(topN(fs, 10)) != 4 {
		t.Error("topN returned more than there are")
	}
	if fs[0].Key != "b" {
		t.Error("topN reordered its input")
	}
}

const probeSample = `{"go_version":"go1.27.4","probe":[
 {"repo":"augur","functions":140,"caps":[4,5,6,8,10,15,30],"functions_over_cap":{"neutral":[64,49,35,28,19,7,1],"ccdisc_only":[52,40,31,24,16,6,0],"raw":[67,52,40,30,22,10,2]}},
 {"repo":"vigil","functions":100,"caps":[4,5,6,8,10,15,30],"functions_over_cap":{"neutral":[20,15,10,8,6,3,0]}}]}`

// The probe's guard-neutral row is the one the native layer is compared with.
func TestParseProbeReadsTheNeutralRow(t *testing.T) {
	p, err := parseProbe([]byte(probeSample))
	if err != nil {
		t.Fatal(err)
	}
	a := p["augur"]
	if a.Functions != 140 || a.Over[6] != 35 || a.Over[8] != 28 || a.Over[10] != 19 {
		t.Errorf("augur = %+v", a)
	}
	if a.CompOnly[6] != 31 || a.CompOnly[10] != 16 {
		t.Errorf("augur complexity alone = %v", a.CompOnly)
	}
	if v := p["vigil"]; v.Functions != 100 || v.Over[10] != 6 || v.CompOnly != nil {
		t.Errorf("vigil = %+v", v)
	}
	for _, bad := range []string{``, `{`, `{"probe":[{"repo":"x","functions":1,"caps":[6],"functions_over_cap":{}}]}`,
		`{"probe":[{"repo":"x","functions":1,"caps":[6,8],"functions_over_cap":{"neutral":[1]}}]}`,
		`{"probe":[{"repo":"x","functions":1,"caps":[6,8],"functions_over_cap":{"neutral":[1,1],"ccdisc_only":[1]}}]}`} {
		if _, err := parseProbe([]byte(bad)); err == nil {
			t.Errorf("parseProbe(%q) accepted a bad document", bad)
		}
	}
}

func TestDelta(t *testing.T) {
	for _, c := range []struct {
		native, probe int
		want          string
	}{{5, 5, "0"}, {7, 4, "+3"}, {2, 6, "-4"}} {
		if got := delta(c.native, c.probe); got != c.want {
			t.Errorf("delta(%d, %d) = %q, want %q", c.native, c.probe, got, c.want)
		}
	}
}

func TestRenderTables(t *testing.T) {
	rs := []repoResult{
		{Repo: "augur", SHA: "fa72aa71aef2e86a4ece01d5188ecc50aced7b55", Funcs: []scored{
			{Key: "p.A", File: "a.go", Comp: 12, Cov: 0.5, CRAP: 30}, {Key: "p.B", File: "a.go", Comp: 2, Cov: 1, CRAP: 2}}},
		{Repo: "vigil", SHA: "4a71f03ff8e52736963731b2dd088db643c13e03", Funcs: []scored{
			{Key: "p.C", File: "c.go", Comp: 7, Cov: 1, CRAP: 7}}},
		{Repo: "scry", SHA: "a33d4de203f6fc06f60982736947ec2e0c9099f5", Funcs: []scored{
			{Key: "p.D", File: "d.go", Comp: 3, Cov: 1, CRAP: 3}},
			NotMeasured: []unmeasured{{Key: "p.E", File: "e_windows.go", Comp: 14}}},
	}
	p, err := parseProbe([]byte(probeSample))
	if err != nil {
		t.Fatal(err)
	}
	out := renderTables(rs, p)
	for _, want := range []string{
		"| augur | `fa72aa7` | 2 |",                    // native functions
		"| vigil | `4a71f03` | 1 |",                    // short SHA
		"| **all** | | **5** |",                        // total row: every function, measured or not
		"| scry | `a33d4de` | 2 | n/a | n/a | 1 | 1 |", // 1 not measured; over cap 6 by its complexity
		"**240**",             // probe functions 140 + 100
		"p.A", "a.go", "30.0", // top scorer with its score
		"complexity alone",
		"| augur | 1 | 31 | -30 |", // complexity above 6: native 1 (12), probe 31
		"(<=4)", "(>30)",           // histogram bin labels
	} {
		if !strings.Contains(out, want) {
			t.Errorf("tables lack %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\t") {
		t.Error("tables hold a tab")
	}
	if out != renderTables(rs, p) {
		t.Error("rendering is not deterministic")
	}
}
