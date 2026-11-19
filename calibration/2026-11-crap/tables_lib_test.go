package calibration

// The tables of the calibration record. This file holds no kit code: a
// _test.go file is outside the CRAP scope (GO_TOPS.md §6.2), and the tagged
// calibration test and these helpers are the only code in this directory.

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// scored is one production function as the native CRAP layer scores it.
type scored struct {
	Key  string  `json:"key"`
	File string  `json:"file"`
	Comp int     `json:"comp"`
	Cov  float64 `json:"cov"`
	CRAP float64 `json:"crap"`
}

// repoResult is every scored function of one repository at its pinned commit.
type repoResult struct {
	Repo  string
	SHA   string
	Funcs []scored
}

// probeRepo is the probe's figure for one repository: its function count and,
// per cap, the functions over it under the guard-neutral rule, and the ones
// over it by complexity alone (nil when the probe file lacks that row).
type probeRepo struct {
	Functions int
	Over      map[int]int
	CompOnly  map[int]int
}

var (
	// caps are the CRAP caps the record counts over (the classes' range).
	caps = []float64{6, 8, 10}
	// binEdges are the upper edges of the CRAP histogram; a last bin holds the rest.
	binEdges = []float64{4, 6, 8, 10, 15, 30}
)

// overCap counts the functions above the limit, compared unrounded (§6.2).
func overCap(fs []scored, limit float64) int {
	n := 0
	for _, f := range fs {
		if f.CRAP > limit {
			n++
		}
	}
	return n
}

// overByComplexity counts the functions whose complexity alone is above the
// limit: they fail at every coverage (§6.2), and no test can change that.
func overByComplexity(fs []scored, limit float64) int {
	n := 0
	for _, f := range fs {
		if float64(f.Comp) > limit {
			n++
		}
	}
	return n
}

// histogram counts the functions per bin: bin i holds the scores above the
// previous edge and up to binEdges[i]; the last bin holds the scores above the
// last edge.
func histogram(fs []scored) []int {
	bins := make([]int, len(binEdges)+1)
	for _, f := range fs {
		i, _ := slices.BinarySearchFunc(binEdges, f.CRAP, cmp.Compare[float64])
		bins[i]++
	}
	return bins
}

// topN returns the n highest-scoring functions, highest first and ties by key,
// leaving fs as it was.
func topN(fs []scored, n int) []scored {
	s := slices.Clone(fs)
	slices.SortFunc(s, func(a, b scored) int {
		return cmp.Or(cmp.Compare(b.CRAP, a.CRAP), cmp.Compare(a.Key, b.Key))
	})
	return s[:min(n, len(s))]
}

// parseProbe reads the probe's results.json: per repository, the function
// count and the guard-neutral counts over each cap (the "neutral" row).
func parseProbe(raw []byte) (map[string]probeRepo, error) {
	var doc struct {
		Probe []struct {
			Repo      string `json:"repo"`
			Functions int    `json:"functions"`
			Caps      []int  `json:"caps"`
			Over      struct {
				Neutral    []int `json:"neutral"`
				CCDiscOnly []int `json:"ccdisc_only"`
			} `json:"functions_over_cap"`
		} `json:"probe"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("probe results: %w", err)
	}
	out := map[string]probeRepo{}
	for _, p := range doc.Probe {
		if len(p.Over.Neutral) == 0 || len(p.Over.Neutral) != len(p.Caps) {
			return nil, fmt.Errorf("probe results: %s: %d neutral counts for %d caps", p.Repo, len(p.Over.Neutral), len(p.Caps))
		}
		if n := len(p.Over.CCDiscOnly); n != 0 && n != len(p.Caps) {
			return nil, fmt.Errorf("probe results: %s: %d complexity counts for %d caps", p.Repo, n, len(p.Caps))
		}
		r := probeRepo{Functions: p.Functions, Over: map[int]int{}}
		if len(p.Over.CCDiscOnly) != 0 {
			r.CompOnly = map[int]int{}
		}
		for i, c := range p.Caps {
			r.Over[c] = p.Over.Neutral[i]
			if r.CompOnly != nil {
				r.CompOnly[c] = p.Over.CCDiscOnly[i]
			}
		}
		out[p.Repo] = r
	}
	if len(out) == 0 {
		return nil, errors.New("probe results: no repository")
	}
	return out, nil
}

// delta is native minus probe, signed, "0" when they agree.
func delta(native, probe int) string {
	switch d := native - probe; {
	case d == 0:
		return "0"
	case d > 0:
		return fmt.Sprintf("+%d", d)
	default:
		return fmt.Sprint(d)
	}
}

// renderTables writes the record's three tables: counts against the probe,
// the CRAP histogram and the five top scorers per repository. The output is
// deterministic.
func renderTables(rs []repoResult, probe map[string]probeRepo) string {
	var b strings.Builder
	counts(&b, rs, probe)
	alone(&b, rs, probe)
	hist(&b, rs)
	top(&b, rs)
	return b.String()
}

func counts(b *strings.Builder, rs []repoResult, probe map[string]probeRepo) {
	b.WriteString("| Repo | Commit | Functions | Probe | Δ |")
	for _, c := range caps {
		fmt.Fprintf(b, " Over %g | Probe | Δ |", c)
	}
	b.WriteString("\n|---|---|---|---|---|" + strings.Repeat("---|---|---|", len(caps)) + "\n")

	var n, pn int
	over, pover := make([]int, len(caps)), make([]int, len(caps))
	allProbed := true
	for _, r := range rs {
		p, ok := probe[r.Repo]
		fmt.Fprintf(b, "| %s | `%.7s` | %d | %s | %s |", r.Repo, r.SHA, len(r.Funcs), cell(ok, p.Functions), cellDelta(ok, len(r.Funcs), p.Functions))
		n += len(r.Funcs)
		pn += p.Functions
		allProbed = allProbed && ok
		for i, c := range caps {
			k := overCap(r.Funcs, c)
			over[i] += k
			pover[i] += p.Over[int(c)]
			fmt.Fprintf(b, " %d | %s | %s |", k, cell(ok, p.Over[int(c)]), cellDelta(ok, k, p.Over[int(c)]))
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(b, "| **all** | | **%d** | **%d** | %s |", n, pn, cellDelta(allProbed, n, pn))
	for i := range caps {
		fmt.Fprintf(b, " **%d** | **%d** | %s |", over[i], pover[i], cellDelta(allProbed, over[i], pover[i]))
	}
	b.WriteString("\n\n")
}

// alone writes the count of functions over each cap by complexity alone,
// against the probe's guard-discounted complexity row.
func alone(b *strings.Builder, rs []repoResult, probe map[string]probeRepo) {
	b.WriteString("Over the cap by complexity alone (no test can change these):\n\n| Repo |")
	for _, c := range caps {
		fmt.Fprintf(b, " Comp > %g | Probe | Δ |", c)
	}
	b.WriteString("\n|---|" + strings.Repeat("---|---|---|", len(caps)) + "\n")
	over, pover := make([]int, len(caps)), make([]int, len(caps))
	allProbed := true
	for _, r := range rs {
		p := probe[r.Repo]
		ok := p.CompOnly != nil
		allProbed = allProbed && ok
		fmt.Fprintf(b, "| %s |", r.Repo)
		for i, c := range caps {
			k := overByComplexity(r.Funcs, c)
			over[i] += k
			pover[i] += p.CompOnly[int(c)]
			fmt.Fprintf(b, " %d | %s | %s |", k, cell(ok, p.CompOnly[int(c)]), cellDelta(ok, k, p.CompOnly[int(c)]))
		}
		b.WriteString("\n")
	}
	b.WriteString("| **all** |")
	for i := range caps {
		fmt.Fprintf(b, " **%d** | **%d** | %s |", over[i], pover[i], cellDelta(allProbed, over[i], pover[i]))
	}
	b.WriteString("\n\n")
}

func cell(ok bool, n int) string {
	if !ok {
		return "n/a"
	}
	return fmt.Sprint(n)
}

func cellDelta(ok bool, native, probe int) string {
	if !ok {
		return "n/a"
	}
	return delta(native, probe)
}

func hist(b *strings.Builder, rs []repoResult) {
	labels := make([]string, 0, len(binEdges)+1)
	for _, e := range binEdges {
		labels = append(labels, fmt.Sprintf("(<=%g)", e))
	}
	labels = append(labels, fmt.Sprintf("(>%g)", binEdges[len(binEdges)-1]))
	b.WriteString("| Repo | " + strings.Join(labels, " | ") + " |\n|---|" + strings.Repeat("---|", len(labels)) + "\n")
	total := make([]int, len(labels))
	for _, r := range rs {
		fmt.Fprintf(b, "| %s |", r.Repo)
		for i, n := range histogram(r.Funcs) {
			total[i] += n
			fmt.Fprintf(b, " %d |", n)
		}
		b.WriteString("\n")
	}
	b.WriteString("| **all** |")
	for _, n := range total {
		fmt.Fprintf(b, " **%d** |", n)
	}
	b.WriteString("\n\n")
}

func top(b *strings.Builder, rs []repoResult) {
	b.WriteString("| Repo | Function | File | Comp | Coverage | CRAP |\n|---|---|---|---|---|---|\n")
	for _, r := range rs {
		for _, f := range topN(r.Funcs, 5) {
			fmt.Fprintf(b, "| %s | `%s` | `%s` | %d | %.1f%% | %.1f |\n", r.Repo, f.Key, f.File, f.Comp, 100*f.Cov, f.CRAP)
		}
	}
}
