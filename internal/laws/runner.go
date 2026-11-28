package laws

import (
	"context"
	"fmt"
	"maps"

	"github.com/Hedronite/go-tops/internal/analyzers/driver"
)

// RunnerResult is the outcome of facts plus in-process analyzers.
type RunnerResult struct {
	OK    bool
	ByLaw map[string]int
}

// GoEnvFunc returns go env GOVERSION (injectable for tests).
type GoEnvFunc func() (string, error)

// Run evaluates fact laws and custom analyzers on the module at dir.
func Run(ctx context.Context, dir, class string, goEnv GoEnvFunc) (RunnerResult, error) {
	byLaw := map[string]int{}

	facts, err := CheckFacts(dir, class, goEnv)
	if err != nil {
		return RunnerResult{}, fmt.Errorf("facts: %w", err)
	}
	maps.Copy(byLaw, facts)

	res, err := driver.Run(ctx, dir)
	if err != nil {
		return RunnerResult{}, fmt.Errorf("analyzers: %w", err)
	}
	for id, n := range res.ByLaw {
		byLaw[id] += n
	}

	return RunnerResult{OK: true, ByLaw: byLaw}, nil
}
