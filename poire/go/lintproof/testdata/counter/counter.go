// Package counter holds Go-POIRE counter-examples. Do not copy them.
//
// Each violating line ends in a `want` marker naming the linter that must
// report it. The test in package lintproof fails on a marker with no finding
// and on a finding with no marker.
package counter

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
)

// GO-05: a value receiver copies the lock, so each call locks its own copy.
type Tally struct {
	mu sync.Mutex
	n  int
}

func (t Tally) Count() int { // want govet/copylocks
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.n
}

// GO-07: %v turns the cause into text, so errors.Is can no longer find it.
func Load(name string) error {
	_, err := os.Stat(name)
	if err != nil {
		return fmt.Errorf("load %s: %v", name, err) // want errorlint
	}
	return nil
}

// GO-08: the error from Close on a written file is where a failed flush shows up.
func Save(name string, data []byte) error {
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err != nil {
		return err
	}
	_ = f.Close() // want errcheck
	return nil
}

// GO-11 and GO-03: no context and no Close. The request cannot be cancelled,
// and the connection never goes back to the pool.
func Fetch(url string) (int, error) {
	resp, err := http.Get(url) // want bodyclose noctx
	if err != nil {
		return 0, err
	}
	return resp.StatusCode, nil
}

// Store is declared beside its only implementation.
type Store interface {
	Get(key string) (string, bool)
}

type memStore struct{ m map[string]string }

func (s *memStore) Get(key string) (string, bool) {
	v, ok := s.m[key]
	return v, ok
}

// GO-14: the constructor hides the concrete type behind its producer's interface.
func NewStore() Store { // want ireturn
	return &memStore{m: map[string]string{}}
}

// GO-16: a per-iteration copy that Go 1.22 made redundant.
func Names(users []string) []func() string {
	out := make([]func() string, 0, len(users))
	for _, u := range users {
		u := u // want copyloopvar
		out = append(out, func() string { return u })
	}
	return out
}

// GO-17: the inner err shadows the outer one, and the outer error is what returns.
func Parse(a, b string) error {
	err := check(a)
	if b != "" {
		err := check(b) // want govet/shadow
		if err != nil {
			return err
		}
	}
	return err
}

func check(s string) error {
	if s == "x" {
		return errors.New("bad input")
	}
	return nil
}

// GO-26: bool, int64, bool, int64 pads to 32 bytes; ordered largest first it is 24.
type Sample struct { // want govet/fieldalignment
	ok    bool
	at    int64
	moved bool
	n     int64
}

// GO-30: Sprintf boxes the int into an interface and parses a format string.
func Label(n int) string {
	return fmt.Sprintf("%d", n) // want perfsprint
}

// GO-19: nested conditions where guard clauses would read top to bottom.
func Grade(score int, curved, late bool) string {
	if score >= 0 { // want nestif
		if curved {
			if late {
				if score > 50 {
					return "pass"
				}
			}
		}
	}
	return "fail"
}
