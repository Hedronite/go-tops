package main

import (
	"context"

	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/ratchet"
)

// Wiring the ratchet judge into the runner, which does not import it. The judge
// measures with the baseline package's measurer, and reads the baseline and the acks
// file at the paths the configuration names, as `check` and `baseline` do.
func init() {
	gate.DefaultRatchet = ratchet.NewJudge(ratchet.CollectMeasure(), pathsOf)
}

// pathsOf is the module-relative path of the baseline and of the acks file named by
// the configuration in dir.
func pathsOf(ctx context.Context, dir string) (ratchet.Paths, error) {
	_, acks, rel, err := loadBaselineConfig(ctx, dir)
	if err != nil {
		return ratchet.Paths{}, err
	}
	return ratchet.Paths{Ledger: rel, Acks: acks}, nil
}
