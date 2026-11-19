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
