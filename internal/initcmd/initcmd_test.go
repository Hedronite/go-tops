package initcmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"

	gotops "github.com/Hedronite/go-tops"
)

// The oracle is GO_TOPS.md §12.1 (init), §4.1 (the drop-in set), §10 (the
// lock) and §13 (the adoption record), never the code in this package. The
// kit's own files are two levels up.

const repoRoot = "../.."

// newModule makes a temp Go module and returns its directory.
func newModule(t *testing.T, module, goLine string) string {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, "go.mod", "module "+module+"\n\ngo "+goLine+"\n")
	return dir
}

func write(t *testing.T, dir, rel, data string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func opts(dir, class string) Options {
	return Options{Class: class, Dir: dir, Offline: true, Assets: os.DirFS(repoRoot), KitVersion: "v0.1.0"}
}

// printer collects what init prints.
type printer struct{ strings.Builder }

func (p *printer) Printf(format string, a ...any) { fmt.Fprintf(&p.Builder, format, a...) }

func run(o Options) (int, string, string) {
	var out, errp printer
	code := Run(o, &out, &errp)
	return code, out.String(), errp.String()
}

// asMap is a checked type assertion for decoded YAML.
func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("want a mapping, got %T", v)
	}
	return m
}

// listing returns every file under dir, relative, sorted.
func listing(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

// The file set of §12.1 and §4.1, for every launch class, and no baseline.
func TestInitWritesTheDropInSet(t *testing.T) {
	want := []string{
		".github/CODEOWNERS", ".github/workflows/go-tops.yml", ".gitignore", ".go-tops/golangci.yml", "AGENTS.md", "GO-TOPS.md",
		"go-tops.lock", "go-tops.yaml", "go.mod", "justfile", "tools/go.mod",
	}
	for _, class := range launchClasses {
		t.Run(class, func(t *testing.T) {
			dir := newModule(t, "github.com/acme/demo", "1.26")
			if code, out, errs := run(opts(dir, class)); code != ExitOK {
				t.Fatalf("exit %d\n%s%s", code, out, errs)
			}
			if got := listing(t, dir); !reflect.DeepEqual(got, want) {
				t.Fatalf("files %v, want %v", got, want)
			}
		})
	}
}

func TestInitNeverWritesABaseline(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	for _, f := range listing(t, dir) {
		if strings.Contains(f, "baseline") || strings.HasPrefix(f, ".go-tops/") && f != ".go-tops/golangci.yml" {
			t.Errorf("init wrote %s", f)
		}
	}
}

// go-tops.lock is written from the kit's pins and says what the protocol's own
// example says (§10).
func TestLockEqualsTheExampleAndValidates(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	got, want := decodeYAML(t, read(t, dir, "go-tops.lock")), decodeYAML(t, readRepo(t, "config/go-tops.example.lock"))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the embedded pins and config/go-tops.example.lock differ:\n got %v\nwant %v", got, want)
	}
	validate(t, "schema/lock.schema.json", got)
}

func readRepo(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func decodeYAML(t *testing.T, text string) any {
	t.Helper()
	var v any
	if err := yaml.Unmarshal([]byte(text), &v); err != nil {
		t.Fatalf("invalid YAML: %v\n%s", err, text)
	}
	return v
}

func validate(t *testing.T, schemaRel string, inst any) {
	t.Helper()
	if err := schemaError(schemaRel, inst); err != nil {
		t.Fatalf("does not validate against %s: %v", schemaRel, err)
	}
}

func schemaError(schemaRel string, inst any) error {
	file, fragment, _ := strings.Cut(schemaRel, "#")
	abs, err := filepath.Abs(filepath.Join(repoRoot, filepath.FromSlash(file)))
	if err != nil {
		return err
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	url := "file://" + filepath.ToSlash(abs)
	if fragment != "" {
		url += "#" + fragment
	}
	sch, err := c.Compile(url)
	if err != nil {
		return err
	}
	j, err := json.Marshal(inst)
	if err != nil {
		return err
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(j))
	if err != nil {
		return err
	}
	return sch.Validate(doc)
}

// go-tops.yaml validates against the schema and carries the class profile's
// numbers: the floor and the cap are the profile's, so check finds neither
// above nor below the class column (§5.2, §12.1).
func TestConfigValidatesAndCarriesTheProfile(t *testing.T) {
	cases := map[string]struct{ floor, cap float64 }{
		"binary-cli": {70, 10}, "binary-service": {75, 10}, "library-core": {85, 8},
	}
	for class, want := range cases {
		t.Run(class, func(t *testing.T) {
			dir := newModule(t, "github.com/acme/demo", "1.26.4")
			if code, _, errs := run(opts(dir, class)); code != ExitOK {
				t.Fatal(errs)
			}
			cfg := asMap(t, decodeYAML(t, read(t, dir, "go-tops.yaml")))
			validate(t, "schema/go-tops.schema.json", cfg)
			gates := asMap(t, cfg["gates"])
			if got := asMap(t, gates["coverage"])["statements_floor"]; fmt.Sprint(got) != fmt.Sprint(want.floor) {
				t.Errorf("floor %v, want %v", got, want.floor)
			}
			if got := asMap(t, gates["crap"])["cap"]; fmt.Sprint(got) != fmt.Sprint(want.cap) {
				t.Errorf("cap %v, want %v", got, want.cap)
			}
			if cfg["module"] != "github.com/acme/demo" || cfg["class"] != class || cfg["enforce"] != "ratchet" || cfg["protocol"] != mustProtocol(t) {
				t.Errorf("header keys wrong: %v", cfg)
			}
			tc := asMap(t, cfg["toolchain"])
			if tc["go_floor"] != "1.26.4" || tc["go_current"] != "1.27.4" {
				t.Errorf("toolchain %v: the floor is the release the go line names, current is the pin", tc)
			}
		})
	}
}

// §12.1, §3.3, §6.4: init turns CRAP on at the class cap and mutation on with no
// survivor allowed: neither has an off switch's fields. The other layers init
// leaves off carry a reason; gates 1 to 3 have no enabled key.
func TestCrapAndMutationAreOn(t *testing.T) {
	for _, class := range launchClasses {
		t.Run(class, func(t *testing.T) {
			dir := newModule(t, "github.com/acme/demo", "1.26")
			if code, _, errs := run(opts(dir, class)); code != ExitOK {
				t.Fatal(errs)
			}
			cfg := asMap(t, decodeYAML(t, read(t, dir, "go-tops.yaml")))
			gates := asMap(t, cfg["gates"])
			crap := asMap(t, gates["crap"])
			if crap["enabled"] != true || crap["reason"] != nil || crap["expires"] != nil || crap["cap"] == nil {
				t.Errorf("crap must be on with the class cap and no off-switch fields: %v", crap)
			}
			m := asMap(t, gates["mutation"])
			if m["enabled"] != true || m["reason"] != nil || m["expires"] != nil {
				t.Errorf("mutation must be on with no off-switch fields: %v", m)
			}
			if fmt.Sprint(m["unexplained_survivors_max"]) != "0" || m["ledger"] != ".go-tops/survivors.yaml" {
				t.Errorf("mutation allows no unexplained survivor and names its ledger: %v", m)
			}
			for _, g := range []string{"hygiene", "tests", "coverage"} {
				if _, has := asMap(t, gates[g])["enabled"]; has {
					t.Errorf("gate %s has an enabled key: gates 1 to 3 cannot be off", g)
				}
			}
			for name, l := range asMap(t, cfg["layers"]) {
				m := asMap(t, l)
				if m["enabled"] != false || fmt.Sprint(m["reason"]) == "" || m["reason"] == nil {
					t.Errorf("layer %s must be off with a reason: %v", name, m)
				}
			}
		})
	}
}

// An existing file is never overwritten without --force, and a refusal writes
// nothing at all (§12.1).
func TestExistingFilesAreRefusedWithoutForce(t *testing.T) {
	for _, victim := range []string{"go-tops.yaml", "go-tops.lock", ".go-tops/golangci.yml", ".github/workflows/go-tops.yml", ".github/CODEOWNERS", "justfile", "GO-TOPS.md"} {
		t.Run(victim, func(t *testing.T) {
			dir := newModule(t, "github.com/acme/demo", "1.26")
			write(t, dir, victim, "mine\n")
			before := listing(t, dir)
			code, _, errs := run(opts(dir, "binary-cli"))
			if code != ExitRefused || !strings.Contains(errs, victim) {
				t.Fatalf("exit %d, want %d naming %s:\n%s", code, ExitRefused, victim, errs)
			}
			if after := listing(t, dir); !reflect.DeepEqual(before, after) || read(t, dir, victim) != "mine\n" {
				t.Fatalf("a refusal changed the tree: %v -> %v", before, after)
			}
			force := opts(dir, "binary-cli")
			force.Force = true
			if code, _, errs := run(force); code != ExitOK || read(t, dir, victim) == "mine\n" {
				t.Fatalf("--force must overwrite: exit %d\n%s", code, errs)
			}
		})
	}
}

func TestSecondInitWithoutForceRefuses(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	if code, _, _ := run(opts(dir, "binary-cli")); code != ExitRefused {
		t.Fatalf("exit %d, want %d", code, ExitRefused)
	}
}

func TestUsageAndInputRefusals(t *testing.T) {
	cases := []struct {
		name   string
		module string
		goLine string
		mod    func(*Options)
		want   string
	}{
		{"class-without-profile", "github.com/acme/demo", "1.26", func(o *Options) { o.Class = "tui-app" }, "no profile"},
		{"unknown-class", "github.com/acme/demo", "1.26", func(o *Options) { o.Class = "web-app" }, "no profile"},
		{"go-line-below-floor", "github.com/acme/demo", "1.25", nil, "protocol floor"},
		{"go-line-above-pin", "github.com/acme/demo", "1.28", nil, "above the pinned toolchain"},
		{"no-owner-off-github", "example.com/acme/demo", "1.26", nil, "--owner"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := newModule(t, c.module, c.goLine)
			o := opts(dir, "binary-cli")
			if c.mod != nil {
				c.mod(&o)
			}
			code, _, errs := run(o)
			if code != ExitRefused || !strings.Contains(errs, c.want) {
				t.Fatalf("exit %d, want %d with %q:\n%s", code, ExitRefused, c.want, errs)
			}
			if got := listing(t, dir); !reflect.DeepEqual(got, []string{"go.mod"}) {
				t.Fatalf("a refusal wrote files: %v", got)
			}
		})
	}
	t.Run("no-go-mod", func(t *testing.T) {
		dir := t.TempDir()
		if code, _, errs := run(opts(dir, "binary-cli")); code != ExitRefused || !strings.Contains(errs, "go.mod") {
			t.Fatalf("exit %d:\n%s", code, errs)
		}
	})
	t.Run("owner-flag-off-github", func(t *testing.T) {
		dir := newModule(t, "example.com/acme/demo", "1.26")
		o := opts(dir, "binary-cli")
		o.Owner = "platform"
		if code, _, errs := run(o); code != ExitOK {
			t.Fatal(errs)
		}
		if !strings.Contains(read(t, dir, ".github/CODEOWNERS"), "/go-tops.lock") || !strings.Contains(read(t, dir, ".github/CODEOWNERS"), "@platform") {
			t.Error("the CODEOWNERS fragment lacks the owner")
		}
	})
}

var placeholderRe = regexp.MustCompile(`\{[A-Z][A-Z0-9_]*\}`)

// No placeholder survives into a written file.
func TestNoPlaceholderIsLeftOver(t *testing.T) {
	for _, class := range launchClasses {
		dir := newModule(t, "github.com/acme/demo", "1.26")
		if code, _, errs := run(opts(dir, class)); code != ExitOK {
			t.Fatal(errs)
		}
		for _, f := range listing(t, dir) {
			if m := placeholderRe.FindString(read(t, dir, f)); m != "" {
				t.Errorf("%s: %s left over in %s", class, m, f)
			}
		}
	}
}

// The CODEOWNERS fragment covers every file that can weaken the gate (§11).
func TestCodeownersCoversTheGateFiles(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	co := read(t, dir, ".github/CODEOWNERS")
	for _, p := range []string{"/go-tops.yaml", "/go-tops.lock", "/.go-tops/", "/tools/", "/.github/workflows/go-tops.yml"} {
		if !regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(p) + `\s+@acme$`).MatchString(co) {
			t.Errorf("CODEOWNERS has no line for %s with owner @acme:\n%s", p, co)
		}
	}
}

// The workflow is the §11 drop-in: read-only, every action pinned by commit
// SHA, full history, and the one command a developer runs.
func TestWorkflowIsThePinnedDropIn(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	text := read(t, dir, ".github/workflows/go-tops.yml")
	var wf struct {
		Permissions map[string]string `yaml:"permissions"`
		Jobs        map[string]struct {
			Steps []struct {
				Uses string `yaml:"uses"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(text), &wf); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(wf.Permissions, map[string]string{"contents": "read"}) {
		t.Errorf("permissions %v, want contents: read only", wf.Permissions)
	}
	pin := regexp.MustCompile(`^[^@\s]+@[0-9a-f]{40}$`)
	for name, j := range wf.Jobs {
		for _, s := range j.Steps {
			if s.Uses != "" && !pin.MatchString(s.Uses) {
				t.Errorf("job %s: %q is not pinned by a 40-hex commit SHA", name, s.Uses)
			}
		}
	}
	for _, want := range []string{"fetch-depth: 0", "go tool -modfile=tools/go.mod gotops gate", "macos-latest", "go: \"1.26\"", "go: \"1.27.4\""} {
		if !strings.Contains(text, want) {
			t.Errorf("workflow lacks %q", want)
		}
	}
	if _, has := wf.Jobs["gate"]; !has || len(wf.Jobs) != 1 {
		t.Errorf("want one job named gate (GO_TOPS.md section 16 item 26), got %v", wf.Jobs)
	}
}

// tools/go.mod pins the module tools of §2 and, for a release build, gotops.
func TestToolsModPinsTheModuleTools(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26.4")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	mod := read(t, dir, "tools/go.mod")
	for _, want := range []string{
		"module github.com/acme/demo/tools", "go 1.26.4\n",
		"\tgotest.tools/gotestsum\n", "gotest.tools/gotestsum v1.13.0",
		"\tgolang.org/x/vuln/cmd/govulncheck\n", "golang.org/x/vuln v1.9.0",
		"\tgithub.com/google/go-licenses/v2\n", "github.com/google/go-licenses/v2 v2.0.1",
		"\tgithub.com/Hedronite/go-tops/cmd/gotops\n", "github.com/Hedronite/go-tops v0.1.0",
	} {
		if !strings.Contains(mod, want) {
			t.Errorf("tools/go.mod lacks %q:\n%s", want, mod)
		}
	}
}

func TestDevelopmentBuildLeavesGotopsOutWithANote(t *testing.T) {
	for _, v := range []string{"", "0.0.0-dev", "v0.0.0-dev", "v0.1"} {
		dir := newModule(t, "github.com/acme/demo", "1.26")
		o := opts(dir, "binary-cli")
		o.KitVersion = v
		code, out, errs := run(o)
		if code != ExitOK || strings.Contains(read(t, dir, "tools/go.mod"), "go-tops") || !strings.Contains(out, "not a release") {
			t.Errorf("kit version %q: exit %d, out %q, err %q", v, code, out, errs)
		}
	}
}

// --offline writes no go.sum and runs no tidy; otherwise tidy runs once, and
// its failure is a failure of the run, not of the files (§12.1).
func TestTidyRunsUnlessOffline(t *testing.T) {
	var calls []string
	tidy := func(dir string) error { calls = append(calls, dir); return nil }
	dir := newModule(t, "github.com/acme/demo", "1.26")
	o := opts(dir, "binary-cli")
	o.Offline, o.Tidy = true, tidy
	if code, out, _ := run(o); code != ExitOK || len(calls) != 0 || !strings.Contains(out, "--offline") {
		t.Fatalf("offline: exit %d, tidy calls %v, out %q", code, calls, out)
	}
	dir2 := newModule(t, "github.com/acme/demo", "1.26")
	o2 := opts(dir2, "binary-cli")
	o2.Offline, o2.Tidy = false, tidy
	if code, _, _ := run(o2); code != ExitOK || !reflect.DeepEqual(calls, []string{dir2}) {
		t.Fatalf("online: tidy calls %v, want one in %s", calls, dir2)
	}
	dir3 := newModule(t, "github.com/acme/demo", "1.26")
	o3 := opts(dir3, "binary-cli")
	o3.Offline, o3.Tidy = false, func(context.Context, string) error { return fmt.Errorf("no network") }
	if code, _, errs := run(o3); code != ExitFailure || !strings.Contains(errs, "no network") {
		t.Fatalf("a tidy failure must exit %d, got %d:\n%s", ExitFailure, code, errs)
	}
}

// §13: init writes the adoption record with items 2 to 4 and 6 filled.
func TestAdoptionRecordFillsItems2To4And6(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	if code, _, errs := run(opts(dir, "library-core")); code != ExitOK {
		t.Fatal(errs)
	}
	rec := read(t, dir, "GO-TOPS.md")
	for _, want := range []string{
		"https://github.com/Hedronite/go-tops",
		"2. **Protocol version.** " + mustProtocol(t),
		"`gotops` v0.1.0", "toolchain go1.27.4", "golangci-lint 2.15.0", "govulncheck v1.9.0", "gotestsum v1.13.0", "go-licenses v2.0.1",
		"`library-core`: statement coverage floor 85%, CRAP cap 8",
		".go-tops/baseline.json", ".go-tops/survivors.yaml",
		"8. **Known deviations.** None.",
	} {
		if !strings.Contains(rec, want) {
			t.Errorf("GO-TOPS.md lacks %q:\n%s", want, rec)
		}
	}
	for i := 1; i <= 8; i++ {
		if !strings.Contains(rec, fmt.Sprintf("\n%d. **", i)) {
			t.Errorf("GO-TOPS.md has no item %d", i)
		}
	}
}

// mustProtocol is the protocol this binary implements: the constant of the
// embedded schema, which the version check keeps equal to GO_TOPS.md.
func mustProtocol(t *testing.T) string {
	t.Helper()
	p, err := gotops.Protocol()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// §12.1, §6.7: after init the next step is the first ledger: run
// gotops baseline --init on the ledger leg's build, then commit the file.
func TestInitPrintsTheBaselineNextStep(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	code, out, errs := run(opts(dir, "binary-cli"))
	if code != ExitOK {
		t.Fatal(errs)
	}
	var next string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "next: ") {
			next = l
		}
	}
	for _, want := range []string{"gotops baseline --init", "ledger leg", "commit", ".go-tops/baseline.json"} {
		if !strings.Contains(next, want) {
			t.Errorf("the next-step line %q lacks %q\n%s", next, want, out)
		}
	}
}

// GO-TOPS.md item 6 says where the baseline lives, who writes it, which leg
// is the ledger leg, how to write the first ledger and how to refresh it.
func TestAdoptionRecordItem6NamesTheLedgerLeg(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	rec := read(t, dir, "GO-TOPS.md")
	i := strings.Index(rec, "\n6. **")
	j := strings.Index(rec, "\n7. **")
	if i < 0 || j < i {
		t.Fatalf("no item 6 before item 7:\n%s", rec)
	}
	item := rec[i:j]
	for _, want := range []string{
		".go-tops/baseline.json", "only `gotops baseline` writes it", "ledger leg", "`ubuntu` leg",
		"`skip`", "gotops baseline --init", "workflow_dispatch", "go-tops-baseline", "gotops baseline`",
		"`init`", "`refresh`", "only on the ledger leg's build", "linux/amd64", "golang:1.", "git config --global --add safe.directory /repo", "needs network", "vulnerability database", "--accept-regression", "is manual", ".go-tops/survivors.yaml", ".go-tops/acks.yaml",
	} {
		if !strings.Contains(item, want) {
			t.Errorf("item 6 lacks %q:\n%s", want, item)
		}
	}
}
