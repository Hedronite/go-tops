package crap

import (
	"errors"
	"fmt"
	"go/build"
	"io/fs"
	"path"
	"path/filepath"
	"slices"

	"github.com/Hedronite/go-tops/internal/gobuild"
)

// Selection is what a build selects from the module's source tree: the .go files it
// compiles, and the packages that have at least one compiled non-test file. It is
// how a leg that is not the ledger leg finds what the ledger leg compiles (§6.7,
// rulings C3, C3d): a finding, a suppression or a package outside it is judged on
// the leg that compiles it.
type Selection struct {
	// Files holds the module-relative, slash-separated path of every .go file the
	// build selects, test files included: the lint run and the test run read them.
	Files map[string]bool
	// Packages holds the import path of every package with a compiled non-test file.
	Packages map[string]bool
}

// Select reads the module in dir and returns what the build selects. It reads the
// build constraints of each file, and the cgo setting, as the go command does for
// the test run (`go test -race`, so the race tag is set). It runs no build.
func Select(dir string, build gobuild.Build) (Selection, error) {
	module, err := moduleOf(dir)
	if err != nil {
		return Selection{}, err
	}
	ctxt := contextFor(build)
	sel := Selection{Files: map[string]bool{}, Packages: map[string]bool{}}
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		if skip := visitDir(dir, p, d); skip != nil {
			return skip
		}
		return sel.add(ctxt, dir, p, module)
	})
	if err != nil {
		return Selection{}, fmt.Errorf("select the files of %s: %w", dir, err)
	}
	return sel, nil
}

// contextFor is the go/build context of a build: its platform, tags and cgo
// setting, with the race tag of the test run.
func contextFor(b gobuild.Build) build.Context {
	ctxt := build.Default
	ctxt.GOOS, ctxt.GOARCH, ctxt.CgoEnabled = b.GOOS, b.GOARCH, b.CGOEnabled
	if ctxt.GOOS == "" {
		ctxt.GOOS = build.Default.GOOS
	}
	if ctxt.GOARCH == "" {
		ctxt.GOARCH = build.Default.GOARCH
	}
	ctxt.BuildTags = append(slices.Clone(b.Tags), gobuild.RaceTag)
	return ctxt
}

// add records the files and the package of one directory.
func (s Selection) add(ctxt build.Context, root, dir, module string) error {
	pkg, err := ctxt.ImportDir(dir, 0)
	var noGo *build.NoGoError
	if err != nil && !errors.As(err, &noGo) {
		return fmt.Errorf("%s: %w", dir, err)
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)
	for _, name := range slices.Concat(pkg.GoFiles, pkg.CgoFiles, pkg.TestGoFiles, pkg.XTestGoFiles) {
		s.Files[path.Join(rel, name)] = true
	}
	if len(pkg.GoFiles)+len(pkg.CgoFiles) > 0 {
		s.Packages[importPath(module, rel)] = true
	}
	return nil
}

func importPath(module, rel string) string {
	if rel == "." {
		return module
	}
	return module + "/" + rel
}
