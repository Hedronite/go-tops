package ratchet

import (
	"math"

	"github.com/Hedronite/go-tops/internal/baseline"
)

// recorded reports whether r is the record of change c: the same kind and key and
// the same new value. The record's old value is not compared. A record outlives
// the merge that brought it in, and a later accepted raise of the same entry keeps
// the first record's old value, so it need not be the diff base's (the baseline
// package's rule, Regression).
func recorded(c baseline.Change, r Regression) bool {
	return r.Kind == c.Kind && r.Key == c.Key && math.Abs(r.To-c.To) <= tolerance
}
