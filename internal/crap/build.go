package crap

import (
	"fmt"
	"go/build"
	"path/filepath"
	"slices"

	"github.com/Hedronite/go-tops/internal/complexity"
	"github.com/Hedronite/go-tops/internal/gobuild"
)

// ref names a function by where it starts.
type ref struct {
	file      string
	line, col int
}

func refOf(f *fn) ref { return ref{f.File, f.Span.Start.Line, f.Span.Start.Col} }

// parts is the production functions split by whether the run's build compiled
// their file.
type parts struct {
	measured, notMeasured []*fn
}

// split matches the functions of the typed load of the run's build with the
// functions of every file. A function the typed load has is measured and takes
// its typed figures; a function in a file the load did not compile is not
// measured. A function missing from a file the load did compile is an error:
// the two parses disagree and a count would be a guess.
func split(all []*fn, typed []complexity.Func) (parts, error) {
	byRef := map[ref]complexity.Func{}
	compiled := map[string]bool{}
	for _, t := range typed {
		byRef[ref{t.File, t.Span.Start.Line, t.Span.Start.Col}] = t
		compiled[t.File] = true
	}
	var p parts
	for _, f := range all {
		if t, ok := byRef[refOf(f)]; ok {
			f.Func = t
			p.measured = append(p.measured, f)
			continue
		}
		if compiled[f.File] {
			return parts{}, fmt.Errorf("%s: %s is in the source but not in the typed load of the same file", f.File, f.Name)
		}
		p.notMeasured = append(p.notMeasured, f)
	}
	return p, nil
}

// target is a GOOS and GOARCH.
type target struct{ goos, goarch string }

// targets are the platforms a file outside the run's build is tried under, in
// order. The list is fixed so that a count does not depend on the host.
var targets = []target{
	{"linux", "amd64"},
	{"darwin", "arm64"},
	{"windows", "amd64"},
	{"freebsd", "amd64"},
	{"linux", "arm64"},
	{"darwin", "amd64"},
	{"windows", "arm64"},
	{"openbsd", "amd64"},
	{"netbsd", "amd64"},
	{"solaris", "amd64"},
	{"illumos", "amd64"},
	{"dragonfly", "amd64"},
	{"aix", "ppc64"},
	{"plan9", "amd64"},
	{"js", "wasm"},
	{"wasip1", "wasm"},
	{"android", "arm64"},
	{"ios", "arm64"},
	{"linux", "386"},
	{"linux", "arm"},
	{"linux", "riscv64"},
	{"linux", "ppc64le"},
	{"linux", "s390x"},
	{"windows", "386"},
	{"freebsd", "arm64"},
}

// typedComp gives the not-measured functions a typed comp (§6.3 guard
// discount on) where a platform that compiles their file also type-checks the
// module. The others keep the comp counted without the discount, which errs
// high. With the discount off there is nothing to type.
func typedComp(dir string, build gobuild.Build, cfg complexity.Config, notMeasured []*fn) {
	if !cfg.Discount {
		return
	}
	pending := map[string][]*fn{}
	for _, f := range notMeasured {
		pending[f.File] = append(pending[f.File], f)
	}
	tried := map[target]bool{}
	for {
		t, ok := nextTarget(dir, pending, tried)
		if !ok {
			return
		}
		tried[t] = true
		typed, err := complexity.Load(dir, build.Platform(t.goos, t.goarch).LoadEnv(), "./...")
		if err != nil {
			continue
		}
		funcs, err := complexity.Analyze(dir, typed, cfg)
		if err != nil {
			continue
		}
		apply(pending, funcs)
	}
}

// nextTarget returns the first untried platform that compiles one of the
// pending files.
func nextTarget(dir string, pending map[string][]*fn, tried map[target]bool) (target, bool) {
	files := make([]string, 0, len(pending))
	for f := range pending {
		files = append(files, f)
	}
	slices.Sort(files)
	for _, f := range files {
		for _, t := range targets {
			if !tried[t] && compiles(dir, f, t) {
				return t, true
			}
		}
	}
	return target{}, false
}

// compiles reports whether the build constraints of file, its name and its
// //go:build line, admit it under t.
func compiles(dir, file string, t target) bool {
	ctxt := build.Default
	ctxt.GOOS, ctxt.GOARCH, ctxt.CgoEnabled = t.goos, t.goarch, false
	abs := filepath.Join(dir, filepath.FromSlash(file))
	ok, err := ctxt.MatchFile(filepath.Dir(abs), filepath.Base(abs))
	return err == nil && ok
}

// apply copies the typed figures onto the pending functions the load covers
// and retires their files.
func apply(pending map[string][]*fn, funcs []complexity.Func) {
	byRef := map[ref]complexity.Func{}
	for _, t := range funcs {
		byRef[ref{t.File, t.Span.Start.Line, t.Span.Start.Col}] = t
	}
	for file, fs := range pending {
		done := false
		for _, f := range fs {
			if t, ok := byRef[refOf(f)]; ok {
				f.Func, f.typed, done = t, true, true
			}
		}
		if done {
			delete(pending, file)
		}
	}
}
