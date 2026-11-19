// Package complexity counts the cyclomatic complexity of Go functions by the
// rule of GO_TOPS.md §6.3 and reports what the CRAP layer needs besides the
// count: where each function and each discounted error guard lies in the
// source, and whether a function has the table shape of §6.8.
//
// The counting rule is gocyclo's, with two protocol additions. Function
// literals count inside the function that holds them, and a package-level
// function literal is scored on its own. An error guard that §6.3 forgives
// weighs 0, and its body is reported as a span so that coverage can leave the
// same statements out (guard-neutral cov(f), §6.2).
//
// The guard test is typed, so it needs a package loaded with type information:
// use [Load], then [Analyze].
package complexity

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Config is the complexity block of a class's CRAP gate (GO_TOPS.md §9).
type Config struct {
	// Discount is complexity.err_guard_discount: weigh an error guard that
	// meets §6.3 at 0. When false no guard is discounted and no guard span is
	// reported. A discounted guard forgives its `if` only: a branch inside its
	// return operands (a closure, `&&`, a second call that decides) still
	// counts, while the whole body of the guard leaves coverage.
	Discount bool
	// WrapFunctions is complexity.wrap_functions: entries of the form
	// "import/path.Name", each a function whose result wraps its error argument.
	WrapFunctions []string
}

// Pos is a line and a byte column, both 1-based, as a coverage profile counts
// them.
type Pos struct{ Line, Col int }

// Span is a source range, start inclusive and end exclusive, in the file of
// the [Func] that holds it.
type Span struct{ Start, End Pos }

func (p Pos) before(q Pos) bool {
	return p.Line < q.Line || (p.Line == q.Line && p.Col < q.Col)
}

// Contains reports whether the profile block that runs from start to end lies
// inside the span.
func (s Span) Contains(start, end Pos) bool {
	return !start.before(s.Start) && !s.End.before(end)
}

// Func is one scored function: a declaration, or a package-level function
// literal.
type Func struct {
	Pkg        string        // import path of the package
	PkgName    string        // package clause name
	Recv       string        // receiver base type name, without * and type arguments; "" for a function
	Name       string        // function name; for a package-level literal, the variable it initializes
	File       string        // module-relative, slash-separated
	Span       Span          // from `func` to just after the closing brace
	Complexity int           // §6.3, with the Config the run used
	Guards     []Span        // bodies of the discounted guards inside the function, in source order
	Decl       *ast.FuncDecl // nil for a package-level function literal
}

// Load loads the packages matching patterns, relative to dir, with syntax and
// type information. Test files are not loaded. A package error, syntax or type,
// fails the load: a count over code that does not compile is no count.
//
// env is added to the process environment, and the build it describes decides
// which files are loaded: files that a build constraint excludes under it are
// not (see [packages.Package.IgnoredFiles]). The CRAP layer measures the files
// the gate's test run compiled, so its caller passes that run's build: the
// test layer's environment (CGO_ENABLED=1, GOFLAGS) plus the race tag, because
// `go test -race` sets it and a wrong environment flips every `//go:build race`
// and `//go:build !race` file. GOOS and GOARCH select another platform.
//
// GOFLAGS is always set, so that a -mod, -tags or -race inherited from the
// process environment or from `go env -w` never leaks into the load: it is
// env's GOFLAGS entry, or "-count=1" when env has none (an empty value would
// not override `go env -w`). The build flags a caller wants go in that entry,
// for example "GOFLAGS=-count=1 -tags=race".
func Load(dir string, env []string, patterns ...string) ([]*packages.Package, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedImports | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo,
		Dir: dir,
		Env: append(append(os.Environ(), "GOFLAGS=-count=1"), env...),
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf("load packages: %w", err)
	}
	var errs []error
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			errs = append(errs, e)
		}
	})
	if len(errs) > 0 {
		return nil, fmt.Errorf("load packages: %d errors, first: %w", len(errs), errs[0])
	}
	return pkgs, nil
}

// Analyze scores every function of pkgs, which [Load] returned, and returns
// them ordered by file path, then source position. root is the module
// directory the file paths are made relative to.
func Analyze(root string, pkgs []*packages.Package, cfg Config) ([]Func, error) {
	wrap := map[string]bool{}
	for _, w := range cfg.WrapFunctions {
		wrap[w] = true
	}
	rel := newRelativizer(root).rel
	var out []Func
	for _, p := range pkgs {
		if cfg.Discount && p.TypesInfo == nil {
			return nil, fmt.Errorf("package %s was loaded without type information", p.PkgPath)
		}
		goFiles := map[string]bool{}
		for _, f := range p.GoFiles {
			goFiles[f] = true
		}
		for _, file := range p.Syntax {
			fa := fileAnalyzer{pkg: p, rel: rel, goFiles: goFiles}
			if cfg.Discount {
				fa.guards = findGuards(file, p.TypesInfo, wrap)
			}
			funcs, err := fa.analyze(file)
			if err != nil {
				return nil, err
			}
			out = append(out, funcs...)
		}
	}
	slices.SortStableFunc(out, func(a, b Func) int {
		if c := strings.Compare(a.File, b.File); c != 0 {
			return c
		}
		return compareLine(a.Span.Start, b.Span.Start)
	})
	return out, nil
}

func compareLine(a, b Pos) int {
	if a.Line != b.Line {
		return a.Line - b.Line
	}
	return a.Col - b.Col
}

type fileAnalyzer struct {
	pkg *packages.Package
	rel func(file string) (string, error)
	// goFiles holds the source files of the package, by the name go list gives
	// them. A cgo package is type-checked from files cgo generates in the build
	// cache, whose line directives map the translated sources back to the .go
	// files; the generated glue maps to nothing in goFiles. Only a function
	// whose position is in a source file is the module's.
	goFiles map[string]bool
	guards  map[*ast.IfStmt]*ast.BlockStmt // nil when the discount is off
}

func (a *fileAnalyzer) analyze(file *ast.File) ([]Func, error) {
	var out []Func
	for _, d := range file.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			f, ok, err := a.score(d, d.Recv, d.Name.Name)
			if err != nil {
				return nil, err
			}
			if ok {
				f.Decl = d
				out = append(out, f)
			}
		case *ast.GenDecl:
			lits, err := a.literals(d)
			if err != nil {
				return nil, err
			}
			out = append(out, lits...)
		}
	}
	return out, nil
}

// literals scores the outermost function literals in the initializers of a
// package-level var declaration. A literal inside another literal belongs to
// the outer one (§6.3).
func (a *fileAnalyzer) literals(d *ast.GenDecl) ([]Func, error) {
	var out []Func
	if d.Tok != token.VAR {
		return nil, nil
	}
	for _, spec := range d.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for i, v := range vs.Values {
			name := vs.Names[min(i, len(vs.Names)-1)].Name
			for _, lit := range outermostLiterals(v) {
				f, ok, err := a.score(lit, nil, name)
				if err != nil {
					return nil, err
				}
				if ok {
					out = append(out, f)
				}
			}
		}
	}
	return out, nil
}

func outermostLiterals(e ast.Expr) []*ast.FuncLit {
	var lits []*ast.FuncLit
	ast.Inspect(e, func(n ast.Node) bool {
		lit, ok := n.(*ast.FuncLit)
		if ok {
			lits = append(lits, lit)
		}
		return !ok
	})
	return lits
}

// score counts one function. ok is false for a function that is not in a
// source file of the package (see fileAnalyzer.goFiles): it is not the module's.
func (a *fileAnalyzer) score(node ast.Node, recv *ast.FieldList, name string) (f Func, ok bool, err error) {
	fset := a.pkg.Fset
	start := fset.Position(node.Pos())
	if a.goFiles != nil && !a.goFiles[start.Filename] {
		return Func{}, false, nil
	}
	file, err := a.rel(start.Filename)
	if err != nil {
		return Func{}, false, err
	}
	recvName, err := receiverName(recv)
	if err != nil {
		return Func{}, false, fmt.Errorf("%s: %w", file, err)
	}
	comp, guards := a.count(node)
	return Func{
		Pkg:        a.pkg.PkgPath,
		PkgName:    a.pkg.Name,
		Recv:       recvName,
		Name:       name,
		File:       file,
		Span:       spanOf(fset, node),
		Complexity: comp,
		Guards:     guards,
	}, true, nil
}

// count is the §6.3 count of node, with the guards the run discounts.
func (a *fileAnalyzer) count(node ast.Node) (int, []Span) {
	comp := 1
	var guards []Span
	ast.Inspect(node, func(n ast.Node) bool {
		if body, ok := a.guardBody(n); ok {
			guards = append(guards, spanOf(a.pkg.Fset, body))
			return true
		}
		comp += weight(n)
		return true
	})
	return comp, guards
}

func (a *fileAnalyzer) guardBody(n ast.Node) (*ast.BlockStmt, bool) {
	ifs, ok := n.(*ast.IfStmt)
	if !ok {
		return nil, false
	}
	body, ok := a.guards[ifs]
	return body, ok
}

func spanOf(fset *token.FileSet, n ast.Node) Span {
	s, e := fset.Position(n.Pos()), fset.Position(n.End())
	return Span{Start: Pos{s.Line, s.Column}, End: Pos{e.Line, e.Column}}
}

// receiverName is the base type name of a method receiver: `T`, `*T`, `T[K]`
// and `*T[K, V]` all give "T". A receiver of any other form is an error.
func receiverName(recv *ast.FieldList) (string, error) {
	if recv == nil || len(recv.List) == 0 {
		return "", nil
	}
	e := recv.List[0].Type
	for {
		switch t := e.(type) {
		case *ast.Ident:
			return t.Name, nil
		case *ast.StarExpr:
			e = t.X
		case *ast.ParenExpr:
			e = t.X
		case *ast.IndexExpr:
			e = t.X
		case *ast.IndexListExpr:
			e = t.X
		default:
			return "", fmt.Errorf("a receiver of the form %T has no base type name", e)
		}
	}
}

// relativizer makes file names relative to the module root, slash-separated.
// The root and each file's directory are made absolute and resolved through
// symbolic links before they are compared: go/packages reports physical
// paths, while a root given relative to a working directory reached through a
// link (macOS temporary directories, any linked tree) is logical, and the two
// only meet once both are physical. A file outside the root is an error: it is
// not a file of the module.
type relativizer struct {
	root    string
	rootErr error
	dirs    map[string]string // directory as given to its physical form
}

func newRelativizer(root string) *relativizer {
	r, err := physical(root)
	return &relativizer{root: r, rootErr: err, dirs: map[string]string{}}
}

func (z *relativizer) rel(file string) (string, error) {
	if z.rootErr != nil {
		return "", z.rootErr
	}
	dir := filepath.Dir(file)
	phys, ok := z.dirs[dir]
	if !ok {
		var err error
		if phys, err = physical(dir); err != nil {
			return "", err
		}
		z.dirs[dir] = phys
	}
	rel, err := filepath.Rel(z.root, filepath.Join(phys, filepath.Base(file)))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside %s", file, z.root)
	}
	return filepath.ToSlash(rel), nil
}

// physical is p made absolute, with every symbolic link resolved.
func physical(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", p, err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", p, err)
	}
	return real, nil
}

var errorType = types.Universe.Lookup("error").Type()
