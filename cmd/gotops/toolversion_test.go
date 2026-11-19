package main

import (
	"bytes"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/cli"
)

// The report names the gotops that gated (GO_TOPS.md §12.3 tool_version). A release
// links its version in. A gotops an adopter runs through `go tool` is built from a
// module version, a pseudo-version while the kit has no tag (ruling D3 a), and the
// build info carries it; only a build with neither says 0.0.0-dev.

func withBuild(t *testing.T, linked string, info *debug.BuildInfo) {
	t.Helper()
	oldVersion, oldRead := packageVersion, readBuildInfo
	t.Cleanup(func() { packageVersion, readBuildInfo = oldVersion, oldRead })
	packageVersion = linked
	readBuildInfo = func() (*debug.BuildInfo, bool) { return info, info != nil }
}

func TestToolVersionComesFromTheBuildInfoOfAPinnedBuild(t *testing.T) {
	const pseudo = "v0.0.0-20261119113245-1a3473689a78"
	for _, c := range []struct {
		name, linked string
		info         *debug.BuildInfo
		want         string
	}{
		{"a pseudo-version pin", "0.0.0-dev", &debug.BuildInfo{Main: debug.Module{Version: pseudo}}, "0.0.0-20261119113245-1a3473689a78"},
		{"a tagged module version", "0.0.0-dev", &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}, "1.2.3"},
		{"a release links its version in", "1.4.0", &debug.BuildInfo{Main: debug.Module{Version: pseudo}}, "1.4.0"},
		{"a build from a working tree", "0.0.0-dev", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, "0.0.0-dev"},
		{"no module version", "0.0.0-dev", &debug.BuildInfo{}, "0.0.0-dev"},
		{"no build info", "0.0.0-dev", nil, "0.0.0-dev"},
	} {
		t.Run(c.name, func(t *testing.T) {
			withBuild(t, c.linked, c.info)
			if got := toolVersion(); got != c.want {
				t.Errorf("toolVersion = %q, want %q", got, c.want)
			}
		})
	}
}

func TestVersionCommandPrintsTheSameVersionTheReportCarries(t *testing.T) {
	withBuild(t, "0.0.0-dev", &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20261119113245-1a3473689a78"}})
	t.Chdir(t.TempDir())
	var out, errb bytes.Buffer
	if code := runVersion(cli.NewEnv(&out, &errb), nil); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "package 0.0.0-20261119113245-1a3473689a78\n") {
		t.Errorf("version output %q should carry the build info's version", out.String())
	}
}
