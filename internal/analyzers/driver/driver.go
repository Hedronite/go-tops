// Package driver runs the Go-TOPS law analyzers in-process (D10a).
package driver

import (
	"context"
	"fmt"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/Hedronite/go-tops/internal/analyzers"
	"github.com/Hedronite/go-tops/internal/analyzers/ctxroot"
)

// Result holds per-law diagnostic counts from one analyzer run.
type Result struct {
	ByLaw map[string]int
}

// Run loads ./... under dir and runs every custom analyzer.
func Run(_ context.Context, dir string) (Result, error) {
	ctxroot.ModuleDir = dir
	defer analyzers.ResetConfig()

	fset := token.NewFileSet()
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Dir:   dir,
		Fset:  fset,
		Tests: true,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return Result{}, err
	}

	out := map[string]int{}
	sizes := types.SizesFor("gc", "amd64")
	for _, a := range analyzers.All() {
		law := analyzers.LawID[a.Name]
		for _, pkg := range pkgs {
			if skipPackage(pkg) {
				continue
			}
			if shouldSkipPkg(pkg) {
				continue
			}
			var diags []analysis.Diagnostic
			pass := &analysis.Pass{
				Analyzer:   a,
				Fset:       fset,
				Files:      pkg.Syntax,
				Pkg:        pkg.Types,
				TypesInfo:  pkg.TypesInfo,
				TypesSizes: sizes,
				ResultOf:   map[*analysis.Analyzer]any{},
				Report: func(d analysis.Diagnostic) {
					diags = append(diags, d)
				},
				ImportObjectFact:  func(obj types.Object, fact analysis.Fact) bool { return false },
				AllObjectFacts:    func() []analysis.ObjectFact { return nil },
				ImportPackageFact: func(pkg *types.Package, fact analysis.Fact) bool { return false },
				AllPackageFacts:   func() []analysis.PackageFact { return nil },
			}
			if _, err := a.Run(pass); err != nil {
				return Result{}, fmt.Errorf("%s: %w", a.Name, err)
			}
			out[law] += len(diags)
		}
	}
	return Result{ByLaw: out}, nil
}

func skipPackage(pkg *packages.Package) bool {
	return pkg.IllTyped || len(pkg.Syntax) == 0 || pkg.Types == nil
}

func shouldSkipPkg(pkg *packages.Package) bool {
	for _, f := range pkg.CompiledGoFiles {
		if strings.Contains(f, string(filepath.Separator)+"testdata"+string(filepath.Separator)) {
			if strings.Contains(f, string(filepath.Separator)+"internal"+string(filepath.Separator)+"analyzers"+string(filepath.Separator)) {
				return true
			}
		}
	}
	return false
}
