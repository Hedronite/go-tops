package hygiene

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/lock"
)

// The oracle is GO_TOPS.md §3.1 (gate 1), §3.3 (outcomes), §3.4 rule 5 (no
// silent empties) and §12.2 (exit 2 for a lock that does not load).

const lockTextTemplate = `protocol: "@PROTOCOL@"
toolchain: go1.27.4
trust:
  mode: checksum
  note: copied from the upstream release checksum file
binaries:
  - name: golangci-lint
    version: "2.15.0"
    url: https://example.com/v{version}/golangci-lint-{version}-{os}-{arch}.tar.gz
    extract: golangci-lint-{version}-{os}-{arch}/golangci-lint
    sha256:
      darwin/arm64: d1e409481a8ca846c4bc97e781eb5cb1bd5c23353a0b3977a1cbda1b1b6447d5
      darwin/amd64: 767b899a28285e88ded2823d5dcc4bc5ef681173eb2d80f029a7b17ee4ece3b8
      linux/amd64: 9b7fe6a6bf6334b98decf8d86b11415ea7889afdabdd1959fdce23af2a9ebcea
      linux/arm64: b4d8f17068b35ee6060cc2f69a16fb48740d706fbba81661c318649c114a6998
    executable_sha256:
      darwin/arm64: 71b89744ef6dd92dad9026b70489d1ad660da79ef90dfb5aabe995d916bec8a9
      darwin/amd64: e04962742507a5f37a4002a7a8b2b549f6864f17807eb5d534d947f6311a051f
      linux/amd64: d9af4564c05beb2f5e130119aec45e5e89d28e5017d558d3ca382659f2427894
      linux/arm64: 14680d7674eb1fed0405446670e15dcffb74e979ef0dcd41f998c088fd6babfb
`

var lockText = strings.Replace(lockTextTemplate, "@PROTOCOL@", protocolID, 1)

const cleanStream = `{"config":{"protocol_version":"v1.0.0","scanner_name":"govulncheck","db_last_modified":"2026-11-17T09:12:44Z"}}
{"progress":{"message":"Scanning your code and 3 packages across 1 dependent module for known vulnerabilities..."}}
`

const reachableStream = cleanStream + `{"finding":{"osv":"GO-2026-0001","fixed_version":"v1.2.3","trace":[{"module":"stdlib","version":"v1.27.4"}]}}
{"finding":{"osv":"GO-2026-0001","fixed_version":"v1.2.3","trace":[{"module":"stdlib","version":"v1.27.4","package":"net/http"}]}}
{"finding":{"osv":"GO-2026-0001","fixed_version":"v1.2.3","trace":[{"module":"stdlib","package":"net/http","function":"ListenAndServe"},{"module":"example.com/app","function":"main"}]}}
`

const importedOnlyStream = cleanStream + `{"finding":{"osv":"GO-2026-0002","fixed_version":"v1.2.3","trace":[{"module":"stdlib","version":"v1.27.4"}]}}
{"finding":{"osv":"GO-2026-0002","fixed_version":"v1.2.3","trace":[{"module":"stdlib","version":"v1.27.4","package":"net/http"}]}}
`

// fake is a scripted Executor and tool resolver. Each command key is the argv
// without the tool prefix; a handler may replace the default healthy answer.
type fake struct {
	t        *testing.T
	dir      string
	calls    [][]string
	override map[string]func(argv []string) (gate.Output, error)
	missing  map[string]bool
	lintDoc  string // what `golangci-lint run` writes to the JSON path; "-" writes nothing
}

func newFake(t *testing.T) *fake {
	t.Helper()
	return &fake{t: t, dir: t.TempDir(), override: map[string]func([]string) (gate.Output, error){}, missing: map[string]bool{}, lintDoc: `{"Issues":[],"Report":{}}`}
}

func (f *fake) Resolve(_ context.Context, name string) ([]string, error) {
	if f.missing[name] {
		return nil, gate.ErrToolMissing
	}
	return []string{name}, nil
}

func out(s string) gate.Output { return gate.Output{Stdout: []byte(s)} }

func (f *fake) Run(_ context.Context, c gate.Cmd) (gate.Output, error) {
	f.calls = append(f.calls, c.Argv)
	key := strings.Join(c.Argv, " ")
	for prefix, h := range f.override {
		if strings.HasPrefix(key, prefix) {
			return h(c.Argv)
		}
	}
	switch {
	case key == "go mod tidy -diff":
		return gate.Output{}, nil
	case key == "go mod verify":
		return out("all modules verified\n"), nil
	case key == "go env GOVERSION":
		return out("go1.27.4\n"), nil
	case strings.HasPrefix(key, "golangci-lint linters"):
		return out(`{"Enabled":[{"name":"depguard"},{"name":"errcheck"}],"Disabled":[]}`), nil
	case strings.HasPrefix(key, "golangci-lint fmt --diff"):
		return gate.Output{}, nil
	case key == "golangci-lint version --short":
		return out("2.15.0\n"), nil
	case strings.HasPrefix(key, "golangci-lint run"):
		if f.lintDoc != "-" {
			p := filepath.Join(f.dir, ".go-tops", "lint-style.json")
			if err := os.WriteFile(p, []byte(f.lintDoc), 0o644); err != nil {
				f.t.Fatal(err)
			}
		}
		return gate.Output{}, nil
	case strings.HasPrefix(key, "govulncheck -json"):
		return out(cleanStream), nil
	case strings.HasPrefix(key, "go-licenses check"):
		return gate.Output{}, nil
	}
	f.t.Fatalf("unexpected command: %s", key)
	return gate.Output{}, nil
}

func (f *fake) on(prefix string, o gate.Output) {
	f.override[prefix] = func([]string) (gate.Output, error) { return o, nil }
}

func (f *fake) env(cfg gate.Config) *gate.Env {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, "go-tops.lock"), []byte(lockText), 0o644); err != nil {
		f.t.Fatal(err)
	}
	return &gate.Env{Dir: f.dir, Config: cfg, Exec: f, Tools: f}
}

func (f *fake) count(prefix string) int {
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(strings.Join(c, " "), prefix) {
			n++
		}
	}
	return n
}

func runLayer(t *testing.T, f *fake, cfg gate.Config) gate.Result {
	t.Helper()
	return Layer{}.Run(context.Background(), f.env(cfg))
}

func steps(t *testing.T, r gate.Result) map[string]map[string]any {
	t.Helper()
	m := map[string]map[string]any{}
	for name, key := range keys {
		if key == "depguard" {
			if e, ok := r.Details[key].(map[string]any); ok {
				m[name] = e
			}
			continue // present only when deps.depguard is true
		}
		e, ok := r.Details[key].(map[string]any)
		if !ok {
			t.Fatalf("details.%s = %T, want a check entry (schema hygieneDetails)", key, r.Details[key])
		}
		if e["outcome"] == nil {
			t.Fatalf("details.%s has no outcome: %v", key, e)
		}
		m[name] = e
	}
	return m
}

// onlyFails asserts that exactly one step failed, and returns its message.
func onlyFails(t *testing.T, r gate.Result, name string) string {
	t.Helper()
	return failsOnly(t, r, name)
}

// failsOnly asserts that the named step failed and every step but the also
// ones passed, and returns the first named step's message.
func failsOnly(t *testing.T, r gate.Result, name string, also ...string) string {
	t.Helper()
	if r.Outcome != gate.Fail {
		t.Fatalf("outcome %s (%s), want fail", r.Outcome, r.Reason)
	}
	var msg string
	for n, s := range steps(t, r) {
		switch {
		case n == name && s["outcome"] != "fail":
			t.Errorf("step %q is %v, want fail", n, s["outcome"])
		case n == name:
			if m, ok := s["message"].(string); ok {
				msg = m
			}
		case slices.Contains(also, n):
		case s["outcome"] != "pass":
			t.Errorf("step %q is %v, want pass: only %q was planted", n, s["outcome"], name)
		}
	}
	if msg == "" {
		t.Errorf("step %q has no message: a failure must say why", name)
	}
	return msg
}

func TestLayerIdentity(t *testing.T) {
	l := Layer{}
	if l.ID() != "hygiene" || l.Tier() != 1 || len(l.Needs()) != 0 {
		t.Fatalf("%s tier %d needs %v; §3.1, §3.5: hygiene is tier 1 and needs nothing", l.ID(), l.Tier(), l.Needs())
	}
	found := false
	for _, x := range gate.Layers() {
		if x.ID() == "hygiene" {
			_, found = x.(Layer)
		}
	}
	if !found {
		t.Error("importing the package did not register the real layer in place of the stub")
	}
}

func TestAllClean(t *testing.T) {
	f := newFake(t)
	r := runLayer(t, f, gate.Config{Module: "example.com/app"})
	if r.Outcome != gate.Pass {
		t.Fatalf("outcome %s: %s", r.Outcome, r.Reason)
	}
	if got := len(steps(t, r)); got != 7 {
		t.Errorf("%d steps, want the 7 checks of §3.1", got)
	}
	if r.Details["golangci_lint"] != "v2.15.0" || r.Details["findings"] != 0 {
		t.Errorf("details %v", r.Details)
	}
	if r.VulnDBTimestamp != "2026-11-17T09:12:44Z" {
		t.Errorf("vuln db timestamp %q", r.VulnDBTimestamp)
	}
	v, ok := r.Details["vulnerabilities"].(map[string]any)
	if !ok || v["time_dependent"] != true || v["db_last_modified"] != "2026-11-17T09:12:44Z" || v["fail_on"] != "reachable" {
		t.Errorf("vulnerabilities %v: the check must mark itself time-dependent and record the database timestamp", r.Details["vulnerabilities"])
	}
	for _, k := range []string{"tidy", "verify", "toolchain", "format", "lint", "vulnerabilities", "licenses", "golangci_lint", "findings"} {
		if _, ok := r.Details[k]; !ok {
			t.Errorf("details lack %q (hygieneDetails requires it)", k)
		}
	}
}

// G4b: there is no standalone go vet, and golangci-lint runs once.
func TestOneLintRunAndNoVet(t *testing.T) {
	f := newFake(t)
	runLayer(t, f, gate.Config{})
	if n := f.count("golangci-lint run"); n != 1 {
		t.Errorf("%d `golangci-lint run` invocations, want 1", n)
	}
	for _, c := range f.calls {
		for _, a := range c {
			if a == "vet" {
				t.Errorf("a standalone vet ran: %v", c)
			}
		}
	}
	for _, c := range f.calls {
		if strings.HasPrefix(strings.Join(c, " "), "golangci-lint run") && !strings.Contains(strings.Join(c, " "), "--output.json.path=.go-tops/lint-style.json") {
			t.Errorf("the lint run does not save its JSON to .go-tops/lint-style.json: %v", c)
		}
	}
}

func TestLintConfigIsPassedToBothCommands(t *testing.T) {
	f := newFake(t)
	runLayer(t, f, gate.Config{Hygiene: gate.HygieneConfig{LintConfig: "ci/lint.yml"}})
	for _, p := range []string{"golangci-lint fmt --diff --config ci/lint.yml", "golangci-lint run --config ci/lint.yml"} {
		if f.count(p) != 1 {
			t.Errorf("no call starting %q in %v", p, f.calls)
		}
	}
}

// ---- go mod tidy -diff, go mod verify ----

func TestTidyDiffFails(t *testing.T) {
	f := newFake(t)
	f.on("go mod tidy -diff", gate.Output{ExitCode: 1, Stdout: []byte("-require example.com/zzz v1.0.0\n")})
	msg := onlyFails(t, runLayer(t, f, gate.Config{}), "go mod tidy -diff")
	if !strings.Contains(msg, "example.com/zzz") {
		t.Errorf("message %q does not show the diff", msg)
	}
	// a diff on stdout fails even when the exit code says 0
	f = newFake(t)
	f.on("go mod tidy -diff", out("-require example.com/zzz v1.0.0\n"))
	onlyFails(t, runLayer(t, f, gate.Config{}), "go mod tidy -diff")
}

func TestVerifyEmptyOrFailedFails(t *testing.T) {
	for name, o := range map[string]gate.Output{
		"empty output": {},
		"exit 1":       {ExitCode: 1, Stdout: []byte("example.com/x v1.0.0: checksum mismatch\n")},
		"odd output":   out("hello\n"),
	} {
		f := newFake(t)
		f.on("go mod verify", o)
		if msg := onlyFails(t, runLayer(t, f, gate.Config{}), "go mod verify"); msg == "" {
			t.Errorf("%s: no message", name)
		}
	}
}

// ---- toolchain pin ----

func TestGoVersionMustEqualTheLockToolchain(t *testing.T) {
	f := newFake(t)
	f.on("go env GOVERSION", out("go1.27.3\n"))
	msg := onlyFails(t, runLayer(t, f, gate.Config{}), "go env GOVERSION")
	if !strings.Contains(msg, "go1.27.3") || !strings.Contains(msg, "go1.27.4") {
		t.Errorf("message %q must name the toolchain and the pin", msg)
	}
	f = newFake(t)
	f.on("go env GOVERSION", gate.Output{})
	onlyFails(t, runLayer(t, f, gate.Config{}), "go env GOVERSION")
}

// ---- formatting ----

func TestFormatDiffFails(t *testing.T) {
	f := newFake(t)
	f.on("golangci-lint fmt --diff", gate.Output{ExitCode: 1, Stdout: []byte("diff main.go.orig main.go\n--- main.go.orig\n+++ main.go\n@@ -1 +1 @@\n")})
	msg := onlyFails(t, runLayer(t, f, gate.Config{}), "golangci-lint fmt --diff")
	if !strings.Contains(msg, "main.go") {
		t.Errorf("message %q does not name the file", msg)
	}
	f = newFake(t)
	f.on("golangci-lint fmt --diff", gate.Output{ExitCode: 3, Stderr: []byte("failed to load config")})
	onlyFails(t, runLayer(t, f, gate.Config{}), "golangci-lint fmt --diff")
}

// ---- the adopter-style lint pass ----

func TestLintFindingFails(t *testing.T) {
	f := newFake(t)
	f.lintDoc = `{"Issues":[{"FromLinter":"errcheck","Text":"Error return value is not checked","Pos":{"Filename":"main.go","Line":6}}],"Report":{}}`
	f.override["golangci-lint run"] = func([]string) (gate.Output, error) {
		if err := os.WriteFile(filepath.Join(f.dir, ".go-tops", "lint-style.json"), []byte(f.lintDoc), 0o644); err != nil {
			t.Fatal(err)
		}
		return gate.Output{ExitCode: 1}, nil
	}
	r := runLayer(t, f, gate.Config{})
	msg := onlyFails(t, r, "golangci-lint run")
	if !strings.Contains(msg, "errcheck") || !strings.Contains(msg, "main.go:6") {
		t.Errorf("message %q", msg)
	}
	if r.Details["findings"] != 1 {
		t.Errorf("findings %v, want 1", r.Details["findings"])
	}
}

// §3.4 rule 5: a lint run that saved nothing usable judged nothing.
func TestLintEmptyOutputFails(t *testing.T) {
	cases := map[string]string{
		"empty file":      "",
		"blank file":      "  \n",
		"not JSON":        "oops",
		"wrong document":  `{"Report":{}}`,
		"JSON array":      `[]`,
		"no file written": "-",
	}
	for name, doc := range cases {
		f := newFake(t)
		f.lintDoc = doc
		msg := onlyFails(t, runLayer(t, f, gate.Config{}), "golangci-lint run")
		if msg == "" {
			t.Errorf("%s: no message", name)
		}
	}
}

// A previous run's passing result must not stand in for this run's: the
// layer clears the file before it runs the tool.
func TestStaleLintResultIsNotRead(t *testing.T) {
	f := newFake(t)
	f.lintDoc = "-"
	if err := os.MkdirAll(filepath.Join(f.dir, ".go-tops"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, ".go-tops", "lint-style.json"), []byte(`{"Issues":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	onlyFails(t, runLayer(t, f, gate.Config{}), "golangci-lint run")
}

func TestLintExitCodeWithoutFindingsFails(t *testing.T) {
	f := newFake(t)
	f.override["golangci-lint run"] = func([]string) (gate.Output, error) {
		if err := os.WriteFile(filepath.Join(f.dir, ".go-tops", "lint-style.json"), []byte(`{"Issues":[]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		return gate.Output{ExitCode: 3, Stderr: []byte("typecheck failed")}, nil
	}
	msg := onlyFails(t, runLayer(t, f, gate.Config{}), "golangci-lint run")
	if !strings.Contains(msg, "exit 3") {
		t.Errorf("message %q", msg)
	}
}

// ---- govulncheck ----

func TestReachableVulnerabilityFails(t *testing.T) {
	f := newFake(t)
	f.on("govulncheck -json", out(reachableStream))
	r := runLayer(t, f, gate.Config{})
	msg := onlyFails(t, r, "govulncheck -json")
	if !strings.Contains(msg, "GO-2026-0001") || !strings.Contains(msg, "v1.2.3") {
		t.Errorf("message %q must name the vulnerability and its fix", msg)
	}
	if r.VulnDBTimestamp != "2026-11-17T09:12:44Z" {
		t.Errorf("a failing scan must still record the database timestamp, got %q", r.VulnDBTimestamp)
	}
}

// fail_on: reachable: a package that is only imported, or a module that is
// only required, is recorded and does not fail (§3.1).
func TestImportedOnlyVulnerabilityDoesNotFail(t *testing.T) {
	f := newFake(t)
	f.on("govulncheck -json", out(importedOnlyStream))
	r := runLayer(t, f, gate.Config{Hygiene: gate.HygieneConfig{FailOn: "reachable"}})
	if r.Outcome != gate.Pass {
		t.Fatalf("outcome %s: %s", r.Outcome, r.Reason)
	}
	v, ok := r.Details["vulnerabilities"].(map[string]any)
	if !ok || v["other"] != 1 {
		t.Errorf("vulnerabilities %v, want the unreached finding counted", r.Details["vulnerabilities"])
	}
}

// govulncheck exits 0 with -json whatever it finds, so the gate judges the
// parsed findings, never the exit code: a reachable finding with exit 0 fails.
func TestFindingsAreJudgedNotTheExitCode(t *testing.T) {
	f := newFake(t)
	f.on("govulncheck -json", gate.Output{ExitCode: 0, Stdout: []byte(reachableStream)})
	onlyFails(t, runLayer(t, f, gate.Config{}), "govulncheck -json")
}

func TestVulnerabilityEmptyOrBrokenOutputFails(t *testing.T) {
	cases := map[string]gate.Output{
		"empty":             {},
		"no config message": out(`{"progress":{"message":"x"}}` + "\n"),
		"not JSON":          out("govulncheck: panic\n"),
		"bad timestamp":     out(`{"config":{"db_last_modified":"yesterday"}}` + "\n"),
		"exit 1":            {ExitCode: 1, Stderr: []byte("govulncheck: loading packages failed")},
	}
	for name, o := range cases {
		f := newFake(t)
		f.on("govulncheck -json", o)
		if msg := onlyFails(t, runLayer(t, f, gate.Config{}), "govulncheck -json"); msg == "" {
			t.Errorf("%s: no message", name)
		}
	}
}

func TestVulnDBIsPassedAsDashDB(t *testing.T) {
	f := newFake(t)
	runLayer(t, f, gate.Config{Hygiene: gate.HygieneConfig{VulnDB: "file:///mirror/db"}})
	if f.count("govulncheck -json -db file:///mirror/db ./...") != 1 {
		t.Errorf("calls %v", f.calls)
	}
	f = newFake(t)
	runLayer(t, f, gate.Config{})
	if f.count("govulncheck -json ./...") != 1 {
		t.Errorf("without a mirror the call is `govulncheck -json ./...`: %v", f.calls)
	}
}

// ---- licenses ----

func TestLicensesUseTheAllowListAndIgnoreTheModule(t *testing.T) {
	f := newFake(t)
	runLayer(t, f, gate.Config{Module: "example.com/app", Hygiene: gate.HygieneConfig{Licenses: []string{"MIT", "ISC"}}})
	if f.count("go-licenses check ./... --allowed_licenses=MIT,ISC --ignore example.com/app") != 1 {
		t.Errorf("calls %v", f.calls)
	}
	f = newFake(t)
	runLayer(t, f, gate.Config{})
	if f.count("go-licenses check ./... --allowed_licenses=MIT,Apache-2.0,BSD-3-Clause,BSD-2-Clause,ISC") != 1 {
		t.Errorf("without a list the default permissive set applies: %v", f.calls)
	}
}

func TestDisallowedLicenseFails(t *testing.T) {
	f := newFake(t)
	f.on("go-licenses check", gate.Output{ExitCode: 1, Stderr: []byte("Not allowed license 'GPL-3.0' found for library 'example.com/dep'.\n")})
	msg := onlyFails(t, runLayer(t, f, gate.Config{}), "go-licenses check")
	if !strings.Contains(msg, "example.com/dep") || !strings.Contains(msg, "MIT") {
		t.Errorf("message %q must name the library and the allow list", msg)
	}
}

// ---- tools ----

// A mapped tool that is missing is tool-missing (§3.3), the layer exits 1, and
// the checks that do not need that tool still run and are reported.
func TestMissingToolIsToolMissing(t *testing.T) {
	f := newFake(t)
	f.missing["govulncheck"] = true
	r := runLayer(t, f, gate.Config{})
	if r.Outcome != gate.ToolMissing {
		t.Fatalf("outcome %s, want tool-missing", r.Outcome)
	}
	s := steps(t, r)
	if s["govulncheck -json"]["outcome"] != "tool-missing" || s["go mod tidy -diff"]["outcome"] != "pass" {
		t.Errorf("steps %v", s)
	}
	if f.count("go mod tidy -diff") != 1 {
		t.Error("the other checks did not run")
	}
}

func TestProgramThatCannotStartIsToolMissing(t *testing.T) {
	f := newFake(t)
	f.override["go-licenses check"] = func([]string) (gate.Output, error) { return gate.Output{}, gate.ErrNotFound }
	r := runLayer(t, f, gate.Config{})
	if r.Outcome != gate.ToolMissing {
		t.Fatalf("outcome %s, want tool-missing", r.Outcome)
	}
}

// ---- preflight: exit 2 ----

func TestPreflightNeedsALoadableLock(t *testing.T) {
	f := newFake(t)
	env := &gate.Env{Dir: f.dir, Exec: f, Tools: f} // no lock written
	err := Layer{}.Preflight(context.Background(), env)
	var ce *gate.ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("a missing lock: err = %v, want a *gate.ConfigError (exit 2)", err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, lock.FileName), []byte("toolchain: go1.27.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (Layer{}).Preflight(context.Background(), env); !errors.As(err, &ce) {
		t.Fatalf("an invalid lock: err = %v, want a *gate.ConfigError", err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, lock.FileName), []byte(lockText), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (Layer{}).Preflight(context.Background(), env); err != nil {
		t.Fatalf("a good lock: %v", err)
	}
}

type lockFailing struct{ *fake }

func (l lockFailing) Resolve(_ context.Context, name string) ([]string, error) {
	if name == "golangci-lint" {
		return nil, &gate.LockError{Err: errors.New("sha mismatch")}
	}
	return l.fake.Resolve(context.Background(), name)
}

func TestPreflightPassesALockMismatchOn(t *testing.T) {
	f := newFake(t)
	env := f.env(gate.Config{})
	env.Tools = lockFailing{f}
	err := Layer{}.Preflight(context.Background(), env)
	var le *gate.LockError
	if !errors.As(err, &le) {
		t.Fatalf("err = %v, want a *gate.LockError", err)
	}
}

// Through the runner: a repository without go-tops.lock exits 2 before any
// layer runs (§12.2), and no report exists.
func TestRunnerExitsTwoWithoutALock(t *testing.T) {
	f := newFake(t)
	_, err := gate.Execute(context.Background(), gate.Config{Module: "m", Class: "binary-cli", Enforce: "hard"}, gate.Options{MaxTier: 1},
		gate.Deps{Dir: f.dir, Layers: []gate.Layer{Layer{}}, Exec: f, Tools: f})
	var ee *gate.ExitError
	if !errors.As(err, &ee) || ee.Code != gate.ExitConfig {
		t.Fatalf("err = %v, want an ExitError with code 2", err)
	}
	var ce *gate.ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("err %v is not a config error", err)
	}
	if f.count("go ") != 0 {
		t.Errorf("a command ran before the exit: %v", f.calls)
	}
}

// B13.3: deps.depguard true needs the depguard linter in the adopter's lint
// config; the check is judged from the linters the tool reports enabled.
func TestDepguardTrueNeedsTheLinterEnabled(t *testing.T) {
	cfg := gate.Config{Hygiene: gate.HygieneConfig{Depguard: true, LintConfig: "ci/lint.yml"}}
	f := newFake(t)
	r := runLayer(t, f, cfg)
	if r.Outcome != gate.Pass || len(steps(t, r)) != 8 {
		t.Fatalf("outcome %s (%s), steps %d", r.Outcome, r.Reason, len(steps(t, r)))
	}
	if f.count("golangci-lint linters --config ci/lint.yml --json") != 1 {
		t.Errorf("calls %v", f.calls)
	}

	f = newFake(t)
	f.on("golangci-lint linters", out(`{"Enabled":[{"name":"errcheck"}],"Disabled":[{"name":"depguard"}]}`))
	msg := onlyFails(t, runLayer(t, f, cfg), "depguard enabled")
	if !strings.Contains(msg, "depguard") || !strings.Contains(msg, "ci/lint.yml") {
		t.Errorf("message %q", msg)
	}
}

func TestDepguardFalseRunsNoCheck(t *testing.T) {
	f := newFake(t)
	r := runLayer(t, f, gate.Config{Hygiene: gate.HygieneConfig{Depguard: false}})
	if _, present := r.Details["depguard"]; present || f.count("golangci-lint linters") != 0 {
		t.Fatalf("the depguard check ran with deps.depguard false: %v %v", r.Details["depguard"], f.calls)
	}
}

func TestDepguardCheckFailsOnEmptyOrBrokenOutput(t *testing.T) {
	cfg := gate.Config{Hygiene: gate.HygieneConfig{Depguard: true}}
	for name, o := range map[string]gate.Output{
		"empty": {}, "not JSON": out("oops"), "exit 3": {ExitCode: 3, Stderr: []byte("bad config")},
	} {
		f := newFake(t)
		f.on("golangci-lint linters", o)
		if msg := onlyFails(t, runLayer(t, f, cfg), "depguard enabled"); msg == "" {
			t.Errorf("%s: no message", name)
		}
	}
}

// A failed check always carries a message, even when the tool printed nothing:
// the report schema requires one for every check that did not pass.
func TestFailedCheckWithNoOutputStillSaysWhy(t *testing.T) {
	f := newFake(t)
	f.on("go mod verify", gate.Output{ExitCode: 1})
	msg := onlyFails(t, runLayer(t, f, gate.Config{}), "go mod verify")
	if strings.TrimSpace(msg) == "" {
		t.Fatal("no message")
	}
}
