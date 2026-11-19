// Package crap scores every production function of a module by GO_TOPS.md
// §6.2: CRAP(f) = comp(f)² × (1 − cov(f))³ + comp(f), with comp from
// internal/complexity (§6.3) and cov(f) the guard-neutral statement coverage of
// f in a merged profile. The layer in internal/layers/crap judges the list
// [Score] returns; calibration reads the same list.
//
// # Scope
//
// Every production function of the module's own packages: declared functions,
// methods, and package-level function literals. Excluded are _test.go files,
// generated files (a *.gen.go name, or a "Code generated ... DO NOT EDIT."
// header) and testdata, vendor, dot and underscore directories. A directory
// with its own go.mod is another module.
//
// # Measured and not measured
//
// A coverage profile holds a block only for the files the run's build
// compiled. A production function in any other file (a _windows.go file on a
// Linux run, a file behind a tag) is not measured on this run. It is never
// scored cov = 0. It is still counted: [Report.NotMeasured] lists it with its
// comp(f), typed under a GOOS and GOARCH that compile its file where the
// package type-checks there, and with the guard discount off otherwise, which
// errs high.
//
// # Identity
//
// The key of a function (§6.7) is its import path, receiver type and name:
// "example.com/m/p.Name" or "example.com/m/p.Recv.Name" (no *, no type
// parameters; a package-level literal is named for the variable it
// initializes). Where that key occurs more than once in a package directory,
// counting every .go file whatever its build tags, each occurrence gains
// "@file.go", and where it still repeats within one file, "#n" in source
// order from 1: "example.com/m/p.init@a.go#2". The key is defined here and
// nowhere else.
package crap

import (
	"errors"
	"slices"

	"github.com/Hedronite/go-tops/internal/complexity"
	"github.com/Hedronite/go-tops/internal/gobuild"
)

// ErrNoBlocks is returned when the profile holds no block of a production
// file the run compiled (§3.4 rule 5).
var ErrNoBlocks = errors.New("the profile holds no block of a production file")

// Options are the knobs of a scoring run.
type Options struct {
	// ErrGuardDiscount is complexity.err_guard_discount.
	ErrGuardDiscount bool
	// WrapFunctions is complexity.wrap_functions.
	WrapFunctions []string
	// Build is the build of the run that wrote the profile: the files it
	// compiles are the files the run measured. The CRAP layer passes the test
	// run's own build (the tests layer's Build), so what it scores is what the
	// profile covers. The zero Build leaves the platform to the go command, with
	// cgo off. Loading adds the race tag, as `go test -race` does.
	Build gobuild.Build
}

// Function is one measured production function.
type Function struct {
	Key  string // the identity key
	File string // module-relative, slash-separated
	Recv string // receiver base type name; "" for a function
	Name string // function name, or the variable a package-level literal initializes
	Comp int    // §6.3 after the guard discount
	// Cov is the guard-neutral statement coverage in [0, 1]: the statements in
	// the bodies of discounted guards leave both terms.
	Cov float64
	// CRAP is comp² × (1 − cov)³ + comp, in float64, never rounded.
	CRAP float64
	// Table reports whether the function has the table shape of §6.8.
	Table bool
}

// NotMeasured is a production function in a file the run's build does not
// compile. It has a Comp and no coverage.
type NotMeasured struct {
	Key   string
	File  string
	Recv  string
	Name  string
	Comp  int
	Table bool
	// Typed reports whether Comp is typed (the discount applied under a GOOS
	// and GOARCH that compile the file). False means the discount was off.
	Typed bool
}

// Report is the result of scoring a module.
type Report struct {
	Measured    []Function    // sorted by file, then source position
	NotMeasured []NotMeasured // the same order
	// PlatformLoads is how many times the module was loaded for another GOOS
	// and GOARCH to type the not-measured functions: one load for each platform
	// that compiles at least one of their files.
	PlatformLoads int
	// exist holds the key of every production function in any .go file of the
	// module, whatever its build tags: the existence set.
	exist map[string]bool
}

// Exists reports whether a function with the key is declared in any production
// file of the module, whatever its build tags. A function the run's build does
// not compile exists; it is not measured. The ledger asks this before it
// removes an entry or accepts a rename (§6.7).
func (r Report) Exists(key string) bool { return r.exist[key] }

// Keys returns the existence set, sorted.
func (r Report) Keys() []string {
	keys := make([]string, 0, len(r.exist))
	for k := range r.exist {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// Over returns the measured functions whose CRAP is above cap, compared
// unrounded, and the not-measured functions whose comp is above it. At full
// coverage CRAP equals comp, so a function whose comp is above the cap fails
// at every coverage level (§6.2).
func (r Report) Over(cap float64) ([]Function, []NotMeasured) {
	var m []Function
	for _, f := range r.Measured {
		if f.CRAP > cap {
			m = append(m, f)
		}
	}
	var n []NotMeasured
	for _, f := range r.NotMeasured {
		if float64(f.Comp) > cap {
			n = append(n, f)
		}
	}
	return m, n
}

// Compute is the §6.2 formula, in float64.
func Compute(comp int, cov float64) float64 {
	c := float64(comp)
	miss := 1 - cov
	return c*c*miss*miss*miss + c
}

// model is the production functions of a module under one build.
type model struct {
	module                string
	measured, notMeasured []*fn
	keys                  []string // by fn.id
	platformLoads         int
}

// analyze lists every production function of the module in dir and splits
// them by whether opt.Build compiles their file. With cross set, a function
// the build does not compile also gets the typed comp of a platform that does
// (ruling C2b).
func analyze(dir string, opt Options, cross bool) (*model, error) {
	cfg := complexity.Config{Discount: opt.ErrGuardDiscount, WrapFunctions: opt.WrapFunctions}
	pkgs, err := complexity.Load(dir, opt.Build.LoadEnv(), "./...")
	if err != nil {
		return nil, err
	}
	module, err := moduleOf(dir)
	if err != nil {
		return nil, err
	}
	typed, err := complexity.Analyze(dir, pkgs, cfg)
	if err != nil {
		return nil, err
	}
	src, err := scan(dir, module)
	if err != nil {
		return nil, err
	}
	p, err := split(src.funcs, typed)
	if err != nil {
		return nil, err
	}
	m := &model{module: module, measured: p.measured, notMeasured: p.notMeasured, keys: identities(src.funcs)}
	if cross {
		m.platformLoads = typedComp(dir, opt.Build, cfg, p.notMeasured)
	}
	return m, nil
}

// Score scores the module in dir against the cover profile at profile (the
// .go-tops/cover.out format, written with -coverpkg=./...).
func Score(dir, profile string, opt Options) (Report, error) {
	blocks, err := readProfile(profile)
	if err != nil {
		return Report{}, err
	}
	m, err := analyze(dir, opt, true)
	if err != nil {
		return Report{}, err
	}
	byFile := blocksByFile(blocks, m.module)
	rep := Report{exist: map[string]bool{}, PlatformLoads: m.platformLoads}
	for _, k := range m.keys {
		rep.exist[k] = true
	}
	inProfile := 0
	for _, f := range m.measured {
		bs := byFile[f.File]
		inProfile += len(bs)
		cov := coverage(f, bs)
		rep.Measured = append(rep.Measured, Function{
			Key: m.keys[f.id], File: f.File, Recv: f.Recv, Name: f.Name, Comp: f.Complexity,
			Cov: cov, CRAP: Compute(f.Complexity, cov), Table: complexity.TableShape(f.Decl),
		})
	}
	if inProfile == 0 {
		return Report{}, ErrNoBlocks
	}
	for _, f := range m.notMeasured {
		rep.NotMeasured = append(rep.NotMeasured, NotMeasured{
			Key: m.keys[f.id], File: f.File, Recv: f.Recv, Name: f.Name, Comp: f.Complexity,
			Table: complexity.TableShape(f.Decl), Typed: f.typed,
		})
	}
	return rep, nil
}

// Compiled returns the keys of the production functions that build compiles:
// the functions a run under that build measures. A leg that is not the
// ledger leg asks this with the ledger's recorded build, to find the functions
// the ledger leg does not compile (ruling C3). It reads no profile and compiles
// nothing: the files the build selects come from go/build (Select), and the
// functions from the source, so it needs no C toolchain for another platform.
func Compiled(dir string, build gobuild.Build) (map[string]bool, error) {
	sel, err := Select(dir, build)
	if err != nil {
		return nil, err
	}
	return CompiledBy(dir, sel)
}

// CompiledBy is Compiled for a selection already made: the keys of the production
// functions declared in a file the selection holds.
func CompiledBy(dir string, sel Selection) (map[string]bool, error) {
	module, err := moduleOf(dir)
	if err != nil {
		return nil, err
	}
	src, err := scan(dir, module)
	if err != nil {
		return nil, err
	}
	keys := identities(src.funcs)
	compiled := map[string]bool{}
	for _, f := range src.funcs {
		if sel.Files[f.File] {
			compiled[keys[f.id]] = true
		}
	}
	return compiled, nil
}

// Existence is what exists in a module's source, whatever any build compiles.
type Existence struct {
	// Keys holds the identity of every production function in any .go file,
	// sorted.
	Keys []string
	// Packages holds the import path of every package that has a production
	// .go file, sorted.
	Packages []string
	// Funcs lists every production function of any .go file with what the
	// exemption matcher reads: its key, file, receiver, name and whether it has
	// the table shape of §6.8. It is read from the source alone, so it is the
	// list the CRAP layer matches exemptions against, without a profile.
	Funcs []Declared
}

// Declared is one production function as the source declares it.
type Declared struct {
	Key, File, Recv, Name string
	Table                 bool
}

// Exist lists the identities and the packages of the module in dir by reading
// every production .go file, whatever its build tags. It runs no test and no
// build, so `baseline --rename` can check that an old key has vanished and a new
// key exists without producing a profile. It is the set Score reports as Keys.
func Exist(dir string) (Existence, error) {
	module, err := moduleOf(dir)
	if err != nil {
		return Existence{}, err
	}
	src, err := scan(dir, module)
	if err != nil {
		return Existence{}, err
	}
	ids := identities(src.funcs)
	keys := slices.Clone(ids)
	slices.Sort(keys)
	pkgs := make([]string, 0, len(src.packages))
	for p := range src.packages {
		pkgs = append(pkgs, p)
	}
	slices.Sort(pkgs)
	return Existence{Keys: keys, Packages: pkgs, Funcs: declared(src.funcs, ids)}, nil
}

func declared(funcs []*fn, keys []string) []Declared {
	out := make([]Declared, len(funcs))
	for i, f := range funcs {
		out[i] = Declared{Key: keys[f.id], File: f.File, Recv: f.Recv, Name: f.Name, Table: complexity.TableShape(f.Decl)}
	}
	return out
}
