package complexity

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
)

// guardFinder finds the error guards GO_TOPS.md §6.3 forgives. A guard is an
// `if` with no else whose condition is exactly `x != nil`, where x is an
// identifier of type error assigned by the statement just before the `if` or
// in its init clause, and whose body is one return whose last result is x, a
// fmt.Errorf that wraps x with %w through a literal format, or a call to a
// wrap function with x as an argument.
//
// The test is typed: x is the object the identifier resolves to, so its name
// neither earns nor loses the discount.
type guardFinder struct {
	info *types.Info
	wrap map[string]bool       // "import/path.Name"
	prev map[ast.Stmt]ast.Stmt // a statement and the one before it in its list
}

// findGuards returns every discounted guard of file and its body.
func findGuards(file *ast.File, info *types.Info, wrap map[string]bool) map[*ast.IfStmt]*ast.BlockStmt {
	g := &guardFinder{info: info, wrap: wrap, prev: previousStatements(file)}
	guards := map[*ast.IfStmt]*ast.BlockStmt{}
	ast.Inspect(file, func(n ast.Node) bool {
		if s, ok := n.(*ast.IfStmt); ok && g.isGuard(s) {
			guards[s] = s.Body
		}
		return true
	})
	return guards
}

// previousStatements maps each statement that has a predecessor in its
// statement list to that predecessor.
func previousStatements(file *ast.File) map[ast.Stmt]ast.Stmt {
	prev := map[ast.Stmt]ast.Stmt{}
	link := func(list []ast.Stmt) {
		for i := 1; i < len(list); i++ {
			prev[list[i]] = list[i-1]
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.BlockStmt:
			link(n.List)
		case *ast.CaseClause:
			link(n.Body)
		case *ast.CommClause:
			link(n.Body)
		}
		return true
	})
	return prev
}

func (g *guardFinder) isGuard(s *ast.IfStmt) bool {
	if s.Else != nil {
		return false
	}
	x := g.comparedWithNil(s.Cond)
	return x != nil && g.assigned(s, x) && g.returnsIt(s.Body, x)
}

// comparedWithNil returns x when cond is exactly `x != nil` and x is an
// identifier of type error.
func (g *guardFinder) comparedWithNil(cond ast.Expr) types.Object {
	b, ok := cond.(*ast.BinaryExpr)
	if !ok || b.Op != token.NEQ {
		return nil
	}
	x, ok := b.X.(*ast.Ident)
	if !ok || !g.isNil(b.Y) {
		return nil
	}
	obj := g.info.Uses[x]
	if obj == nil || !types.Identical(obj.Type(), errorType) {
		return nil
	}
	return obj
}

func (g *guardFinder) isNil(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	_, isNil := g.info.Uses[id].(*types.Nil)
	return isNil
}

// assigned reports whether x is assigned in the init clause of s or by the
// statement just before it.
func (g *guardFinder) assigned(s *ast.IfStmt, x types.Object) bool {
	return g.assigns(s.Init, x) || g.assigns(g.prev[s], x)
}

func (g *guardFinder) assigns(stmt ast.Stmt, x types.Object) bool {
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		return (s.Tok == token.ASSIGN || s.Tok == token.DEFINE) && g.names(s.Lhs, x)
	case *ast.DeclStmt:
		return g.declares(s, x)
	}
	return false
}

// declares reports whether a `var` declaration gives x a value.
func (g *guardFinder) declares(s *ast.DeclStmt, x types.Object) bool {
	d, ok := s.Decl.(*ast.GenDecl)
	if !ok || d.Tok != token.VAR {
		return false
	}
	for _, spec := range d.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok || len(vs.Values) == 0 {
			continue
		}
		for _, id := range vs.Names {
			if g.info.Defs[id] == x {
				return true
			}
		}
	}
	return false
}

func (g *guardFinder) names(list []ast.Expr, x types.Object) bool {
	for _, e := range list {
		if g.isObject(e, x) {
			return true
		}
	}
	return false
}

func (g *guardFinder) isObject(e ast.Expr, x types.Object) bool {
	id, ok := e.(*ast.Ident)
	return ok && g.info.ObjectOf(id) == x
}

// returnsIt reports whether body is one return whose last result is x, wraps
// x with %w, or passes x to a wrap function. A bare return does not qualify.
func (g *guardFinder) returnsIt(body *ast.BlockStmt, x types.Object) bool {
	if len(body.List) != 1 {
		return false
	}
	ret, ok := body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) == 0 {
		return false
	}
	last := ret.Results[len(ret.Results)-1]
	if g.isObject(last, x) {
		return true
	}
	call, ok := last.(*ast.CallExpr)
	return ok && g.wraps(call, x)
}

func (g *guardFinder) wraps(call *ast.CallExpr, x types.Object) bool {
	fn := g.callee(call)
	switch {
	case fn == nil:
		return false
	case isFmtErrorf(fn):
		return g.wrapsWithVerb(call, x)
	case g.wrap[fn.Pkg().Path()+"."+fn.Name()]:
		return g.names(call.Args, x)
	}
	return false
}

// callee is the package-level function a call names, or nil: a method, a
// function value and a conversion are not functions a wrap list can name.
func (g *guardFinder) callee(call *ast.CallExpr) *types.Func {
	fun := ast.Unparen(call.Fun)
	switch f := fun.(type) {
	case *ast.IndexExpr:
		fun = f.X
	case *ast.IndexListExpr:
		fun = f.X
	}
	var id *ast.Ident
	switch f := fun.(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
	default:
		return nil
	}
	fn, ok := g.info.Uses[id].(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Signature().Recv() != nil {
		return nil
	}
	return fn
}

func isFmtErrorf(fn *types.Func) bool {
	return fn.Pkg().Path() == "fmt" && fn.Name() == "Errorf"
}

// wrapsWithVerb reports whether call is fmt.Errorf with a string-literal
// format and x as the operand of one of its %w verbs. A format that is not a
// literal, or operands passed as a slice, do not qualify.
func (g *guardFinder) wrapsWithVerb(call *ast.CallExpr, x types.Object) bool {
	if len(call.Args) < 2 || call.Ellipsis.IsValid() {
		return false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	format, err := strconv.Unquote(lit.Value)
	if err != nil {
		return false
	}
	operands := call.Args[1:]
	for _, i := range wrapOperands(format) {
		if i >= 0 && i < len(operands) && g.isObject(operands[i], x) {
			return true
		}
	}
	return false
}
