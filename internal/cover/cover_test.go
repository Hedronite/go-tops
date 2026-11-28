package cover

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The oracle is the format of Go's coverage profile (cmd/cover, "go help
// testflag") and GO_TOPS.md §6.1: a figure is a percentage of statements, and
// a profile that holds no block of a production file fails.

const mod = "example.com/m"

func parse(t *testing.T, text string) *Profile {
	t.Helper()
	p, err := Parse(strings.NewReader(text))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return p
}

func TestParseReadsModeAndBlocks(t *testing.T) {
	p := parse(t, "mode: atomic\n"+
		"example.com/m/a/a.go:10.20,12.3 2 1\n"+
		"example.com/m/a/a.go:14.2,15.9 1 0\n")
	if p.Mode != "atomic" || len(p.Blocks) != 2 {
		t.Fatalf("%+v", p)
	}
	b := p.Blocks[0]
	if b.File != "example.com/m/a/a.go" || b.StartLine != 10 || b.StartCol != 20 || b.EndLine != 12 || b.EndCol != 3 || b.Stmts != 2 || b.Count != 1 {
		t.Errorf("block %+v", b)
	}
}

func TestParseRefusesWhatIsNotAProfile(t *testing.T) {
	bad := map[string]string{
		"empty":          "",
		"blank":          "\n\n",
		"no mode":        "example.com/m/a/a.go:1.1,2.2 1 1\n",
		"unknown mode":   "mode: sometimes\nexample.com/m/a/a.go:1.1,2.2 1 1\n",
		"missing field":  "mode: set\nexample.com/m/a/a.go:1.1,2.2 1\n",
		"no colon":       "mode: set\nexample.com/m/a/a.go 1 1\n",
		"bad range":      "mode: set\nexample.com/m/a/a.go:1.1-2.2 1 1\n",
		"negative count": "mode: set\nexample.com/m/a/a.go:1.1,2.2 1 -1\n",
		"text count":     "mode: set\nexample.com/m/a/a.go:1.1,2.2 1 many\n",
	}
	for name, text := range bad {
		if _, err := Parse(strings.NewReader(text)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
	_, err := Parse(strings.NewReader("mode: set\nexample.com/m/a/a.go:1.1,2.2 1 x\n"))
	var lineErr *LineError
	if !errors.As(err, &lineErr) || lineErr.Line != 2 {
		t.Errorf("the error must name the line: %v", err)
	}
}

// A profile that is a mode line and nothing else measured nothing.
func TestSummarizeOfAnEmptyProfileFails(t *testing.T) {
	p := parse(t, "mode: atomic\n")
	if _, err := p.Summarize(Options{Module: mod}); !errors.Is(err, ErrNoBlocks) {
		t.Fatalf("err = %v, want ErrNoBlocks", err)
	}
}

// 6 statements in a, 3 covered; 4 in b, 4 covered: statements, not lines.
func TestSummarizeCountsStatementsPerPackage(t *testing.T) {
	p := parse(t, "mode: atomic\n"+
		"example.com/m/a/a.go:5.10,9.2 3 1\n"+
		"example.com/m/a/a.go:10.2,12.2 3 0\n"+
		"example.com/m/b/b.go:3.10,5.2 1 7\n"+
		"example.com/m/b/c.go:3.10,5.2 3 2\n")
	s, err := p.Summarize(Options{Module: mod})
	if err != nil {
		t.Fatal(err)
	}
	if s.Statements != 10 || s.Covered != 7 || s.Pct() != 70 {
		t.Errorf("total %d/%d = %v, want 7/10 = 70", s.Covered, s.Statements, s.Pct())
	}
	if len(s.Packages) != 2 {
		t.Fatalf("packages %+v", s.Packages)
	}
	a, b := s.Packages[0], s.Packages[1]
	if a.Path != "example.com/m/a" || a.Statements != 6 || a.Covered != 3 || a.Pct() != 50 {
		t.Errorf("a %+v", a)
	}
	if b.Path != "example.com/m/b" || b.Statements != 4 || b.Covered != 4 || b.Pct() != 100 {
		t.Errorf("b %+v", b)
	}
	if a.Uncovered() != 3 || b.Uncovered() != 0 {
		t.Errorf("uncovered %d %d", a.Uncovered(), b.Uncovered())
	}
}

// With -coverpkg=./... a block can be listed once per test binary. It is one
// block: its statements count once and it is covered if any run covered it.
func TestDuplicateBlocksMerge(t *testing.T) {
	p := parse(t, "mode: atomic\n"+
		"example.com/m/a/a.go:5.10,9.2 4 0\n"+
		"example.com/m/a/a.go:5.10,9.2 4 3\n"+
		"example.com/m/a/a.go:10.2,12.2 2 0\n"+
		"example.com/m/a/a.go:10.2,12.2 2 0\n")
	s, err := p.Summarize(Options{Module: mod})
	if err != nil {
		t.Fatal(err)
	}
	if s.Statements != 6 || s.Covered != 4 {
		t.Fatalf("%d/%d, want 4/6", s.Covered, s.Statements)
	}
}

func TestBelowListsPackagesUnderTheFloorWorstFirst(t *testing.T) {
	p := parse(t, "mode: set\n"+
		"example.com/m/a/a.go:1.1,2.2 6 1\nexample.com/m/a/a.go:3.1,4.2 4 0\n"+ // 60%
		"example.com/m/b/b.go:1.1,2.2 1 1\nexample.com/m/b/b.go:3.1,4.2 9 0\n"+ // 10%
		"example.com/m/c/c.go:1.1,2.2 7 1\nexample.com/m/c/c.go:3.1,4.2 3 0\n"+ // 70%: at the floor
		"example.com/m/d/d.go:1.1,2.2 10 9\n")
	s, err := p.Summarize(Options{Module: mod})
	if err != nil {
		t.Fatal(err)
	}
	below := s.Below(70)
	if len(below) != 2 || below[0].Path != "example.com/m/b" || below[1].Path != "example.com/m/a" {
		t.Fatalf("below %+v", below)
	}
	if got := s.Below(0); len(got) != 0 {
		t.Errorf("a floor of 0 admits everything: %+v", got)
	}
}

// A package at exactly the floor meets it.
func TestBelowIsStrict(t *testing.T) {
	p := parse(t, "mode: set\nexample.com/m/c/c.go:1.1,2.2 7 1\nexample.com/m/c/c.go:3.1,4.2 3 0\n")
	s, err := p.Summarize(Options{Module: mod})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Below(70)) != 0 {
		t.Error("70% is not below a floor of 70%")
	}
	if len(s.Below(70.0001)) != 1 {
		t.Error("70% is below a floor of 70.0001%")
	}
}

// §6.1: generated files are excluded by default: by the header and by the
// .gen.go name.
func TestGeneratedFilesAreExcluded(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a/real.go", "package a\n\nfunc F() {}\n")
	write("a/stub.go", "// Code generated by mockgen. DO NOT EDIT.\n\npackage a\n\nfunc G() {}\n")
	write("a/late.go", "package a\n\n// Code generated by hand. DO NOT EDIT.\nfunc H() {}\n") // after the package clause: not a header
	write("a/table.gen.go", "package a\n\nfunc T() {}\n")
	p := parse(t, "mode: set\n"+
		"example.com/m/a/real.go:3.10,3.12 1 1\n"+
		"example.com/m/a/stub.go:5.10,5.12 5 0\n"+
		"example.com/m/a/late.go:4.10,4.12 1 0\n"+
		"example.com/m/a/table.gen.go:3.10,3.12 4 0\n")
	s, err := p.Summarize(Options{Module: mod, Dir: dir, ExcludeGenerated: true})
	if err != nil {
		t.Fatal(err)
	}
	if s.Statements != 2 || s.Covered != 1 {
		t.Errorf("excluded: %d/%d, want 1/2 (real.go and late.go)", s.Covered, s.Statements)
	}
	s, err = p.Summarize(Options{Module: mod, Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if s.Statements != 11 {
		t.Errorf("not excluded: %d statements, want 11", s.Statements)
	}
}

// A profile whose every block is in a generated file holds no block of a
// production file (§6.1): it fails rather than reading as 0 of 0.
func TestOnlyGeneratedBlocksFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.gen.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := parse(t, "mode: set\nexample.com/m/x.gen.go:1.1,2.2 3 3\n")
	if _, err := p.Summarize(Options{Module: mod, Dir: dir, ExcludeGenerated: true}); !errors.Is(err, ErrNoBlocks) {
		t.Fatalf("err = %v, want ErrNoBlocks", err)
	}
}

// A file the profile names but the disk lacks is production code to the gate:
// it cannot be shown to be generated.
func TestUnreadableFileIsProduction(t *testing.T) {
	p := parse(t, "mode: set\nexample.com/m/a/gone.go:1.1,2.2 2 0\n")
	s, err := p.Summarize(Options{Module: mod, Dir: t.TempDir(), ExcludeGenerated: true})
	if err != nil || s.Statements != 2 {
		t.Fatalf("%+v %v", s, err)
	}
}

func TestPctOfNothingIsZeroNotNaN(t *testing.T) {
	if (Summary{}).Pct() != 0 || (Package{}).Pct() != 0 {
		t.Fatal("a figure over no statements must be 0")
	}
}

// Files outside the module path (a dependency listed by -coverpkg, a
// stdlib file) stay in the profile under their own package: the gate does not
// guess which are the module's own. -coverpkg=./... lists only the module's.
func TestFilesAreGroupedByDirectory(t *testing.T) {
	p := parse(t, "mode: set\nexample.com/m/x/y/z.go:1.1,2.2 1 1\nexample.com/m/z.go:1.1,2.2 1 0\n")
	s, err := p.Summarize(Options{Module: mod})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Packages) != 2 || s.Packages[0].Path != "example.com/m" || s.Packages[1].Path != "example.com/m/x/y" {
		t.Fatalf("%+v", s.Packages)
	}
}
