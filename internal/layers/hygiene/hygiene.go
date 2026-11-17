// Package hygiene is gate 1 of GO_TOPS.md §3.1: module hygiene, formatting,
// one adopter-style lint pass, the vulnerability scan and the license allow
// list. It runs every check and reports each, so one run shows every failure.
//
// Importing the package registers the layer with the runner.
package hygiene

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/lock"
)

// ID is the layer's name in the config and the report.
const ID = "hygiene"

// LintJSON is where the adopter-style lint run saves its findings (§3.1).
const LintJSON = ".go-tops/lint-style.json"

// DefaultLicenses is the allow list used when go-tops.yaml states none: the
// permissive licenses of the example in §9.
var DefaultLicenses = []string{"MIT", "Apache-2.0", "BSD-3-Clause", "BSD-2-Clause", "ISC"}

// The tools the layer maps (§10): golangci-lint is a downloaded binary, the
// other two are module tools in tools/go.mod.
const (
	golangciLint = "golangci-lint"
	govulncheck  = "govulncheck"
	goLicenses   = "go-licenses"
)

func init() { gate.Register(Layer{}) }

// Layer is the hygiene gate.
type Layer struct{}

// ID implements gate.Layer.
func (Layer) ID() string { return ID }

// Tier implements gate.Layer: hygiene is part of the merge gate.
func (Layer) Tier() int { return 1 }

// Needs implements gate.Layer: hygiene needs no other layer (§3.5).
func (Layer) Needs() []string { return nil }

// Preflight finds, before any layer runs, what makes the run invalid rather
// than failed: a go-tops.lock that is missing or does not load, and a
// downloaded binary that fails its lock check (exit 2, §12.2).
func (Layer) Preflight(ctx context.Context, env *gate.Env) error {
	if _, err := lock.LoadDir(env.Dir); err != nil {
		return &gate.ConfigError{Err: err}
	}
	return gate.PreflightTools(ctx, env, golangciLint, govulncheck, goLicenses)
}

// step is one check of the gate and how it came out.
type step struct {
	name    string
	outcome gate.Outcome // pass, fail or tool-missing
	message string
}

// Run implements gate.Layer.
func (Layer) Run(ctx context.Context, env *gate.Env) gate.Result {
	lk, err := lock.LoadDir(env.Dir)
	if err != nil {
		msg := "cannot read the toolchain pin: " + err.Error()
		return gate.Result{Outcome: gate.Fail, Reason: msg, Details: map[string]any{"error": msg}}
	}
	h := &run{ctx: ctx, env: env, lock: lk}
	steps := []step{
		h.tidy(),
		h.verify(),
		h.goVersion(),
		h.format(),
		h.lint(),
		h.vulns(),
		h.licenses(),
	}
	// deps.depguard: the dependency policy is judged by depguard in the lint
	// pass (§3.1), so the adopter's lint config must enable it.
	if env.Config.Hygiene.Depguard {
		steps = append(steps, h.depguard())
	}
	return h.result(steps)
}

// run holds what the steps share.
type run struct {
	ctx  context.Context
	env  *gate.Env
	lock *lock.Lock

	lintVersion string
	findings    int
	vuln        *vulnReport
	// extra holds the figures a check adds to its entry in the details, by key.
	extra map[string]map[string]any
}

// keys maps a check's command to its key in the details (B11, schema
// hygieneDetails).
var keys = map[string]string{
	"go mod tidy -diff":        "tidy",
	"go mod verify":            "verify",
	"go env GOVERSION":         "toolchain",
	"golangci-lint fmt --diff": "format",
	"golangci-lint run":        "lint",
	"govulncheck -json":        "vulnerabilities",
	"go-licenses check":        "licenses",
	"depguard enabled":         "depguard",
}

func (h *run) note(key string, kv map[string]any) {
	if h.extra == nil {
		h.extra = map[string]map[string]any{}
	}
	h.extra[key] = kv
}

func (h *run) exec(argv ...string) (gate.Output, error) {
	return h.env.Exec.Run(h.ctx, gate.Cmd{Dir: h.env.Dir, Argv: argv})
}

// tool resolves a mapped tool to its argv prefix. A tool that is missing is
// reported as a tool-missing step, not as a failure of the code under test.
func (h *run) tool(name string) ([]string, error) {
	return h.env.Tools.Resolve(h.ctx, name)
}

func passed(name string) step { return step{name: name, outcome: gate.Pass} }

func failed(name, format string, a ...any) step {
	return step{name: name, outcome: gate.Fail, message: fmt.Sprintf(format, a...)}
}

func missing(name string, err error) step {
	return step{name: name, outcome: gate.ToolMissing, message: err.Error()}
}

// cantStart is the step for a command that did not start at all.
func cantStart(name string, err error) step {
	if errors.Is(err, gate.ErrNotFound) {
		return missing(name, err)
	}
	return failed(name, "%v", err)
}

// head returns the first n non-empty lines of text, joined for a message.
func head(text string, n int) string {
	var out []string
	for _, ln := range strings.Split(text, "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			out = append(out, ln)
			if len(out) == n {
				break
			}
		}
	}
	return strings.Join(out, " | ")
}

// changedLines returns the first n added or removed lines of a unified diff,
// without the file headers.
func changedLines(diff string, n int) string {
	var out []string
	for _, ln := range strings.Split(diff, "\n") {
		if (strings.HasPrefix(ln, "-") && !strings.HasPrefix(ln, "---")) || (strings.HasPrefix(ln, "+") && !strings.HasPrefix(ln, "+++")) {
			if t := strings.TrimSpace(ln); len(t) > 1 {
				out = append(out, t)
				if len(out) == n {
					break
				}
			}
		}
	}
	return strings.Join(out, " | ")
}

// tidy: `go mod tidy -diff` prints what tidy would change and exits 1 when it
// would change anything.
func (h *run) tidy() step {
	const name = "go mod tidy -diff"
	out, err := h.exec("go", "mod", "tidy", "-diff")
	if err != nil {
		return cantStart(name, err)
	}
	if out.ExitCode == 0 && strings.TrimSpace(string(out.Stdout)) == "" {
		return passed(name)
	}
	if d := changedLines(string(out.Stdout), 4); d != "" {
		return failed(name, "go.mod or go.sum is not tidy: %s", d)
	}
	return failed(name, "exit %d: %s", out.ExitCode, head(string(out.Stderr), 3))
}

// verify: `go mod verify` prints "all modules verified" and exits 0. A run
// that prints nothing verified nothing (§3.4 rule 5).
func (h *run) verify() step {
	const name = "go mod verify"
	out, err := h.exec("go", "mod", "verify")
	if err != nil {
		return cantStart(name, err)
	}
	text := strings.TrimSpace(string(out.Stdout))
	switch {
	case out.ExitCode != 0:
		return failed(name, "%s", head(text+"\n"+string(out.Stderr), 3))
	case text == "":
		return failed(name, "no output: nothing was verified")
	case !strings.Contains(text, "verified"):
		return failed(name, "unexpected output: %s", head(text, 2))
	}
	return passed(name)
}

// goVersion: `go env GOVERSION` equals the toolchain go-tops.lock pins.
func (h *run) goVersion() step {
	const name = "go env GOVERSION"
	out, err := h.exec("go", "env", "GOVERSION")
	if err != nil {
		return cantStart(name, err)
	}
	got := strings.TrimSpace(string(out.Stdout))
	if got != "" {
		h.note("toolchain", map[string]any{"go_version": got, "pinned": h.lock.Toolchain})
	}
	switch {
	case out.ExitCode != 0:
		return failed(name, "exit %d: %s", out.ExitCode, head(string(out.Stderr), 3))
	case got == "":
		return failed(name, "no output")
	case got != h.lock.Toolchain:
		return failed(name, "the toolchain is %s, go-tops.lock pins %s", got, h.lock.Toolchain)
	}
	return passed(name)
}

func (h *run) lintConfigArgs() []string {
	if c := h.env.Config.Hygiene.LintConfig; c != "" {
		return []string{"--config", c}
	}
	return nil
}

var diffHeader = regexp.MustCompile(`(?m)^diff \S+\.orig (\S+)`)

// format: `golangci-lint fmt --diff` prints a diff for every file the
// configured formatters would change.
func (h *run) format() step {
	const name = "golangci-lint fmt --diff"
	argv, err := h.tool(golangciLint)
	if err != nil {
		return missing(name, err)
	}
	out, err := h.exec(append(append(append([]string{}, argv...), "fmt", "--diff"), h.lintConfigArgs()...)...)
	if err != nil {
		return cantStart(name, err)
	}
	diff := string(out.Stdout)
	if out.ExitCode == 0 && strings.TrimSpace(diff) == "" {
		return passed(name)
	}
	var files []string
	for _, m := range diffHeader.FindAllStringSubmatch(diff, -1) {
		files = append(files, m[1])
	}
	if len(files) > 0 {
		list := make([]any, len(files))
		for i, f := range files {
			list[i] = f
		}
		h.note("format", map[string]any{"files": list})
		return failed(name, "%d file(s) need formatting: %s", len(files), strings.Join(files, ", "))
	}
	return failed(name, "exit %d: %s", out.ExitCode, head(string(out.Stderr)+"\n"+diff, 3))
}

// lintResult is the part of golangci-lint's JSON the gate reads.
type lintResult struct {
	Issues []struct {
		FromLinter string
		Text       string
		Pos        struct {
			Filename string
			Line     int
		}
	}
}

// lint: one adopter-style `golangci-lint run`, its JSON saved to
// .go-tops/lint-style.json. The gate judges the saved document: a missing,
// empty or unreadable file fails (§3.4 rule 5), and so does any finding.
func (h *run) lint() step {
	const name = "golangci-lint run"
	argv, err := h.tool(golangciLint)
	if err != nil {
		return missing(name, err)
	}
	if v, verr := h.exec(append(append([]string{}, argv...), "version", "--short")...); verr == nil && v.ExitCode == 0 {
		if s := strings.TrimSpace(string(v.Stdout)); s != "" {
			h.lintVersion = "v" + strings.TrimPrefix(s, "v")
		}
	}
	path := filepath.Join(h.env.Dir, filepath.FromSlash(LintJSON))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return failed(name, "%v", err)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return failed(name, "cannot clear the previous result: %v", err)
	}
	args := append(append(append([]string{}, argv...), "run"), h.lintConfigArgs()...)
	out, err := h.exec(append(args, "--output.json.path="+LintJSON, "./...")...)
	if err != nil {
		return cantStart(name, err)
	}
	doc, err := os.ReadFile(path)
	if err != nil || len(strings.TrimSpace(string(doc))) == 0 {
		return failed(name, "no result: %s was not written or is empty (exit %d: %s)", LintJSON, out.ExitCode, head(string(out.Stderr), 2))
	}
	res, err := parseLint(doc)
	if err != nil {
		return failed(name, "%s: %v", LintJSON, err)
	}
	h.findings = len(res.Issues)
	by := map[string]any{}
	for _, is := range res.Issues {
		n := 0
		if v, ok := by[is.FromLinter].(int); ok {
			n = v
		}
		by[is.FromLinter] = n + 1
	}
	h.note("lint", map[string]any{"findings": h.findings, "by_linter": by})
	switch {
	case h.findings > 0:
		return failed(name, "%d finding(s): %s", h.findings, summarizeLint(res))
	case out.ExitCode != 0:
		return failed(name, "exit %d with no findings in %s: %s", out.ExitCode, LintJSON, head(string(out.Stderr), 3))
	}
	return passed(name)
}

func parseLint(doc []byte) (lintResult, error) {
	var top map[string]any
	if err := jsonUnmarshal(doc, &top); err != nil {
		return lintResult{}, fmt.Errorf("not JSON: %w", err)
	}
	if _, ok := top["Issues"]; !ok {
		return lintResult{}, errors.New("not a golangci-lint result: no Issues key")
	}
	var res lintResult
	if err := jsonUnmarshal(doc, &res); err != nil {
		return lintResult{}, err
	}
	return res, nil
}

// summarizeLint names the linters behind the findings, most first, and the
// first location.
func summarizeLint(r lintResult) string {
	count := map[string]int{}
	for _, is := range r.Issues {
		count[is.FromLinter]++
	}
	names := make([]string, 0, len(count))
	for n := range count {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if count[names[i]] != count[names[j]] {
			return count[names[i]] > count[names[j]]
		}
		return names[i] < names[j]
	})
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, fmt.Sprintf("%s %d", n, count[n]))
	}
	first := r.Issues[0]
	return fmt.Sprintf("%s; first: %s:%d %s", strings.Join(parts, ", "), first.Pos.Filename, first.Pos.Line, first.Text)
}

// depguard: with deps.depguard true, the adopter lint config must enable the
// depguard linter, or the dependency policy is not enforced anywhere. The set of
// enabled linters is what `golangci-lint linters --json` reports for the
// config, so presets, `default:` and `disable:` are all resolved by the tool.
func (h *run) depguard() step {
	const name = "depguard enabled"
	argv, err := h.tool(golangciLint)
	if err != nil {
		return missing(name, err)
	}
	args := append(append(append([]string{}, argv...), "linters"), h.lintConfigArgs()...)
	out, err := h.exec(append(args, "--json")...)
	if err != nil {
		return cantStart(name, err)
	}
	if out.ExitCode != 0 {
		return failed(name, "golangci-lint linters exited %d: %s", out.ExitCode, head(string(out.Stderr), 3))
	}
	if strings.TrimSpace(string(out.Stdout)) == "" {
		return failed(name, "no output: the enabled linters are unknown")
	}
	var doc struct {
		Enabled []struct {
			Name string `json:"name"`
		} `json:"Enabled"`
	}
	if err := jsonUnmarshal(out.Stdout, &doc); err != nil {
		return failed(name, "output is not golangci-lint JSON: %v", err)
	}
	for _, l := range doc.Enabled {
		if l.Name == "depguard" {
			return passed(name)
		}
	}
	return failed(name, "go-tops.yaml sets deps.depguard true, but the lint config %s does not enable the depguard linter", firstNonEmpty(h.env.Config.Hygiene.LintConfig, defaultLintConfig))
}

const defaultLintConfig = ".golangci.yml"

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// licenses: `go-licenses check` against the allow list. The module's own
// license is not a dependency, so the module is ignored.
func (h *run) licenses() step {
	const name = "go-licenses check"
	argv, err := h.tool(goLicenses)
	if err != nil {
		return missing(name, err)
	}
	allow := h.env.Config.Hygiene.Licenses
	if len(allow) == 0 {
		allow = DefaultLicenses
	}
	list := make([]any, len(allow))
	for i, a := range allow {
		list[i] = a
	}
	h.note("licenses", map[string]any{"allowed": list})
	args := append(append([]string{}, argv...), "check", "./...", "--allowed_licenses="+strings.Join(allow, ","))
	if m := h.env.Config.Module; m != "" {
		args = append(args, "--ignore", m)
	}
	out, err := h.exec(args...)
	if err != nil {
		return cantStart(name, err)
	}
	if out.ExitCode == 0 {
		return passed(name)
	}
	return failed(name, "a dependency is outside the allow list [%s]: %s", strings.Join(allow, ", "), head(string(out.Stderr)+"\n"+string(out.Stdout), 4))
}

// result folds the steps into the layer's outcome and the typed details of
// schema hygieneDetails: one entry per check under its key, the lint version
// and the style-lint finding count.
func (h *run) result(steps []step) gate.Result {
	var bad, absent []string
	d := map[string]any{}
	for _, s := range steps {
		key := keys[s.name]
		e := map[string]any{"outcome": string(s.outcome)}
		if s.message != "" {
			e["message"] = s.message
		}
		for k, v := range h.extra[key] {
			e[k] = v
		}
		d[key] = e
		switch s.outcome {
		case gate.Fail:
			bad = append(bad, s.name+": "+s.message)
		case gate.ToolMissing:
			absent = append(absent, s.name+": "+s.message)
		}
	}
	version := h.lintVersion
	if version == "" {
		version = "unknown"
	}
	d["golangci_lint"] = version
	d["findings"] = h.findings
	res := gate.Result{Details: d}
	if h.vuln != nil {
		res.VulnDBTimestamp = h.vuln.dbLastModified
	}
	switch {
	case len(absent) > 0:
		res.Outcome = gate.ToolMissing
		res.Reason = fmt.Sprintf("%d check(s) lack a tool: %s", len(absent), strings.Join(absent, "; "))
	case len(bad) > 0:
		res.Outcome = gate.Fail
		res.Reason = fmt.Sprintf("%d of %d checks failed: %s", len(bad), len(steps), strings.Join(bad, "; "))
	default:
		res.Outcome = gate.Pass
	}
	return res
}
