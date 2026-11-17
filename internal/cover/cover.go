// Package cover reads Go coverage profiles and summarizes them as GO_TOPS.md
// §6.1 requires: statements, per package and in total, over the module's own
// production files.
package cover

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ErrNoBlocks is returned by Summarize when the profile holds no block of a
// production file: it is empty, or every block is in an excluded file (§6.1,
// §3.4 rule 5).
var ErrNoBlocks = errors.New("the profile holds no block of a production file")

// Block is one line of a profile: a stretch of source holding Stmts statements
// that ran Count times.
type Block struct {
	File                                 string
	StartLine, StartCol, EndLine, EndCol int
	Stmts, Count                         int
}

// Profile is a parsed coverage profile.
type Profile struct {
	Mode   string // set, count or atomic
	Blocks []Block
}

// Parse reads a profile in the format of cmd/cover: a "mode:" line, then
// "name.go:line.col,line.col numStmts count" lines.
func Parse(r io.Reader) (*Profile, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	p := &Profile{}
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if p.Mode == "" {
			mode, ok := strings.CutPrefix(line, "mode: ")
			if !ok || (mode != "set" && mode != "count" && mode != "atomic") {
				return nil, fmt.Errorf("line %d: a profile starts with a mode line (set, count or atomic), got %q", n, line)
			}
			p.Mode = mode
			continue
		}
		b, err := parseBlock(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		p.Blocks = append(p.Blocks, b)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if p.Mode == "" {
		return nil, errors.New("the profile is empty: no mode line")
	}
	return p, nil
}

var blockRE = regexp.MustCompile(`^(.+):(\d+)\.(\d+),(\d+)\.(\d+) (\d+) (\d+)$`)

func parseBlock(line string) (Block, error) {
	m := blockRE.FindStringSubmatch(line)
	if m == nil {
		return Block{}, fmt.Errorf("not a profile block: %q", line)
	}
	var v [6]int
	for i := range v {
		n, err := strconv.Atoi(m[i+2])
		if err != nil {
			return Block{}, fmt.Errorf("not a profile block: %q", line)
		}
		v[i] = n
	}
	var cnt int
	cnt, err := strconv.Atoi(m[7])
	if err != nil {
		return Block{}, fmt.Errorf("not a profile block: %q", line)
	}
	return Block{File: m[1], StartLine: v[0], StartCol: v[1], EndLine: v[2], EndCol: v[3], Stmts: v[4], Count: cnt}, nil
}

// Options say which files are the module's production code.
type Options struct {
	// Module is the module path; a profile file name is an import path under it.
	Module string
	// Dir is the module root on disk, used to read files for the generated check.
	Dir string
	// ExcludeGenerated drops files named *.gen.go and files whose header says
	// "Code generated ... DO NOT EDIT." (§6.1).
	ExcludeGenerated bool
}

// Package is the statement count of one package.
type Package struct {
	Path       string
	Statements int
	Covered    int
}

// Pct is the percentage of statements covered; 0 when there are none.
func (p Package) Pct() float64 { return pct(p.Covered, p.Statements) }

// Uncovered is the number of statements not covered.
func (p Package) Uncovered() int { return p.Statements - p.Covered }

// Summary is the coverage of the production code of a profile.
type Summary struct {
	Packages   []Package // sorted by import path
	Statements int
	Covered    int
}

// Pct is the percentage of statements covered in total.
func (s Summary) Pct() float64 { return pct(s.Covered, s.Statements) }

// Below returns the packages whose coverage is under floor percent, the
// lowest first. A package at the floor meets it.
func (s Summary) Below(floor float64) []Package {
	var out []Package
	for _, p := range s.Packages {
		if p.Pct() < floor {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Pct() < out[j].Pct() })
	return out
}

func pct(covered, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(covered) * 100 / float64(total)
}

type blockKey struct {
	file           string
	sl, sc, el, ec int
}

// Summarize totals the profile's statements per package. A block listed more
// than once (one per test binary under -coverpkg) is one block, covered when
// any listing ran it. Files excluded by opt are left out; if nothing remains
// it returns ErrNoBlocks.
func (p *Profile) Summarize(opt Options) (Summary, error) {
	type agg struct{ stmts, count int }
	blocks := map[blockKey]*agg{}
	for _, b := range p.Blocks {
		k := blockKey{b.File, b.StartLine, b.StartCol, b.EndLine, b.EndCol}
		if a, ok := blocks[k]; ok {
			a.count += b.Count
			continue
		}
		blocks[k] = &agg{stmts: b.Stmts, count: b.Count}
	}
	excluded := map[string]bool{}
	skip := func(file string) bool {
		if !opt.ExcludeGenerated {
			return false
		}
		if v, ok := excluded[file]; ok {
			return v
		}
		v := generated(opt, file)
		excluded[file] = v
		return v
	}
	pkgs := map[string]*Package{}
	for k, a := range blocks {
		if skip(k.file) {
			continue
		}
		dir := path.Dir(k.file)
		pk := pkgs[dir]
		if pk == nil {
			pk = &Package{Path: dir}
			pkgs[dir] = pk
		}
		pk.Statements += a.stmts
		if a.count > 0 {
			pk.Covered += a.stmts
		}
	}
	var s Summary
	for _, pk := range pkgs {
		s.Packages = append(s.Packages, *pk)
		s.Statements += pk.Statements
		s.Covered += pk.Covered
	}
	if len(s.Packages) == 0 {
		return Summary{}, ErrNoBlocks
	}
	sort.Slice(s.Packages, func(i, j int) bool { return s.Packages[i].Path < s.Packages[j].Path })
	return s, nil
}

var generatedHeader = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)

// generated reports whether the profile's file is generated: by its name, or
// by a "Code generated ... DO NOT EDIT." line before the package clause. A
// file that cannot be read is not shown to be generated.
func generated(opt Options, file string) bool {
	if strings.HasSuffix(file, ".gen.go") {
		return true
	}
	rel, ok := strings.CutPrefix(file, opt.Module+"/")
	if !ok || opt.Dir == "" {
		return false
	}
	f, err := os.Open(filepath.Join(opt.Dir, filepath.FromSlash(rel)))
	if err != nil {
		return false
	}
	found := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if generatedHeader.MatchString(line) {
			found = true
			break
		}
		if strings.HasPrefix(line, "package ") {
			break
		}
	}
	if err := f.Close(); err != nil {
		return false
	}
	return found
}
