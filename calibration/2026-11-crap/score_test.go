//go:build calibration

package calibration

import "errors"

// scoreModule returns every production function of the module in dir, scored by
// the native CRAP layer from the coverage profile at profile: its identity,
// file, complexity, guard-neutral coverage and CRAP (GO_TOPS.md §6.2). It is the
// only place this directory touches internal/crap.
//
// Stand-in until G1 merges: replace the body with the call into internal/crap.
func scoreModule(dir, profile string) ([]scored, error) {
	return nil, errors.New("internal/crap has not merged: scoreModule is a stand-in")
}
