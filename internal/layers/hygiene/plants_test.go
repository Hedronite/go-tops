package hygiene

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/gate"
)

// The plants run the real tools (golangci-lint on PATH, govulncheck and
// go-licenses from tools/go.mod) against small modules written into a temp
// directory. Each plant breaks one check of §3.1 gate 1 and the test asserts
// that exactly that step fails and every other step passes. A tool that is not
// there is a failing test, not a skip: a plant that did not run proved nothing.

const (
	mainOK = "package main\n\nfunc main() {}\n"

	golangciYML = `version: "2"
linters:
  default: none
  enable: [errcheck]
formatters:
  enable: [gofmt]
`
)

// vulnDB writes a Go vulnerability database that marks net/http.ListenAndServe
// in the standard library as affected before a release that does not exist, so
// any module calling it has a reachable finding and a module that only imports
// net/http has an unreached one.
func vulnDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	const ts = "2099-01-02T03:04:05Z"
	files := map[string]string{
		"index/db.json":      `{"modified":"` + ts + `"}`,
		"index/modules.json": `[{"path":"stdlib","vulns":[{"id":"GO-2099-0001","modified":"` + ts + `","fixed":"1.99.0"}]}]`,
		"index/vulns.json":   `[{"id":"GO-2099-0001","modified":"` + ts + `","aliases":[]}]`,
		"ID/GO-2099-0001.json": `{"schema_version":"1.3.1","id":"GO-2099-0001","modified":"` + ts + `","published":"2099-01-01T00:00:00Z",` +
			`"summary":"planted","details":"A vulnerability planted for the tests.",` +
			`"affected":[{"package":{"name":"stdlib","ecosystem":"Go"},"ranges":[{"type":"SEMVER","events":[{"introduced":"0"},{"fixed":"1.99.0"}]}],` +
			`"ecosystem_specific":{"imports":[{"path":"net/http","symbols":["ListenAndServe"]}]}}],` +
			`"database_specific":{"url":"https://pkg.go.dev/vuln/GO-2099-0001"}}`,
	}
	for name, body := range files {
		write(t, filepath.Join(dir, name), body)
	}
	return "file://" + dir
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func goVersion(t *testing.T) string {
	t.Helper()
	b, err := exec.CommandContext(t.Context(), "go", "env", "GOVERSION").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	write(t, to, string(b))
}

// module is a plant: the files of a small module, over the clean one.
type module map[string]string

// plant builds the module in a temp dir and returns the layer's result. The
// clean base is a formatted, tidy, lint-clean module with no dependency, a
// matching toolchain pin and the repository's own tools/go.mod.
func plant(t *testing.T, over module, cfg gate.HygieneConfig) gate.Result {
	t.Helper()
	dir := t.TempDir()
	files := module{
		"go.mod":        "module example.com/app\n\ngo 1.26\n",
		"main.go":       mainOK,
		".golangci.yml": golangciYML,
		"go-tops.lock":  strings.Replace(lockText, "go1.27.4", goVersion(t), 1),
	}
	for name, body := range over {
		if body == "-" {
			delete(files, name)
			continue
		}
		files[name] = body
	}
	for name, body := range files {
		write(t, filepath.Join(dir, name), body)
	}
	root := filepath.Join("..", "..", "..", "tools")
	copyFile(t, filepath.Join(root, "go.mod"), filepath.Join(dir, "tools", "go.mod"))
	copyFile(t, filepath.Join(root, "go.sum"), filepath.Join(dir, "tools", "go.sum"))
	cfg.LintConfig = ".golangci.yml"
	if len(cfg.Licenses) == 0 {
		cfg.Licenses = []string{"MIT", "Apache-2.0"}
	}
	if cfg.VulnDB == "" {
		cfg.VulnDB = vulnDB(t)
	}
	ex := gate.OS{}
	env := &gate.Env{
		Dir:    dir,
		Config: gate.Config{Module: "example.com/app", Hygiene: cfg},
		Exec:   ex,
		Tools:  &gate.ToolResolver{Dir: dir, Exec: ex, Binary: gate.DefaultBinaries()},
	}
	return Layer{}.Run(context.Background(), env)
}

func TestPlantsCleanModulePasses(t *testing.T) {
	r := plant(t, nil, gate.HygieneConfig{})
	if r.Outcome != gate.Pass {
		t.Fatalf("a clean module must pass, or the plants prove nothing: %s", r.Reason)
	}
	if r.VulnDBTimestamp != "2099-01-02T03:04:05Z" {
		t.Errorf("the database timestamp of the fixture, got %q", r.VulnDBTimestamp)
	}
	if r.Details["findings"] != 0 {
		t.Errorf("details %v", r.Details)
	}
}

// 1. an unformatted file
func TestPlantUnformattedFile(t *testing.T) {
	r := plant(t, module{"main.go": "package main\n\nfunc main()  {}\n"}, gate.HygieneConfig{})
	// golangci-lint run reports the same gofmt finding when the formatter is
	// enabled, so the lint step fails with it; nothing else does.
	if msg := failsOnly(t, r, "golangci-lint fmt --diff", "golangci-lint run"); !strings.Contains(msg, "main.go") {
		t.Errorf("message %q does not name main.go", msg)
	}
}

// 2. a lint finding
func TestPlantLintFinding(t *testing.T) {
	r := plant(t, module{"main.go": "package main\n\nimport \"os\"\n\nfunc main() {\n\tos.Remove(\"x\")\n}\n"}, gate.HygieneConfig{})
	if msg := onlyFails(t, r, "golangci-lint run"); !strings.Contains(msg, "errcheck") {
		t.Errorf("message %q does not name the linter", msg)
	}
	if r.Details["findings"] != 1 {
		t.Errorf("findings %v, want 1", r.Details["findings"])
	}
}

// 3. an untidy go.mod: a requirement nothing imports
func TestPlantUntidyGoMod(t *testing.T) {
	r := plant(t, module{
		"go.mod":        "module example.com/app\n\ngo 1.26\n\nrequire example.com/dep v1.0.0\n\nreplace example.com/dep => ./dep\n",
		"dep/go.mod":    "module example.com/dep\n\ngo 1.26\n",
		"dep/dep.go":    "package dep\n",
		"dep/LICENSE":   "MIT License\n",
		".golangci.yml": golangciYML,
	}, gate.HygieneConfig{})
	if msg := onlyFails(t, r, "go mod tidy -diff"); !strings.Contains(msg, "example.com/dep") {
		t.Errorf("message %q does not show the diff", msg)
	}
}

// 4. a reachable vulnerability, from a fixture database read with -db file://
func TestPlantReachableVulnerability(t *testing.T) {
	r := plant(t, module{"main.go": "package main\n\nimport \"net/http\"\n\nfunc main() {\n\tif err := http.ListenAndServe(\":0\", nil); err != nil {\n\t\tpanic(err)\n\t}\n}\n"}, gate.HygieneConfig{})
	if msg := onlyFails(t, r, "govulncheck -json"); !strings.Contains(msg, "GO-2099-0001") || !strings.Contains(msg, "1.99.0") {
		t.Errorf("message %q", msg)
	}
	if r.VulnDBTimestamp != "2099-01-02T03:04:05Z" {
		t.Errorf("the report must carry the database timestamp, got %q", r.VulnDBTimestamp)
	}
}

// fail_on: reachable: the same module importing net/http without calling the
// affected symbol is recorded and passes.
func TestPlantUnreachedVulnerabilityPasses(t *testing.T) {
	r := plant(t, module{"main.go": "package main\n\nimport \"net/http\"\n\nfunc main() { _ = http.StatusText(200) }\n"}, gate.HygieneConfig{FailOn: "reachable"})
	if r.Outcome != gate.Pass {
		t.Fatalf("an unreached finding must not fail under fail_on: reachable: %s", r.Reason)
	}
	v, ok := r.Details["vuln"].(map[string]any)
	if !ok || v["other"] != 1 {
		t.Errorf("vuln details %v, want the unreached finding counted", r.Details["vuln"])
	}
}

// 5. a disallowed license: a dependency licensed Apache-2.0 against an allow
// list of MIT alone; with Apache-2.0 allowed the same module passes.
func TestPlantDisallowedLicense(t *testing.T) {
	apache, err := os.ReadFile(filepath.Join("..", "..", "..", "LICENSE-APACHE"))
	if err != nil {
		t.Fatal(err)
	}
	mod := module{
		"go.mod":      "module example.com/app\n\ngo 1.26\n\nrequire example.com/dep v1.0.0\n\nreplace example.com/dep => ./dep\n",
		"main.go":     "package main\n\nimport \"example.com/dep\"\n\nfunc main() { dep.F() }\n",
		"dep/go.mod":  "module example.com/dep\n\ngo 1.26\n",
		"dep/dep.go":  "package dep\n\n// F does nothing.\nfunc F() {}\n",
		"dep/LICENSE": string(apache),
	}
	r := plant(t, mod, gate.HygieneConfig{Licenses: []string{"MIT"}})
	if msg := onlyFails(t, r, "go-licenses check"); !strings.Contains(msg, "example.com/dep") {
		t.Errorf("message %q does not name the dependency", msg)
	}
	if r := plant(t, mod, gate.HygieneConfig{Licenses: []string{"MIT", "Apache-2.0"}}); r.Outcome != gate.Pass {
		t.Fatalf("with Apache-2.0 allowed the same module must pass: %s", r.Reason)
	}
}

// 6. a toolchain mismatch: the lock pins a release the host is not running
func TestPlantToolchainMismatch(t *testing.T) {
	r := plant(t, module{"go-tops.lock": strings.Replace(lockText, "go1.27.4", "go1.20.1", 1)}, gate.HygieneConfig{})
	if msg := onlyFails(t, r, "go env GOVERSION"); !strings.Contains(msg, "go1.20.1") {
		t.Errorf("message %q does not name the pin", msg)
	}
}

// An empty lint JSON fails, here with the real tool: the config names a
// linter that does not exist, so golangci-lint writes no result.
func TestPlantLintThatSavesNothing(t *testing.T) {
	r := plant(t, module{".golangci.yml": "version: \"2\"\nlinters:\n  default: none\n  enable: [nosuchlinter]\n"}, gate.HygieneConfig{})
	if r.Outcome != gate.Fail {
		t.Fatalf("outcome %s, want fail", r.Outcome)
	}
	if got := steps(t, r)["golangci-lint run"]["outcome"]; got != "fail" {
		t.Errorf("lint step %v, want fail", got)
	}
}
