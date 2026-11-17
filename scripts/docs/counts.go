package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The law catalog is the set of tables in GO_TOPS.md whose header row starts
// with "ID" and has a "v0.1" column; each body row's first cell is a law ID
// "G-NN". Three numbers come from it:
//
//	catalog  every row, retired or not (retired laws keep their ID)
//	active   rows not retired
//	v01      rows whose v0.1 cell is exactly "✓" and that are not retired
//	notEval  active - v01: the laws a v0.1 report lists as not-evaluated
//
// A row is retired when its Law cell, after leading "~", "*" and spaces,
// begins with "Retired" (any case). Counts stated in prose are checked against
// these; see countPats for the phrases that are recognised.
type catalog struct {
	Catalog, Active, V01, Retired int
}

// notEvaluated is the "not_evaluated" figure of a report: active laws that
// are not judged at v0.1. Retired rows are in neither term.
func (c catalog) notEvaluated() int { return c.Active - c.V01 }

const catalogFile = "GO_TOPS.md"

// countedFiles are the files whose stated counts must match the catalog.
var countedFiles = []string{catalogFile, "README.md"}

var idCellRe = regexp.MustCompile(`^G-(\d+)$`)

func parseCatalog(f *mdFile) (catalog, []finding) {
	var c catalog
	var out []finding
	ids := map[int]int{} // id -> first line
	inTable, width, v01Col, lawCol := false, 0, -1, -1
	for i, line := range f.Lines {
		n := i + 1
		if f.InFence[i] || !strings.HasPrefix(strings.TrimSpace(line), "|") {
			inTable = false
			continue
		}
		cells := splitRow(line)
		if !inTable {
			if cells[0] == "ID" {
				v01Col, lawCol = indexOf(cells, "v0.1"), indexOf(cells, "Law")
				if v01Col >= 0 && lawCol >= 0 {
					inTable, width = true, len(cells)
				}
			}
			continue
		}
		m := idCellRe.FindStringSubmatch(cells[0])
		if m == nil {
			continue // separator row
		}
		if len(cells) != width {
			out = append(out, finding{f.Rel, n, fmt.Sprintf("%s has %d cells, the table header has %d", cells[0], len(cells), width)})
			continue
		}
		id, _ := strconv.Atoi(m[1])
		if first, dup := ids[id]; dup {
			out = append(out, finding{f.Rel, n, fmt.Sprintf("%s repeats the row at line %d", cells[0], first)})
			continue
		}
		ids[id] = n
		c.Catalog++
		if isRetired(cells[lawCol]) {
			c.Retired++
			continue
		}
		c.Active++
		if cells[v01Col] == "✓" {
			c.V01++
		}
	}
	var missing []string
	for id := 1; id <= len(ids); id++ {
		if _, ok := ids[id]; !ok {
			missing = append(missing, fmt.Sprintf("G-%02d", id))
		}
	}
	if len(missing) > 0 {
		out = append(out, finding{f.Rel, 0, "catalog IDs are not G-01..G-NN without gaps; missing " + strings.Join(missing, ", ")})
	}
	return c, out
}

func indexOf(cells []string, want string) int {
	for i, c := range cells {
		if c == want {
			return i
		}
	}
	return -1
}

func isRetired(lawCell string) bool {
	s := strings.TrimLeft(lawCell, "~* ")
	return len(s) >= 7 && strings.EqualFold(s[:7], "retired")
}

// A statement is a count the prose makes about the catalog.
type statement struct {
	Qty  string // "catalog", "active", "v01" or "not_evaluated"
	N    int
	Text string
}

type countPat struct {
	re   *regexp.Regexp
	read func(m []string) []statement
}

func one(qty string, group int) func(m []string) []statement {
	return func(m []string) []statement {
		n, _ := strconv.Atoi(m[group])
		return []statement{{Qty: qty, N: n, Text: m[0]}}
	}
}

// countPats are tried in order; text a pattern matches is blanked so a later,
// looser pattern cannot read it again. A phrase not listed here is not
// checked: add its pattern here, with a test, before relying on it.
var countPats = []countPat{
	{regexp.MustCompile(`\b(\d+) of (\d+)(?: laws judged| judged| laws)\b`), func(m []string) []statement {
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		return []statement{{"v01", a, m[0]}, {"catalog", b, m[0]}}
	}},
	{regexp.MustCompile(`"catalog":\s*(\d+)`), one("catalog", 1)},
	{regexp.MustCompile(`"judged":\s*(\d+)`), one("v01", 1)},
	{regexp.MustCompile(`"not_evaluated":\s*(\d+)`), one("not_evaluated", 1)},
	{regexp.MustCompile("`G-01`(?:…|\\.\\.\\.)`G-(\\d+)`"), one("catalog", 1)},
	{regexp.MustCompile(`\b(\d+) active (v0\.1 )?laws\b`), func(m []string) []statement {
		n, _ := strconv.Atoi(m[1])
		if m[2] != "" {
			return []statement{{"v01", n, m[0]}}
		}
		return []statement{{"active", n, m[0]}}
	}},
	{regexp.MustCompile(`\b(\d+) laws? (?:judged|mechanically)\b`), one("v01", 1)},
	{regexp.MustCompile(`\bjudge (\d+) laws\b`), one("v01", 1)},
	{regexp.MustCompile(`\b(\d+) judged\b`), one("v01", 1)},
	{regexp.MustCompile(`\b(\d+) laws\b`), one("catalog", 1)},
}

// otherSuiteRe matches the sibling protocols, whose counts are not this
// catalog's.
var otherSuiteRe = regexp.MustCompile(`\b(?:Rust-TOPS|Bend\d*|Lean)\b`)

// segments splits a line into the units counted separately: table cells, or
// the whole line.
func segments(line string) []string {
	if strings.HasPrefix(strings.TrimSpace(line), "|") {
		return splitRow(line)
	}
	return []string{line}
}

// clauses returns the byte ranges of s that are read as units. A clause ends
// at ";", at a comma or a sentence-ending "." outside parentheses, and at the
// end of s, so "(53 laws, 15 judged)" stays whole and "judge 18 laws (...),
// mirroring Rust-TOPS's" splits in two.
func clauses(s string) [][2]int {
	masked := maskCode(s)
	var out [][2]int
	start, depth := 0, 0
	for i := 0; i < len(masked); i++ {
		switch c := masked[i]; {
		case c == '(':
			depth++
		case c == ')' && depth > 0:
			depth--
		case depth == 0 && (c == ';' || c == ',' || (c == '.' && (i+1 == len(masked) || masked[i+1] == ' '))):
			out = append(out, [2]int{start, i})
			start = i + 1
		}
	}
	return append(out, [2]int{start, len(masked)})
}

// readStatements returns the catalog counts a line states. A clause that names
// a sibling suite is skipped, and only that clause: the numbers around it in
// the same line or table cell are still read.
func readStatements(line string) []statement {
	var out []statement
	for _, seg := range segments(line) {
		for _, c := range clauses(seg) {
			out = append(out, readClause(seg[c[0]:c[1]])...)
		}
	}
	return out
}

func readClause(text string) []statement {
	if otherSuiteRe.MatchString(text) {
		return nil
	}
	var out []statement
	for _, p := range countPats {
		for _, loc := range p.re.FindAllStringSubmatchIndex(text, -1) {
			m := make([]string, len(loc)/2)
			for i := range m {
				if loc[2*i] >= 0 {
					m[i] = text[loc[2*i]:loc[2*i+1]]
				}
			}
			out = append(out, p.read(m)...)
			text = text[:loc[0]] + strings.Repeat(" ", loc[1]-loc[0]) + text[loc[1]:]
		}
	}
	return out
}

func (c catalog) value(qty string) int {
	switch qty {
	case "catalog":
		return c.Catalog
	case "active":
		return c.Active
	case "not_evaluated":
		return c.notEvaluated()
	}
	return c.V01
}

func checkCounts(r *repo) checkResult {
	res := checkResult{}
	src, ok := r.byRel[catalogFile]
	if !ok {
		res.Findings = append(res.Findings, finding{File: catalogFile, Msg: "not found: the law catalog lives here"})
		return res
	}
	cat, fnd := parseCatalog(src)
	res.Findings = append(res.Findings, fnd...)
	if cat.Catalog == 0 && len(fnd) == 0 {
		res.Findings = append(res.Findings, finding{File: catalogFile, Msg: "no catalog table found (header ID … Law … v0.1)"})
		return res
	}
	stated := 0
	for _, name := range countedFiles {
		f, ok := r.byRel[name]
		if !ok {
			continue
		}
		for i, line := range f.Lines {
			for _, s := range readStatements(line) {
				stated++
				if want := cat.value(s.Qty); s.N != want {
					res.Findings = append(res.Findings, finding{name, i + 1, fmt.Sprintf(
						"%q states %s = %d; the catalog has %s = %d", s.Text, s.Qty, s.N, s.Qty, want)})
				}
			}
		}
	}
	sort.SliceStable(res.Findings, func(i, j int) bool { return res.Findings[i].File < res.Findings[j].File })
	res.Summary = fmt.Sprintf("catalog %d (active %d, retired %d), v0.1 %d; %d stated counts checked", cat.Catalog, cat.Active, cat.Retired, cat.V01, stated)
	res.Vacuous = stated == 0
	return res
}
