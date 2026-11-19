package crap_test

import (
	"slices"
	"sort"
	"testing"

	"github.com/Hedronite/go-tops/internal/crap"
	"github.com/Hedronite/go-tops/internal/gobuild"
)

func compiledKeys(t *testing.T, dir string, b gobuild.Build) []string {
	t.Helper()
	set, err := crap.Compiled(dir, b)
	if err != nil {
		t.Fatalf("%+v: %v", b, err)
	}
	var keys []string
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// The ledger build's functions are found without compiling that build. The tests
// layer's build has cgo on, and a module that imports net needs runtime/cgo, which
// the go command builds only for an OS whose C toolchain is here: a type-checking
// answer would fail for linux on a macOS runner, and for macOS on a Linux one.
func TestCompiledNeedsNoToolchainForTheNamedPlatform(t *testing.T) {
	dir := fixtureModule(t, map[string]string{
		"main.go":      "package m\n\nimport \"net\"\n\nfunc Run() string { return net.JoinHostPort(\"h\", \"1\") }\n",
		"x_linux.go":   "package m\n\nfunc OnLinux() {}\n",
		"x_darwin.go":  "package m\n\nfunc OnMac() {}\n",
		"x_windows.go": "package m\n\nfunc OnWindows() {}\n",
	})
	for _, c := range []struct {
		goos, goarch, only string
	}{
		{"linux", "amd64", "OnLinux"}, {"linux", "arm64", "OnLinux"},
		{"darwin", "arm64", "OnMac"}, {"darwin", "amd64", "OnMac"},
		{"windows", "amd64", "OnWindows"},
	} {
		b := gobuild.Build{GOOS: c.goos, GOARCH: c.goarch, CGOEnabled: true}
		want := []string{module + "." + c.only, module + ".Run"}
		if got := compiledKeys(t, dir, b); !slices.Equal(got, want) {
			t.Errorf("%s/%s with cgo: compiled %v, want %v", c.goos, c.goarch, got, want)
		}
	}
}

// A cgo file is compiled when the ledger build has cgo on and is not when it has it
// off, as the go command selects it; the answer comes from go/build, not from a load.
func TestCompiledFollowsTheCgoSettingOfTheLedgerBuild(t *testing.T) {
	dir := fixtureModule(t, map[string]string{
		"main.go": "package m\n\nfunc Run() {}\n",
		"c.go":    "package m\n\n// int one(void) { return 1; }\nimport \"C\"\n\nfunc One() int { return int(C.one()) }\n",
	})
	on := compiledKeys(t, dir, gobuild.Build{GOOS: "linux", GOARCH: "amd64", CGOEnabled: true})
	off := compiledKeys(t, dir, gobuild.Build{GOOS: "linux", GOARCH: "amd64"})
	if want := []string{module + ".One", module + ".Run"}; !slices.Equal(on, want) {
		t.Errorf("cgo on: compiled %v, want %v", on, want)
	}
	if want := []string{module + ".Run"}; !slices.Equal(off, want) {
		t.Errorf("cgo off: compiled %v, want %v", off, want)
	}
}
