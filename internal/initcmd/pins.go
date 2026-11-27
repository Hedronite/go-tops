package initcmd

import "strings"

// The kit's embedded pins. init writes them into go-tops.lock and
// tools/go.mod, so the first gate run is already pinned (GO_TOPS.md §10).
// A test keeps the lock pins equal to config/go-tops.example.lock.
const (
	toolchain = "go1.27.4"
	// protocolFloorMinor is the minor release of the language floor (§2).
	protocolFloorMinor = 26
)

// platforms are the os/arch pairs the lock pins, in the order the lock lists them.
var platforms = []string{"darwin/arm64", "darwin/amd64", "linux/amd64", "linux/arm64"}

type binaryPin struct {
	Name, Version, URL, Extract string
	SHA256                      map[string]string // of the downloaded archive
	Executable                  map[string]string // of the executable inside it
}

var golangciLint = binaryPin{
	Name:    "golangci-lint",
	Version: "2.15.0",
	URL:     "https://github.com/golangci/golangci-lint/releases/download/v{version}/golangci-lint-{version}-{os}-{arch}.tar.gz",
	Extract: "golangci-lint-{version}-{os}-{arch}/golangci-lint",
	SHA256: map[string]string{
		"darwin/arm64": "d1e409481a8ca846c4bc97e781eb5cb1bd5c23353a0b3977a1cbda1b1b6447d5",
		"darwin/amd64": "767b899a28285e88ded2823d5dcc4bc5ef681173eb2d80f029a7b17ee4ece3b8",
		"linux/amd64":  "9b7fe6a6bf6334b98decf8d86b11415ea7889afdabdd1959fdce23af2a9ebcea",
		"linux/arm64":  "b4d8f17068b35ee6060cc2f69a16fb48740d706fbba81661c318649c114a6998",
	},
	Executable: map[string]string{
		"darwin/arm64": "71b89744ef6dd92dad9026b70489d1ad660da79ef90dfb5aabe995d916bec8a9",
		"darwin/amd64": "e04962742507a5f37a4002a7a8b2b549f6864f17807eb5d534d947f6311a051f",
		"linux/amd64":  "d9af4564c05beb2f5e130119aec45e5e89d28e5017d558d3ca382659f2427894",
		"linux/arm64":  "14680d7674eb1fed0405446670e15dcffb74e979ef0dcd41f998c088fd6babfb",
	},
}

// moduleTool is a tool pinned in tools/go.mod and run with go tool -modfile.
type moduleTool struct {
	Package, Module, Version string
}

var moduleTools = []moduleTool{
	{"github.com/google/go-licenses/v2", "github.com/google/go-licenses/v2", "v2.0.1"},
	{"golang.org/x/vuln/cmd/govulncheck", "golang.org/x/vuln", "v1.9.0"},
	{"gotest.tools/gotestsum", "gotest.tools/gotestsum", "v1.13.0"},
}

const gotopsPackage, gotopsModule = "github.com/Hedronite/go-tops/cmd/gotops", "github.com/Hedronite/go-tops"

// goCurrent is go_current for a module whose go line names the floor: the
// language version of the pinned toolchain (go1.27.4 -> 1.27), or the pinned
// release itself (1.27.4) when the floor names a release. Go orders the release
// 1.27.0 above the language version 1.27, so check (go_floor above go_current)
// would refuse the pair if go_current stayed a language version.
func goCurrent(floor string) string {
	v := strings.TrimPrefix(toolchain, "go")
	if strings.Count(floor, ".") == 2 {
		return v
	}
	parts := strings.SplitN(v, ".", 3)
	return parts[0] + "." + parts[1]
}
