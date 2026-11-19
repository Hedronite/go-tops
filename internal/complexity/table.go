package complexity

import "go/ast"

// TableShape reports whether fn is a dispatch table as GO_TOPS.md §6.8
// defines it, the one shape a `crap` exemption with `shape: table-switch` may
// cover:
//
//  1. the body is one switch or type switch, optionally followed by one return;
//  2. every clause, default included, has a body of exactly one statement: a
//     return, an assignment, an expression statement or a channel send;
//  3. that statement holds no if, for, range, switch, select, go, defer,
//     function literal, && or ||, and at most one call expression.
//
// The check is syntactic. A nil fn, or one with no body, is not a table.
func TableShape(fn *ast.FuncDecl) bool {
	if fn == nil || fn.Body == nil {
		return false
	}
	clauses, ok := switchClauses(fn.Body.List)
	if !ok {
		return false
	}
	for _, c := range clauses {
		if !isArm(c) {
			return false
		}
	}
	return true
}

// switchClauses returns the clauses of the one switch that makes up body, or
// false when body is anything else.
func switchClauses(body []ast.Stmt) ([]ast.Stmt, bool) {
	if len(body) == 2 {
		if _, ok := body[1].(*ast.ReturnStmt); !ok {
			return nil, false
		}
		body = body[:1]
	}
	if len(body) != 1 {
		return nil, false
	}
	switch s := body[0].(type) {
	case *ast.SwitchStmt:
		return s.Body.List, true
	case *ast.TypeSwitchStmt:
		return s.Body.List, true
	}
	return nil, false
}

// isArm reports whether clause is a case or default clause with one statement
// of the permitted kinds and shape.
func isArm(clause ast.Stmt) bool {
	c, ok := clause.(*ast.CaseClause)
	if !ok || len(c.Body) != 1 {
		return false
	}
	switch c.Body[0].(type) {
	case *ast.ReturnStmt, *ast.AssignStmt, *ast.ExprStmt, *ast.SendStmt:
		return plain(c.Body[0])
	}
	return false
}

// plain reports whether s holds no branching construct and at most one call.
func plain(s ast.Stmt) bool {
	calls, ok := 0, true
	ast.Inspect(s, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt,
			*ast.SelectStmt, *ast.GoStmt, *ast.DeferStmt, *ast.FuncLit:
			ok = false
		case *ast.BinaryExpr:
			if isShortCircuit(n) {
				ok = false
			}
		case *ast.CallExpr:
			calls++
		}
		return ok
	})
	return ok && calls <= 1
}
