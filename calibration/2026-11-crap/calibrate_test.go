//go:build calibration

package calibration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// TestCalibrate scores every production function of the five calibration repos
// with the native CRAP code, writes results.json and tables.md, and checks the
// scorer against the gate's own report on one repo. run.sh sets the
// environment; the record is written by this test and by nothing else:
//
//	CALIBRATION_WORK   a directory with <repo>/ clones, <repo>.cover profiles and repos.txt ("name sha")
//	CALIBRATION_OUT    where results.json, tables.md and crosscheck.md go
//	CALIBRATION_PROBE  the probe's results.json, for the comparison
//	CALIBRATION_XREPO  the repo the gate was run on, with CALIBRATION_XDIR holding report-<cap>.json
func TestCalibrate(t *testing.T) {
	work, out := env(t, "CALIBRATION_WORK"), env(t, "CALIBRATION_OUT")
	rs := scoreAll(t, work)
	raw, err := os.ReadFile(env(t, "CALIBRATION_PROBE"))
	if err != nil {
		t.Fatal(err)
	}
	probe, err := parseProbe(raw)
	if err != nil {
		t.Fatal(err)
	}
	writeResults(t, out, rs)
	write(t, filepath.Join(out, "tables.md"), titled("Calibration tables, 2026-11", renderTables(rs, probe)))
	crossCheck(t, out, rs)
}

func env(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Fatalf("%s is not set; run calibration/2026-11-crap/run.sh", key)
	}
	return v
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// scoreAll scores each repo of repos.txt, functions in key order.
func scoreAll(t *testing.T, work string) []repoResult {
	t.Helper()
	list, err := os.ReadFile(filepath.Join(work, "repos.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var rs []repoResult
	for line := range strings.Lines(string(list)) {
		name, sha, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			t.Fatalf("repos.txt: bad line %q", line)
		}
		fs, ns, err := scoreModule(filepath.Join(work, name), filepath.Join(work, name+".cover"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(fs) == 0 {
			t.Fatalf("%s: the scorer measured no production function", name)
		}
		slices.SortFunc(fs, func(a, b scored) int { return strings.Compare(a.Key, b.Key) })
		slices.SortFunc(ns, func(a, b unmeasured) int { return strings.Compare(a.Key, b.Key) })
		rs = append(rs, repoResult{Repo: name, SHA: sha, Funcs: fs, NotMeasured: ns})
	}
	return rs
}

func writeResults(t *testing.T, out string, rs []repoResult) {
	t.Helper()
	type repoDoc struct {
		Repo        string       `json:"repo"`
		Commit      string       `json:"commit"`
		Functions   []scored     `json:"functions"`
		NotMeasured []unmeasured `json:"not_measured"`
	}
	doc := struct {
		GoVersion string    `json:"go_version"`
		Platform  string    `json:"platform"`
		Caps      []float64 `json:"caps"`
		Repos     []repoDoc `json:"repos"`
	}{GoVersion: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH, Caps: caps}
	for _, r := range rs {
		doc.Repos = append(doc.Repos, repoDoc{r.Repo, r.SHA, r.Funcs, r.NotMeasured})
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(out, "results.json"), string(b)+"\n")
}

// crossCheck compares the scorer's over-cap count on one repo with the number
// of violations in the report the gate wrote at the same cap. A difference
// fails the test, and the table says what was compared.
func crossCheck(t *testing.T, out string, rs []repoResult) {
	t.Helper()
	repo, dir := env(t, "CALIBRATION_XREPO"), env(t, "CALIBRATION_XDIR")
	i := slices.IndexFunc(rs, func(r repoResult) bool { return r.Repo == repo })
	if i < 0 {
		t.Fatalf("cross-check repo %s is not in repos.txt", repo)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "| Repo | Cap | Gate violations | Scorer over cap (measured, plus not measured by complexity) | Agree |\n|---|---|---|---|---|\n")
	for _, c := range caps {
		got, err := gateViolations(filepath.Join(dir, fmt.Sprintf("report-%g.json", c)))
		if err != nil {
			t.Fatalf("cap %g: %v", c, err)
		}
		want := rs[i].over(c)
		fmt.Fprintf(&b, "| %s | %g | %d | %d | %t |\n", repo, c, got, want, got == want)
		if got != want {
			t.Errorf("%s at cap %g: the gate reports %d violations, the scorer %d functions over the cap", repo, c, got, want)
		}
	}
	write(t, filepath.Join(out, "crosscheck.md"), titled("Gate cross-check, 2026-11", b.String()))
}

// gateViolations reads the crap layer's violations from a gate report.
func gateViolations(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var rep struct {
		Layers []struct {
			ID      string `json:"id"`
			Details struct {
				Violations []json.RawMessage `json:"violations"`
			} `json:"details"`
		} `json:"layers"`
	}
	if err := json.Unmarshal(raw, &rep); err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}
	for _, l := range rep.Layers {
		if l.ID == "crap" {
			return len(l.Details.Violations), nil
		}
	}
	return 0, fmt.Errorf("%s: no crap layer in the report", path)
}
