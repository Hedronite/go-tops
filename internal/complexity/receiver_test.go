package complexity

import (
	"go/ast"
	"testing"
)

func recvList(e ast.Expr) *ast.FieldList {
	return &ast.FieldList{List: []*ast.Field{{Type: e}}}
}

func TestReceiverName(t *testing.T) {
	ident := &ast.Ident{Name: "T"}
	tests := []struct {
		name string
		recv *ast.FieldList
		want string
	}{
		{"none", nil, ""},
		{"empty", &ast.FieldList{}, ""},
		{"value", recvList(ident), "T"},
		{"pointer", recvList(&ast.StarExpr{X: ident}), "T"},
		{"parenthesized pointer", recvList(&ast.ParenExpr{X: &ast.StarExpr{X: ident}}), "T"},
		{"generic", recvList(&ast.IndexExpr{X: ident, Index: &ast.Ident{Name: "K"}}), "T"},
		{"generic pointer, two parameters", recvList(&ast.StarExpr{X: &ast.IndexListExpr{X: ident}}), "T"},
	}
	for _, tc := range tests {
		got, err := receiverName(tc.recv)
		if err != nil || got != tc.want {
			t.Errorf("%s: (%q, %v), want %q", tc.name, got, err, tc.want)
		}
	}
}

// An unknown receiver form is an error, never a made-up name that would end up
// in a function identity.
func TestReceiverNameRejectsUnknownForms(t *testing.T) {
	for _, e := range []ast.Expr{&ast.ArrayType{Elt: &ast.Ident{Name: "T"}}, &ast.SelectorExpr{X: &ast.Ident{Name: "p"}, Sel: &ast.Ident{Name: "T"}}} {
		if got, err := receiverName(recvList(e)); err == nil {
			t.Errorf("%T: got %q, want an error", e, got)
		}
	}
}
