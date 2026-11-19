package ratchet_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/gobuild"
	"github.com/Hedronite/go-tops/internal/layers/tests"
)

// A leg whose build is not the ledger's skips the ratchet (GO_TOPS.md §6.7, ruling
// C3). It must find what the ledger build compiles without compiling it: the test
// build is always cgo_enabled true, and type-checking a module for another OS
// under cgo needs that OS's C toolchain, which a runner does not have. The
// module has no cgo file, so nothing in it needs one; only the standard library's
// runtime/cgo does, and the answer must not depend on it.

// importsNet gives the module what a real adopter has: a package that imports net.
// Under cgo and the race tag the standard library behind it needs runtime/cgo, which
// the go command can only build for a target OS that has a C toolchain here.
func (p *ratchetProject) importsNet(t *testing.T) {
	t.Helper()
	src := "package low\n\nimport \"net\"\n\nfunc Addr() string { return net.JoinHostPort(\"h\", \"1\") }\n"
	if err := os.WriteFile(filepath.Join(p.dir, "low", "addr.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
}

// ledgerBuilds are builds the ledger may record, each with cgo on as the tests
// layer's build has it.
func ledgerBuilds(host gobuild.Build) map[string]gobuild.Build {
	return map[string]gobuild.Build{
		"linux/amd64":   {GOOS: "linux", GOARCH: "amd64", CGOEnabled: true},
		"linux/arm64":   {GOOS: "linux", GOARCH: "arm64", CGOEnabled: true},
		"darwin/arm64":  {GOOS: "darwin", GOARCH: "arm64", CGOEnabled: true},
		"darwin/amd64":  {GOOS: "darwin", GOARCH: "amd64", CGOEnabled: true},
		"windows/amd64": {GOOS: "windows", GOARCH: "amd64", CGOEnabled: true},
		"a tag":         {GOOS: host.GOOS, GOARCH: host.GOARCH, Tags: []string{"integration"}, CGOEnabled: true},
	}
}

func TestOtherLegSkipsWhateverBuildTheLedgerRecords(t *testing.T) {
	probe := legacyProject(t)
	host, err := gobuild.Resolve(probe.dir, tests.Build)
	if err != nil {
		t.Fatal(err)
	}
	for name, ledger := range ledgerBuilds(host) {
		t.Run(name, func(t *testing.T) {
			if ledger.Equal(host) {
				t.Skipf("%s is this host's build: it is the ledger leg here", name)
			}
			p := legacyProject(t)
			p.importsNet(t)
			p.ledgerBuild = &ledger
			p.writeFirstLedger(t)
			run := runGateWith(t, p, "ratchet", otherLegLayers(t, p))
			r := run.Report.Ratchet
			if r == nil || r.Outcome != "skip" {
				t.Fatalf("ratchet %+v, want skip: this leg is not the ledger leg\n%s", r, run.Text)
			}
			// The ledger holds the legacy function and the lint findings, so the leg
			// that does not judge them fails neither the CRAP layer nor hygiene.
			if o := outcomes(run); o["crap"] != "pass" || o["hygiene"] != "pass" || run.ExitCode != 0 {
				t.Errorf("layers %v exit %d, want crap and hygiene pass and exit 0: the ledger holds the legacy function and the findings\n%s", o, run.ExitCode, run.Text)
			}
			if strings.Contains(run.Text, "runtime/cgo") {
				t.Errorf("the run compiled the ledger build: %s", run.Text)
			}
		})
	}
}

// Functions the ledger build compiles and this one does not stay carried (C3d): a
// file only the ledger build selects does not hold its functions to the cap here,
// and a file only this build selects does.
func TestOtherLegStillHoldsWhatOnlyItCompiles(t *testing.T) {
	probe := legacyProject(t)
	host, err := gobuild.Resolve(probe.dir, tests.Build)
	if err != nil {
		t.Fatal(err)
	}
	ledger := gobuild.Build{GOOS: "windows", GOARCH: "amd64", CGOEnabled: true}
	if ledger.Equal(host) {
		ledger.GOOS = "linux"
	}
	p := legacyProject(t)
	p.ledgerBuild = &ledger
	p.writeFirstLedger(t)
	big := "//go:build " + host.GOOS + "\n\npackage low\n\nfunc OnlyHere(a bool) int {\n" + strings.Repeat("\tif a {\n\t\treturn 1\n\t}\n", 11) + "\treturn 0\n}\n"
	if err := os.WriteFile(filepath.Join(p.dir, "low", "only_here.go"), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	run := runGateWith(t, p, "ratchet", otherLegLayers(t, p))
	if o := outcomes(run); o["crap"] != "fail" || !strings.Contains(run.Text, "example.com/m/low.OnlyHere") {
		t.Errorf("layers %v, want crap fail naming OnlyHere, which the ledger build does not compile\n%s", o, run.Text)
	}
	if r := run.Report.Ratchet; r == nil || r.Outcome != "skip" {
		t.Errorf("ratchet %+v, want skip", r)
	}
}

// When what the ledger build compiles cannot be read at all, the leg does not
// judge: the ratchet is a skip that says why, and the leg holds every function to
// the cap rather than carry any of them on a guess. It never fails the ratchet.
func TestOtherLegThatCannotReadTheLedgerSourcesSkipsAndSaysWhy(t *testing.T) {
	probe := legacyProject(t)
	host, err := gobuild.Resolve(probe.dir, tests.Build)
	if err != nil {
		t.Fatal(err)
	}
	ledger := gobuild.Build{GOOS: "windows", GOARCH: "amd64", CGOEnabled: true}
	if ledger.Equal(host) {
		ledger.GOOS = "linux"
	}
	p := legacyProject(t)
	p.importsNet(t)
	p.ledgerBuild = &ledger
	p.writeFirstLedger(t)
	// Under the ledger build two packages share a directory, so go/build refuses to
	// select its files; under this host's build neither file is compiled at all.
	for _, f := range []string{"x", "y"} {
		src := "package low\n"
		if f == "y" {
			src = "package other\n"
		}
		if err := os.WriteFile(filepath.Join(p.dir, "low", f+"_"+ledger.GOOS+".go"), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run := runGateWith(t, p, "ratchet", otherLegLayers(t, p))
	r := run.Report.Ratchet
	if r == nil || r.Outcome != "skip" {
		t.Fatalf("ratchet %+v, want skip: a leg that is not the ledger leg never fails the ratchet\n%s", r, run.Text)
	}
	if !strings.Contains(run.Text, "could not find what the ledger build compiles") {
		t.Errorf("the run does not say why it could not read the ledger build's sources:\n%s", run.Text)
	}
}
