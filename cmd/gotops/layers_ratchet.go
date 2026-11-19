package main

import (
	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/ratchet"
)

// Wiring the ratchet judge into the runner, which does not import it. The judge
// measures with the baseline package's measurer, and reads the acks file the
// configuration names, as `baseline` does.
func init() {
	gate.DefaultRatchet = ratchet.NewJudge(ratchet.CollectMeasure(acksOf))
}

// acksOf is the module-relative path of the acks file of the configuration in dir.
func acksOf(dir string) (string, error) {
	_, acks, _, err := loadBaselineConfig(dir)
	return acks, err
}
