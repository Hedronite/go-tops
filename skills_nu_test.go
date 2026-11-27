//go:build skills

package gotops

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The skills under .agents/skills are Nushell scripts that wrap gotops. These
// tests run them for real, with a stand-in gotops that writes a doctored report
// and exits as told, so each verdict is the script's and none is the gate's. They
// need Nushell and fail, never skip, without it: run with -tags skills.

// nuVersion is the Nushell release the skills are written for, the one the
// Rust-TOPS envelope is pinned to.
const nuVersion = "0.108.0"

// envelope is the one object every skill prints.
type envelope struct {
	OK      bool           `json:"ok"`
	Command string         `json:"command"`
	Error   string         `json:"error"`
	Data    map[string]any `json:"data"`
}

func needNu(t *testing.T) {
	t.Helper()
	path, err := exec.LookPath("nu")
	if err != nil {
		t.Fatalf("nu is required under -tags skills: %v", err)
	}
	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		t.Fatalf("nu --version: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != nuVersion {
		t.Fatalf("nu %s is required under -tags skills, found %s", nuVersion, got)
	}
}

func skillScript(t *testing.T, skill string) string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, ".agents", "skills", skill, skill+".nu")
}

// runSkill runs a skill's script in dir and returns the one JSON object it
// printed, checked to be exactly {ok, command, error, data}.
func runSkill(t *testing.T, skill, dir string, env []string, args ...string) envelope {
	t.Helper()
	needNu(t)
	cmd := exec.Command("nu", append([]string{skillScript(t, skill)}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("nu %s %v: %v\n%s", skill, args, err, stderr.String())
	}
	return decodeEnvelope(t, stdout.Bytes())
}

func decodeEnvelope(t *testing.T, out []byte) envelope {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(out))
	var raw map[string]json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		t.Fatalf("the output is not one JSON object: %v\n%s", err, out)
	}
	if dec.More() {
		t.Fatalf("the output holds more than one JSON value:\n%s", out)
	}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if want := []string{"command", "data", "error", "ok"}; !slices.Equal(keys, want) {
		t.Fatalf("the object has keys %v, want %v", keys, want)
	}
	var e envelope
	if err := json.Unmarshal(out, &e); err != nil {
		t.Fatal(err)
	}
	return e
}

// fakeGotopsScript stands in for gotops: it logs its argv, then acts on the
// subcommand as the FAKE_* variables say.
const fakeGotopsScript = `#!/bin/sh
echo "$@" >> "$FAKE_LOG"
case "$1" in
gate)
  if [ -n "$FAKE_REPORT" ]; then mkdir -p .go-tops; cp "$FAKE_REPORT" .go-tops/report.json; fi
  exit "${FAKE_EXIT:-0}" ;;
init)
  exit "${FAKE_INIT:-0}" ;;
baseline)
  if [ -n "$FAKE_LEDGER" ]; then mkdir -p .go-tops; cp "$FAKE_LEDGER" .go-tops/baseline.json; fi
  exit "${FAKE_BASELINE:-0}" ;;
check)
  if [ -n "$FAKE_CHECK_OUT" ]; then echo "$FAKE_CHECK_OUT" >&2; fi
  exit "${FAKE_CHECK:-0}" ;;
esac
exit 64
`

// fake is a stand-in gotops and the files it reads and writes.
type fake struct {
	dir  string
	argv string // the --gotops value
	log  string
	env  []string
}

func newFake(t *testing.T) *fake {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-gotops.sh")
	if err := os.WriteFile(script, []byte(fakeGotopsScript), 0o755); err != nil {
		t.Fatal(err)
	}
	f := &fake{dir: dir, argv: "sh " + script, log: filepath.Join(dir, "argv.log")}
	f.env = []string{"FAKE_LOG=" + f.log}
	return f
}

func (f *fake) set(kv ...string) *fake {
	f.env = append(f.env, kv...)
	return f
}

// file writes a file next to the fake and returns its path.
func (f *fake) file(t *testing.T, name string, content []byte) string {
	t.Helper()
	p := filepath.Join(f.dir, name)
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// calls is the argv lines the fake was run with, in order.
func (f *fake) calls(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(f.log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// A report is a map the cases doctor: it starts as a real report of a module that
// passes under ruling C9.
type report map[string]any

func loadReport(t *testing.T) report {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "skills", "report-c9.json"))
	if err != nil {
		t.Fatal(err)
	}
	var r report
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r report) obj(key string) map[string]any {
	m, ok := r[key].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return m
}

func (r report) layer(id string) map[string]any {
	layers := asList(r["layers"])
	for _, l := range layers {
		if m, ok := l.(map[string]any); ok && m["id"] == id {
			return m
		}
	}
	return map[string]any{}
}

func (r report) bytes(t *testing.T) []byte {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// gateCase is one doctored report and the verdict the gate skill must reach.
type gateCase struct {
	name   string
	doctor func(report)
	code   string // the stand-in gate's exit code
	args   []string
	ok     bool
	err    string // a substring of the error when not ok
}

func failedItems(items ...string) func(report) {
	return func(r report) {
		s := r.obj("summary")
		list := make([]any, len(items))
		for i, it := range items {
			list[i] = it
		}
		s["failed"] = list
	}
}

func both(fs ...func(report)) func(report) {
	return func(r report) {
		for _, f := range fs {
			f(r)
		}
	}
}

func setLayer(id, outcome string) func(report) {
	return func(r report) { r.layer(id)["outcome"] = outcome }
}

func setLaw(key string, v any) func(report) {
	return func(r report) { r.obj("laws")[key] = v }
}

func setRatchet(outcome string) func(report) {
	return func(r report) { r.obj("ratchet")["outcome"] = outcome }
}

func setRunBuild(goos string) func(report) {
	return func(r report) {
		r.obj("ratchet")["run_build"] = map[string]any{"goos": goos, "goarch": "amd64", "tags": []any{}, "cgo_enabled": true}
		r.obj("ratchet")["ledger_build"] = map[string]any{"goos": "plan9", "goarch": "amd64", "tags": []any{}, "cgo_enabled": true}
	}
}

func setRunTags(tags ...string) func(report) {
	return func(r report) {
		list := make([]any, len(tags))
		for i, tag := range tags {
			list[i] = tag
		}
		build := asObj(r.obj("ratchet")["run_build"])
		build["tags"] = list
		r.obj("ratchet")["run_build"] = build
	}
}

func setExit(n int) func(report) {
	return func(r report) { r.obj("summary")["exit_code"] = n }
}

func dropKey(key string) func(report) {
	return func(r report) { delete(r, key) }
}

func addResult(id, verdict string) func(report) {
	return func(r report) {
		laws := r.obj("laws")
		rs := asList(laws["results"])
		laws["results"] = append(rs, map[string]any{"id": id, "verdict": verdict, "reader": "tool"})
	}
}

var gateCases = []gateCase{
	{"C9: the required laws alone, ratchet pass", nil, "1", nil, true, ""},
	{"M5: laws judged and gate exits 0", both(setLaw("judged", 16), setLaw("required_not_evaluated", 0), setLayer("laws", "pass"), failedItems(), setExit(0)), "0", nil, true, ""},
	{"exit 0 with nothing failed", both(failedItems(), setExit(0)), "0", nil, true, ""},
	{"exit 0 while the report says exit 1", nil, "0", nil, false, "exit_code"},
	{"exit 1 while the report says exit 0", setExit(0), "1", nil, false, "exit_code"},
	{"the report has no exit code", func(r report) { delete(r.obj("summary"), "exit_code") }, "1", nil, false, "exit_code"},
	{"ratchet skip on another build", both(setRatchet("skip"), setRunBuild("linux")), "1", nil, true, ""},
	{"ratchet skip on the ledger's own build", setRatchet("skip"), "1", nil, false, "own build"},
	{"ratchet skip on the ledger's build with other tags", both(setRatchet("skip"), setRunTags("integration")), "1", nil, true, ""},
	{"ratchet skip with no builds named", both(setRatchet("skip"), func(r report) { delete(r.obj("ratchet"), "run_build") }), "1", nil, false, "own build"},
	{"ratchet pass", setRatchet("pass"), "1", nil, true, ""},
	{"a law that is not required and not evaluated is a gap, not a failure", addResult("G-40", "not-evaluated"), "1", nil, true, ""},
	{"advisory with a failing layer", both(func(r report) { r["enforce"] = "advisory"; delete(r, "ratchet") }, setLayer("crap", "fail"), failedItems("crap"), setExit(0)), "0", nil, false, "crap"},
	{"advisory with the required laws alone", both(func(r report) { r["enforce"] = "advisory"; delete(r, "ratchet") }, setExit(0)), "0", nil, true, ""},
	{"ratchet fail", both(setRatchet("fail"), failedItems("ratchet", "laws")), "1", nil, false, "ratchet"},
	{"ratchet fail listed nowhere", setRatchet("fail"), "1", nil, false, "ratchet"},
	{"no ratchet outcome under enforce ratchet", dropKey("ratchet"), "1", nil, false, "ratchet outcome"},
	{"a layer failed with the laws", both(setLayer("crap", "fail"), failedItems("crap", "laws")), "1", nil, false, "crap"},
	{"a layer failed and is not listed", setLayer("tests", "fail"), "1", nil, false, "tests"},
	{"a layer was not run", setLayer("coverage", "not-run"), "1", nil, false, "coverage"},
	{"a failed item besides the laws", failedItems("coverage", "laws"), "1", nil, false, "coverage"},
	{"exit 1 with nothing failed", failedItems(), "1", nil, false, "no failed item"},
	{"a required law was evaluated", setLaw("required_not_evaluated", 15), "1", nil, false, "required law"},
	{"no law is required", both(setLaw("required", 0), setLaw("required_not_evaluated", 0)), "1", nil, false, "no law is required"},
	{"a law was judged", setLaw("judged", 1), "1", nil, false, "judged"},
	{"a law is refused", addResult("G-41", "refuse"), "1", nil, false, "G-41"},
	{"a refused law beside a judged one", both(setLaw("judged", 1), addResult("G-30", "refuse")), "1", nil, false, "refused"},
	{"the run was partial", both(func(r report) { r.obj("summary")["outcome"] = "partial" }, failedItems(), setExit(0)), "0", nil, false, "partial"},
	{"a layer in range did not run", func(r report) { r.obj("layer_counts")["ran"] = 3 }, "1", nil, false, "partial"},
	{"exit 0 while the report names a failed layer", both(setLayer("tests", "fail"), failedItems("tests"), setExit(0)), "0", nil, false, "tests"},
	{"no layers in the report", dropKey("layers"), "1", nil, false, "layers"},
	{"no laws record", dropKey("laws"), "1", nil, false, "laws"},
	{"no summary", dropKey("summary"), "1", nil, false, "summary"},
	{"no enforce mode", dropKey("enforce"), "1", nil, false, "enforce"},
	{"exit 2", nil, "2", nil, false, "exited 2"},
	{"exit 3", nil, "3", nil, false, "exited 3"},
	{"exit 4", nil, "4", nil, false, "exited 4"},
	{"exit 64", nil, "64", nil, false, "exited 64"},
	// the mutation layer (GO_TOPS.md section 6.4)
	{"an unexplained survivor", both(setMutation("fail", survivorDetails(1)), failedItems("mutation", "laws")), "1", nil, false, survivorRowKey},
	{"mutation fail with no history", both(setMutation("fail", map[string]any{"error": "no merge base: check out with fetch-depth: 0"}), failedItems("mutation", "laws")), "1", nil, false, "fetch-depth: 0"},
	{"mutation vacuous", setMutation("vacuous", vacuousDetails()), "1", nil, true, ""},
	{"mutation pass", setMutation("pass", survivorDetails(0)), "1", nil, true, ""},
	{"mutation pass that counts an unexplained survivor", setMutation("pass", survivorDetails(1)), "1", nil, false, "unexplained survivor"},
	{"mutation off until a passed day", func(r report) { r.layer("mutation")["expires"] = "2001-01-01" }, "1", nil, false, "after today"},
	{"vacuous on another layer", setLayer("coverage", "vacuous"), "1", nil, false, "coverage"},
}

var survivorRowKey = "internal/x/x.go:Limit:CONDITIONALS_BOUNDARY:" + strings.Repeat("a", 64)

// survivorRow is one row of the report's survivors (GO_TOPS.md section 12.3).
func survivorRow(key string, line int, status string, explained any) map[string]any {
	part := strings.Split(key, ":")
	return map[string]any{
		"key": key, "file": part[0], "function": part[1], "mutator": part[2], "line_hash": part[3],
		"line": line, "status": status, "explained": explained,
	}
}

// mutationDetails are the details of a mutation layer that ran, with its rows.
func mutationDetails(killed, explained, unexplained, dropped int, rows []any) map[string]any {
	return map[string]any{
		"base": "origin/main", "base_sha": strings.Repeat("0", 40),
		"mutants": killed + explained + unexplained, "killed": killed, "survived_explained": explained,
		"survived_unexplained": unexplained, "max": 0, "timed_out": 0, "timed_out_keys": []any{},
		"dropped": dropped, "not_measured": 0, "exempt": []any{}, "survivors": rows,
	}
}

// survivorDetails are the details of a mutation layer with n unexplained
// survivors of the one key, beside one the ledger explains.
func survivorDetails(n int) map[string]any {
	rows := []any{survivorRow("internal/x/x.go:Old:ARITHMETIC_BASE:"+strings.Repeat("b", 64), 3, "LIVED", "ledger")}
	for range n {
		rows = append(rows, survivorRow(survivorRowKey, 7, "NOT COVERED", nil))
	}
	return mutationDetails(3, 1, n, 2, rows)
}

// vacuousDetails are what a layer that judged nothing carries.
func vacuousDetails() map[string]any { return mutationDetails(0, 0, 0, 0, []any{}) }

// SkillReports are the reports the skill tests run on: the fixture as it is and
// with each mutation state the cases put into it.
func SkillReports(t *testing.T) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for name, change := range map[string]func(report){
		"the fixture":                   func(report) {},
		"an unexplained survivor":       setMutation("fail", survivorDetails(1)),
		"a pass":                        setMutation("pass", survivorDetails(0)),
		"vacuous":                       setMutation("vacuous", vacuousDetails()),
		"a failure with no measurement": setMutation("fail", map[string]any{"error": "no merge base: check out with fetch-depth: 0"}),
	} {
		r := loadReport(t)
		change(r)
		out[name] = r.bytes(t)
	}
	return out
}

// setMutation turns the fixture's switched-off mutation layer into one that ran.
func setMutation(outcome string, details map[string]any) func(report) {
	return func(r report) {
		layer := r.layer("mutation")
		delete(layer, "reason")
		delete(layer, "expires")
		layer["outcome"], layer["details"] = outcome, details
	}
}

// The skill reports the mutation counts and each unexplained survivor, and lists
// a vacuous layer without counting it as a failure.
func TestGateSkillReportsTheMutationLayer(t *testing.T) {
	judge := func(t *testing.T, change func(report), exit string) envelope {
		t.Helper()
		f := newFake(t)
		r := loadReport(t)
		change(r)
		f.set("FAKE_REPORT="+f.file(t, "report.json", r.bytes(t)), "FAKE_EXIT="+exit)
		return runSkill(t, "go-tops-gate", f.dir, f.env, "--gotops", f.argv)
	}
	failed := judge(t, both(setMutation("fail", survivorDetails(1)), failedItems("mutation", "laws")), "1")
	m := asObj(failed.Data["mutation"])
	for key, want := range map[string]float64{"mutants": 5, "killed": 3, "survived_explained": 1, "survived_unexplained": 1, "dropped": 2} {
		if m[key] != want {
			t.Errorf("data.mutation.%s = %v, want %v", key, m[key], want)
		}
	}
	rows := asList(m["unexplained"])
	if len(rows) != 1 || asStr(asObj(rows[0])["key"]) != survivorRowKey || asStr(asObj(rows[0])["status"]) != "NOT COVERED" || asObj(rows[0])["line"] != float64(7) {
		t.Errorf("data.mutation.unexplained = %v, want the one unexplained row", rows)
	}
	vacuous := judge(t, setMutation("vacuous", vacuousDetails()), "1")
	if m = asObj(vacuous.Data["mutation"]); !vacuous.OK || asStr(m["outcome"]) != "vacuous" || len(asList(m["unexplained"])) != 0 {
		t.Errorf("a vacuous layer is listed and is not a failure: ok %v, %v", vacuous.OK, m)
	}
}

func TestGateSkillJudgesTheReportUnderC9(t *testing.T) {
	for _, c := range gateCases {
		t.Run(c.name, func(t *testing.T) {
			f := newFake(t)
			r := loadReport(t)
			if c.doctor != nil {
				c.doctor(r)
			}
			rep := f.file(t, "in.json", r.bytes(t))
			f.set("FAKE_REPORT="+rep, "FAKE_EXIT="+c.code)
			got := runSkill(t, "go-tops-gate", t.TempDir(), f.env, append([]string{"--gotops", f.argv}, c.args...)...)
			if got.OK != c.ok {
				t.Fatalf("ok = %v, want %v: %s", got.OK, c.ok, got.Error)
			}
			if c.ok && got.Error != "" {
				t.Errorf("an ok result has the error %q", got.Error)
			}
			if !c.ok && !strings.Contains(got.Error, c.err) {
				t.Errorf("the error %q does not name %q", got.Error, c.err)
			}
			if !strings.Contains(got.Command, "gate --max-tier 1") {
				t.Errorf("the command %q is not the gate's argv", got.Command)
			}
		})
	}
}

func TestGateSkillFailsClosedOnTheReportItReads(t *testing.T) {
	good := loadReport(t)
	for _, c := range []struct {
		name   string
		report []byte // nil: the gate writes none
		err    string
	}{
		{"no report", nil, "wrote no"},
		{"a report that is not JSON", []byte("the gate crashed\n"), "not a JSON report"},
		{"an empty report", []byte(""), "not a JSON report"},
		{"a truncated report", good.bytes(t)[:200], "not a JSON report"},
		{"a JSON array", []byte("[]"), "not a JSON report"},
		{"a report with no fields", []byte("{}"), "enforce"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFake(t)
			dir := t.TempDir()
			f.set("FAKE_EXIT=1")
			if c.report != nil {
				f.set("FAKE_REPORT=" + f.file(t, "in.json", c.report))
			}
			// A report from an earlier run is not this run's: the script removes it.
			stale := filepath.Join(dir, ".go-tops", "report.json")
			if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(stale, good.bytes(t), 0o644); err != nil {
				t.Fatal(err)
			}
			got := runSkill(t, "go-tops-gate", dir, f.env, "--gotops", f.argv)
			if got.OK || !strings.Contains(got.Error, c.err) {
				t.Fatalf("ok = %v, error %q; want a failure naming %q", got.OK, got.Error, c.err)
			}
		})
	}
}

func TestGateSkillReportsAMissingGotops(t *testing.T) {
	got := runSkill(t, "go-tops-gate", t.TempDir(), nil, "--gotops", "no-such-gotops-binary")
	if got.OK || !strings.Contains(got.Error, "command not found") {
		t.Fatalf("ok = %v, error %q", got.OK, got.Error)
	}
}

func TestGateSkillUsesTheAdoptersPinnedTool(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	mod := "module tools\n\ngo 1.27.0\n\ntool github.com/Hedronite/go-tops/cmd/gotops\n"
	if err := os.WriteFile(filepath.Join(dir, "tools", "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	got := runSkill(t, "go-tops-gate", dir, nil)
	if want := "go tool -modfile=tools/go.mod gotops gate --max-tier 1"; got.Command != want {
		t.Fatalf("command = %q, want %q", got.Command, want)
	}
	if got.OK {
		t.Fatal("a pinned tool that cannot run gave ok: true")
	}
}

func TestGateSkillReportsEachLayerAndLaw(t *testing.T) {
	f := newFake(t)
	rep := f.file(t, "in.json", loadReport(t).bytes(t))
	f.set("FAKE_REPORT="+rep, "FAKE_EXIT=1")
	got := runSkill(t, "go-tops-gate", t.TempDir(), f.env, "--gotops", f.argv)
	layers := asList(got.Data["layers"])
	if len(layers) != 6 {
		t.Errorf("layers = %d, want the 6 of the report", len(layers))
	}
	laws := asObj(got.Data["laws"])
	gaps := asList(laws["gaps"])
	if len(gaps) == 0 {
		t.Error("the not-evaluated laws are not listed as gaps")
	}
	for _, g := range gaps {
		if v := asObj(g)["verdict"]; v != "not-evaluated" {
			t.Errorf("a gap has the verdict %v", v)
		}
	}
}

// --- go-tops-adopt ---

const (
	ledgerLinux  = `{"build":{"goos":"linux","goarch":"amd64","tags":[],"cgo_enabled":true},"judged_laws":0,"ledger":{}}`
	ledgerDarwin = `{"build":{"goos":"darwin","goarch":"arm64","tags":[],"cgo_enabled":true},"judged_laws":0,"ledger":{}}`
	ledgerTagged = `{"build":{"goos":"linux","goarch":"amd64","tags":["integration"],"cgo_enabled":true},"judged_laws":0,"ledger":{}}`
)

const notCommitted = "go-tops.yaml: /baseline: under ratchet the baseline .go-tops/baseline.json is not tracked by git (§6.7) (baseline)"

func TestAdoptSkillRunsInitBaselineAndCheck(t *testing.T) {
	for _, c := range []struct {
		name     string
		env      []string
		ledger   string
		args     []string
		ok       bool
		err      string
		steps    int
		commit   bool // commit_required
		wantNext bool
	}{
		{"all three exit 0 on the ledger leg's build", nil, ledgerLinux, nil, true, "", 3, false, false},
		{"only the commit is left", []string{"FAKE_CHECK=2", "FAKE_CHECK_OUT=" + notCommitted}, ledgerLinux, nil, true, "", 3, true, true},
		{"the ledger differs from HEAD", []string{"FAKE_CHECK=2", "FAKE_CHECK_OUT=go-tops.yaml: /baseline: under ratchet the baseline .go-tops/baseline.json differs from HEAD (§6.7) (baseline)"}, ledgerLinux, nil, true, "", 3, true, true},
		{"check names another issue", []string{"FAKE_CHECK=2", "FAKE_CHECK_OUT=" + notCommitted + "\ngo-tops.yaml: /class: unknown (config)"}, ledgerLinux, nil, false, "exited 2", 3, false, false},
		{"check names a baseline issue of another kind", []string{"FAKE_CHECK=2", "FAKE_CHECK_OUT=go-tops.yaml: /baseline: the ledger names a function that is gone (baseline)"}, ledgerLinux, nil, false, "exited 2", 3, false, false},
		{"check exits 3 with the baseline's message", []string{"FAKE_CHECK=3", "FAKE_CHECK_OUT=" + notCommitted}, ledgerLinux, nil, false, "exited 3", 3, false, false},
		{"check exits 3", []string{"FAKE_CHECK=3", "FAKE_CHECK_OUT=cannot read the repository"}, ledgerLinux, nil, false, "exited 3", 3, false, false},
		{"init exits 2", []string{"FAKE_INIT=2"}, ledgerLinux, nil, false, "init", 1, false, false},
		{"baseline --init refuses", []string{"FAKE_BASELINE=1"}, ledgerLinux, nil, false, "baseline --init exited 1", 2, false, false},
		{"a ledger from a Mac is not an adoption", nil, ledgerDarwin, nil, false, "darwin/arm64", 2, false, false},
		{"a ledger from another build is not an adoption", nil, ledgerTagged, nil, false, "not the ledger leg", 2, false, false},
		{"no ledger was written", nil, "", nil, false, "unknown build", 2, false, false},
		{"a dry run accepts the Mac ledger", nil, ledgerDarwin, []string{"--allow-host-ledger"}, true, "", 3, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFake(t).set(c.env...)
			if c.ledger != "" {
				f.set("FAKE_LEDGER=" + f.file(t, "ledger.json", []byte(c.ledger)))
			}
			args := append([]string{"adopt", "--class", "binary-service", "--owner", "acme", "--gotops", f.argv}, c.args...)
			got := runSkill(t, "go-tops-adopt", t.TempDir(), f.env, args...)
			if got.OK != c.ok || !strings.Contains(got.Error, c.err) {
				t.Fatalf("ok = %v, error %q; want ok %v with %q", got.OK, got.Error, c.ok, c.err)
			}
			steps := asList(got.Data["steps"])
			if len(steps) != c.steps {
				t.Errorf("%d steps ran, want %d: %v", len(steps), c.steps, f.calls(t))
			}
			if got.Data["commit_required"] != nil && got.Data["commit_required"] != c.commit {
				t.Errorf("commit_required = %v, want %v", got.Data["commit_required"], c.commit)
			}
			if _, ok := got.Data["next"]; ok != c.wantNext {
				t.Errorf("next present = %v, want %v", ok, c.wantNext)
			}
		})
	}
}

func TestAdoptSkillPassesItsArgumentsToInit(t *testing.T) {
	f := newFake(t)
	f.set("FAKE_LEDGER=" + f.file(t, "ledger.json", []byte(ledgerLinux)))
	got := runSkill(t, "go-tops-adopt", t.TempDir(), f.env, "adopt", "--class", "library-core", "--owner", "acme", "--force", "--offline", "--gotops", f.argv)
	if !got.OK {
		t.Fatal(got.Error)
	}
	want := []string{"init --class library-core --owner acme --force --offline", "baseline --init", "check"}
	if calls := f.calls(t); !slices.Equal(calls, want) {
		t.Errorf("the stand-in was called with %q, want %q", calls, want)
	}
	for _, w := range want {
		if !strings.Contains(got.Command, w) {
			t.Errorf("the command %q lacks %q", got.Command, w)
		}
	}
}

func TestAdoptSkillRefusesWhatItCannotRun(t *testing.T) {
	for _, c := range []struct {
		name string
		args []string
		err  string
	}{
		{"a class that does not exist", []string{"adopt", "--class", "kernel", "--gotops", "sh"}, "--class must be one of"},
		{"no class", []string{"adopt", "--gotops", "sh"}, "--class must be one of"},
		{"a missing gotops", []string{"adopt", "--class", "binary-cli", "--gotops", "no-such-gotops-binary"}, "command not found"},
		{"no command", nil, "name a command"},
		{"an unknown command", []string{"mend"}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFake(t)
			dir := t.TempDir()
			cmd := exec.Command("nu", append([]string{skillScript(t, "go-tops-adopt")}, c.args...)...)
			cmd.Dir, cmd.Env = dir, append(os.Environ(), f.env...)
			out, err := cmd.Output()
			if c.err == "" {
				if err == nil {
					t.Fatal("nu ran an unknown command without an error")
				}
				return // nu's own usage error: there is no command to run
			}
			got := decodeEnvelope(t, out)
			if got.OK || !strings.Contains(got.Error, c.err) {
				t.Fatalf("ok = %v, error %q; want a failure naming %q", got.OK, got.Error, c.err)
			}
			if calls := f.calls(t); len(calls) != 0 {
				t.Errorf("gotops ran: %v", calls)
			}
		})
	}
}

func TestAdoptSkillChecksAnAdoptedModule(t *testing.T) {
	for _, c := range []struct {
		name string
		code string
		ok   bool
	}{{"check passes", "0", true}, {"check fails", "2", false}} {
		t.Run(c.name, func(t *testing.T) {
			f := newFake(t).set("FAKE_CHECK="+c.code, "FAKE_CHECK_OUT=go-tops.yaml: /baseline: not tracked by git (baseline)")
			got := runSkill(t, "go-tops-adopt", t.TempDir(), f.env, "check", "--gotops", f.argv)
			if got.OK != c.ok {
				t.Fatalf("ok = %v, want %v: %s", got.OK, c.ok, got.Error)
			}
			if calls := f.calls(t); !slices.Equal(calls, []string{"check"}) {
				t.Errorf("calls = %q, want only check", calls)
			}
		})
	}
}

// asList and asObj read a decoded JSON value as a list or an object: anything
// else is empty, so a test that expects more fails on its own assertion.
func asList(v any) []any {
	l, ok := v.([]any)
	if !ok {
		return nil
	}
	return l
}

func asObj(v any) map[string]any {
	m, ok := v.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return m
}

func asStr(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// The first ledger comes from the pull-request run on the ledger leg (friction F2:
// the manual run needs the workflow on the default branch, which the adoption
// pull request is what adds). The error for a ledger on another build says so.
func TestAdoptSkillSaysWhereTheFirstLedgerComesFrom(t *testing.T) {
	f := newFake(t)
	f.set("FAKE_LEDGER=" + f.file(t, "ledger.json", []byte(ledgerDarwin)))
	got := runSkill(t, "go-tops-adopt", t.TempDir(), f.env, "adopt", "--class", "binary-cli", "--owner", "acme", "--gotops", f.argv)
	if got.OK {
		t.Fatal("a ledger from a Mac was accepted")
	}
	for _, want := range []string{"pull-request run", "go-tops-baseline", "unchanged", "after the workflow is on the default branch"} {
		if !strings.Contains(got.Error, want) {
			t.Errorf("the error lacks %q:\n%s", want, got.Error)
		}
	}
	if strings.Contains(got.Error, "by hand with mode init") {
		t.Errorf("the error sends the adopter to the manual run first, which cannot start before the workflow is on the default branch:\n%s", got.Error)
	}
}
