package tests

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Hedronite/go-tops/internal/cover"
	"github.com/Hedronite/go-tops/internal/gate"
)

// Coverage is gate 3: statement coverage of the profile the tests layer wrote.
// It never runs the tests itself, so a test cannot pass in one gate and be
// absent from another (§3.1).
//
// Merging integration coverage (go build -cover, GOCOVERDIR) into the profile
// is not part of this layer yet; the profile it judges is the unit run's, which
// can only understate coverage.
type Coverage struct{}

// ID implements gate.Layer.
func (Coverage) ID() string { return CoverageID }

// Tier implements gate.Layer.
func (Coverage) Tier() int { return 1 }

// Needs implements gate.Layer: coverage needs the tests (§3.5).
func (Coverage) Needs() []string { return []string{TestsID} }

// Run implements gate.Layer. The floor applies to each package, and so to the
// total (§6.7, hard): a package below it fails; a missing, empty or
// block-less profile fails (§3.4 rule 5, §6.1).
func (Coverage) Run(_ context.Context, env *gate.Env) gate.Result {
	// Merging integration coverage is not part of this release. A config that
	// turns it on must not read as a pass on unit coverage alone.
	if env.Config.Coverage.IntegrationEnabled {
		return failErr("gates.coverage.integration.enabled is true: integration merge not implemented in this release")
	}
	sum, size, err := summarizeProfile(env)
	if err != nil {
		return failErr("%v", err)
	}
	return judgeFloor(sum, size, env.Config.Coverage)
}

// summarizeProfile reads the profile the tests layer wrote and totals it per
// package. A profile that is missing, empty, malformed or without a block of a
// production file measured nothing.
func summarizeProfile(env *gate.Env) (cover.Summary, int, error) {
	raw, err := os.ReadFile(filepath.Join(env.Dir, filepath.FromSlash(ProfilePath)))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return cover.Summary{}, 0, fmt.Errorf("the coverage profile %s does not exist: the test run wrote none", ProfilePath)
	case err != nil:
		return cover.Summary{}, 0, fmt.Errorf("cannot read %s: %w", ProfilePath, err)
	case len(strings.TrimSpace(string(raw))) == 0:
		return cover.Summary{}, 0, fmt.Errorf("the coverage profile %s is empty", ProfilePath)
	}
	prof, err := cover.Parse(strings.NewReader(string(raw)))
	if err != nil {
		return cover.Summary{}, 0, fmt.Errorf("%s: %w", ProfilePath, err)
	}
	cc := env.Config.Coverage
	sum, err := prof.Summarize(cover.Options{Module: env.Config.Module, Dir: env.Dir, ExcludeGenerated: cc.ExcludeGenerated})
	if err != nil {
		return cover.Summary{}, 0, fmt.Errorf("%s: %w", ProfilePath, err)
	}
	return sum, len(raw), nil
}

// judgeFloor passes when every package meets the floor, and otherwise fails
// naming the packages furthest below it.
func judgeFloor(sum cover.Summary, profileBytes int, cc gate.CoverageConfig) gate.Result {
	d := map[string]any{"statements_pct": sum.Pct(), "floor": cc.StatementsFloor, "profile_bytes": profileBytes}
	below := sum.Below(cc.StatementsFloor)
	if len(below) == 0 {
		return gate.Result{Outcome: gate.Pass, Details: d}
	}
	return gate.Result{Outcome: gate.Fail, Reason: belowMessage(below, sum.Pct(), cc.StatementsFloor), Details: d}
}

// belowMessage lists up to five packages under the floor, lowest first, and how
// many more there are.
func belowMessage(below []cover.Package, total, floor float64) string {
	parts := make([]string, 0, 5)
	for _, p := range below {
		if len(parts) == 5 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s %.1f%% (%d of %d statements uncovered)", p.Path, p.Pct(), p.Uncovered(), p.Statements))
	}
	msg := fmt.Sprintf("%d package(s) below the %g%% statement floor (total %.1f%%): %s", len(below), floor, total, strings.Join(parts, "; "))
	if len(below) > len(parts) {
		msg += fmt.Sprintf("; and %d more", len(below)-len(parts))
	}
	return msg
}
