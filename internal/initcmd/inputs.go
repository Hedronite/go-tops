package initcmd

import (
	"bufio"
	"fmt"
	"go/version"
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
	depguard bool   // deps.depguard: init writes it true, so the lint config enables the linter
	protocol string // the protocol the binary implements: the embedded schema constant
	goFloor  string // 1.26
	owner    string
	kitTool  bool // gotops goes into tools/go.mod
	warnings []string

	goCurrent     string // go_current of go-tops.yaml
	ownerFromPath bool   // the CODEOWNERS owner came from the module path, not --owner
	toolsText     string // the new tools/go.mod: the existing one with the kit's pins added
	pinned        string // the gotops version tools/go.mod pins, "" when it pins none
}

func (in *inputs) notes() []string {
	notes := append([]string(nil), in.warnings...)
	if in.ownerFromPath {
		notes = append(notes, "the CODEOWNERS owner "+in.owner+" comes from the module path: a bare organisation is not a valid owner, only a user or @org/team is; pass --owner with one, and check .github/CODEOWNERS on the default branch with gh api repos/<owner>/<repo>/codeowners/errors")
	}
	return append(notes, lintBinaryNote)
}

// lintBinaryNote is the F6 advice: the gate finds golangci-lint in .go-tops/bin
// or on PATH (GO_TOPS.md §10), and a test that walks the module tree reads the
// binary in .go-tops/bin as source.
const lintBinaryNote = "golangci-lint is found in .go-tops/bin or on PATH: a repository whose tests walk the module tree (a name guard, a scrub) keeps the binary outside the tree, on PATH, because a walk reads the files of .go-tops/bin too; install the pinned release into a directory outside the checkout, as the workflow's install step does"

func gatherInputs(o Options) (*inputs, error) {
	if !contains(launchClasses, o.Class) {
		return nil, refuse("--class %q has no profile; the launch classes are %s", o.Class, strings.Join(launchClasses, ", "))
	}
	proto, err := gotops.Protocol()
	if err != nil {
		return nil, err
	}
	in := &inputs{o: o, protocol: proto, depguard: true}
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
	if err := in.resolveToolsMod(); err != nil {
		return nil, err
	}
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
			in.goFloor = m[1] + "." + m[2] + m[3]
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
	if version.Compare("go"+in.goFloor, toolchain) > 0 {
		return refuse("the go line of go.mod says %s, above the pinned toolchain %s", in.goFloor, toolchain)
	}
	in.goCurrent = goCurrent()
	if strings.Count(in.goFloor, ".") == 2 {
		// A floor that names a release is compared with go_current in Go's order, in
		// which the language version 1.27 sorts below the release 1.27.0: go_current
		// then names the pinned release.
		in.goCurrent = strings.TrimPrefix(toolchain, "go")
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
		in.ownerFromPath = true
	}
	return nil
}

// resolveKit decides whether gotops itself is pinned in tools/go.mod: only a
// release version can be.
func (in *inputs) resolveKit() {
	in.kitTool = releaseRe.MatchString(in.o.KitVersion) && !strings.HasSuffix(in.o.KitVersion, "-dev")
}

// resolveToolsMod builds the new tools/go.mod: the existing one with the kit's
// missing pins added, or a fresh one. The record reads the gotops pin from the
// result, so it names what the file pins and not what this build is.
func (in *inputs) resolveToolsMod() error {
	existing, found, err := existingToolsMod(in.o.Dir)
	if err != nil {
		return err
	}
	in.toolsText = in.freshToolsMod()
	if found {
		if in.toolsText, err = in.mergeToolsMod(existing); err != nil {
			return err
		}
	}
	if in.pinned = pinnedKit(in.toolsText); in.pinned == "" {
		in.warnings = append(in.warnings, "this gotops build is not a release: gotops is not in tools/go.mod; add it with go get -tool -modfile=tools/go.mod "+gotopsPackage+"@<version>")
	}
	return nil
}
