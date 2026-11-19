package complexity

import (
	"go/ast"
	"go/token"
)

// Count is the baseline of GO_TOPS.md §6.3, which matches gocyclo:
// 1 + if + for + range + case + comm + && + ||. A default clause is not
// counted, and function literals inside node count. node is a function
// declaration or a function literal. Count applies no discount; it needs no
// type information.
func Count(node ast.Node) int {
	comp := 1
	ast.Inspect(node, func(n ast.Node) bool {
		comp += weight(n)
		return true
	})
	return comp
}

// weight is what node n adds to the complexity of the function around it.
func weight(n ast.Node) int {
	switch n := n.(type) {
	case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
		return 1
	case *ast.CaseClause:
		return decision(n.List != nil)
	case *ast.CommClause:
		return decision(n.Comm != nil)
	case *ast.BinaryExpr:
		return decision(isShortCircuit(n))
	}
	return 0
}

// decision is 1 when the node is a branch and 0 when it is a default clause or
// an operator that does not branch.
func decision(branches bool) int {
	if branches {
		return 1
	}
	return 0
}

func isShortCircuit(b *ast.BinaryExpr) bool {
	return b.Op == token.LAND || b.Op == token.LOR
}
