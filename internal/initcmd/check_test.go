package initcmd

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Hedronite/go-tops/internal/config"
)

// What `gotops check` accepts for a repo init just wrote (§12.1, §6.7). init
// writes enforce: ratchet and no baseline (the first-adoption walk-through,
// step 1), and check rejects a missing baseline until `gotops baseline --init`
// writes one (M2, SPEC-m1 B6). The first test therefore commits a minimal empty
// ledger, a stand-in for baseline --init that init never writes itself.

const emptyLedger = "{\"build\":{\"goos\":\"linux\",\"goarch\":\"amd64\",\"tags\":[],\"cgo_enabled\":true},\"ledger\":{},\"judged_laws\":0}\n"

func fixedNow() time.Time { return time.Date(2026, 11, 17, 15, 4, 5, 0, time.UTC) }

func checkDir(t *testing.T, dir string) config.Result {
	t.Helper()
	res, err := config.Check(dir, config.Options{Now: fixedNow})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	return res
}

// gitIn runs git in dir under a fixed identity that belongs to no one.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=init-test", "GIT_AUTHOR_EMAIL=init-test@example.com",
		"GIT_COMMITTER_NAME=init-test", "GIT_COMMITTER_EMAIL=init-test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// For every launch class, init then check passes once the baseline exists, is
// tracked and is clean against HEAD.
func TestInitThenCheckPasses(t *testing.T) {
	for _, class := range launchClasses {
		t.Run(class, func(t *testing.T) {
			dir := newModule(t, "github.com/acme/demo", "1.26")
			if code, _, errs := run(opts(dir, class)); code != ExitOK {
				t.Fatal(errs)
			}
			write(t, dir, ".go-tops/baseline.json", emptyLedger)
			gitIn(t, dir, "init", "-q")
			gitIn(t, dir, "add", "-A")
			gitIn(t, dir, "commit", "-q", "-m", "adopt")
			if res := checkDir(t, dir); len(res.Issues) != 0 {
				t.Fatalf("check issues: %v", res.Issues)
			}
		})
	}
}

// Right after a bare init, under ratchet, check finds exactly the missing
// baseline: init writes none, and a missing baseline is a finding (§12.1).
func TestBareInitThenCheckFailsOnTheMissingBaselineOnly(t *testing.T) {
	for _, class := range launchClasses {
		t.Run(class, func(t *testing.T) {
			dir := newModule(t, "github.com/acme/demo", "1.26")
			if code, _, errs := run(opts(dir, class)); code != ExitOK {
				t.Fatal(errs)
			}
			issues := checkDir(t, dir).Issues
			if len(issues) != 1 || issues[0].Rule != "baseline" || !strings.Contains(issues[0].Msg, "does not exist") {
				t.Fatalf("want exactly the missing-baseline finding, got %v", issues)
			}
		})
	}
}

// init's own output passes check when the repo is hard-enforced: no baseline is
// needed, so every other file init wrote is accepted as it stands.
func TestEveryOtherFileInitWritesPassesCheck(t *testing.T) {
	for _, class := range launchClasses {
		t.Run(class, func(t *testing.T) {
			dir := newModule(t, "github.com/acme/demo", "1.26")
			if code, _, errs := run(opts(dir, class)); code != ExitOK {
				t.Fatal(errs)
			}
			write(t, dir, "go-tops.yaml", strings.Replace(read(t, dir, "go-tops.yaml"), "enforce: ratchet", "enforce: hard", 1))
			if res := checkDir(t, dir); len(res.Issues) != 0 {
				t.Fatalf("check issues: %v", res.Issues)
			}
		})
	}
}
