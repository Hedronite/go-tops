package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/config"
)

// The oracle is GO_TOPS.md §12.1 (`gate`), §12.2 (the exit codes) and §6.7
// (advisory): the command loads the configuration through `check` and the
// defaults of the class, writes the report, and exits as the table says.

// gotopsEnv runs the test binary as gotops with extra environment.
func gotopsEnv(t *testing.T, dir string, env []string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], args...)
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), asMain+"=1", "CI="), env...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		code = exit.ExitCode()
	default:
		t.Fatalf("run gotops %v: %v", args, err)
	}
	return code, out.String(), errb.String()
}

// gitModule is module(t, config) in a git repository with one commit.
func gitModule(t *testing.T, cfg string) string {
	t.Helper()
	dir := module(t, cfg)
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A"},
		{"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "init"},
	} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

// fuzz is a class layer that stays a stub until M6, at tier 2: an enabled
// layer with no command, so a run that includes it fails with not-run.
var stubRun = []string{"gate", "--max-tier", "2", "--only", "fuzz"}

func TestGateRunsOnTheEffectiveConfigAndWritesAReport(t *testing.T) {
	dir := gitModule(t, goodConfig(t))
	code, stdout, stderr := gotopsEnv(t, dir, nil, stubRun...)
	if code != 1 {
		t.Fatalf("exit %d, want 1 (an enabled layer with no command is not-run)\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "not-run") {
		t.Errorf("stdout %q", stdout)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".go-tops", "report.json"))
	if err != nil {
		t.Fatalf("no report: %v", err)
	}
	for _, want := range []string{`"class": "binary-cli"`, `"enforce": "hard"`, `"max_tier": 2`, `"id": "fuzz"`, `"outcome": "not-run"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("the report lacks %s", want)
		}
	}
}

// §12.2 code 2: a configuration that check rejects exits 2, prints the issue
// with its pointer, and writes no report.
func TestGateExits2OnABadConfig(t *testing.T) {
	dir := gitModule(t, goodConfig(t)+"gates:\n  crap:\n    cap: 12\n")
	code, _, stderr := gotopsEnv(t, dir, nil, stubRun...)
	if code != 2 || !strings.Contains(stderr, "/gates/crap/cap") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, ".go-tops", "report.json")); err == nil {
		t.Fatal("a refused run wrote a report")
	}
}

func TestGateExits2WithNoConfigOrNoLock(t *testing.T) {
	dir := gitModule(t, "")
	if code, _, stderr := gotopsEnv(t, dir, nil, stubRun...); code != 2 || !strings.Contains(stderr, "go-tops.yaml") {
		t.Fatalf("no config: exit %d, stderr %q", code, stderr)
	}
	dir = gitModule(t, goodConfig(t))
	if err := os.Remove(filepath.Join(dir, "go-tops.lock")); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := gotopsEnv(t, dir, nil, stubRun...); code != 2 || !strings.Contains(stderr, "go-tops.lock") {
		t.Fatalf("no lock: exit %d, stderr %q", code, stderr)
	}
}

// §12.2 code 3: a directory git cannot read.
func TestGateExits3WhenTheRepositoryCannotBeRead(t *testing.T) {
	dir := module(t, goodConfig(t)) // not a git repository
	if code, _, stderr := gotopsEnv(t, dir, nil, stubRun...); code != 3 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
}

// §12.2 code 4: refused under CI=true.
func TestGateExits4InCIForDryAndPartialRuns(t *testing.T) {
	dir := gitModule(t, goodConfig(t))
	for _, args := range [][]string{{"gate", "--dry-run"}, {"gate", "--only", "tests"}, {"gate", "--max-tier", "0"}} {
		if code, _, stderr := gotopsEnv(t, dir, []string{"CI=true"}, args...); code != 4 {
			t.Errorf("%v: exit %d, stderr %q", args, code, stderr)
		}
	}
	// the same runs are allowed off CI
	if code, stdout, _ := gotopsEnv(t, dir, nil, "gate", "--dry-run"); code != 0 || !strings.Contains(stdout, "Nothing ran") {
		t.Errorf("local dry run: exit %d\n%s", code, stdout)
	}
}

// B1: under advisory a started run exits 0 and says so.
func TestGateAdvisoryExitsZeroAndSaysSo(t *testing.T) {
	cfg := strings.Replace(goodConfig(t), "enforce: hard", "enforce: advisory\nadvisory:\n  reason: the kit gates itself in advisory mode until M5\n  owner: platform\n  expires: \"2099-12-31\"", 1)
	dir := gitModule(t, cfg)
	code, stdout, stderr := gotopsEnv(t, dir, nil, stubRun...)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q\n%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "NOT ENFORCED (advisory, expires 2099-12-31): outcome FAIL") {
		t.Errorf("stdout %q", stdout)
	}
}

// B10: a config that states nothing about the gates still runs with the cap
// and floor of its class; toGateConfig reads the effective configuration.
func TestToGateConfigByClass(t *testing.T) {
	want := map[string]struct {
		cap, floor float64
		required   int
		benchOn    bool
	}{
		"binary-cli":     {10, 70, 16, false},
		"binary-service": {10, 75, 16, true},
		"library-core":   {8, 85, 16, true},
		"parser-codec":   {8, 90, 0, true},
	}
	for class, w := range want {
		eff, err := config.Effective(&config.Config{Class: class, Module: "example.com/m", Enforce: "hard"})
		if err != nil {
			t.Fatal(err)
		}
		c, err := toGateConfig(eff)
		if err != nil {
			t.Fatalf("%s: %v", class, err)
		}
		if c.Crap.Cap != w.cap || c.Coverage.StatementsFloor != w.floor || len(c.RequiredLaws) != w.required {
			t.Errorf("%s: cap %v floor %v required %d, want %v %v %d", class, c.Crap.Cap, c.Coverage.StatementsFloor, len(c.RequiredLaws), w.cap, w.floor, w.required)
		}
		if c.Layers["bench"].Enabled != w.benchOn {
			t.Errorf("%s: bench enabled %v, want %v", class, c.Layers["bench"].Enabled, w.benchOn)
		}
		if !c.Layers["crap"].Enabled || !c.Layers["mutation"].Enabled || !c.Coverage.ExcludeGenerated || c.Hygiene.LintConfig == "" || len(c.Hygiene.Licenses) == 0 {
			t.Errorf("%s: %+v", class, c)
		}
	}
}

func TestToGateConfigRefusesAConfigThatIsNotEffective(t *testing.T) {
	if _, err := toGateConfig(&config.Config{Class: "binary-cli"}); err == nil {
		t.Fatal("a config with no cap was accepted")
	}
}

func TestToGateConfigCarriesSwitchesAdvisoryAndExemptions(t *testing.T) {
	off := false
	cfg := &config.Config{
		Class: "binary-cli", Module: "example.com/m", Enforce: "advisory",
		Advisory:   &config.Advisory{Reason: "bootstrap until M5", Owner: "platform", Expires: "2099-12-31"},
		Exemptions: []config.Exemption{{Path: "a.go", Symbol: "F", Layer: "crap", Shape: "table-switch", Reason: "dispatch table, one call per arm", Owner: "platform", Expires: "2099-12-31"}},
	}
	cfg.Gates.Crap.Enabled, cfg.Gates.Crap.Reason, cfg.Gates.Crap.Expires = &off, "lands at M2", "2099-03-01"
	cfg.Report.Path = "out/r.json"
	eff, err := config.Effective(cfg)
	if err != nil {
		t.Fatal(err)
	}
	c, err := toGateConfig(eff)
	if err != nil {
		t.Fatal(err)
	}
	if lc := c.Layers["crap"]; lc.Enabled || lc.Reason != "lands at M2" || lc.Expires != "2099-03-01" {
		t.Errorf("crap %+v", lc)
	}
	if c.AdvisoryExpires != "2099-12-31" || c.ReportPath != "out/r.json" || c.Base != "origin/main" {
		t.Errorf("%+v", c)
	}
	if len(c.Exemptions) != 1 || c.Exemptions[0].Symbol != "F" || c.Exemptions[0].Shape != "table-switch" {
		t.Errorf("exemptions %+v", c.Exemptions)
	}
}
