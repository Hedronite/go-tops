package mutate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMutatedLineHashOracles(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		line    int
		col     int
		mutator string
		want    string
	}{
		{
			name:    "conditionals_boundary_trailing_comment_dropped",
			src:     "package p\nfunc f(a, b int) bool {\n\treturn a < b // trailing comment\n}\n",
			line:    3,
			col:     11,
			mutator: "CONDITIONALS_BOUNDARY",
			want:    "feaccb4b61bcfee0368ad3cd4edda16bbd391bbce0018a9d658f5cc65fa4301c",
		},
		{
			name:    "increment_decrement_explicit_semicolons_kept",
			src:     "package p\nfunc f(n int) int {\n\tx := 0\n\tfor i := 0; i < n; i++ {\n\t\tx++\n\t}\n\treturn x\n}\n",
			line:    4,
			col:     22,
			mutator: "INCREMENT_DECREMENT",
			want:    "39789f093b70ec013f3f5b22bb942f66ed6b72b4abd5f38d03db8229f42fedb8",
		},
		{
			name:    "invert_logical_protocol_default_on",
			src:     "package p\nfunc f(x, y bool) bool {\n\treturn x || y\n}\n",
			line:    3,
			col:     11,
			mutator: "INVERT_LOGICAL",
			want:    "f5b2e363025e88166cbe9b9a4464b72036b89613ad553c499f5a1c9ed8ceb00b",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MutatedLineHash([]byte(tc.src), tc.line, tc.col, tc.mutator)
			if err != nil {
				t.Fatalf("MutatedLineHash error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("hash = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestMutatedLineHashStabilityAndColDistinction(t *testing.T) {
	early := "package p\nfunc f(a, b, c, d int) bool {\n\treturn a < b && c < d\n}\n"
	moved := "package p\n\n\n\n\nfunc f(a, b, c, d int) bool {\n\treturn a < b && c < d\n}\n"

	hEarly1, err := MutatedLineHash([]byte(early), 3, 11, "CONDITIONALS_BOUNDARY")
	if err != nil {
		t.Fatal(err)
	}
	hMoved1, err := MutatedLineHash([]byte(moved), 7, 11, "CONDITIONALS_BOUNDARY")
	if err != nil {
		t.Fatal(err)
	}
	if hEarly1 != hMoved1 {
		t.Fatalf("moved line hash changed: %s vs %s", hEarly1, hMoved1)
	}

	hEarly2, err := MutatedLineHash([]byte(early), 3, 20, "CONDITIONALS_BOUNDARY")
	if err != nil {
		t.Fatal(err)
	}
	if hEarly1 == hEarly2 {
		t.Fatalf("two mutants of same mutator on one line collided: %s", hEarly1)
	}

	if _, err := MutatedLineHash([]byte(early), 3, 99, "CONDITIONALS_BOUNDARY"); err == nil {
		t.Fatal("expected error for missing column")
	}
	if _, err := MutatedLineHash([]byte(early), 3, 11, "UNKNOWN_MUTATOR"); err == nil {
		t.Fatal("expected error for unknown mutator")
	}
}

func TestScanModuleEnclosingFunc(t *testing.T) {
	dir := t.TempDir()
	src := `package p

type Cache[K comparable, V any] struct{}

func (c *Cache[K, V]) Get(a, b int) bool {
	fn := func() bool {
		return a < b
	}
	return fn()
}

var checkLimit = func(x int) bool {
	return x < 10
}

func topLevel(x int) bool {
	return x > 0
}
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/p\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "p.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := ScanModule(dir)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		line int
		col  int
		want string
	}{
		{name: "funclit_inside_generic_method", line: 7, col: 12, want: "Cache.Get"},
		{name: "package_var_funclit", line: 13, col: 11, want: "checkLimit"},
		{name: "top_level_func", line: 17, col: 11, want: "topLevel"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := idx.EnclosingFunc("p.go", tc.line, tc.col); got != tc.want {
				t.Fatalf("EnclosingFunc = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCountMutantsOnLinesIgnoresPointerStars(t *testing.T) {
	src := `package p

func f(p *int, a, b int) bool {
	if p == nil {
		return a < b && b > 0
	}
	for i := 0; i < 2; i++ {
		a += -b
		break
	}
	return false
}
`
	enabled := EnabledMutators()
	lines := map[int]bool{3: true, 4: true, 5: true, 7: true, 8: true, 9: true}
	got, err := CountMutantsOnLines([]byte(src), lines, enabled)
	if err != nil {
		t.Fatal(err)
	}
	// Line 3: `*int` pointer (0). Line 4: `==` (1). Line 5: `<`(2)+`&&`(1)+`>`(2)=5.
	// Line 7: `<`(2)+`++`(1)=3. Line 8: `+=`(0 enabled)+unary `-`(2: INVERT_NEGATIVES+ARITHMETIC_BASE)=2.
	// Line 9: `break`(0 enabled). Total = 11.
	if got != 11 {
		t.Fatalf("CountMutantsOnLines = %d, want 11", got)
	}
}
