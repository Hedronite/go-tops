package tests

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/gate"
)

func profileEnv(t *testing.T, profile string, floor float64) *gate.Env {
	t.Helper()
	dir := t.TempDir()
	if profile != "-" {
		p := filepath.Join(dir, ".go-tops", "cover.out")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(profile), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &gate.Env{Dir: dir, Config: gate.Config{Module: "m", Coverage: gate.CoverageConfig{StatementsFloor: floor}}}
}

// 70 of 100 statements, over 4 blocks on 40 lines: the figure is statements.
const seventy = "mode: atomic\n" +
	"m/a/a.go:1.1,40.2 50 1\n" +
	"m/a/a.go:41.1,41.9 20 1\n" +
	"m/a/a.go:42.1,60.2 30 0\n"

func TestCoveragePassesAtTheFloor(t *testing.T) {
	r := Coverage{}.Run(context.Background(), profileEnv(t, seventy, 70))
	if r.Outcome != gate.Pass {
		t.Fatalf("%s: %s", r.Outcome, r.Reason)
	}
	if r.Details["statements_pct"] != 70.0 || r.Details["floor"] != 70.0 || r.Details["profile_bytes"] != len(seventy) {
		t.Fatalf("details %v", r.Details)
	}
}

// G5b: a package under the floor fails, and the message names it; the
// percentage is of statements, so 69.99 of 100 fails a floor of 70.
func TestCoverageUnderTheFloorFails(t *testing.T) {
	r := Coverage{}.Run(context.Background(), profileEnv(t, seventy, 70.5))
	if r.Outcome != gate.Fail || !strings.Contains(r.Reason, "m/a") || !strings.Contains(r.Reason, "70.0%") {
		t.Fatalf("%s: %s", r.Outcome, r.Reason)
	}
	if r.Details["statements_pct"] != 70.0 {
		t.Errorf("a failed gate still reports its figures: %v", r.Details)
	}
}

// The floor holds for every package: one weak package fails a run whose total
// is above the floor.
func TestOneWeakPackageFailsTheRun(t *testing.T) {
	prof := "mode: atomic\n" +
		"m/big/b.go:1.1,2.2 90 1\n" +
		"m/weak/w.go:1.1,2.2 1 1\nm/weak/w.go:3.1,4.2 9 0\n"
	r := Coverage{}.Run(context.Background(), profileEnv(t, prof, 70))
	if r.Outcome != gate.Fail || !strings.Contains(r.Reason, "m/weak") || strings.Contains(r.Reason, "m/big") {
		t.Fatalf("%s: %s", r.Outcome, r.Reason)
	}
	if r.Details["statements_pct"] != 91.0 {
		t.Errorf("total %v", r.Details["statements_pct"])
	}
}

// G5b: an empty or missing profile fails (§3.4 rule 5, §6.1).
func TestMissingOrEmptyProfileFails(t *testing.T) {
	cases := map[string]string{
		"missing":       "-",
		"zero bytes":    "",
		"blank":         "\n\n",
		"mode only":     "mode: atomic\n",
		"not a profile": "hello\n",
		"bad block":     "mode: atomic\nm/a/a.go:1.1 2 1\n",
	}
	for name, prof := range cases {
		r := Coverage{}.Run(context.Background(), profileEnv(t, prof, 0))
		if r.Outcome != gate.Fail {
			t.Errorf("%s: %s", name, r.Outcome)
		}
		if msg, ok := r.Details["error"].(string); !ok || msg == "" {
			t.Errorf("%s: details %v, want {error}", name, r.Details)
		}
	}
}

// §6.1: generated files are left out when configured, and a profile with
// nothing else holds no block of a production file.
func TestGeneratedFilesAreExcludedWhenConfigured(t *testing.T) {
	prof := "mode: atomic\nm/a/real.go:1.1,2.2 10 1\nm/a/x.gen.go:1.1,2.2 90 0\n"
	env := profileEnv(t, prof, 90)
	if r := (Coverage{}).Run(context.Background(), env); r.Outcome != gate.Fail {
		t.Fatalf("without exclusion the generated file drags coverage to 10%%: %s", r.Outcome)
	}
	env.Config.Coverage.ExcludeGenerated = true
	if r := (Coverage{}).Run(context.Background(), env); r.Outcome != gate.Pass {
		t.Fatalf("with exclusion: %s: %s", r.Outcome, r.Reason)
	}
	only := profileEnv(t, "mode: atomic\nm/a/x.gen.go:1.1,2.2 90 0\n", 0)
	only.Config.Coverage.ExcludeGenerated = true
	if r := (Coverage{}).Run(context.Background(), only); r.Outcome != gate.Fail {
		t.Fatalf("only generated blocks: %s", r.Outcome)
	}
}

// The coverage layer never runs go test: it reads the file of the one run.
func TestCoverageRunsNothing(t *testing.T) {
	env := profileEnv(t, seventy, 10)
	env.Exec = &scripted{t: t, on: map[string]func(gate.Cmd) (gate.Output, error){}}
	if r := (Coverage{}).Run(context.Background(), env); r.Outcome != gate.Pass {
		t.Fatalf("%s: %s", r.Outcome, r.Reason)
	}
}

// B15: integration.enabled true must not read as a pass on unit coverage alone.
func TestIntegrationEnabledFailsBecauseTheMergeIsNotImplemented(t *testing.T) {
	env := profileEnv(t, seventy, 10)
	env.Config.Coverage.IntegrationEnabled = true
	r := Coverage{}.Run(context.Background(), env)
	if r.Outcome != gate.Fail || !strings.Contains(r.Reason, "integration merge not implemented in this release") {
		t.Fatalf("%s: %s", r.Outcome, r.Reason)
	}
	if msg, ok := r.Details["error"].(string); !ok || msg == "" {
		t.Errorf("details %v, want {error}", r.Details)
	}
	env.Config.Coverage.IntegrationEnabled = false
	if r := (Coverage{}).Run(context.Background(), env); r.Outcome != gate.Pass {
		t.Fatalf("with integration off the same profile passes: %s", r.Outcome)
	}
}
