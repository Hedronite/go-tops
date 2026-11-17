package gate

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cmdDeps(t *testing.T, ls []Layer, c Config) CommandDeps {
	t.Helper()
	d := deps(ls)
	return CommandDeps{
		Dir: t.TempDir(), Layers: ls, Exec: d.Exec, Tools: d.Tools, Now: d.Now, ToolVersion: "0.0.0-dev",
		LoadConfig: func(string) (Config, error) { return c, nil },
	}
}

func run(args []string, d CommandDeps) (int, string, string) {
	var so, se bytes.Buffer
	code := Main(args, d, &so, &se)
	return code, so.String(), se.String()
}

func reportFile(d CommandDeps) string { return filepath.Join(d.Dir, ".go-tops", "report.json") }

func TestMainWritesAValidReportAndExits(t *testing.T) {
	c := cfg()
	c.RequiredLaws = nil
	d := cmdDeps(t, gateLayers(nil, nil), c)
	code, out, _ := run(nil, d)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	b, err := os.ReadFile(reportFile(d))
	if err != nil {
		t.Fatalf("no report written: %v", err)
	}
	if err := validateDoc(b); err != nil {
		t.Fatalf("the written report is invalid: %v", err)
	}
	if !strings.Contains(out, "PASS") {
		t.Errorf("text:\n%s", out)
	}
}

func TestMainJSONPrintsTheReport(t *testing.T) {
	c := cfg()
	c.RequiredLaws = nil
	d := cmdDeps(t, gateLayers(nil, nil), c)
	code, out, _ := run([]string{"--json"}, d)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if err := validateDoc([]byte(out)); err != nil {
		t.Fatalf("--json output is not a valid report: %v", err)
	}
}

// A failing run still writes its report (§6.6).
func TestMainFailingRunWritesReport(t *testing.T) {
	d := cmdDeps(t, gateLayers(nil, map[string]Outcome{"tests": Fail}), cfg())
	code, _, _ := run(nil, d)
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	if _, err := os.Stat(reportFile(d)); err != nil {
		t.Fatalf("a failing run wrote no report: %v", err)
	}
}

// §6.6: a run refused before any layer runs (2, 3, 4) writes no report.
func TestMainRefusedRunsWriteNoReport(t *testing.T) {
	d := cmdDeps(t, gateLayers(nil, nil), cfg())
	d.LoadConfig = func(string) (Config, error) {
		return Config{}, errors.New("gates.crap.cap: 12 is above the class cap 10")
	}
	code, _, errText := run(nil, d)
	if code != 2 || !strings.Contains(errText, "gates.crap.cap") {
		t.Fatalf("config error: exit %d, stderr %q", code, errText)
	}
	assertNoReport(t, d)

	d = cmdDeps(t, gateLayers(nil, nil), cfg())
	d.CI = true
	if code, _, _ = run([]string{"--dry-run"}, d); code != 4 {
		t.Fatalf("CI dry run: exit %d", code)
	}
	assertNoReport(t, d)
	if code, _, _ = run([]string{"--only", "tests"}, d); code != 4 {
		t.Fatalf("CI --only: exit %d", code)
	}
	assertNoReport(t, d)

	d = cmdDeps(t, gateLayers(nil, nil), cfg())
	d.Exec.(*fakeExec).gitFail = true
	if code, _, _ = run(nil, d); code != 3 {
		t.Fatalf("unreadable repository: exit %d", code)
	}
	assertNoReport(t, d)
}

func assertNoReport(t *testing.T, d CommandDeps) {
	t.Helper()
	if _, err := os.Stat(reportFile(d)); err == nil {
		t.Fatal("a refused run wrote a report")
	}
}

func TestMainDryRunPrintsPlanAndWritesNothing(t *testing.T) {
	var ran []string
	d := cmdDeps(t, gateLayers(&ran, nil), cfg())
	code, out, _ := run([]string{"--dry-run"}, d)
	if code != 0 || len(ran) != 0 || !strings.Contains(out, "would run") {
		t.Fatalf("exit %d ran %v\n%s", code, ran, out)
	}
	assertNoReport(t, d)
}

func TestMainUsageErrorsExitTwo(t *testing.T) {
	d := cmdDeps(t, gateLayers(nil, nil), cfg())
	for _, args := range [][]string{{"--nope"}, {"extra"}, {"--max-tier", "9"}, {"--max-tier", "-1"}, {"--max-tier", "x"}, {"--only", "nolayer"}} {
		if code, _, _ := run(args, d); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
	assertNoReport(t, d)
}

func TestMainHelp(t *testing.T) {
	code, out, _ := run([]string{"--help"}, cmdDeps(t, nil, cfg()))
	if code != 0 || !strings.Contains(out, "--max-tier") || !strings.Contains(out, "CI=true") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestMainReportPathFromConfig(t *testing.T) {
	c := cfg()
	c.RequiredLaws = nil
	c.ReportPath = "out/r.json"
	d := cmdDeps(t, gateLayers(nil, nil), c)
	if code, _, _ := run(nil, d); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(d.Dir, "out", "r.json")); err != nil {
		t.Fatal(err)
	}
}

// Advisory: the printed run and the exit code.
func TestMainAdvisory(t *testing.T) {
	c := cfg()
	c.Enforce, c.AdvisoryExpires = "advisory", "2027-01-31"
	d := cmdDeps(t, gateLayers(nil, map[string]Outcome{"tests": Fail}), c)
	code, out, _ := run(nil, d)
	if code != 0 || !strings.Contains(out, "NOT ENFORCED (advisory, expires 2027-01-31): outcome FAIL") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

// A verdict nobody can read is not reported as one: output that cannot be
// written is an environment error (3).
func TestMainUnwritableOutputIsAnEnvironmentError(t *testing.T) {
	c := cfg()
	c.RequiredLaws = nil
	d := cmdDeps(t, gateLayers(nil, nil), c)
	var se bytes.Buffer
	if code := Main(nil, d, brokenWriter{}, &se); code != 3 {
		t.Fatalf("exit %d, want 3", code)
	}
}
