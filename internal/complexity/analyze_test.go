package complexity_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Hedronite/go-tops/internal/complexity"
)

func analyzeSource(t *testing.T, cfg complexity.Config, files map[string]string) []complexity.Func {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/m\n\ngo 1.26\n")
	for name, src := range files {
		writeFile(t, filepath.Join(dir, name), src)
	}
	pkgs, err := complexity.Load(dir, nil, "./...")
	if err != nil {
		t.Fatal(err)
	}
	funcs, err := complexity.Analyze(dir, pkgs, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return funcs
}

// A guard's body is reported as the span a coverage block must lie inside to
// be left out of cov(f): from the opening brace to just after the closing one.
func TestGuardSpanCoversTheBody(t *testing.T) {
	src := `package m

func mk() error { return nil }

func F() error {
	err := mk()
	if err != nil {
		return err
	}
	if err := mk(); err != nil { return err }
	return nil
}
`
	funcs := analyzeSource(t, complexity.Config{Discount: true}, map[string]string{"m.go": src})
	var f complexity.Func
	for _, c := range funcs {
		if c.Name == "F" {
			f = c
		}
	}
	want := []complexity.Span{
		{Start: complexity.Pos{Line: 7, Col: 16}, End: complexity.Pos{Line: 9, Col: 3}},
		{Start: complexity.Pos{Line: 10, Col: 29}, End: complexity.Pos{Line: 10, Col: 43}},
	}
	if len(f.Guards) != len(want) {
		t.Fatalf("guards = %+v, want %+v", f.Guards, want)
	}
	for i, w := range want {
		if f.Guards[i] != w {
			t.Errorf("guard %d = %+v, want %+v", i, f.Guards[i], w)
		}
	}
	// A block the profile would hold for the first body: it starts after the
	// brace and ends at the closing brace.
	if !f.Guards[0].Contains(complexity.Pos{Line: 7, Col: 17}, complexity.Pos{Line: 9, Col: 2}) {
		t.Error("the guard body block is not inside its span")
	}
	// The block of the statement after the guard is not.
	if f.Guards[0].Contains(complexity.Pos{Line: 10, Col: 2}, complexity.Pos{Line: 11, Col: 12}) {
		t.Error("a block after the guard is inside its span")
	}
	if got := f.Span.Start; got != (complexity.Pos{Line: 5, Col: 1}) {
		t.Errorf("function starts at %+v, want 5:1", got)
	}
	if !f.Span.Contains(complexity.Pos{Line: 5, Col: 18}, complexity.Pos{Line: 12, Col: 2}) {
		t.Error("the whole body is not inside the function span")
	}
}

func TestFunctionLiteralsAtPackageLevel(t *testing.T) {
	src := `package m

var direct = func(a, b bool) bool { return a && b }

var table = map[string]func(int) int{
	"one": func(n int) int {
		if n > 0 {
			return n
		}
		return 0
	},
	"two": func(n int) int { return n },
}

var blank, other = func() int { return 1 }, func() int { return 2 }

func Host() func() bool {
	return func() bool { return direct(true, false) || direct(false, true) }
}
`
	funcs := analyzeSource(t, complexity.Config{}, map[string]string{"m.go": src})
	type key struct {
		name string
		comp int
	}
	var got []key
	for _, f := range funcs {
		if f.Decl != nil {
			continue
		}
		got = append(got, key{f.Name, f.Complexity})
	}
	want := []key{{"direct", 2}, {"table", 2}, {"table", 1}, {"blank", 1}, {"other", 1}}
	if len(got) != len(want) {
		t.Fatalf("literals = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("literal %d = %v, want %v", i, got[i], want[i])
		}
	}
	for _, f := range funcs {
		if f.Name == "Host" && f.Complexity != 2 {
			t.Errorf("Host = %d, want 2: its literal counts inside it", f.Complexity)
		}
	}
}

func TestReceiversAndOrder(t *testing.T) {
	a := `package m

type Box[T any] struct{ v T }

func (b Box[T]) Get() T { return b.v }

type Pair[K comparable, V any] struct{ k K }

func (p *Pair[K, V]) Key() K { return p.k }

func init() {}
`
	b := `package m

func init() {}

func Z() {}
`
	funcs := analyzeSource(t, complexity.Config{}, map[string]string{"a.go": a, "b.go": b})
	type key struct{ file, recv, name string }
	var got []key
	for _, f := range funcs {
		got = append(got, key{f.File, f.Recv, f.Name})
	}
	want := []key{
		{"a.go", "Box", "Get"},
		{"a.go", "Pair", "Key"},
		{"a.go", "", "init"},
		{"b.go", "", "init"},
		{"b.go", "", "Z"},
	}
	if len(got) != len(want) {
		t.Fatalf("functions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("function %d = %v, want %v (source order within a file, files by path)", i, got[i], want[i])
		}
	}
	for _, f := range funcs {
		if f.Pkg != "example.com/m" || f.PkgName != "m" {
			t.Errorf("%s: package %q (%q), want example.com/m (m)", f.Name, f.Pkg, f.PkgName)
		}
	}
}

// Production scope is G1's, but the loader must not widen it silently: test
// files are not loaded, so they are never scored.
func TestTestFilesAreNotLoaded(t *testing.T) {
	funcs := analyzeSource(t, complexity.Config{}, map[string]string{
		"m.go":      "package m\n\nfunc P() {}\n",
		"m_test.go": "package m\n\nimport \"testing\"\n\nfunc TestP(t *testing.T) {}\n",
	})
	for _, f := range funcs {
		if f.Name == "TestP" {
			t.Error("a _test.go function was analyzed")
		}
	}
}

func TestLoadFailsOnTypeErrors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/m\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "m.go"), "package m\n\nfunc F() int { return \"not an int\" }\n")
	if _, err := complexity.Load(dir, nil, "./..."); err == nil {
		t.Fatal("a package that does not type-check loaded without error")
	}
}

func TestLoadFailsOnSyntaxErrors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/m\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "m.go"), "package m\n\nfunc F( {\n")
	if _, err := complexity.Load(dir, nil, "./..."); err == nil {
		t.Fatal("a package with a syntax error loaded without error")
	}
}

func TestAnalyzeRejectsUntypedPackages(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/m\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "m.go"), "package m\n\nfunc F() {}\n")
	pkgs, err := complexity.Load(dir, nil, "./...")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pkgs {
		p.TypesInfo = nil
	}
	if _, err := complexity.Analyze(dir, pkgs, complexity.Config{Discount: true}); err == nil {
		t.Error("the discount ran without type information")
	}
}

func TestPositionsLineUp(t *testing.T) {
	funcs := analyzeSource(t, complexity.Config{}, map[string]string{"m.go": "package m\n\nfunc F() {\n}\n"})
	if len(funcs) != 1 {
		t.Fatalf("%d functions, want 1", len(funcs))
	}
	s := funcs[0].Span
	if s.Start != (complexity.Pos{Line: 3, Col: 1}) || s.End != (complexity.Pos{Line: 4, Col: 2}) {
		t.Errorf("span = %+v, want 3:1 to 4:2", s)
	}
}

// A relative root reached through a symbolic link: the working directory is a
// link to the module, as macOS temporary directories (/var is /private/var)
// and any linked tree are, so the root is logical while go/packages reports
// physical paths. File names must still come out module-relative.
func TestRelativeRootThroughASymlinkedWorkingDirectory(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	writeFile(t, filepath.Join(real, "go.mod"), "module example.com/m\n\ngo 1.26\n")
	writeFile(t, filepath.Join(real, "m.go"), "package m\n\nfunc F() {}\n")
	writeFile(t, filepath.Join(real, "sub", "s.go"), "package sub\n\nfunc G() {}\n")
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("no symbolic links here: %v", err)
	}
	t.Chdir(link)
	for _, root := range []string{".", "../link", "sub/.."} {
		pkgs, err := complexity.Load(root, nil, "./...")
		if err != nil {
			t.Fatalf("root %q: %v", root, err)
		}
		funcs, err := complexity.Analyze(root, pkgs, complexity.Config{})
		if err != nil {
			t.Fatalf("root %q: %v", root, err)
		}
		var files []string
		for _, f := range funcs {
			files = append(files, f.File)
		}
		if want := []string{"m.go", "sub/s.go"}; !slices.Equal(files, want) {
			t.Errorf("root %q: files %v, want %v", root, files, want)
		}
	}
}

// A file that is not under the root is an error, never a path with dots.
func TestFileOutsideTheRootIsAnError(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/m\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "m.go"), "package m\n\nfunc F() {}\n")
	pkgs, err := complexity.Load(dir, nil, "./...")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := complexity.Analyze(other, pkgs, complexity.Config{}); err == nil {
		t.Error("files outside the root were made relative to it")
	}
}

const cgoSource = `package m

/*
static int add(int a, int b) { return a + b; }
*/
import "C"

// Add is in a cgo file: the package is type-checked from files cgo generates,
// and the function must come back under the name of its source file.
func Add(a, b int) int {
	if a > 0 {
		return int(C.add(C.int(a), C.int(b)))
	}
	return 0
}

func Plain() {}
`

func requireCC(t *testing.T) {
	t.Helper()
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	if _, err := exec.LookPath(cc); err != nil {
		t.Skipf("no C compiler (%s): the cgo fixture needs one", cc)
	}
}

// A cgo package must analyze like any other: its functions come back under the
// .go files, never the cache files cgo writes, and the glue functions cgo adds
// are not the module's.
func TestCgoPackages(t *testing.T) {
	requireCC(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/m\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "c.go"), cgoSource)
	writeFile(t, filepath.Join(dir, "plain.go"), "package m\n\nfunc Other() int { return 1 }\n")
	pkgs, err := complexity.Load(dir, []string{"CGO_ENABLED=1"}, "./...")
	if err != nil {
		t.Fatal(err)
	}
	for _, discount := range []bool{false, true} {
		funcs, err := complexity.Analyze(dir, pkgs, complexity.Config{Discount: discount})
		if err != nil {
			t.Fatalf("discount %v: %v", discount, err)
		}
		type key struct {
			file, name string
			comp       int
		}
		var got []key
		for _, f := range funcs {
			got = append(got, key{f.File, f.Name, f.Complexity})
		}
		want := []key{{"c.go", "Add", 2}, {"c.go", "Plain", 1}, {"plain.go", "Other", 1}}
		if !slices.Equal(got, want) {
			t.Errorf("discount %v: functions %v, want %v", discount, got, want)
		}
	}
}

// With cgo off, a file that imports "C" is not compiled, so its functions are
// not loaded; they are not reported as measured.
func TestCgoFileIsNotLoadedWithCgoOff(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/m\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "c.go"), cgoSource)
	writeFile(t, filepath.Join(dir, "plain.go"), "package m\n\nfunc Other() int { return 1 }\n")
	pkgs, err := complexity.Load(dir, []string{"CGO_ENABLED=0"}, "./...")
	if err != nil {
		t.Fatal(err)
	}
	funcs, err := complexity.Analyze(dir, pkgs, complexity.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range funcs {
		if f.File == "c.go" {
			t.Errorf("%s was analyzed with cgo off", f.Name)
		}
	}
	var ignored []string
	for _, p := range pkgs {
		for _, f := range p.IgnoredFiles {
			ignored = append(ignored, filepath.Base(f))
		}
	}
	if !slices.Contains(ignored, "c.go") {
		t.Errorf("c.go is not among the ignored files %v: a caller cannot tell it was not compiled", ignored)
	}
}

// The build flags in the process environment must not leak into a load: the
// build is the one the caller passed.
func TestInheritedGoflagsDoNotLeak(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/m\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "a.go"), "package m\n\nfunc A() {}\n")
	writeFile(t, filepath.Join(dir, "b.go"), "//go:build extra\n\npackage m\n\nfunc B() {}\n")
	t.Setenv("GOFLAGS", "-tags=extra")
	pkgs, err := complexity.Load(dir, nil, "./...")
	if err != nil {
		t.Fatal(err)
	}
	funcs, err := complexity.Analyze(dir, pkgs, complexity.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(funcs) != 1 || funcs[0].Name != "A" {
		t.Errorf("functions %v: an inherited -tags changed the build", funcs)
	}
	// The caller's own GOFLAGS entry is the one that counts.
	pkgs, err = complexity.Load(dir, []string{"GOFLAGS=-tags=extra"}, "./...")
	if err != nil {
		t.Fatal(err)
	}
	funcs, err = complexity.Analyze(dir, pkgs, complexity.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(funcs) != 2 {
		t.Errorf("functions %v, want A and B under the caller's tags", funcs)
	}
}

// The race tag flips files; the caller passes it for a race test run.
func TestRaceTagSelectsFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/m\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "norace.go"), "//go:build !race\n\npackage m\n\nfunc Plain() {}\n")
	writeFile(t, filepath.Join(dir, "race.go"), "//go:build race\n\npackage m\n\nfunc Raced() {}\n")
	for env, want := range map[string]string{"GOFLAGS=-count=1": "Plain", "GOFLAGS=-count=1 -tags=race": "Raced"} {
		pkgs, err := complexity.Load(dir, []string{env}, "./...")
		if err != nil {
			t.Fatal(err)
		}
		funcs, err := complexity.Analyze(dir, pkgs, complexity.Config{})
		if err != nil {
			t.Fatal(err)
		}
		if len(funcs) != 1 || funcs[0].Name != want {
			t.Errorf("%s: functions %v, want %s", env, funcs, want)
		}
	}
}
