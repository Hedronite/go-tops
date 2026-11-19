package crap

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/Hedronite/go-tops/internal/complexity"
	"github.com/Hedronite/go-tops/internal/cover"
)

// block is one block of the merged profile.
type block struct {
	start, end complexity.Pos
	stmts      int
	covered    bool
}

func readProfile(path string) ([]cover.Block, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("the coverage profile %s does not exist", path)
	}
	if err != nil {
		return nil, fmt.Errorf("read the coverage profile: %w", err)
	}
	if strings.TrimSpace(string(raw)) == "" {
		return nil, fmt.Errorf("the coverage profile %s is empty", path)
	}
	prof, err := cover.Parse(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return prof.Blocks, nil
}

type blockKey struct {
	file           string
	sl, sc, el, ec int
}

// blocksByFile merges the profile: a block listed more than once (one listing
// per test binary under -coverpkg) is one block, covered when any listing ran
// it, as the coverage layer reads it. Files are keyed module-relative; a block
// outside the module is dropped.
func blocksByFile(blocks []cover.Block, module string) map[string][]block {
	merged := map[blockKey]*block{}
	var order []blockKey
	for _, b := range blocks {
		rel, ok := strings.CutPrefix(b.File, module+"/")
		if !ok {
			continue
		}
		k := blockKey{rel, b.StartLine, b.StartCol, b.EndLine, b.EndCol}
		m, seen := merged[k]
		if !seen {
			m = &block{
				start: complexity.Pos{Line: b.StartLine, Col: b.StartCol},
				end:   complexity.Pos{Line: b.EndLine, Col: b.EndCol},
				stmts: b.Stmts,
			}
			merged[k] = m
			order = append(order, k)
		}
		m.covered = m.covered || b.Count > 0
	}
	out := map[string][]block{}
	for _, k := range order {
		out[k.file] = append(out[k.file], *merged[k])
	}
	return out
}

// coverage is cov(f) of GO_TOPS.md §6.2, guard-neutral: the covered statements
// over all statements of the blocks inside f, closures included, leaving out
// the blocks inside the body of a discounted guard. A function that has
// statements and no block has cov 0; one with no statement left has cov 1.
func coverage(f *fn, blocks []block) float64 {
	var inside []block
	for _, b := range blocks {
		if f.Span.Contains(b.start, b.end) {
			inside = append(inside, b)
		}
	}
	if len(inside) == 0 {
		if f.HasStatements {
			return 0
		}
		return 1
	}
	total, covered := 0, 0
	for _, b := range inside {
		if inGuard(f.Guards, b) {
			continue
		}
		total += b.stmts
		if b.covered {
			covered += b.stmts
		}
	}
	if total == 0 {
		return 1
	}
	return float64(covered) / float64(total)
}

func inGuard(guards []complexity.Span, b block) bool {
	for _, g := range guards {
		if g.Contains(b.start, b.end) {
			return true
		}
	}
	return false
}
