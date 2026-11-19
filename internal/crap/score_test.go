package crap_test

import (
	"math"
	"testing"

	"github.com/Hedronite/go-tops/internal/crap"
)

func scoreOf(t *testing.T, dir, prof string, discount bool) crap.Report {
	t.Helper()
	rep, err := crap.Score(dir, prof, crap.Options{ErrGuardDiscount: discount, Build: linux})
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func find(t *testing.T, rep crap.Report, name string) crap.Function {
	t.Helper()
	for _, f := range rep.Measured {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("%s is not among the measured functions: %+v", name, rep.Measured)
	return crap.Function{}
}

// The worked examples of §6.2 through the whole scorer: the function is fixture
// code of the stated complexity, the profile holds the stated statements, and
// the figure the scorer returns is the table's. Rows 8 to 10 are one function
// with three guards: row 8 discounts them, row 10 turns the discount off.
func TestWorkedExamplesEndToEnd(t *testing.T) {
	cases := []struct {
		row      int
		comp     int
		covered  int
		total    int
		guards   int // guard bodies in the function, none covered
		discount bool
		want     float64
	}{
		{1, 14, 52, 100, 0, true, 35.7},
		{2, 8, 70, 100, 0, true, 9.7},
		{3, 8, 68, 100, 0, true, 10.1},
		{4, 10, 40, 40, 0, true, 10.0},
		{5, 11, 44, 44, 0, true, 11.0},
		{7, 8, 99, 100, 0, true, 8.000064},
		{8, 6, 14, 17, 3, true, 6.2},
		{10, 9, 14, 17, 3, false, 11.2}, // 17 outside the guards, 3 inside: 14 of 20
	}
	for _, c := range cases {
		ifs := c.comp - 1
		if c.guards > 0 && !c.discount {
			ifs -= c.guards // the guards count in comp when the discount is off
		}
		s := shapeFunc(ifs, c.guards)
		lines := []string{
			block("m.go", s.tail, 2, s.tail, 12, c.covered, 1),
			block("m.go", s.tail+1, 2, s.tail+1, 12, c.total-c.covered, 0),
		}
		for _, g := range s.guardLines {
			lines = append(lines, block("m.go", g, 17, g+2, 2, 1, 0))
		}
		dir := fixtureModule(t, map[string]string{"m.go": s.src})
		rep := scoreOf(t, dir, profile(t, dir, lines...), c.discount)
		f := find(t, rep, "F")
		if f.Comp != c.comp {
			t.Errorf("row %d: comp %d, want %d", c.row, f.Comp, c.comp)
		}
		if math.Abs(f.CRAP-c.want) > 0.05 {
			t.Errorf("row %d: CRAP %v, want %v", c.row, f.CRAP, c.want)
		}
		if c.row == 7 && !(f.CRAP > 8) {
			t.Errorf("row 7: CRAP %v is not above the cap 8: it was rounded", f.CRAP)
		}
	}
}

// Row 11: a dispatch table of 29 cases and a default, 31 statements, all
// covered. comp is 30 and it is table-shaped.
func TestRow11DispatchTable(t *testing.T) {
	src := "package m\n\nfunc F(n int) int {\n\tswitch n {\n"
	for i := 1; i <= 29; i++ {
		src += "\tcase " + itoa(i) + ":\n\t\treturn " + itoa(i) + "\n"
	}
	src += "\tdefault:\n\t\treturn 0\n\t}\n}\n"
	dir := fixtureModule(t, map[string]string{"m.go": src})
	rep := scoreOf(t, dir, profile(t, dir, block("m.go", 4, 2, 66, 2, 31, 1)), true)
	f := find(t, rep, "F")
	if f.Comp != 30 || f.CRAP != 30 || !f.Table {
		t.Errorf("row 11: %+v, want comp 30, CRAP 30, table-shaped", f)
	}
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

// A function that has statements and no block in the profile has cov 0; one
// with no statements has cov 1; neither is guessed from a neighbour.
func TestCoverageEdgeRules(t *testing.T) {
	src := `package m

func Empty() {}

func Silent(a bool) int {
	if a {
		return 1
	}
	return 0
}

func Covered() int {
	return 1
}
`
	dir := fixtureModule(t, map[string]string{"m.go": src})
	rep := scoreOf(t, dir, profile(t, dir, block("m.go", 12, 18, 14, 2, 1, 1)), true)
	if f := find(t, rep, "Empty"); f.Cov != 1 || f.CRAP != 1 {
		t.Errorf("Empty: %+v, want cov 1", f)
	}
	if f := find(t, rep, "Silent"); f.Cov != 0 || f.CRAP != 6 { // comp 2: 4 * 1 + 2
		t.Errorf("Silent: %+v, want cov 0 and CRAP 6", f)
	}
	if f := find(t, rep, "Covered"); f.Cov != 1 {
		t.Errorf("Covered: %+v, want cov 1", f)
	}
}

// A block listed once per test binary is one block, covered when any listing
// ran it, as the coverage layer reads the profile.
func TestDuplicateBlocksMerge(t *testing.T) {
	src := "package m\n\nfunc F() int {\n\treturn 1\n}\n"
	dir := fixtureModule(t, map[string]string{"m.go": src})
	for _, counts := range [][2]int{{0, 4}, {4, 0}} {
		prof := profile(t, dir, block("m.go", 3, 14, 5, 2, 1, counts[0]), block("m.go", 3, 14, 5, 2, 1, counts[1]))
		if f := find(t, scoreOf(t, dir, prof, true), "F"); f.Cov != 1 {
			t.Errorf("listings %v: cov %v, want 1: one of two listings ran the block", counts, f.Cov)
		}
	}
}

// Closures are inside their function: their blocks count for it and for no
// other; a function does not take credit for a neighbour's blocks.
func TestClosureBlocksBelongToTheirFunction(t *testing.T) {
	src := `package m

func Outer(xs []int) func() int {
	n := 0
	for range xs {
		n++
	}
	return func() int {
		if n > 1 {
			return n
		}
		return 0
	}
}

func Other() int {
	return 1
}
`
	dir := fixtureModule(t, map[string]string{"m.go": src})
	prof := profile(t, dir,
		block("m.go", 4, 2, 8, 2, 4, 1),     // Outer body up to the closure
		block("m.go", 9, 3, 11, 4, 1, 0),    // inside the closure: not run
		block("m.go", 12, 3, 12, 12, 1, 0),  // inside the closure: not run
		block("m.go", 16, 2, 17, 2, 100, 1), // Other: must not reach Outer
	)
	rep := scoreOf(t, dir, prof, true)
	outer := find(t, rep, "Outer")
	if outer.Comp != 3+0 || math.Abs(outer.Cov-4.0/6.0) > 1e-12 {
		t.Errorf("Outer: comp %d cov %v, want comp 3 (for, closure if) and cov 4/6", outer.Comp, outer.Cov)
	}
	if other := find(t, rep, "Other"); other.Cov != 1 {
		t.Errorf("Other: cov %v, want 1", other.Cov)
	}
}

func TestPackageLevelLiteralsAreScoredOnTheirOwn(t *testing.T) {
	src := `package m

var Check = func(n int) bool {
	if n > 0 {
		return true
	}
	return false
}
`
	dir := fixtureModule(t, map[string]string{"m.go": src})
	rep := scoreOf(t, dir, profile(t, dir, block("m.go", 3, 30, 8, 2, 3, 1)), true)
	f := find(t, rep, "Check")
	if f.Key != module+".Check" || f.Comp != 2 {
		t.Errorf("literal: %+v, want key %s.Check, comp 2", f, module)
	}
}

func TestGenericsAndTheirMethods(t *testing.T) {
	src := `package m

type Box[T any] struct{ v *T }

func (b Box[T]) Get() (T, bool) {
	var zero T
	if b.v == nil {
		return zero, false
	}
	return *b.v, true
}

func (b *Box[T]) Set(v T) { b.v = &v }

func Map[T, U any](xs []T, f func(T) U) []U {
	var out []U
	for _, x := range xs {
		out = append(out, f(x))
	}
	return out
}
`
	dir := fixtureModule(t, map[string]string{"m.go": src})
	prof := profile(t, dir,
		block("m.go", 5, 29, 10, 2, 4, 1),
		block("m.go", 13, 28, 13, 38, 1, 1),
		block("m.go", 15, 45, 20, 2, 4, 0),
	)
	rep := scoreOf(t, dir, prof, true)
	want := map[string]struct {
		key  string
		comp int
		cov  float64
	}{
		"Get": {module + ".Box.Get", 2, 1},
		"Set": {module + ".Box.Set", 1, 1},
		"Map": {module + ".Map", 2, 0},
	}
	for name, w := range want {
		f := find(t, rep, name)
		if f.Key != w.key || f.Comp != w.comp || f.Cov != w.cov {
			t.Errorf("%s: %+v, want key %s comp %d cov %v", name, f, w.key, w.comp, w.cov)
		}
	}
}

// A function whose every block in the profile lies in a guard body has no
// statement left after the discount: cov is 1, not 0 and not a division by
// zero (§6.2).
func TestNothingLeftAfterTheDiscountIsFullyCovered(t *testing.T) {
	s := shapeFunc(0, 1)
	dir := fixtureModule(t, map[string]string{"m.go": s.src})
	g := s.guardLines[0]
	prof := profile(t, dir, block("m.go", g, 17, g+2, 2, 1, 0))
	f := find(t, scoreOf(t, dir, prof, true), "F")
	if f.Cov != 1 || f.CRAP != float64(f.Comp) {
		t.Errorf("F: cov %v CRAP %v, want cov 1 and CRAP = comp", f.Cov, f.CRAP)
	}
}
