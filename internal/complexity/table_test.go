package complexity_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/complexity"
)

// The oracle for TableShape, from GO_TOPS.md §6.8. A function is table-shaped
// when (1) its body is one switch or type switch, optionally followed by one
// return; (2) every clause, default included, has a body of exactly one
// statement: a return, an assignment, an expression statement or a channel
// send; (3) that statement holds no if, for, range, switch, select, go, defer,
// function literal, && or ||, and at most one call expression.

func parseFunc(t *testing.T, src string) *ast.FuncDecl {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "t.go", "package p\n"+src, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok {
			return fn
		}
	}
	t.Fatal("no function in source")
	return nil
}

func TestTableShape(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want bool
	}{
		// Table-shaped.
		{"switch with default", `func f(n int) string { switch n { case 1: return "a"; case 2: return "b"; default: return "z" } }`, true},
		{"switch then return", `func f(n int) (s string) { switch n { case 1: s = "a"; case 2: s = "b" }; return s }`, true},
		{"type switch", `func f(v any) int { switch v.(type) { case int: return 1; case string: return 2; default: return 0 } }`, true},
		{"type switch with binding", `func f(v any) int { switch x := v.(type) { case int: return x; default: return 0 } }`, true},
		{"switch without tag", `func f(n int) int { switch { case n < 1: return 1; default: return 2 } }`, true},
		{"expression statement arms", `func f(n int) { switch n { case 1: one(); case 2: two(); default: other() } }`, true},
		{"one call per arm", `func f(n int) int { switch n { case 1: return g(1); default: return g(2) } }`, true},
		{"channel send arm", `func f(n int, c chan int) { switch n { case 1: c <- 1; default: c <- 2 } }`, true},
		{"assignment arm with a call", `func f(n int) (x int) { switch n { case 1: x = g(n) }; return }`, true},
		{"multi-value return arm", `func f(n int) (int, error) { switch n { case 1: return 1, nil; default: return 0, e } }`, true},

		// Condition 1: the body.
		{"no switch", `func f(n int) int { return n }`, false},
		{"empty body", `func f() {}`, false},
		{"no body", `func f(n int) int`, false},
		{"statement before the switch", `func f(n int) int { x := n; switch x { case 1: return 1 }; return 0 }`, false},
		{"switch then two statements", `func f(n int) int { switch n { case 1: return 1 }; n++; return 0 }`, false},
		{"switch then a non-return", `func f(n int) { switch n { case 1: one() }; two() }`, false},
		{"select is not a switch", `func f(c chan int) { select { case <-c: one(); default: two() } }`, false},
		{"two switches", `func f(n int) int { switch n { case 1: return 1 }; switch n { case 2: return 2 }; return 0 }`, false},

		// Condition 2: one statement per clause.
		{"clause with two statements", `func f(n int) int { switch n { case 1: x := 1; return x; default: return 0 } }`, false},
		{"default with two statements", `func f(n int) int { switch n { case 1: return 1; default: one(); return 0 } }`, false},
		{"empty clause", `func f(n int) { switch n { case 1: one(); default: } }`, false},
		{"empty case", `func f(n int) { switch n { case 1: ; case 2: one() } }`, false},
		{"fallthrough arm", `func f(n int) { switch n { case 1: fallthrough; default: one() } }`, false},
		{"break arm", `func f(n int) { switch n { case 1: break; default: one() } }`, false},
		{"increment arm", `func f(n, x int) { switch n { case 1: x++ } }`, false},
		{"declaration arm", `func f(n int) { switch n { case 1: var x int } }`, false},
		{"block arm", `func f(n int) { switch n { case 1: { one() } } }`, false},

		// Condition 3: what a clause statement holds.
		{"arm with an if", `func f(n int) int { switch n { case 1: if n > 0 { return 1 }; default: return 0 }; return 0 }`, false},
		{"arm with a for", `func f(n int) { switch n { case 1: for { one() } } }`, false},
		{"arm with a range", `func f(xs []int) { switch len(xs) { case 1: for range xs { one() } } }`, false},
		{"arm with a nested switch", `func f(n int) { switch n { case 1: switch n { case 2: one() } } }`, false},
		{"arm with a nested type switch", `func f(n int, v any) { switch n { case 1: switch v.(type) { case int: one() } } }`, false},
		{"arm with a select", `func f(n int, c chan int) { switch n { case 1: select { case <-c: one() } } }`, false},
		{"arm with go", `func f(n int) { switch n { case 1: go one() } }`, false},
		{"arm with defer", `func f(n int) { switch n { case 1: defer one() } }`, false},
		{"arm with a function literal", `func f(n int) func() { switch n { case 1: return func() {} }; return nil }`, false},
		{"arm with &&", `func f(n int, a, b bool) bool { switch n { case 1: return a && b }; return false }`, false},
		{"arm with ||", `func f(n int, a, b bool) bool { switch n { case 1: return a || b }; return false }`, false},
		{"arm with two calls", `func f(n int) int { switch n { case 1: return g(h(n)) }; return 0 }`, false},
		{"arm with a conversion around a call", `func f(n int) int64 { switch n { case 1: return int64(g(n)) }; return 0 }`, false},
		{"arm with two calls in a result list", `func f(n int) (int, int) { switch n { case 1: return g(1), g(2) }; return 0, 0 }`, false},
		{"arm with a call chain", `func f(n int) int { switch n { case 1: return a().b() }; return 0 }`, false},
		{"one bad arm among good ones", `func f(n int) int { switch n { case 1: return 1; case 2: return 2; case 3: if n > 0 { return 3 }; default: return 0 }; return 0 }`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := complexity.TableShape(parseFunc(t, tc.src)); got != tc.want {
				t.Errorf("TableShape = %v, want %v\n%s", got, tc.want, tc.src)
			}
		})
	}
}

func TestTableShapeNilDecl(t *testing.T) {
	if complexity.TableShape(nil) {
		t.Error("TableShape(nil) = true, want false")
	}
}

// §6.2 row 11: one switch with 29 case clauses and a default, each clause a
// single return. comp is 1 + 29 = 30, the default is not counted, and the
// function is table-shaped.
func TestWorkedExampleRow11IsATable(t *testing.T) {
	var b strings.Builder
	b.WriteString("func f(n int) int {\n\tswitch n {\n")
	for i := 1; i <= 29; i++ {
		b.WriteString("\tcase " + strconv.Itoa(i) + ":\n\t\treturn " + strconv.Itoa(i*2) + "\n")
	}
	b.WriteString("\tdefault:\n\t\treturn 0\n\t}\n}\n")
	fn := parseFunc(t, b.String())
	if !complexity.TableShape(fn) {
		t.Error("row 11 dispatch table is not table-shaped")
	}
	if got := complexity.Count(fn); got != 30 {
		t.Errorf("row 11 complexity = %d, want 30", got)
	}
}
