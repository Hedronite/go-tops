package initcmd

import (
	"io/fs"
	"os/exec"
	"strings"
	"testing"
)

// The friction rows of the first adoption (evidence/m3/FRICTION: F2, F3, F5 to
// F9, F11) and rulings R5 and R6, each as a test from the row. The oracle is
// GO_TOPS.md §4.1, §4.2, §6.7, §10 and §11 and what an adopter does with the files.

// An adopter's own lint config stays byte for byte as it is (R5): init writes the
// kit's rules to .go-tops/golangci.yml, points the hygiene layer at it, and needs
// no --force for a repository that has a .golangci.yml.
func TestInitLeavesTheAdoptersLintConfigAlone(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	const mine = "version: \"2\"\nrun:\n  timeout: 5m\nlinters:\n  enable: [misspell]\n"
	write(t, dir, ".golangci.yml", mine)
	if code, out, errs := run(opts(dir, "binary-service")); code != ExitOK {
		t.Fatalf("init refused a repository that has its own .golangci.yml: exit %d\n%s%s", code, out, errs)
	}
	if got := read(t, dir, ".golangci.yml"); got != mine {
		t.Errorf("the adopter's .golangci.yml changed:\n%s", got)
	}
	cfg := asMap(t, decodeYAML(t, read(t, dir, "go-tops.yaml")))
	lint := asMap(t, asMap(t, asMap(t, cfg["gates"])["hygiene"])["golangci_lint"])
	if lint["config"] != ".go-tops/golangci.yml" {
		t.Errorf("gates.hygiene.golangci_lint.config = %v, want .go-tops/golangci.yml", lint["config"])
	}
	kit := asMap(t, decodeYAML(t, read(t, dir, ".go-tops/golangci.yml")))
	if got := asMap(t, kit["run"])["relative-path-mode"]; got != "gomod" {
		t.Errorf("run.relative-path-mode = %v, want gomod: finding paths are relative to the module, and an anchored exclusion applies only under gomod", got)
	}
	if asMap(t, asMap(t, kit["linters"])["settings"])["errcheck"] == nil {
		t.Error("the kit config lost its errcheck settings")
	}
}

func TestInitNeverNamesAnOwnerLintFileAsGateFile(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	if strings.Contains(read(t, dir, ".github/CODEOWNERS"), "/.golangci.yml") {
		t.Error("CODEOWNERS names /.golangci.yml, a file the gate no longer reads")
	}
}

// pinnedToolsMod is a tools/go.mod made as §4.2 says: go get -tool for gotops at a
// pseudo-version, one more tool of the adopter's, a comment and a replace.
const pinnedToolsMod = `module github.com/acme/demo/tools

go 1.26

// kept: the adopter's own words
tool github.com/Hedronite/go-tops/cmd/gotops
tool example.com/other/cmd/lint

require (
	github.com/Hedronite/go-tops v0.0.0-20261119101010-1a34736abcde
	example.com/other v1.2.3 // indirect
)

replace example.com/other => ../other
`

// F3: init adds its pins to an existing tools/go.mod and never drops a line.
func TestInitMergesIntoAnExistingToolsMod(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	write(t, dir, "tools/go.mod", pinnedToolsMod)
	o := opts(dir, "binary-cli")
	o.KitVersion = "v0.0.0-dev" // a build with no release of its own
	code, out, errs := run(o)
	if code != ExitOK {
		t.Fatalf("init refused a tools/go.mod that pins gotops: exit %d\n%s%s", code, out, errs)
	}
	got := read(t, dir, "tools/go.mod")
	if !strings.HasPrefix(got, strings.TrimRight(pinnedToolsMod, "\n")) {
		t.Errorf("a line of the existing tools/go.mod was changed or dropped:\n%s", got)
	}
	for _, want := range []string{
		"\tgotest.tools/gotestsum\n", "gotest.tools/gotestsum v1.13.0",
		"\tgolang.org/x/vuln/cmd/govulncheck\n", "golang.org/x/vuln v1.9.0",
		"\tgithub.com/google/go-licenses/v2\n", "github.com/google/go-licenses/v2 v2.0.1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the kit's pin %q is missing:\n%s", want, got)
		}
	}
	if strings.Count(got, "github.com/Hedronite/go-tops/cmd/gotops") != 1 {
		t.Errorf("the gotops tool line was duplicated or dropped:\n%s", got)
	}
	record := read(t, dir, "GO-TOPS.md")
	if !strings.Contains(record, "v0.0.0-20261119101010-1a34736abcde") || strings.Contains(record, "not pinned") {
		t.Errorf("the adoption record does not name the pin tools/go.mod holds:\n%s", record)
	}
	if strings.Contains(out, "not a release") {
		t.Errorf("init says gotops is not pinned, but tools/go.mod pins it:\n%s", out)
	}
}

func TestInitAddsNothingToAToolsModThatHasTheKitPins(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	first := read(t, dir, "tools/go.mod")
	force := opts(dir, "binary-cli")
	force.Force = true
	if code, _, errs := run(force); code != ExitOK {
		t.Fatal(errs)
	}
	if again := read(t, dir, "tools/go.mod"); again != first {
		t.Errorf("a second init changed tools/go.mod:\n%s\n---\n%s", first, again)
	}
}

func TestInitRefusesAKitToolPinnedAtAnotherVersion(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	const mine = "module github.com/acme/demo/tools\n\ngo 1.26\n\ntool golang.org/x/vuln/cmd/govulncheck\n\nrequire golang.org/x/vuln v1.0.0\n"
	write(t, dir, "tools/go.mod", mine)
	before := listing(t, dir)
	code, _, errs := run(opts(dir, "binary-cli"))
	if code != ExitRefused || !strings.Contains(errs, "golang.org/x/vuln") || !strings.Contains(errs, "v1.9.0") {
		t.Fatalf("exit %d, want %d naming the module and the kit's version:\n%s", code, ExitRefused, errs)
	}
	if after := listing(t, dir); strings.Join(after, " ") != strings.Join(before, " ") || read(t, dir, "tools/go.mod") != mine {
		t.Errorf("a refusal wrote files: %v", after)
	}
}

// F5: init writes the .gitignore block itself, so a git add -A after a gate run
// commits the ledger and the ledgers beside it and nothing the gate wrote.
func TestInitWritesAGitignoreBlockThatKeepsTheLedgersTracked(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := newModule(t, "github.com/acme/demo", "1.26")
	write(t, dir, ".gitignore", "/bin/\n*.log\n")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	text := read(t, dir, ".gitignore")
	if !strings.HasPrefix(text, "/bin/\n*.log\n") {
		t.Errorf("the adopter's lines were changed:\n%s", text)
	}
	gitIn(t, dir, "init", "-q")
	ignored := map[string]bool{
		".go-tops/bin/golangci-lint": true, ".go-tops/report.json": true, ".go-tops/lint-style.json": true, ".go-tops/cover.out": true,
		".go-tops/baseline.json": false, ".go-tops/acks.yaml": false, ".go-tops/survivors.yaml": false, ".go-tops/golangci.yml": false,
	}
	for rel, want := range ignored {
		write(t, dir, rel, "x\n")
		err := exec.CommandContext(t.Context(), "git", "-C", dir, "check-ignore", "-q", rel).Run()
		if got := err == nil; got != want {
			t.Errorf("%s: ignored = %v, want %v", rel, got, want)
		}
	}
}

func TestGitignoreBlockIsReplacedNotRepeated(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	write(t, dir, ".gitignore", "before\n")
	force := opts(dir, "binary-cli")
	for range 2 {
		if code, _, errs := run(force); code != ExitOK {
			t.Fatal(errs)
		}
		force.Force = true
	}
	text := read(t, dir, ".gitignore")
	if strings.Count(text, "# go-tops:begin") != 1 || strings.Count(text, "# go-tops:end") != 1 || !strings.HasPrefix(text, "before\n") {
		t.Errorf("want the adopter's line and one block:\n%s", text)
	}
	write(t, dir, ".gitignore", "before\n# go-tops:begin\nno end\n")
	if code, _, errs := run(force); code != ExitRefused || !strings.Contains(errs, ".gitignore") {
		t.Errorf("a marker without its pair: exit %d, want %d naming .gitignore:\n%s", code, ExitRefused, errs)
	}
}

func TestInitNoLongerAsksForAGitignoreByHand(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	_, out, _ := run(opts(dir, "binary-cli"))
	if strings.Contains(out, "to .gitignore") {
		t.Errorf("init still tells the adopter to edit .gitignore, which it writes:\n%s", out)
	}
}

// F6: the binary goes outside the tree for a repository whose tests walk it.
func TestRecordAndNotesSayTheLintBinaryGoesOutsideTheTree(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	_, out, _ := run(opts(dir, "binary-cli"))
	for name, text := range map[string]string{"the init notes": out, "GO-TOPS.md": read(t, dir, "GO-TOPS.md")} {
		for _, want := range []string{"outside", "PATH", "walk"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s lack %q on where golangci-lint goes", name, want)
			}
		}
	}
}

// F7: the floor is the release the go line names.
func TestFloorKeepsThePatchReleaseOfTheGoLine(t *testing.T) {
	for _, c := range []struct {
		goLine, floor, current string
		leg                    string
	}{
		{"1.27.0", "1.27.0", "1.27.4", "go1.27.0|go1.27.0.*) ;;"},
		{"1.26.4", "1.26.4", "1.27.4", "go1.26.4|go1.26.4.*) ;;"},
		{"1.26", "1.26", "1.27", "go1.26|go1.26.*) ;;"},
	} {
		t.Run(c.goLine, func(t *testing.T) {
			dir := newModule(t, "github.com/acme/demo", c.goLine)
			if code, out, errs := run(opts(dir, "binary-cli")); code != ExitOK {
				t.Fatalf("exit %d\n%s%s", code, out, errs)
			}
			tc := asMap(t, asMap(t, decodeYAML(t, read(t, dir, "go-tops.yaml")))["toolchain"])
			if tc["go_floor"] != c.floor || tc["go_current"] != c.current {
				t.Errorf("toolchain %v, want go_floor %s and go_current %s", tc, c.floor, c.current)
			}
			if !strings.Contains(read(t, dir, ".github/workflows/go-tops.yml"), c.leg) {
				t.Errorf("the floor leg does not guard the release %s", c.floor)
			}
			write(t, dir, ".go-tops/baseline.json", emptyLedger)
			gitIn(t, dir, "init", "-q")
			gitIn(t, dir, "add", "-A")
			gitIn(t, dir, "commit", "-q", "-m", "adopt")
			if res := checkDir(t, dir); len(res.Issues) != 0 {
				t.Errorf("check refuses what init wrote for go %s: %v", c.goLine, res.Issues)
			}
		})
	}
}

func TestAGoLineAboveThePinnedToolchainIsRefused(t *testing.T) {
	for _, line := range []string{"1.27.9", "1.28", "1.28.0"} {
		dir := newModule(t, "github.com/acme/demo", line)
		if code, _, errs := run(opts(dir, "binary-cli")); code != ExitRefused || !strings.Contains(errs, "above the pinned toolchain") {
			t.Errorf("go %s: exit %d, want %d:\n%s", line, code, ExitRefused, errs)
		}
	}
}

// F8: a CODEOWNERS owner taken from the module path may be a bare organisation.
func TestOwnerFromTheModulePathIsFlaggedAndAnExplicitOwnerIsNot(t *testing.T) {
	dir := newModule(t, "github.com/Acme/demo", "1.26")
	_, out, _ := run(opts(dir, "binary-cli"))
	if !strings.Contains(out, "@Acme") || !strings.Contains(out, "organisation") || !strings.Contains(out, "--owner") || !strings.Contains(out, "codeowners/errors?ref=<branch>") {
		t.Errorf("a derived owner is not flagged:\n%s", out)
	}
	dir = newModule(t, "github.com/Acme/demo", "1.26")
	o := opts(dir, "binary-cli")
	o.Owner = "@Acme/platform"
	_, out, _ = run(o)
	if strings.Contains(out, "organisation") {
		t.Errorf("an explicit owner is flagged:\n%s", out)
	}
}

// F9: the job is bounded.
func TestWorkflowJobHasATimeout(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	if !strings.Contains(read(t, dir, ".github/workflows/go-tops.yml"), "\n    timeout-minutes: 30\n") {
		t.Error("the gate job has no timeout-minutes")
	}
}

// F11: gotops has no laws command until M5, so nothing init writes says to run one.
func TestNoGeneratedFileDocumentsALawsCommand(t *testing.T) {
	for _, class := range launchClasses {
		t.Run(class, func(t *testing.T) {
			dir := newModule(t, "github.com/acme/demo", "1.26")
			if code, _, errs := run(opts(dir, class)); code != ExitOK {
				t.Fatal(errs)
			}
			for _, rel := range listing(t, dir) {
				if rel == "go.mod" {
					continue
				}
				text := read(t, dir, rel)
				if strings.Contains(text, "gotops laws") {
					t.Errorf("%s documents gotops laws, which exits 2 as an unknown command", rel)
				}
				if rel == "justfile" && strings.Contains(text, "laws:") {
					t.Errorf("the justfile has a laws recipe")
				}
			}
		})
	}
}

// F11 at the source: no template the binary embeds documents a laws command.
func TestNoEmbeddedTemplateNamesALawsCommand(t *testing.T) {
	seen := 0
	err := fs.WalkDir(templates, "templates", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := templates.ReadFile(p)
		if err != nil {
			return err
		}
		seen++
		bad := []string{"gotops laws", "{{gotops}} laws"}
		if p == "templates/justfile" {
			bad = append(bad, "\nlaws:") // a recipe; go-tops.yaml has a laws key, which is config
		}
		for _, b2 := range bad {
			if strings.Contains(string(b), b2) {
				t.Errorf("%s names a laws command (%q), which exits 2 until the laws layer lands", p, b2)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen < 6 {
		t.Errorf("only %d templates were read", seen)
	}
}

// F7: go_current names the pinned release when the floor names a release, so the
// pair passes check; the language version otherwise.
func TestGoCurrentNamesThePinnedReleaseForAReleaseFloor(t *testing.T) {
	for floor, want := range map[string]string{"1.26": "1.27", "1.27": "1.27", "1.26.4": "1.27.4", "1.27.0": "1.27.4"} {
		if got := goCurrent(floor); got != want {
			t.Errorf("goCurrent(%q) = %q, want %q", floor, got, want)
		}
	}
}
