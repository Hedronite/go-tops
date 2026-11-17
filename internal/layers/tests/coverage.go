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
	path := filepath.Join(env.Dir, filepath.FromSlash(ProfilePath))
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return failErr("the coverage profile %s does not exist: the test run wrote none", ProfilePath)
	}
	if err != nil {
		return failErr("cannot read %s: %v", ProfilePath, err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return failErr("the coverage profile %s is empty", ProfilePath)
	}
	prof, err := cover.Parse(strings.NewReader(string(raw)))
	if err != nil {
		return failErr("%s: %v", ProfilePath, err)
	}
	cc := env.Config.Coverage
	sum, err := prof.Summarize(cover.Options{Module: env.Config.Module, Dir: env.Dir, ExcludeGenerated: cc.ExcludeGenerated})
	if err != nil {
		return failErr("%s: %v", ProfilePath, err)
	}
	d := map[string]any{"statements_pct": sum.Pct(), "floor": cc.StatementsFloor, "profile_bytes": len(raw)}
	below := sum.Below(cc.StatementsFloor)
	if len(below) == 0 {
		return gate.Result{Outcome: gate.Pass, Details: d}
	}
	parts := make([]string, 0, 5)
	for _, p := range below {
		if len(parts) == 5 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s %.1f%% (%d of %d statements uncovered)", p.Path, p.Pct(), p.Uncovered(), p.Statements))
	}
	msg := fmt.Sprintf("%d package(s) below the %g%% statement floor (total %.1f%%): %s", len(below), cc.StatementsFloor, sum.Pct(), strings.Join(parts, "; "))
	if len(below) > len(parts) {
		msg += fmt.Sprintf("; and %d more", len(below)-len(parts))
	}
	return gate.Result{Outcome: gate.Fail, Reason: msg, Details: d}
}
