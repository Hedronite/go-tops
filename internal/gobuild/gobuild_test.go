package gobuild_test

import (
	"slices"
	"testing"

	"github.com/Hedronite/go-tops/internal/gobuild"
)

func TestEnv(t *testing.T) {
	tests := []struct {
		name string
		b    gobuild.Build
		want []string
	}{
		{"zero", gobuild.Build{}, []string{"CGO_ENABLED=0", "GOFLAGS=-count=1"}},
		{"the test run", gobuild.Build{CGOEnabled: true}, []string{"CGO_ENABLED=1", "GOFLAGS=-count=1"}},
		{
			"platform and tags",
			gobuild.Build{GOOS: "linux", GOARCH: "arm64", Tags: []string{"a", "b"}, CGOEnabled: true},
			[]string{"CGO_ENABLED=1", "GOOS=linux", "GOARCH=arm64", "GOFLAGS=-count=1 -tags=a,b"},
		},
	}
	for _, tc := range tests {
		if got := tc.b.Env(); !slices.Equal(got, tc.want) {
			t.Errorf("%s: Env = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The test run is `go test -race`, which sets the race tag: loading must see it.
func TestLoadEnvAddsTheRaceTag(t *testing.T) {
	b := gobuild.Build{GOOS: "linux", CGOEnabled: true, Tags: []string{"x"}}
	want := []string{"CGO_ENABLED=1", "GOOS=linux", "GOFLAGS=-count=1 -tags=x,race"}
	if got := b.LoadEnv(); !slices.Equal(got, want) {
		t.Errorf("LoadEnv = %v, want %v", got, want)
	}
	if got := (gobuild.Build{CGOEnabled: true}).LoadEnv(); !slices.Equal(got, []string{"CGO_ENABLED=1", "GOFLAGS=-count=1 -tags=race"}) {
		t.Errorf("LoadEnv of the test run = %v", got)
	}
	if slices.Contains(b.Tags, "race") || len(b.Tags) != 1 {
		t.Errorf("LoadEnv changed the build's own tags: %v", b.Tags)
	}
	r := gobuild.Build{Tags: []string{"race"}}
	if got := r.LoadEnv(); !slices.Equal(got, []string{"CGO_ENABLED=0", "GOFLAGS=-count=1 -tags=race"}) {
		t.Errorf("LoadEnv repeated the race tag: %v", got)
	}
}

func TestPlatformTurnsCgoOff(t *testing.T) {
	b := gobuild.Build{GOOS: "linux", GOARCH: "amd64", CGOEnabled: true, Tags: []string{"x"}}.Platform("windows", "arm64")
	if b.GOOS != "windows" || b.GOARCH != "arm64" || b.CGOEnabled || !slices.Equal(b.Tags, []string{"x"}) {
		t.Errorf("Platform = %+v", b)
	}
}

// Resolve names the platform the go command would build for, and nothing else.
func TestResolveFillsThePlatform(t *testing.T) {
	t.Setenv("GOOS", "linux")
	t.Setenv("GOARCH", "arm64")
	got, err := gobuild.Resolve(t.TempDir(), gobuild.Build{Tags: []string{"x"}, CGOEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.GOOS != "linux" || got.GOARCH != "arm64" || !slices.Equal(got.Tags, []string{"x"}) || !got.CGOEnabled {
		t.Errorf("Resolve = %+v, want linux/arm64 with the tags and cgo as given", got)
	}
}

func TestResolveKeepsWhatIsNamed(t *testing.T) {
	t.Setenv("GOOS", "linux")
	t.Setenv("GOARCH", "arm64")
	got, err := gobuild.Resolve(t.TempDir(), gobuild.Build{GOOS: "windows", GOARCH: "386"})
	if err != nil {
		t.Fatal(err)
	}
	if got.GOOS != "windows" || got.GOARCH != "386" {
		t.Errorf("Resolve = %+v, want the named platform", got)
	}
	half, err := gobuild.Resolve(t.TempDir(), gobuild.Build{GOOS: "windows"})
	if err != nil {
		t.Fatal(err)
	}
	if half.GOOS != "windows" || half.GOARCH != "arm64" {
		t.Errorf("Resolve = %+v, want windows with the environment's arm64", half)
	}
}

// A resolved build names a platform, so a recorded one cannot match every
// platform.
func TestResolveNeverLeavesThePlatformEmpty(t *testing.T) {
	got, err := gobuild.Resolve(t.TempDir(), gobuild.Build{CGOEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.GOOS == "" || got.GOARCH == "" {
		t.Errorf("Resolve = %+v: a platform is empty", got)
	}
}

func TestResolveFailsWhenGoCannotAnswer(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := gobuild.Resolve(t.TempDir(), gobuild.Build{}); err == nil {
		t.Error("Resolve succeeded with no go command")
	}
}

func TestBuildEqual(t *testing.T) {
	a := gobuild.Build{GOOS: "linux", GOARCH: "amd64", Tags: []string{"b", "a"}, CGOEnabled: true}
	for name, c := range map[string]struct {
		b    gobuild.Build
		want bool
	}{
		"same, tags in another order": {gobuild.Build{GOOS: "linux", GOARCH: "amd64", Tags: []string{"a", "b"}, CGOEnabled: true}, true},
		"a repeated tag":              {gobuild.Build{GOOS: "linux", GOARCH: "amd64", Tags: []string{"a", "b", "a"}, CGOEnabled: true}, true},
		"other os":                    {gobuild.Build{GOOS: "darwin", GOARCH: "amd64", Tags: []string{"a", "b"}, CGOEnabled: true}, false},
		"other arch":                  {gobuild.Build{GOOS: "linux", GOARCH: "arm64", Tags: []string{"a", "b"}, CGOEnabled: true}, false},
		"other tags":                  {gobuild.Build{GOOS: "linux", GOARCH: "amd64", Tags: []string{"a"}, CGOEnabled: true}, false},
		"cgo off":                     {gobuild.Build{GOOS: "linux", GOARCH: "amd64", Tags: []string{"a", "b"}}, false},
	} {
		if got := a.Equal(c.b); got != c.want {
			t.Errorf("%s: Equal = %v, want %v", name, got, c.want)
		}
	}
	if !(gobuild.Build{}).Equal(gobuild.Build{Tags: []string{}}) {
		t.Error("a nil and an empty tag list are the same build")
	}
}
