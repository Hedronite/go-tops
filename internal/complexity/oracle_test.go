package complexity_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/complexity"
)

// The oracle for the error-guard discount. Every case is written from
// GO_TOPS.md §6.3, never from the implementation: each is one function whose
// complexity the protocol fixes, with the discount on and with it off.
//
// A case is a function declaration written with FN as its name. `comp` is its
// complexity with the discount on, `guards` the number of guards §6.3 forgives
// in it, so the figure with the discount off is comp + guards. `wrap` marks a
// case that is discounted only through complexity.wrap_functions.

const oraclePrelude = `package oracle

import (
	"errors"
	"fmt"

	"example.com/oracle/wrap"
)

var errSentinel = errors.New("sentinel")

type myErr struct{}

func (*myErr) Error() string { return "my" }

func mk() error         { return nil }
func mk2() (int, error) { return 0, nil }
func mkMy() *myErr      { return nil }

func wrapIt(err error) error { return fmt.Errorf("it: %w", err) }

var (
	_ = errors.Is
	_ = fmt.Errorf
	_ = wrap.Wrap
)

`

const oracleWrapPkg = `package wrap

import "fmt"

func Wrap(err error) error { return fmt.Errorf("w: %w", err) }

func With(msg string, err error) error { return fmt.Errorf("%s: %w", msg, err) }

func Other(err error) error { return fmt.Errorf("o: %w", err) }

func Gen[T any](err error) error { return fmt.Errorf("g: %w", err) }

func Gen2[T, U any](err error) error { return fmt.Errorf("g2: %w", err) }

type W struct{}

func (W) Wrap(err error) error { return Wrap(err) }
`

type oracleCase struct {
	name   string
	src    string
	comp   int
	guards int
	wrap   bool
}

var oracleCases = []oracleCase{
	// Qualifying guards (§6.3): the discount applies.
	{name: "ReturnErr", comp: 1, guards: 1, src: `func FN() error { err := mk(); if err != nil { return err }; return nil }`},
	{name: "WrapW", comp: 1, guards: 1, src: `func FN() error { err := mk(); if err != nil { return fmt.Errorf("ctx: %w", err) }; return nil }`},
	{name: "WrapWSecondVerb", comp: 1, guards: 1, src: `func FN() error { err := mk(); if err != nil { return fmt.Errorf("%s: %w", "ctx", err) }; return nil }`},
	{name: "WrapWTwoVerbs", comp: 1, guards: 1, src: `func FN() error { err := mk(); if err != nil { return fmt.Errorf("%w: %w", errSentinel, err) }; return nil }`},
	{name: "WrapWExplicitIndex", comp: 1, guards: 1, src: `func FN() error { err := mk(); if err != nil { return fmt.Errorf("%v: %[1]w", err) }; return nil }`},
	{name: "WrapWPercentLiteral", comp: 1, guards: 1, src: `func FN() error { err := mk(); if err != nil { return fmt.Errorf("100%% %w", err) }; return nil }`},
	{name: "WrapWStarWidth", comp: 1, guards: 1, src: `func FN() error { err := mk(); if err != nil { return fmt.Errorf("%*d %w", 3, 4, err) }; return nil }`},
	{name: "MultiResult", comp: 1, guards: 1, src: `func FN() (int, error) { n, err := mk2(); if err != nil { return n, err }; return n, nil }`},
	{name: "NotNamedErr", comp: 1, guards: 1, src: `func FN() error { bad := mk(); if bad != nil { return bad }; return nil }`},
	{name: "InitClause", comp: 1, guards: 1, src: `func FN() error { if e := mk(); e != nil { return e }; return nil }`},
	{name: "AssignedByEquals", comp: 1, guards: 1, src: `func FN() error { var err error; err = mk(); if err != nil { return err }; return nil }`},
	{name: "AssignedByVarDecl", comp: 1, guards: 1, src: `func FN() error { var err = mk(); if err != nil { return err }; return nil }`},
	{name: "ElseIfWithInit", comp: 2, guards: 1, src: `func FN(ok bool) error { if ok { return nil } else if e := mk(); e != nil { return e }; return nil }`},
	{name: "InCaseBody", comp: 2, guards: 1, src: `func FN(n int) error { switch n { case 1: err := mk(); if err != nil { return err } }; return nil }`},
	{name: "InClosure", comp: 1, guards: 1, src: `func FN() func() error { return func() error { err := mk(); if err != nil { return err }; return nil } }`},
	{name: "TwoGuards", comp: 1, guards: 2, src: `func FN() error { err := mk(); if err != nil { return err }; err = mk(); if err != nil { return fmt.Errorf("x: %w", err) }; return nil }`},
	{name: "ClosureInGuardStillCounts", comp: 2, guards: 1, src: `func FN() (int, error) { err := mk(); if err != nil { return func() int { if errSentinel != nil { return 1 }; return 2 }(), err }; return 0, nil }`},
	{name: "WrapFunction", comp: 1, guards: 1, wrap: true, src: `func FN() error { err := mk(); if err != nil { return wrap.Wrap(err) }; return nil }`},
	{name: "WrapFunctionErrNotFirst", comp: 1, guards: 1, wrap: true, src: `func FN() error { err := mk(); if err != nil { return wrap.With("ctx", err) }; return nil }`},
	{name: "WrapFunctionSamePackage", comp: 1, guards: 1, wrap: true, src: `func FN() error { err := mk(); if err != nil { return wrapIt(err) }; return nil }`},
	{name: "WrapFunctionInstantiated", comp: 1, guards: 1, wrap: true, src: `func FN() error { err := mk(); if err != nil { return wrap.Gen[int](err) }; return nil }`},
	{name: "WrapFunctionTwoTypeArguments", comp: 1, guards: 1, wrap: true, src: `func FN() error { err := mk(); if err != nil { return wrap.Gen2[int, string](err) }; return nil }`},
	{name: "RawStringFormat", comp: 1, guards: 1, src: "func FN() error { err := mk(); if err != nil { return fmt.Errorf(`ctx: %w`, err) }; return nil }"},
	{name: "InSelectCase", comp: 2, guards: 1, src: `func FN(c chan int) error { select { case <-c: err := mk(); if err != nil { return err } }; return nil }`},
	{name: "MoreVerbsThanOperands", comp: 1, guards: 1, src: `func FN() error { err := mk(); if err != nil { return fmt.Errorf("%w %w", err) }; return nil }`},

	// Non-qualifying guards: each counts as an ordinary `if`.
	{name: "PercentV", comp: 2, src: `func FN() error { err := mk(); if err != nil { return fmt.Errorf("x: %v", err) }; return nil }`},
	{name: "NoVerb", comp: 2, src: `func FN() error { err := mk(); if err != nil { return fmt.Errorf("x", err) }; return nil }`},
	{name: "WrapsAnotherOperand", comp: 2, src: `func FN() error { err := mk(); if err != nil { return fmt.Errorf("%w", errSentinel) }; return nil }`},
	{name: "FormatIsConstant", comp: 2, src: `func FN() error { const format = "x: %w"; err := mk(); if err != nil { return fmt.Errorf(format, err) }; return nil }`},
	{name: "FormatIsConcatenation", comp: 2, src: `func FN() error { err := mk(); if err != nil { return fmt.Errorf("x: "+"%w", err) }; return nil }`},
	{name: "SpreadOperands", comp: 2, src: `func FN() error { args := []any{mk()}; err := mk(); if err != nil { return fmt.Errorf("%w", args...) }; return nil }`},
	{name: "ErrorsIs", comp: 2, src: `func FN() error { err := mk(); if errors.Is(err, errSentinel) { return err }; return nil }`},
	{name: "SentinelEquals", comp: 2, src: `func FN() error { err := mk(); if err == errSentinel { return err }; return nil }`},
	{name: "SentinelNotEquals", comp: 2, src: `func FN() error { err := mk(); if err != errSentinel { return err }; return nil }`},
	{name: "ReversedNil", comp: 2, src: `func FN() error { err := mk(); if nil != err { return err }; return nil }`},
	{name: "BareReturn", comp: 2, src: `func FN() (err error) { err = mk(); if err != nil { return }; return }`},
	{name: "ReturnsNil", comp: 2, src: `func FN() error { err := mk(); if err != nil { return nil }; return nil }`},
	{name: "ReturnsAnotherError", comp: 2, src: `func FN() error { other := mk(); err := mk(); if err != nil { return other }; return nil }`},
	{name: "HasElse", comp: 2, src: `func FN() error { err := mk(); if err != nil { return err } else { println("ok") }; return nil }`},
	{name: "ElseHoldsReturnNil", comp: 2, src: `func FN() error { err := mk(); if err != nil { return err } else { return nil } }`},
	{name: "LogsFirst", comp: 2, src: `func FN() error { err := mk(); if err != nil { println("x"); return err }; return nil }`},
	{name: "Swallows", comp: 2, src: `func FN() error { err := mk(); if err != nil { println("x") }; return nil }`},
	{name: "ConditionHasAnd", comp: 3, src: `func FN() error { err := mk(); if err != nil && errSentinel != nil { return err }; return nil }`},
	{name: "TypeIsNotError", comp: 2, src: `func FN() error { err := mkMy(); if err != nil { return err }; return nil }`},
	{name: "NotAssignedJustBefore", comp: 2, src: `func FN() error { err := mk(); println("x"); if err != nil { return err }; return nil }`},
	{name: "AssignedTwoBack", comp: 2, src: `func FN() error { err := mk(); other := mk(); if err != nil { return err }; return other }`},
	{name: "Parameter", comp: 2, src: `func FN(err error) error { if err != nil { return err }; return nil }`},
	{name: "WrapFunctionNotListed", comp: 2, src: `func FN() error { err := mk(); if err != nil { return wrap.Other(err) }; return nil }`},
	{name: "WrapMethodIsNotAFunction", comp: 2, src: `func FN() error { err := mk(); if err != nil { return wrap.W{}.Wrap(err) }; return nil }`},
	{name: "WrapVerbHasNoOperand", comp: 2, src: `func FN() error { err := mk(); if err != nil { return fmt.Errorf("%v %w", err) }; return nil }`},
	{name: "WrapVerbWithNoOperands", comp: 2, src: `func FN() error { err := mk(); if err != nil { return fmt.Errorf("%w") }; return nil }`},
	{name: "WrapVerbIndexPastTheOperands", comp: 2, src: `func FN() error { err := mk(); if err != nil { return fmt.Errorf("%[3]w", err) }; return nil }`},
	{name: "UnparsableFormat", comp: 2, src: `func FN() error { err := mk(); if err != nil { return fmt.Errorf("%[x]w", err) }; return nil }`},
	{name: "CompareWithACall", comp: 2, src: `func FN() error { err := mk(); if err != mk() { return err }; return nil }`},
	{name: "FunctionValueIsNotAWrapFunction", comp: 2, src: `func FN() error { f := wrap.Wrap; err := mk(); if err != nil { return f(err) }; return nil }`},
	{name: "FunctionLiteralCall", comp: 2, src: `func FN() error { err := mk(); if err != nil { return func(e error) error { return e }(err) }; return nil }`},
	{name: "VarWithoutValue", comp: 2, src: `func FN() error { var err error; if err != nil { return err }; return nil }`},
	{name: "AssignedByAnotherDeclaration", comp: 2, src: `func FN() error { err := mk(); var other = mk(); if err != nil { return err }; return other }`},
	{name: "ConstBefore", comp: 2, src: `func FN() error { err := mk(); const c = 1; if err != nil { return err }; return nil }`},
}

const oracleWrapList = "example.com/oracle/wrap.Wrap,example.com/oracle/wrap.With,example.com/oracle/wrap.Gen,example.com/oracle/wrap.Gen2,example.com/oracle.wrapIt"

func writeOracle(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	var src strings.Builder
	src.WriteString(oraclePrelude)
	for _, c := range oracleCases {
		src.WriteString(strings.ReplaceAll(c.src, "FN", c.name))
		src.WriteString("\n\n")
	}
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/oracle\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "oracle.go"), src.String())
	writeFile(t, filepath.Join(dir, "wrap", "wrap.go"), oracleWrapPkg)
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func byName(t *testing.T, funcs []complexity.Func, pkg string) map[string]complexity.Func {
	t.Helper()
	out := map[string]complexity.Func{}
	for _, f := range funcs {
		if f.Pkg == pkg && f.Recv == "" {
			out[f.Name] = f
		}
	}
	return out
}

func TestGuardOracle(t *testing.T) {
	dir := writeOracle(t)
	pkgs, err := complexity.Load(dir, nil, "./...")
	if err != nil {
		t.Fatal(err)
	}
	wrapList := strings.Split(oracleWrapList, ",")
	configs := []struct {
		name string
		cfg  complexity.Config
		want func(c oracleCase) int
	}{
		{
			"discount on, wrap functions listed",
			complexity.Config{Discount: true, WrapFunctions: wrapList},
			func(c oracleCase) int { return c.comp },
		},
		{
			"discount on, no wrap functions",
			complexity.Config{Discount: true},
			func(c oracleCase) int {
				if c.wrap {
					return c.comp + c.guards
				}
				return c.comp
			},
		},
		{
			"discount off",
			complexity.Config{WrapFunctions: wrapList},
			func(c oracleCase) int { return c.comp + c.guards },
		},
	}
	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			funcs, err := complexity.Analyze(dir, pkgs, tc.cfg)
			if err != nil {
				t.Fatal(err)
			}
			got := byName(t, funcs, "example.com/oracle")
			for _, c := range oracleCases {
				f, ok := got[c.name]
				if !ok {
					t.Errorf("%s: not reported", c.name)
					continue
				}
				if want := tc.want(c); f.Complexity != want {
					t.Errorf("%s: complexity %d, want %d", c.name, f.Complexity, want)
				}
			}
		})
	}
}

func TestGuardSpansTrackTheDiscount(t *testing.T) {
	dir := writeOracle(t)
	pkgs, err := complexity.Load(dir, nil, "./...")
	if err != nil {
		t.Fatal(err)
	}
	on, err := complexity.Analyze(dir, pkgs, complexity.Config{Discount: true})
	if err != nil {
		t.Fatal(err)
	}
	off, err := complexity.Analyze(dir, pkgs, complexity.Config{})
	if err != nil {
		t.Fatal(err)
	}
	gotOn, gotOff := byName(t, on, "example.com/oracle"), byName(t, off, "example.com/oracle")
	for _, c := range oracleCases {
		wantOn := c.guards
		if c.wrap {
			wantOn = 0
		}
		if n := len(gotOn[c.name].Guards); n != wantOn {
			t.Errorf("%s: %d guard spans with the discount on, want %d", c.name, n, wantOn)
		}
		if n := len(gotOff[c.name].Guards); n != 0 {
			t.Errorf("%s: %d guard spans with the discount off, want 0", c.name, n)
		}
	}
}
