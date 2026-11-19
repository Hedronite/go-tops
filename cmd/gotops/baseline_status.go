package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/Hedronite/go-tops/internal/baseline"
	"github.com/Hedronite/go-tops/internal/cli"
	"github.com/Hedronite/go-tops/internal/ratchet"
)

// Exit codes of `baseline --status`: the answer to "is a first ledger due?".
const (
	statusDue     = 0 // a first ledger is due
	statusNotDue  = 1 // present, deleted or not-ratchet
	statusUnknown = 2 // the base cannot be read, or the configuration is invalid
)

// statusLine is the one JSON line `baseline --status` prints. Base is null when
// the answer did not need the diff base; Build is this run's build, set when a
// first ledger is due; Reason says why a run could not tell.
type statusLine struct {
	State   string          `json:"state"`
	Path    string          `json:"path,omitempty"`
	BaseRef string          `json:"base_ref,omitempty"`
	Tree    bool            `json:"tree"`
	Base    *bool           `json:"base"`
	Build   *baseline.Build `json:"build,omitempty"`
	Reason  string          `json:"reason,omitempty"`
}

// runStatus prints the status line and returns its exit code. It reads the tree
// and git and writes nothing.
func runStatus(ctx context.Context, env cli.Env, a baselineArgs) int {
	line, code := status(ctx, a)
	out, err := json.Marshal(line)
	if err != nil {
		env.Stderr.Printf("gotops baseline: %v\n", err)
		return baselineEnv
	}
	env.Stdout.Printf("%s\n", out)
	return code
}

func status(ctx context.Context, a baselineArgs) (statusLine, int) {
	dir, err := os.Getwd()
	if err != nil {
		return cannotTell(statusLine{}, err.Error())
	}
	cfg, _, rel, err := loadBaselineConfig(dir)
	if err != nil {
		return cannotTell(statusLine{}, err.Error())
	}
	line := statusLine{Path: rel, BaseRef: cfg.BaseRef(a.base)}
	if state := treeState(cfg.Enforce, dir, rel); state != "" {
		line.State, line.Tree = state, state == "present"
		return line, statusNotDue
	}
	return dueOrDeleted(ctx, dir, line)
}

// treeState is what the tree alone says. Only enforce: ratchet reads a ledger, so
// under advisory (none is written) and hard (the floor and the cap are the
// thresholds) none is ever due: not-ratchet. Under ratchet a ledger in the tree is
// present, and one the head tracks that the tree lost is deleted (`baseline --init`
// refuses it too). "" means the tree has none and head had none.
func treeState(enforce, dir, rel string) string {
	if enforce != "ratchet" {
		return "not-ratchet"
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err == nil {
		return "present"
	}
	if trackedAtHead(dir, rel) {
		return "deleted"
	}
	return ""
}

// dueOrDeleted reads the diff base as the ratchet does. A base that has the ledger
// the tree lacks is a deletion, which is not a reset; one that has none makes the
// first ledger due.
func dueOrDeleted(ctx context.Context, dir string, line statusLine) (statusLine, int) {
	b := ratchet.ReadBase(dir, line.BaseRef, line.Path)
	if b.Err != nil {
		return cannotTell(line, b.Err.Error())
	}
	has := b.Ledger != nil
	line.Base = &has
	if has {
		line.State = "deleted"
		return line, statusNotDue
	}
	build, err := runBuild(ctx, dir)
	if err != nil {
		return cannotTell(line, "read the build: "+err.Error())
	}
	line.State, line.Build = "first-ledger-due", &build
	return line, statusDue
}

func cannotTell(line statusLine, reason string) (statusLine, int) {
	line.State, line.Reason = "error", reason
	return line, statusUnknown
}
