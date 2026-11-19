//go:build calibration

package calibration

import (
	"github.com/Hedronite/go-tops/internal/crap"
	"github.com/Hedronite/go-tops/internal/layers/tests"
)

// scoreModule scores the module in dir against the coverage profile at
// profile with the native CRAP code, under the build the tests layer runs
// (GO_TOPS.md §6.2): every production function the build compiles, scored, and
// every one it does not, with its complexity only. It is the only place this
// directory touches internal/crap. The knobs are the profile defaults of the
// launch classes: the error-guard discount on and no wrap functions.
func scoreModule(dir, profile string) ([]scored, []unmeasured, error) {
	rep, err := crap.Score(dir, profile, crap.Options{ErrGuardDiscount: true, Build: tests.Build})
	if err != nil {
		return nil, nil, err
	}
	measured := make([]scored, 0, len(rep.Measured))
	for _, f := range rep.Measured {
		measured = append(measured, scored{Key: f.Key, File: f.File, Comp: f.Comp, Cov: f.Cov, CRAP: f.CRAP})
	}
	not := make([]unmeasured, 0, len(rep.NotMeasured))
	for _, f := range rep.NotMeasured {
		not = append(not, unmeasured{Key: f.Key, File: f.File, Comp: f.Comp, Typed: f.Typed})
	}
	return measured, not, nil
}
