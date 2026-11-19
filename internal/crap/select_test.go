package crap_test

import (
	"slices"
	"testing"

	"github.com/Hedronite/go-tops/internal/crap"
	"github.com/Hedronite/go-tops/internal/gobuild"
)

func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// A build selects the files its platform, tags and cgo setting leave in, test files
// included, and the packages with a compiled non-test file.
func TestSelectFollowsTheBuild(t *testing.T) {
	dir := fixtureModule(t, map[string]string{
		"main.go":                "package m\n\nfunc Run() {}\n",
		"main_test.go":           "package m\n\nimport \"testing\"\n\nfunc TestRun(t *testing.T) {}\n",
		"x_linux.go":             "package m\n\nfunc L() {}\n",
		"x_darwin.go":            "package m\n\nfunc D() {}\n",
		"x_darwin_test.go":       "package m\n\nimport \"testing\"\n\nfunc TestD(t *testing.T) {}\n",
		"tagged.go":              "//go:build integration\n\npackage m\n\nfunc T() {}\n",
		"onlymac/m_darwin.go":    "package onlymac\n\nfunc M() {}\n",
		"onlytypes/types.go":     "package onlytypes\n\ntype X int\n",
		"onlytests/only_test.go": "package onlytests\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n",
		"testdata/t.go":          "package td\n\nfunc T() {}\n",
		"nested/go.mod":          "module example.com/other\n\ngo 1.26\n",
		"nested/n.go":            "package nested\n\nfunc N() {}\n",
	})
	linuxSel, err := crap.Select(dir, gobuild.Build{GOOS: "linux", GOARCH: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := []string{"main.go", "main_test.go", "onlytests/only_test.go", "onlytypes/types.go", "x_linux.go"}
	if got := keys(linuxSel.Files); !slices.Equal(got, wantFiles) {
		t.Errorf("linux files %v, want %v", got, wantFiles)
	}
	wantPkgs := []string{module, module + "/onlytypes"}
	if got := keys(linuxSel.Packages); !slices.Equal(got, wantPkgs) {
		t.Errorf("linux packages %v, want %v: a package of only test files, or of only another platform's files, is not compiled", got, wantPkgs)
	}

	mac, err := crap.Select(dir, gobuild.Build{GOOS: "darwin", GOARCH: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	if !mac.Files["x_darwin.go"] || !mac.Files["x_darwin_test.go"] || mac.Files["x_linux.go"] || !mac.Packages[module+"/onlymac"] {
		t.Errorf("darwin selection %v %v", keys(mac.Files), keys(mac.Packages))
	}

	tagged, err := crap.Select(dir, gobuild.Build{GOOS: "linux", GOARCH: "amd64", Tags: []string{"integration"}})
	if err != nil {
		t.Fatal(err)
	}
	if !tagged.Files["tagged.go"] || linuxSel.Files["tagged.go"] {
		t.Errorf("the integration tag does not select tagged.go")
	}
}

// The race tag is set, as for `go test -race`; cgo files need cgo.
func TestSelectSetsTheRaceTagAndHonoursCgo(t *testing.T) {
	dir := fixtureModule(t, map[string]string{
		"norace.go": "//go:build !race\n\npackage m\n\nfunc Plain() {}\n",
		"race.go":   "//go:build race\n\npackage m\n\nfunc Raced() {}\n",
		"c.go":      "package m\n\n/*\nstatic int one(void) { return 1; }\n*/\nimport \"C\"\n\nfunc One() int { return int(C.one()) }\n",
	})
	off, err := crap.Select(dir, gobuild.Build{GOOS: "linux", GOARCH: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if !off.Files["race.go"] || off.Files["norace.go"] || off.Files["c.go"] {
		t.Errorf("cgo off: %v, want race.go only", keys(off.Files))
	}
	on, err := crap.Select(dir, gobuild.Build{GOOS: "linux", GOARCH: "amd64", CGOEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if !on.Files["c.go"] {
		t.Errorf("cgo on: %v, want c.go", keys(on.Files))
	}
}

func TestSelectFailsWithoutAModule(t *testing.T) {
	if _, err := crap.Select(t.TempDir(), gobuild.Build{}); err == nil {
		t.Error("a directory with no go.mod was selected")
	}
}
