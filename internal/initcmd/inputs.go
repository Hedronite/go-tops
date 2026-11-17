package initcmd

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	gotops "github.com/Hedronite/go-tops"
)

// launchClasses are the classes with an embedded profile (§5.1). The other
// classes are schema-valid and have no profile, so init refuses them.
var launchClasses = []string{"binary-cli", "binary-service", "library-core"}

// profile is the part of a class profile (§5.3) init copies into go-tops.yaml.
type profile struct {
	Gates struct {
		Coverage struct {
			Floor float64 `yaml:"statements_floor"`
		}
		Crap struct {
			Cap float64
		}
	}
	Layers map[string]struct {
		Reason        string  `yaml:"reason"`
		RegressionPct float64 `yaml:"regression_pct"`
	}
}

// inputs is everything a file template needs.
type inputs struct {
	o        Options
	profile  profile
	module   string
	protocol string // the protocol the binary implements: the embedded schema constant
	goFloor  string // 1.26
	owner    string
	kitTool  bool // gotops goes into tools/go.mod
	warnings []string
}

func (in *inputs) notes() []string {
	notes := append([]string(nil), in.warnings...)
	return append(notes, "add .go-tops/bin/ and .go-tops/report.json to .gitignore: neither is tracked")
}

func gatherInputs(o Options) (*inputs, error) {
	if !contains(launchClasses, o.Class) {
		return nil, refuse("--class %q has no profile; the launch classes are %s", o.Class, strings.Join(launchClasses, ", "))
	}
	proto, err := gotops.Protocol()
	if err != nil {
		return nil, err
	}
	in := &inputs{o: o, protocol: proto}
	if err := in.loadProfile(); err != nil {
		return nil, err
	}
	if err := in.readGoMod(); err != nil {
		return nil, err
	}
	if err := in.resolveOwner(); err != nil {
		return nil, err
	}
	in.resolveKit()
	return in, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (in *inputs) loadProfile() error {
	data, err := fs.ReadFile(in.o.Assets, "profiles/"+in.o.Class+".yaml")
	if err != nil {
		return fmt.Errorf("embedded profile for %s: %w", in.o.Class, err)
	}
	if err := yaml.Unmarshal(data, &in.profile); err != nil {
		return fmt.Errorf("embedded profile for %s: %w", in.o.Class, err)
	}
	return nil
}

var (
	moduleRe  = regexp.MustCompile(`^module\s+("?)(\S+?)("?)\s*(//.*)?$`)
	goLineRe  = regexp.MustCompile(`^go\s+(\d+)\.(\d+)(\.\d+)?\s*(//.*)?$`)
	githubRe  = regexp.MustCompile(`^github\.com/([^/]+)/[^/]+`)
	releaseRe = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)
)

// readGoMod takes the module path and the language floor from go.mod. The
// floor is the minor release of the go line; it must meet the protocol floor
// (§2) and not exceed the pinned toolchain.
func (in *inputs) readGoMod() error {
	f, err := os.Open(filepath.Join(in.o.Dir, "go.mod"))
	if err != nil {
		return refuse("no go.mod in %s: run init in a Go module", in.o.Dir)
	}
	defer f.Close() //nolint:errcheck // read-only file: the close error carries nothing
	var floorMinor int
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if m := moduleRe.FindStringSubmatch(line); m != nil && in.module == "" {
			in.module = m[2]
		}
		if m := goLineRe.FindStringSubmatch(line); m != nil && in.goFloor == "" {
			if floorMinor, err = strconv.Atoi(m[2]); err != nil {
				return err
			}
			in.goFloor = m[1] + "." + m[2]
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return in.checkFloor(floorMinor)
}

func (in *inputs) checkFloor(minor int) error {
	if in.module == "" || in.goFloor == "" {
		return refuse("go.mod needs a module line and a go line")
	}
	if !strings.HasPrefix(in.goFloor, "1.") || minor < protocolFloorMinor {
		return refuse("the go line of go.mod says %s; the protocol floor is 1.%d (§2)", in.goFloor, protocolFloorMinor)
	}
	cur, err := goCurrentMinor()
	if err != nil {
		return err
	}
	if minor > cur {
		return refuse("the go line of go.mod says %s, above the pinned toolchain %s", in.goFloor, toolchain)
	}
	return nil
}

// resolveOwner takes --owner, else the owner of a github.com module path. An
// owner GitHub cannot resolve makes CODEOWNERS silently inert, so none at all
// is a refusal.
func (in *inputs) resolveOwner() error {
	switch owner := strings.TrimSpace(in.o.Owner); {
	case owner != "":
		in.owner = "@" + strings.TrimPrefix(owner, "@")
	default:
		m := githubRe.FindStringSubmatch(in.module)
		if m == nil {
			return refuse("the module %s is not on github.com: pass --owner <handle> for CODEOWNERS", in.module)
		}
		in.owner = "@" + m[1]
	}
	return nil
}

// resolveKit decides whether gotops itself is pinned in tools/go.mod: only a
// release version can be.
func (in *inputs) resolveKit() {
	in.kitTool = releaseRe.MatchString(in.o.KitVersion) && !strings.HasSuffix(in.o.KitVersion, "-dev")
	if !in.kitTool {
		in.warnings = append(in.warnings, "this gotops build is not a release: gotops is not in tools/go.mod; add it with go get -tool -modfile=tools/go.mod "+gotopsPackage+"@<version>")
	}
}
