package tests

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/gate"
)

// The plants run the real go command on small modules in a temp directory:
// each breaks one rule of gates 2 and 3, and the test asserts the layer fails
// for that reason. The clean module passes first, so a plant proves the rule
// and not a broken fixture.

type files map[string]string

func writeModule(t *testing.T, over files) string {
	t.Helper()
	dir := t.TempDir()
	base := files{
		"go.mod":      "module example.com/p\n\ngo 1.26\n",
		"a/a.go":      "package a\n\n// Add adds.\nfunc Add(x, y int) int { return x + y }\n",
		"a/a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"no\")\n\t}\n}\n",
	}
	for k, v := range over {
		if v == "-" {
			delete(base, k)
			continue
		}
		base[k] = v
	}
	for name, body := range base {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func realEnv(dir string, floor float64) *gate.Env {
	return &gate.Env{Dir: dir, Exec: gate.OS{}, Config: gate.Config{Module: "example.com/p", Coverage: gate.CoverageConfig{StatementsFloor: floor}}}
}

// both layers in order, the way the runner does
func runBoth(t *testing.T, dir string, floor float64) (gate.Result, gate.Result) {
	t.Helper()
	env := realEnv(dir, floor)
	ctx := context.Background()
	if err := (Tests{}).Preflight(ctx, env); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	tr := Tests{}.Run(ctx, env)
	cr := Coverage{}.Run(ctx, env)
	return tr, cr
}

func TestPlantCleanModulePasses(t *testing.T) {
	tr, cr := runBoth(t, writeModule(t, nil), 90)
	if tr.Outcome != gate.Pass || cr.Outcome != gate.Pass {
		t.Fatalf("tests %s (%s), coverage %s (%s)", tr.Outcome, tr.Reason, cr.Outcome, cr.Reason)
	}
	if tr.Details["tests"] != 1 || tr.Details["packages"] != 1 || tr.Details["race"] != true {
		t.Errorf("details %v", tr.Details)
	}
	if seed, ok := tr.Details["shuffle_seed"].(int64); !ok || seed == 0 {
		t.Errorf("no shuffle seed recorded: %v", tr.Details)
	}
	if cr.Details["statements_pct"] != 100.0 {
		t.Errorf("coverage %v", cr.Details)
	}
}

// 1. a failing test
func TestPlantFailingTest(t *testing.T) {
	tr, _ := runBoth(t, writeModule(t, files{"a/a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 4 {\n\t\tt.Fatal(\"want 4\")\n\t}\n}\n"}), 0)
	if tr.Outcome != gate.Fail || !strings.Contains(tr.Reason, "TestAdd") {
		t.Fatalf("%s: %s", tr.Outcome, tr.Reason)
	}
}

// 2. a data race: the test reports pass logic-wise but the detector fires
func TestPlantDataRace(t *testing.T) {
	racy := files{
		"a/a.go":      "package a\n\nvar N int\n\n// Inc races.\nfunc Inc() { N++ }\n",
		"a/a_test.go": "package a\n\nimport (\n\t\"sync\"\n\t\"testing\"\n)\n\nfunc TestRace(t *testing.T) {\n\tvar wg sync.WaitGroup\n\tfor i := 0; i < 2; i++ {\n\t\twg.Add(1)\n\t\tgo func() { defer wg.Done(); Inc() }()\n\t}\n\twg.Wait()\n}\n",
	}
	tr, _ := runBoth(t, writeModule(t, racy), 0)
	if tr.Outcome != gate.Fail || !strings.Contains(tr.Reason, "TestRace") {
		t.Fatalf("%s: %s", tr.Outcome, tr.Reason)
	}
}

// A passing test that prints the race banner has not raced: the run passes,
// because the gate reads the result of the run and not the text a test logs.
func TestPlantRaceBannerInALogIsNotARace(t *testing.T) {
	logs := files{"a/a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tt.Log(\"WARNING: DATA RACE\")\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"no\")\n\t}\n}\n"}
	tr, _ := runBoth(t, writeModule(t, logs), 0)
	if tr.Outcome != gate.Pass {
		t.Fatalf("%s: %s", tr.Outcome, tr.Reason)
	}
}

// B15: an inherited GOFLAGS must not hide a test from the gate. Each flag below
// would skip, shorten or blind the run; the broken test still fails it.
func TestPlantInheritedGOFLAGSDoNotHideABrokenTest(t *testing.T) {
	broken := files{"a/a_test.go": "package a\n\nimport (\n\t\"testing\"\n)\n\nfunc TestOK(t *testing.T) {}\n\nfunc TestBroken(t *testing.T) {\n\tif testing.Short() {\n\t\tt.Skip(\"short\")\n\t}\n\tt.Fatal(\"broken\")\n}\n"}
	vetBroken := files{"a/a_test.go": "package a\n\nimport (\n\t\"fmt\"\n\t\"testing\"\n)\n\nfunc TestOK(t *testing.T) {}\n\nfunc TestVet(t *testing.T) {\n\tfmt.Printf(\"%d\\n\", \"x\")\n}\n"}
	for _, c := range []struct {
		flags string
		mod   files
	}{
		{"-skip=TestBroken", broken}, {"-short", broken}, {"-run=TestOK", broken}, {"-vet=off", vetBroken},
	} {
		t.Run(c.flags, func(t *testing.T) {
			t.Setenv("GOFLAGS", c.flags)
			tr, _ := runBoth(t, writeModule(t, c.mod), 0)
			if tr.Outcome != gate.Fail {
				t.Fatalf("GOFLAGS=%s: %s: %s; the broken test was hidden from the gate", c.flags, tr.Outcome, tr.Reason)
			}
		})
	}
}

// The same through `go env -w`, which an empty GOFLAGS would not override: the
// settings file names GOFLAGS=-skip=TestBroken.
func TestPlantGOFLAGSFromTheGoEnvFileDoNotHideABrokenTest(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "goenv")
	if err := os.WriteFile(envFile, []byte("GOFLAGS=-skip=TestBroken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOENV", envFile)
	t.Setenv("GOFLAGS", "")
	broken := files{"a/a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestOK(t *testing.T) {}\n\nfunc TestBroken(t *testing.T) { t.Fatal(\"broken\") }\n"}
	tr, _ := runBoth(t, writeModule(t, broken), 0)
	if tr.Outcome != gate.Fail {
		t.Fatalf("%s: %s; the go env file hid the broken test", tr.Outcome, tr.Reason)
	}
}

// A parent test whose only subtest is skipped ran nothing.
func TestPlantParentWithOnlyASkippedSubtest(t *testing.T) {
	skipped := files{"a/a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestParent(t *testing.T) {\n\tt.Run(\"sub\", func(t *testing.T) { t.Skip(\"later\") })\n}\n"}
	tr, _ := runBoth(t, writeModule(t, skipped), 0)
	if tr.Outcome != gate.Fail || !strings.Contains(tr.Reason, "no test ran") {
		t.Fatalf("%s: %s", tr.Outcome, tr.Reason)
	}
}

// The tests figure counts leaf tests: one test with two passing subtests is two.
func TestPlantTestsFigureCountsLeaves(t *testing.T) {
	subs := files{"a/a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestParent(t *testing.T) {\n\tt.Run(\"a\", func(t *testing.T) {})\n\tt.Run(\"b\", func(t *testing.T) {})\n}\n\nfunc TestOther(t *testing.T) {}\n"}
	tr, _ := runBoth(t, writeModule(t, subs), 0)
	if tr.Outcome != gate.Pass || tr.Details["tests"] != 3 {
		t.Fatalf("%s %v: want 3 leaves (Parent/a, Parent/b, Other)", tr.Outcome, tr.Details)
	}
}

// 3. a zero-test package set: the module has code and no test file
func TestPlantZeroTests(t *testing.T) {
	tr, cr := runBoth(t, writeModule(t, files{"a/a_test.go": "-"}), 70)
	if tr.Outcome != gate.Fail || !strings.Contains(tr.Reason, "no test ran") {
		t.Fatalf("tests %s: %s", tr.Outcome, tr.Reason)
	}
	// the untested package is at 0% against a floor of 70: coverage fails too
	if cr.Outcome != gate.Fail {
		t.Fatalf("coverage %s: %s", cr.Outcome, cr.Reason)
	}
}

// 4. an empty profile: the run was green but its profile has no blocks, or none
func TestPlantEmptyProfile(t *testing.T) {
	dir := writeModule(t, nil)
	env := realEnv(dir, 0)
	ctx := context.Background()
	if tr := (Tests{}).Run(ctx, env); tr.Outcome != gate.Pass {
		t.Fatalf("tests %s: %s", tr.Outcome, tr.Reason)
	}
	prof := filepath.Join(dir, ".go-tops", "cover.out")
	for name, body := range map[string]string{"zero bytes": "", "mode only": "mode: atomic\n"} {
		if err := os.WriteFile(prof, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if cr := (Coverage{}).Run(ctx, env); cr.Outcome != gate.Fail {
			t.Errorf("%s: coverage %s", name, cr.Outcome)
		}
	}
	if err := os.Remove(prof); err != nil {
		t.Fatal(err)
	}
	if cr := (Coverage{}).Run(ctx, env); cr.Outcome != gate.Fail {
		t.Errorf("missing: coverage %s", cr.Outcome)
	}
}

// 5. a package under the floor, from a real run
func TestPlantPackageUnderTheFloor(t *testing.T) {
	under := files{"a/a.go": "package a\n\n// Add adds.\nfunc Add(x, y int) int { return x + y }\n\n// Never is not tested.\nfunc Never() int {\n\tx := 1\n\tx++\n\treturn x\n}\n"}
	tr, cr := runBoth(t, writeModule(t, under), 90)
	if tr.Outcome != gate.Pass {
		t.Fatalf("tests %s: %s", tr.Outcome, tr.Reason)
	}
	if cr.Outcome != gate.Fail || !strings.Contains(cr.Reason, "example.com/p/a") {
		t.Fatalf("coverage %s: %s", cr.Outcome, cr.Reason)
	}
	// 1 of 4 statements: statements, not lines (the file has 9 code lines)
	if pct, ok := cr.Details["statements_pct"].(float64); !ok || pct != 25.0 {
		t.Errorf("statements_pct %v, want 25", cr.Details["statements_pct"])
	}
}

// 6. no C compiler: the run cannot start and exits 3 (preflight)
func TestPlantNoCCompiler(t *testing.T) {
	t.Setenv("CC", "/nonexistent/cc")
	err := Tests{}.Preflight(context.Background(), realEnv(writeModule(t, nil), 0))
	var ee *gate.EnvError
	if !errors.As(err, &ee) {
		t.Fatalf("err = %v, want a *gate.EnvError", err)
	}
}
