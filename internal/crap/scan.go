package crap

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Hedronite/go-tops/internal/complexity"
)

// fn is a production function through the stages of Score.
type fn struct {
	complexity.Func
	id    int  // position in the all list; keys are indexed by it
	typed bool // for a not-measured function: Complexity is typed
}

// scanned is every production file of a module, whatever its build tags.
type scanned struct {
	funcs []*fn // comp counted without type information, sorted by file, then source position
	// packages holds the import path of every directory with a production .go
	// file, whether or not the file declares a function.
	packages map[string]bool
}

// scan parses every production file under dir, whatever its build tags. module
// is the module path.
func scan(dir, module string) (*scanned, error) {
	out := &scanned{packages: map[string]bool{}}
	var all []*fn
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return visitDir(dir, p, d)
		}
		if !productionName(d.Name()) {
			return nil
		}
		pkgPath, funcs, err := parseFile(dir, module, p)
		if err != nil {
			return err
		}
		if pkgPath != "" {
			out.packages[pkgPath] = true
		}
		all = append(all, funcs...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", dir, err)
	}
	slices.SortStableFunc(all, compareFuncs)
	for i, f := range all {
		f.id = i
	}
	out.funcs = all
	return out, nil
}

func compareFuncs(a, b *fn) int {
	if c := strings.Compare(a.File, b.File); c != 0 {
		return c
	}
	if a.Span.Start.Line != b.Span.Start.Line {
		return a.Span.Start.Line - b.Span.Start.Line
	}
	return a.Span.Start.Col - b.Span.Start.Col
}

// visitDir skips the directories that hold no production code of the module:
// testdata, vendor, dot and underscore directories, and nested modules.
func visitDir(root, p string, d fs.DirEntry) error {
	if p == root {
		return nil
	}
	name := d.Name()
	if name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return filepath.SkipDir
	}
	if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
		return filepath.SkipDir
	}
	return nil
}

func productionName(name string) bool {
	return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") && !strings.HasSuffix(name, ".gen.go")
}

// parseFile lists the functions of one file and the import path of its
// package; a generated file has neither.
func parseFile(root, module, p string) (string, []*fn, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, p, nil, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return "", nil, err
	}
	if ast.IsGenerated(file) {
		return "", nil, nil
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return "", nil, err
	}
	rel = filepath.ToSlash(rel)
	pkgPath := module
	if dir := path.Dir(rel); dir != "." {
		pkgPath = module + "/" + dir
	}
	funcs, err := complexity.AnalyzeSyntax(fset, file, pkgPath, rel)
	if err != nil {
		return "", nil, err
	}
	out := make([]*fn, len(funcs))
	for i, f := range funcs {
		out[i] = &fn{Func: f}
	}
	return pkgPath, out, nil
}

// moduleOf reads the module path from the go.mod in dir.
func moduleOf(dir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line, _, _ = strings.Cut(line, "//")
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module")
		if !ok || !strings.HasPrefix(rest, " ") && !strings.HasPrefix(rest, "\t") {
			continue
		}
		if path := strings.Trim(strings.TrimSpace(rest), "\"`"); path != "" {
			return path, nil
		}
	}
	return "", errors.New("no module line in go.mod")
}
