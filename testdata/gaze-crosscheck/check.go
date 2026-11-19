// Package gazecheck is a fixture for the cross-check of Go-TOPS CRAP against
// gaze. Each function stands for one place where the two can differ: error
// guards (GO_TOPS.md §6.3), closures, package-level function literals,
// generics, a function with no statements, a function with no test, and two
// init functions.
package gazecheck

import (
	"cmp"
	"errors"
	"fmt"
	"strconv"
)

// ErrMissing is the sentinel Lookup and Classify use.
var ErrMissing = errors.New("missing")

// Parse has two guards that return the error (the second wraps it with %w) and
// one real branch. Its test passes a valid number, so neither guard body runs.
func Parse(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	m, err := strconv.Atoi(s + "0")
	if err != nil {
		return 0, fmt.Errorf("parse %q: %w", s, err)
	}
	if n < 0 {
		return 0, errors.New("negative")
	}
	return n + m, nil
}

// Wrapv formats the error with %v, which does not wrap it, so its guard is a
// decision.
func Wrapv(s string) error {
	_, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("bad number: %v", err)
	}
	return nil
}

// Classify tests with errors.Is and returns something other than the error:
// both are decisions.
func Classify(err error) string {
	if errors.Is(err, ErrMissing) {
		return "missing"
	}
	if err != nil {
		return "other"
	}
	return "none"
}

// Dispatch is a switch with a default; the default is not counted.
func Dispatch(op string) int {
	switch op {
	case "add":
		return 1
	case "sub":
		return 2
	case "mul":
		return 3
	case "div":
		return 4
	default:
		return 0
	}
}

// Sum holds its branches in a closure, which counts inside Sum.
func Sum(xs []int, keep func(int) bool) int {
	total := 0
	add := func(x int) {
		if keep != nil && !keep(x) {
			return
		}
		total += x
	}
	for _, x := range xs {
		add(x)
	}
	return total
}

// Double is a package-level function literal: scored on its own.
var Double = func(n int) int {
	if n < 0 {
		return -n
	}
	return n * 2
}

// Max is a generic function.
func Max[T cmp.Ordered](a, b T) T {
	if a > b {
		return a
	}
	return b
}

// Stack is a generic type with a method.
type Stack[T any] struct{ items []T }

// Push adds an item.
func (s *Stack[T]) Push(v T) { s.items = append(s.items, v) }

// Pop removes the last item.
func (s *Stack[T]) Pop() (T, bool) {
	var zero T
	if len(s.items) == 0 {
		return zero, false
	}
	v := s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	return v, true
}

// Noop has no statements.
func Noop() {}

// Untested has no test at all.
func Untested(n int) int {
	if n > 3 {
		return n
	}
	return 0
}

var registered []string

func init() {
	registered = append(registered, "first")
}

func init() {
	if len(registered) > 5 {
		registered = nil
	}
	registered = append(registered, "second")
}
