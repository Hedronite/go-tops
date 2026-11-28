//go:build bootstrap

package initcmd

import (
	"context"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// G1b, ruling R8: the first-ledger bootstrap of the drop-in, proven by running the
// template's own steps. A scratch module is adopted with the pull request's gotops,
// and the ubuntu leg's steps run in order as they would on a pull_request, each
// step's if evaluated for that event. Run 1 has no ledger: the ledger is written on
// this build and uploaded, and the run fails. The ledger is committed as it was
// uploaded; run 2 passes with the ratchet judged. A ledger planted for another build
// fails the judge. It needs network (init tidies tools/go.mod, the hygiene layer
// reads the vulnerability database), git, jq and bash, and fails without them. CI
// runs it on ubuntu-latest: go test -tags bootstrap -run TestFirstLedger ./internal/initcmd.

const bootstrapSource = `package main

import "fmt"

func Classify(n int) string {
	switch {
	case n < 0:
		return "neg"
	case n == 0:
		return "zero"
	}
	return "pos"
}

func main() { fmt.Println(Classify(3)) }
`

const bootstrapTest = `package main

import "testing"

func TestClassify(t *testing.T) {
	if Classify(-1) != "neg" || Classify(0) != "zero" || Classify(2) != "pos" {
		t.Fatal("wrong class")
	}
}
`

// workflowRun is one run of the job on one leg and event.
type workflowRun struct {
	t        *testing.T
	dir      string
	steps    []step
	ctx      map[string]string
	pathAdds []string
	temp     string
	shim     string
	skip     map[string]bool // steps the host already satisfies, by name prefix
	outputs  map[string]map[string]string
	failed   string // the first step that failed: later steps without always() do not run
	ran      []string
	artifact map[string]string // artifact name -> directory holding its files
}

var exprRe = regexp.MustCompile(`\$\{\{\s*([a-z_.]+)\s*\}\}`)

// resolve replaces each ${{ name }} of a value with its value in the run.
func (w *workflowRun) resolve(v string) string {
	return exprRe.ReplaceAllStringFunc(v, func(m string) string {
		name := exprRe.FindStringSubmatch(m)[1]
		if out, ok := strings.CutPrefix(name, "steps."); ok {
			id, rest, _ := strings.Cut(out, ".outputs.")
			return w.outputs[id][rest]
		}
		val, ok := w.ctx[name]
		if !ok {
			w.t.Fatalf("the workflow reads %q, which the test does not model", name)
		}
		return val
	})
}

func (w *workflowRun) context() map[string]string {
	ctx := map[string]string{}
	for k, v := range w.ctx {
		ctx[k] = v
	}
	for id, outs := range w.outputs {
		for k, v := range outs {
			ctx["steps."+id+".outputs."+k] = v
		}
	}
	return ctx
}

func (w *workflowRun) step(s step) {
	t := w.t
	ctx := w.context()
	if !ifRunsCtx(t, s.If, ctx) {
		return
	}
	always := strings.Contains(s.If, "always()")
	if w.failed != "" && !always {
		return
	}
	name := s.Name
	if name == "" {
		name = s.Uses
	}
	for prefix := range w.skip {
		if strings.HasPrefix(name, prefix) {
			return
		}
	}
	w.ran = append(w.ran, name)
	switch {
	case strings.HasPrefix(s.Uses, "actions/upload-artifact@"):
		w.upload(s)
	case s.Run != "":
		w.script(s, name)
	}
}

// upload keeps the files of a step's path as the artifact of its name.
func (w *workflowRun) upload(s step) {
	name := w.resolve(fmt.Sprint(s.With["name"]))
	dest := filepath.Join(w.temp, "artifacts", name)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		w.t.Fatal(err)
	}
	for _, p := range strings.Fields(fmt.Sprint(s.With["path"])) {
		if strings.HasPrefix(p, "!") {
			continue
		}
		src := filepath.Join(w.dir, p)
		if b, err := os.ReadFile(src); err == nil {
			if err := os.WriteFile(filepath.Join(dest, filepath.Base(p)), b, 0o644); err != nil {
				w.t.Fatal(err)
			}
		} else if s.With["if-no-files-found"] == "error" {
			w.fail(s.Name)
			return
		}
	}
	w.artifact[name] = dest
}

func (w *workflowRun) fail(name string) {
	if w.failed == "" {
		w.failed = name
	}
}

// script runs a step's script as GitHub does: bash -e, the step's env, and the
// files for its outputs and its PATH.
func (w *workflowRun) script(s step, name string) {
	t := w.t
	out := filepath.Join(w.temp, "output-"+fmt.Sprint(len(w.ran)))
	pathFile := filepath.Join(w.temp, "path-"+fmt.Sprint(len(w.ran)))
	env := append(os.Environ(), "GITHUB_OUTPUT="+out, "GITHUB_PATH="+pathFile, "RUNNER_TEMP="+w.temp, "CI=true")
	path := w.shim + string(os.PathListSeparator) + strings.Join(w.pathAdds, string(os.PathListSeparator)) + string(os.PathListSeparator) + os.Getenv("PATH")
	env = append(env, "PATH="+path)
	for k, v := range s.Env {
		env = append(env, k+"="+w.resolve(v))
	}
	cmd := exec.CommandContext(context.Background(), "bash", "-e", "-c", s.Run)
	cmd.Dir, cmd.Env = w.dir, env
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	t.Logf("step %q: err=%v\n%s", name, err, tail(buf.String()))
	if b, rerr := os.ReadFile(out); rerr == nil && s.ID != "" {
		w.outputs[s.ID] = map[string]string{}
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if k, v, ok := strings.Cut(line, "="); ok {
				w.outputs[s.ID][k] = v
			}
		}
	}
	if b, rerr := os.ReadFile(pathFile); rerr == nil {
		w.pathAdds = append(w.pathAdds, strings.Fields(string(b))...)
	}
	if err != nil {
		w.fail(name)
	}
}

func tail(s string) string {
	if len(s) > 1500 {
		return "..." + s[len(s)-1500:]
	}
	return s
}

func (w *workflowRun) did(name string) bool {
	for _, r := range w.ran {
		if strings.Contains(r, name) {
			return true
		}
	}
	return false
}

// pullRequestRun runs the ubuntu leg's steps of the job for a pull_request.
func pullRequestRun(t *testing.T, dir, shim, temp string, skip map[string]bool) *workflowRun {
	t.Helper()
	platform := "linux-amd64"
	if runtime.GOOS == "darwin" {
		platform = "darwin-arm64" // a host that is not Linux is its own ledger leg here
	}
	w := &workflowRun{
		t: t, dir: dir, steps: workflowSteps(t, dir), shim: shim, temp: temp, skip: skip,
		ctx: map[string]string{
			"matrix.leg": "ubuntu", "github.event_name": "pull_request", "matrix.lint_platform": platform,
			"matrix.go": strings.TrimPrefix(toolchain, "go"), "github.base_ref": "main",
		},
		outputs:  map[string]map[string]string{},
		artifact: map[string]string{},
	}
	for _, s := range w.steps {
		w.step(s)
	}
	return w
}

// sh runs a command in dir and fails the test unless it succeeds.
func sh(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// gotopsBinary is the pull request's gotops: GOTOPS_BIN, or a build of this tree.
func gotopsBinary(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("GOTOPS_BIN"); p != "" {
		return p
	}
	bin := filepath.Join(t.TempDir(), "gotops")
	sh(t, repoRoot, nil, "go", "build", "-o", bin, "./cmd/gotops")
	abs, err := filepath.Abs(bin)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// goShim is a go that runs the pull request's gotops for `go tool -modfile=...
// gotops` and the real go for everything else.
func goShim(t *testing.T, gotops string) string {
	t.Helper()
	real, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("go is required: %v", err)
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = tool ] && [ \"$2\" = -modfile=tools/go.mod ] && [ \"$3\" = gotops ]; then shift 3; exec " + gotops + " \"$@\"; fi\nexec " + real + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestFirstLedgerBootstrapOnAPullRequest(t *testing.T) {
	for _, tool := range []string{"git", "jq", "bash", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("%s is required under -tags bootstrap: %v", tool, err)
		}
	}
	gotops := gotopsBinary(t)
	shim := goShim(t, gotops)
	temp := t.TempDir()
	dir := filepath.Join(temp, "fx")
	write(t, dir, "go.mod", "module github.com/example/fx\n\ngo 1.27.0\n")
	write(t, dir, "cmd/fx/main.go", bootstrapSource)
	write(t, dir, "cmd/fx/main_test.go", bootstrapTest)
	git := func(args ...string) string {
		return sh(t, dir, []string{"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com"}, "git", append([]string{"-c", "commit.gpgsign=false"}, args...)...)
	}
	git("init", "-q", "-b", "main")
	git("add", "-A")
	git("commit", "-q", "-m", "base")
	git("update-ref", "refs/remotes/origin/main", "HEAD") // the base of the pull request
	git("checkout", "-q", "-b", "adopt")
	sh(t, dir, nil, gotops, "init", "--class", "binary-cli", "--owner", "acme")
	git("add", "-A")
	git("commit", "-q", "-m", "adopt")

	// the install step runs for real unless the host already has the linter on PATH
	skip := map[string]bool{}
	if _, err := exec.LookPath("golangci-lint"); err == nil {
		skip["install golangci-lint"] = true
	}

	// Run 1: no ledger on the base or in the tree.
	first := pullRequestRun(t, dir, shim, temp, skip)
	if first.failed != "the first ledger is written, not committed" {
		t.Fatalf("run 1 failed at %q, want the step that asks for the commit; ran: %v", first.failed, first.ran)
	}
	if got := first.outputs["status"]["code"]; got != "0" {
		t.Fatalf("baseline --status exited %q on a repository with no ledger, want 0", got)
	}
	for _, name := range []string{"write the first ledger on the ledger leg", "upload the first ledger"} {
		if !first.did(name) {
			t.Errorf("run 1 did not run %q: %v", name, first.ran)
		}
	}
	if contains(first.ran, "gate") || first.did("judge the gate") {
		t.Errorf("run 1 ran the gate or its judge while the first ledger was being written: %v", first.ran)
	}
	artifact := filepath.Join(first.artifact["go-tops-baseline"], "baseline.json")
	ledger, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatalf("the run uploaded no go-tops-baseline: %v", err)
	}
	build := sh(t, dir, nil, "jq", "-r", `.build | "\(.goos)/\(.goarch) cgo=\(.cgo_enabled) tags=\(.tags | length)"`, artifact)
	if want := fmt.Sprintf("%s/%s cgo=true tags=0", runtime.GOOS, runtime.GOARCH); build != want {
		t.Errorf("the uploaded ledger records the build %q, want this runner's %q", build, want)
	}
	if _, err := os.Stat(filepath.Join(dir, ".go-tops", "baseline.json")); err != nil {
		t.Fatalf("the ledger leg wrote no .go-tops/baseline.json: %v", err)
	}

	// Commit it as it was uploaded: the same bytes. Nothing else the gate wrote is
	// tracked, because init wrote the .gitignore block.
	write(t, dir, ".go-tops/baseline.json", string(ledger))
	if status := git("status", "--porcelain"); strings.TrimSpace(strings.ReplaceAll(status, "?? .go-tops/baseline.json", "")) != "" {
		t.Errorf("a gate run left files git would track besides the ledger:\n%s", status)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "add the first ledger")

	// Run 2: the ledger is committed. The gate and its judge run, and pass: the
	// ratchet is judged on the ledger leg's own build.
	second := pullRequestRun(t, dir, shim, temp, skip)
	if second.failed != "" {
		t.Fatalf("run 2 failed at %q after the ledger was committed; ran: %v", second.failed, second.ran)
	}
	if got := second.outputs["status"]["code"]; got != "1" {
		t.Errorf("baseline --status exited %q with a ledger present, want 1", got)
	}
	if !second.did("judge the gate") || second.did("write the first ledger") {
		t.Errorf("run 2 ran %v: it must judge the gate and write no ledger", second.ran)
	}
	report := filepath.Join(dir, ".go-tops", "report.json")
	if out := sh(t, dir, nil, "jq", "-r", ".ratchet.outcome", report); out != "pass" {
		t.Errorf("the ratchet outcome is %q, want pass on the ledger leg", out)
	}

	// A ledger planted for another build fails the judge on the ledger leg.
	other := "linux"
	if runtime.GOOS == "linux" {
		other = "darwin"
	}
	planted := strings.Replace(string(ledger), `"goos": "`+runtime.GOOS+`"`, `"goos": "`+other+`"`, 1)
	if planted == string(ledger) {
		t.Fatalf("the ledger has no goos to plant:\n%s", ledger)
	}
	write(t, dir, ".go-tops/baseline.json", planted)
	git("add", "-A")
	git("commit", "-q", "-m", "plant a ledger from another build")
	third := pullRequestRun(t, dir, shim, temp, skip)
	if third.failed != "judge the gate (ruling C9) and the ratchet on the ledger leg" {
		t.Errorf("run 3 failed at %q, want the judge: a ledger for another build must fail it; ran: %v", third.failed, third.ran)
	}
}
