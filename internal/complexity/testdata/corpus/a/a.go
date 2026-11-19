// Package a is the parity corpus: every construct §6.3 counts, written so the
// complexity of each function is fixed by the rule and checked against gocyclo.
package a

import (
	"errors"
	"fmt"
)

var errX = errors.New("x")

func mk() error { return nil }

// Straight has no decision: 1.
func Straight() int { return 1 }

// Branches: 1 + if + else-if + if = 4.
func Branches(n int) int {
	if n > 0 {
		return 1
	} else if n < 0 {
		return -1
	}
	if n == 0 {
		return 0
	}
	return n
}

// SwitchDefault is an n-way switch with a default: the default is not
// counted, so 1 + 3 cases = 4.
func SwitchDefault(n int) string {
	switch n {
	case 1:
		return "one"
	case 2, 3:
		return "few"
	case 4:
		return "four"
	default:
		return "many"
	}
}

// TypeSwitch counts each non-default case: 1 + 2 = 3.
func TypeSwitch(v any) int {
	switch v.(type) {
	case int:
		return 1
	case string:
		return 2
	default:
		return 0
	}
}

// Loops: 1 + for + range + nested for + if = 5.
func Loops(xs []int, n int) int {
	total := 0
	for i := 0; i < n; i++ {
		for range xs {
			total++
		}
	}
	for _, x := range xs {
		if x > 1 {
			total += x
		}
	}
	return total
}

// Logic counts every && and ||: 1 + if + && + || + || = 5.
func Logic(a, b, c, d bool) bool {
	if a && (b || c) {
		return d || a
	}
	return false
}

// Closure counts the branches of the function literal inside it:
// 1 + for + if + if (inside the literal) + && = 5.
func Closure(xs []int) func(int) int {
	for range xs {
		_ = xs
	}
	if len(xs) > 0 {
		return func(n int) int {
			if n > 0 && len(xs) > 1 {
				return n
			}
			return 0
		}
	}
	return nil
}

// Guards has three error guards. With the discount off each counts: 1 + 3 = 4.
func Guards() error {
	err := mk()
	if err != nil {
		return err
	}
	if err = mk(); err != nil {
		return fmt.Errorf("again: %w", err)
	}
	err = mk()
	if err != nil {
		println("logged")
		return err
	}
	return nil
}

// Goto counts nothing for labels, goto or break: 1 + for + if = 3.
func Goto(n int) int {
outer:
	for n > 0 {
		if n == 5 {
			break outer
		}
		n--
	}
	goto done
done:
	return n
}

// Lit is a package-level function literal, scored on its own: 1 + if = 2.
var Lit = func(n int) int {
	if n > 0 {
		return n
	}
	return 0
}

// A and B are two literals in one declaration: A is 1, B is 1 + && = 2.
var (
	A = func() int { return 1 }
	B = func(a, b bool) bool { return a && b }
)

// T has a value and a pointer method.
type T struct{ n int }

// Val: 1 + if = 2.
func (t T) Val() int {
	if t.n > 0 {
		return t.n
	}
	return 0
}

// Ptr: 1 + || = 2.
func (t *T) Ptr(a, b bool) bool { return a || b }

func init() {
	if errX != nil {
		_ = errX
	}
}
