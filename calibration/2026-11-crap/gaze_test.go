//go:build calibration

package calibration

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The oracle is GO_TOPS.md §6.2 and §6.3 worked by hand on the fixture of the
// gaze cross-check (testdata/gaze-crosscheck): for each function its
// complexity after the error-guard discount and the guard-neutral statements
// covered and total by the fixture's own tests. The native scorer must give
// the complexity exactly and CRAP = comp² × (1 − cov)³ + comp to a millionth.
// gaze's own figures, which differ on three rows, are in gaze.json.
func TestGazeFixtureMatchesTheContract(t *testing.T) {
	const module = "example.com/gazecheck"
	want := map[string]struct{ comp, covered, total int }{
		module + ".Parse":           {2, 6, 7}, // two guards discounted, their bodies out of coverage
		module + ".Wrapv":           {2, 3, 4},
		module + ".Classify":        {3, 2, 5},
		module + ".Dispatch":        {5, 1, 2},
		module + ".Sum":             {4, 7, 8},
		module + ".Double":          {2, 2, 3}, // a package-level literal, scored on its own
		module + ".Max":             {2, 2, 3},
		module + ".Stack.Push":      {1, 1, 1},
		module + ".Stack.Pop":       {2, 5, 6},
		module + ".Noop":            {1, 0, 0}, // no statements: cov = 1
		module + ".Untested":        {2, 0, 2},
		module + ".init@check.go#1": {1, 1, 1},
		module + ".init@check.go#2": {2, 2, 3},
	}
	dir, err := filepath.Abs(filepath.Join("..", "..", "testdata", "gaze-crosscheck"))
	if err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(t.TempDir(), "check.cover")
	cmd := exec.CommandContext(t.Context(), "go", "test", "-race", "-count=1", "-coverprofile="+profile, "-coverpkg=./...", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1", "GOFLAGS=-count=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the fixture's tests: %v\n%s", err, out)
	}
	measured, not, err := scoreModule(dir, profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(not) != 0 {
		t.Errorf("the fixture compiles everywhere here, but %d functions are not measured", len(not))
	}
	if len(measured) != len(want) {
		t.Errorf("scored %d functions, want %d", len(measured), len(want))
	}
	for _, f := range measured {
		w, ok := want[f.Key]
		if !ok {
			t.Errorf("unexpected function %s", f.Key)
			continue
		}
		cov := 1.0
		if w.total > 0 {
			cov = float64(w.covered) / float64(w.total)
		}
		c, miss := float64(w.comp), 1-cov
		crap := c*c*miss*miss*miss + c // §6.2, written out here and not taken from the scorer
		if f.Comp != w.comp || math.Abs(f.CRAP-crap) > 1e-6 {
			t.Errorf("%s: native comp %d crap %.6f, contract comp %d crap %.6f", f.Key, f.Comp, f.CRAP, w.comp, crap)
		}
	}
}
