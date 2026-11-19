package complexity

import (
	"go/ast"
	"go/token"
	"slices"
	"testing"
)

// The oracle for the %w operand rules: where in a printf format's operand list
// each %w verb takes its value, by the rules of package fmt for flags, width,
// precision, `*` and explicit argument indexes. A format that cannot be parsed
// yields none, so an unreadable format never earns the discount.
func TestWrapOperands(t *testing.T) {
	tests := []struct {
		format string
		want   []int
	}{
		{"", nil},
		{"no verbs here", nil},
		{"%w", []int{0}},
		{"%v %w", []int{1}},
		{"%w %w", []int{0, 1}},
		{"%v: %v: %w", []int{2}},
		{"%%%w", []int{0}},
		{"100%% %w", []int{0}},
		{"%5w", []int{0}},
		{"%-5w", []int{0}},
		{"%.2w", []int{0}},
		{"%5.2f %w", []int{1}},
		{"%-+# 0d %w", []int{1}},
		{"%*d %w", []int{2}},
		{"%.*f %w", []int{2}},
		{"%*.*f %w", []int{3}},
		{"%[2]w %[1]v", []int{1}},
		{"%[1]v %[1]w", []int{0}},
		{"%[1]v %w", []int{1}},
		{"%[3]*.[2]*[1]w", []int{0}},
		{"%.[1]*w", []int{1}},
		// An operand index may be up to maxOperand, and no further.
		{"%[65536]w", []int{65535}},
		{"%[65536]*w", []int{65536}},
		// Formats that cannot be read.
		{"%[65537]w", nil},
		{"%[9223372036854775807]w", nil},
		{"%[9223372036854775807]*.*w", nil},
		{"%[99999999999999999999]w", nil},
		{"%[65536]*.*w", nil},
		{"%[1]*.[65537]*w", nil},
		{"%", nil},
		{"100%", nil},
		{"%w %", nil},
		{"%-", nil},
		{"%5", nil},
		{"%.", nil},
		{"%5.", nil},
		{"%[", nil},
		{"%[1", nil},
		{"%[1]", nil},
		{"%[x]w", nil},
		{"%[0]w", nil},
		{"%[-1]w", nil},
		{"%.[x]w", nil},
		{"%.[1", nil},
		{"%5.[x]*w", nil},
	}
	for _, tc := range tests {
		got := wrapOperands(tc.format)
		if !slices.Equal(got, tc.want) {
			t.Errorf("wrapOperands(%q) = %v, want %v", tc.format, got, tc.want)
		}
	}
}

// A literal the parser would never hand over, but the finder must not trust:
// an unquotable string is no format, so it earns nothing.
func TestWrapsWithVerbRejectsAnUnquotableLiteral(t *testing.T) {
	g := &guardFinder{}
	call := &ast.CallExpr{
		Fun:  ast.NewIdent("Errorf"),
		Args: []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: `"bad`}, ast.NewIdent("err")},
	}
	if g.wrapsWithVerb(call, nil) {
		t.Error("an unquotable format was read as a wrapping one")
	}
}
