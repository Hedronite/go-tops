package tests

import (
	"encoding/json"
	"strings"
	"testing"
)

// The oracle is the stream `go test -json` prints (test2json and the build
// events of Go 1.24 and later) and GO_TOPS.md §3.1 gate 2.

type ev struct {
	Action, Package, Test, Output, ImportPath, FailedBuild string
}

// stream renders events as the JSON lines go test prints.
func stream(evs ...ev) []byte {
	var b strings.Builder
	for _, e := range evs {
		m := map[string]string{"Action": e.Action}
		for k, v := range map[string]string{"Package": e.Package, "Test": e.Test, "Output": e.Output, "ImportPath": e.ImportPath, "FailedBuild": e.FailedBuild} {
			if v != "" {
				m[k] = v
			}
		}
		line, err := json.Marshal(m)
		if err != nil {
			panic(err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

func seed(pkg, n string) ev {
	return ev{Action: "output", Package: pkg, Output: "-test.shuffle " + n + "\n"}
}

func pass(pkg, test string) []ev {
	return []ev{{Action: "run", Package: pkg, Test: test}, {Action: "pass", Package: pkg, Test: test}}
}

func mustParse(t *testing.T, s []byte) *Result {
	t.Helper()
	r, err := Parse(s)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return r
}

func cat(parts ...[]ev) []ev {
	var out []ev
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func TestParseCountsPackagesTestsAndSkips(t *testing.T) {
	r := mustParse(t, stream(cat(
		[]ev{{Action: "start", Package: "m/a"}, seed("m/a", "111")},
		pass("m/a", "TestAdd"), pass("m/a", "TestAdd/sub"),
		[]ev{{Action: "run", Package: "m/a", Test: "TestSkipped"}, {Action: "skip", Package: "m/a", Test: "TestSkipped"}},
		[]ev{{Action: "pass", Package: "m/a"}},
		[]ev{{Action: "output", Package: "m/b", Output: "?   \tm/b\t[no test files]\n"}, {Action: "skip", Package: "m/b"}},
	)...))
	if r.Packages != 1 || r.Tests != 3 || r.Skipped != 1 || r.Ran() != 2 {
		t.Fatalf("%+v: want 1 package (m/b has no tests), 3 results of which 1 is a skip", r)
	}
	if !r.HasSeed || r.Seed != 111 {
		t.Errorf("seed %d (%v)", r.Seed, r.HasSeed)
	}
	if len(r.FailedTests) != 0 || len(r.Races) != 0 || len(r.BuildErrors) != 0 {
		t.Errorf("%+v", r)
	}
}

func TestParseNamesFailedTests(t *testing.T) {
	r := mustParse(t, stream(cat(
		[]ev{seed("m/c", "5")},
		[]ev{{Action: "run", Package: "m/c", Test: "TestC"}, {Action: "fail", Package: "m/c", Test: "TestC"}},
		[]ev{{Action: "fail", Package: "m/c"}},
	)...))
	if len(r.FailedTests) != 1 || r.FailedTests[0] != "m/c.TestC" {
		t.Fatalf("failed tests %v", r.FailedTests)
	}
	if len(r.FailedPackages) != 0 {
		t.Errorf("a package that failed through its test is not listed apart: %v", r.FailedPackages)
	}
}

// A package that fails with no failing test of its own is named apart:
// a panic in TestMain, a race reported after the tests.
func TestParseNamesPackagesThatFailOutsideATest(t *testing.T) {
	r := mustParse(t, stream(cat(
		pass("m/a", "TestA"),
		[]ev{{Action: "fail", Package: "m/a"}},
	)...))
	if len(r.FailedPackages) != 1 || r.FailedPackages[0] != "m/a" {
		t.Fatalf("%+v", r)
	}
}

func TestParseBuildFailure(t *testing.T) {
	r := mustParse(t, stream(
		ev{Action: "build-output", ImportPath: "m/d", Output: "# m/d\n"},
		ev{Action: "build-output", ImportPath: "m/d", Output: "d/d.go:3: cannot use \"x\" (untyped string constant) as int value in return statement\n"},
		ev{Action: "build-fail", ImportPath: "m/d"},
		ev{Action: "start", Package: "m/d"},
		ev{Action: "output", Package: "m/d", Output: "FAIL\tm/d [build failed]\n"},
		ev{Action: "fail", Package: "m/d", FailedBuild: "m/d"},
	))
	if len(r.BuildErrors) != 1 || !strings.Contains(r.BuildErrors[0], "cannot use") {
		t.Fatalf("build errors %v: the compiler line is kept, the # header is not", r.BuildErrors)
	}
	if len(r.FailedPackages) != 1 {
		t.Errorf("%+v", r)
	}
}

func TestParseFindsADataRace(t *testing.T) {
	r := mustParse(t, stream(
		seed("m/e", "9"),
		ev{Action: "output", Package: "m/e", Test: "TestRace", Output: "WARNING: DATA RACE\n"},
		ev{Action: "output", Package: "m/e", Test: "TestRace", Output: "WARNING: DATA RACE\n"},
		ev{Action: "fail", Package: "m/e", Test: "TestRace"},
	))
	if len(r.Races) != 1 || r.Races[0] != "m/e" {
		t.Fatalf("races %v: one entry per package", r.Races)
	}
}

// Each test binary shuffles with its own seed. The run records the seed of the
// first failing package, so a failure can be replayed; with no failure, the
// first seed printed.
func TestParseSeedPrefersTheFailingPackage(t *testing.T) {
	evs := cat(
		[]ev{seed("m/a", "111")}, pass("m/a", "TestA"), []ev{{Action: "pass", Package: "m/a"}},
		[]ev{seed("m/c", "222")},
		[]ev{{Action: "run", Package: "m/c", Test: "TestC"}, {Action: "fail", Package: "m/c", Test: "TestC"}, {Action: "fail", Package: "m/c"}},
	)
	if r := mustParse(t, stream(evs...)); r.Seed != 222 {
		t.Errorf("seed %d, want the failing package's 222", r.Seed)
	}
	if r := mustParse(t, stream(evs[:3]...)); r.Seed != 111 {
		t.Errorf("seed %d, want the first package's 111", r.Seed)
	}
}

func TestParseNoSeedMeansNoShuffle(t *testing.T) {
	r := mustParse(t, stream(cat(pass("m/a", "TestA"), []ev{{Action: "pass", Package: "m/a"}})...))
	if r.HasSeed {
		t.Fatalf("a seed was invented: %d", r.Seed)
	}
}

func TestParseLargeSeedFitsInt64(t *testing.T) {
	r := mustParse(t, stream(seed("m/a", "1794945368392316000")))
	if r.Seed != 1794945368392316000 {
		t.Fatalf("seed %d", r.Seed)
	}
}

func TestParseRefusesNonStreams(t *testing.T) {
	for name, in := range map[string]string{"empty": "", "blank": "\n \n", "plain text": "panic: runtime error\ngoroutine 1 [running]:\n"} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("%s parsed", name)
		}
	}
}

// Text mixed into the stream is kept as a note and does not hide the events.
func TestParseToleratesStrayText(t *testing.T) {
	in := append([]byte("warning: something\n"), stream(pass("m/a", "TestA")...)...)
	r := mustParse(t, in)
	if r.Tests != 1 || r.Stray != "warning: something" {
		t.Fatalf("%+v", r)
	}
}

// The same output twice is the same test once.
func TestParseDeduplicatesFailedTests(t *testing.T) {
	r := mustParse(t, stream(
		ev{Action: "fail", Package: "m/c", Test: "TestC"},
		ev{Action: "fail", Package: "m/c", Test: "TestC"},
	))
	if len(r.FailedTests) != 1 {
		t.Fatalf("%v", r.FailedTests)
	}
}
