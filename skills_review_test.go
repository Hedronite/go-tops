//go:build skills

package gotops

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// go-tops-review reads a gate report and a diff. These tests build a git
// repository whose base commit holds a ledger, change functions on a branch the
// ways the ratchet states name, and check what the script lists for each.

const reviewModule = "example.com/m"

const reviewBase = `package pkg

func Held(n int) int {
	return n + 1
}

func Raised(n int) int {
	return n + 2
}

func Lowered(n int) int {
	return n + 3
}

func Paid(n int) int {
	return n + 4
}

func Trimmed(n int) int {
	n++
	n--
	return n
}

func (t *T) Method() int {
	return 1
}

func Untouched() int { return 0 }

type T struct{}
`

// The branch edits one line of Held, Raised, Lowered, Paid and Method, deletes a
// line of Trimmed, and adds New.
const reviewFeature = `package pkg

func Held(n int) int {
	return n + 10
}

func Raised(n int) int {
	return n + 20
}

func Lowered(n int) int {
	return n + 30
}

func Paid(n int) int {
	return n + 40
}

func Trimmed(n int) int {
	n++
	return n
}

func (t *T) Method() int {
	return 2
}

func Untouched() int { return 0 }

type T struct{}

func New(n int) int {
	return n * 2
}
`

const reviewNewFile = `package pkg

func Added() int { return 1 }

func (t T) Tail() int {
	return 3
}
`

const reviewLedger = `{"build":{"goos":"linux","goarch":"amd64","tags":[],"cgo_enabled":true},"judged_laws":0,"ledger":{"functions":{` +
	`"example.com/m/pkg.Held":20,"example.com/m/pkg.Raised":12,"example.com/m/pkg.Lowered":30,"example.com/m/pkg.Paid":15,` +
	`"example.com/m/pkg.Untouched":11}}}`

// repo is a throwaway git repository.
type repo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git is required under -tags skills: %v", err)
	}
	r := &repo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", "main")
	return r
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = r.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *repo) write(name, content string) {
	r.t.Helper()
	p := filepath.Join(r.dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repo) commit(msg string) {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "-m", msg)
}

// reviewRepo is a repository on a feature branch off main, with origin/main at
// the base and no report yet.
func reviewRepo(t *testing.T) *repo {
	t.Helper()
	r := newRepo(t)
	r.write("pkg/a.go", reviewBase)
	r.write(".go-tops/baseline.json", reviewLedger)
	r.commit("base")
	r.git("update-ref", "refs/remotes/origin/main", "HEAD")
	r.git("checkout", "-q", "-b", "feat")
	r.write("pkg/a.go", reviewFeature)
	r.write("pkg/b.go", reviewNewFile)
	r.write("pkg/a_test.go", "package pkg\n\nfunc TestHeld() {}\n")
	r.commit("feature")
	return r
}

// writeReport writes a report for the repository's HEAD whose crap layer lists
// the violations, as the gate writes it.
func (r *repo) writeReport(violations []map[string]any) {
	r.t.Helper()
	rep := map[string]any{
		"module":  reviewModule,
		"enforce": "ratchet",
		"git":     map[string]any{"head": r.git("rev-parse", "HEAD"), "base": "origin/main", "dirty": false},
		"ratchet": map[string]any{"outcome": "fail"},
		"summary": map[string]any{"outcome": "fail", "failed": []string{"crap"}},
		"layers": []any{
			map[string]any{"id": "crap", "outcome": "fail", "details": map[string]any{"cap": 10, "violations": violations}},
		},
	}
	b, err := json.Marshal(rep)
	if err != nil {
		r.t.Fatal(err)
	}
	r.write(".go-tops/report.json", string(b))
}

func violation(fn, file string, cc int, cov, crap float64) map[string]any {
	return map[string]any{"func": reviewModule + "/pkg." + fn, "file": file, "cc": cc, "cov_pct": cov, "crap": crap, "baseline": nil}
}

func reviewViolations() []map[string]any {
	return []map[string]any{
		violation("Held", "pkg/a.go", 12, 40, 20),
		violation("Raised", "pkg/a.go", 13, 30, 14.5),
		violation("Lowered", "pkg/a.go", 11, 70, 25),
		violation("New", "pkg/a.go", 15, 10, 60),
		violation("Untouched", "pkg/a.go", 11, 50, 11), // not in the diff
	}
}

type fnRow struct {
	State, File           string
	CRAP, Complexity, Cov *float64
	Ledger                *float64
}

// functionsOf decodes data.functions into rows by function name.
func functionsOf(t *testing.T, e envelope) map[string]fnRow {
	t.Helper()
	b, err := json.Marshal(e.Data["functions"])
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Function    string   `json:"function"`
		File        string   `json:"file"`
		State       string   `json:"state"`
		CRAP        *float64 `json:"crap"`
		Complexity  *float64 `json:"complexity"`
		CoveragePct *float64 `json:"coverage_pct"`
		Ledger      *float64 `json:"ledger"`
	}
	if err := json.Unmarshal(b, &rows); err != nil {
		t.Fatal(err)
	}
	out := map[string]fnRow{}
	for _, r := range rows {
		out[r.Function] = fnRow{State: r.State, File: r.File, CRAP: r.CRAP, Complexity: r.Complexity, Cov: r.CoveragePct, Ledger: r.Ledger}
	}
	return out
}

func TestReviewDiffListsEachTouchedFunctionWithItsRatchetState(t *testing.T) {
	r := reviewRepo(t)
	r.writeReport(reviewViolations())
	got := runSkill(t, "go-tops-review", r.dir, nil, "diff")
	if !got.OK {
		t.Fatal(got.Error)
	}
	rows := functionsOf(t, got)
	want := map[string]string{
		"Held":     "held",      // 20 against an entry of 20
		"Raised":   "raised",    // 14.5 over an entry of 12
		"Lowered":  "lowered",   // 25 under an entry of 30
		"Paid":     "lowered",   // in the ledger, no longer over the cap
		"New":      "new",       // over the cap with no entry
		"Trimmed":  "under-cap", // a deleted line touches its function
		"T.Method": "under-cap",
		"Added":    "under-cap", // a one-line function of a new file
		"T.Tail":   "under-cap",
	}
	for name, state := range want {
		row, ok := rows[name]
		if !ok {
			t.Errorf("%s is not listed", name)
			continue
		}
		if row.State != state {
			t.Errorf("%s is %q, want %q", name, row.State, state)
		}
	}
	if _, ok := rows["Untouched"]; ok {
		t.Error("Untouched is listed, but the diff does not touch it")
	}
	if len(rows) != len(want) {
		t.Errorf("%d functions listed, want %d: %v", len(rows), len(want), rows)
	}
	raised := rows["Raised"]
	if raised.CRAP == nil || *raised.CRAP != 14.5 || raised.Complexity == nil || *raised.Complexity != 13 || raised.Cov == nil || *raised.Cov != 30 || raised.Ledger == nil || *raised.Ledger != 12 {
		t.Errorf("Raised lists %+v: want CRAP 14.5, complexity 13, coverage 30 and the ledger entry 12", raised)
	}
	if rows["Added"].File != "pkg/b.go" {
		t.Errorf("Added is in %q, want pkg/b.go", rows["Added"].File)
	}
}

func TestReviewDiffPutsTheNewAndRaisedFunctionsInAttention(t *testing.T) {
	r := reviewRepo(t)
	r.writeReport(reviewViolations())
	got := runSkill(t, "go-tops-review", r.dir, nil, "diff")
	attention := asList(got.Data["attention"])
	var names []string
	for _, a := range attention {
		names = append(names, asStr(asObj(a)["function"]))
	}
	if len(names) != 2 || !strings.Contains(strings.Join(names, ","), "Raised") || !strings.Contains(strings.Join(names, ","), "New") {
		t.Errorf("attention = %v, want Raised and New", names)
	}
}

func TestReviewDiffListsTheJudgmentRulesAndGivesNoVerdict(t *testing.T) {
	r := reviewRepo(t)
	r.writeReport(reviewViolations())
	got := runSkill(t, "go-tops-review", r.dir, nil, "diff")
	rules := asList(got.Data["judgment"])
	ids := map[string]bool{}
	for _, x := range rules {
		m := asObj(x)
		if m["rule"] == "" {
			t.Errorf("rule %v has no text", m["id"])
		}
		ids[asStr(m["id"])] = true
	}
	for _, id := range []string{"ORACLE", "SCAR", "WEAKEN", "GENERATED", "DELETE", "RECORD", "SUPPRESS"} {
		if !ids[id] {
			t.Errorf("the judgment list lacks %s", id)
		}
	}
	for _, banned := range []string{"verdict", "approve", "pass"} {
		if _, ok := got.Data[banned]; ok {
			t.Errorf("the result carries a %q field: the script gives no verdict", banned)
		}
	}
}

func TestReviewDiffReadsTheWorkingTreeUnlessCommittedOnly(t *testing.T) {
	r := reviewRepo(t)
	r.writeReport(reviewViolations())
	edited := strings.Replace(reviewFeature, "func Untouched() int { return 0 }", "func Untouched() int { return 7 }", 1)
	r.write("pkg/a.go", edited)
	r.write("pkg/c.go", "package pkg\n\nfunc Loose() int { return 9 }\n")
	with := functionsOf(t, runSkill(t, "go-tops-review", r.dir, nil, "diff"))
	if _, ok := with["Untouched"]; !ok {
		t.Error("an edit in the working tree is not listed")
	}
	if _, ok := with["Loose"]; !ok {
		t.Error("an untracked file is not listed")
	}
	without := functionsOf(t, runSkill(t, "go-tops-review", r.dir, nil, "diff", "--committed-only"))
	for _, name := range []string{"Untouched", "Loose"} {
		if _, ok := without[name]; ok {
			t.Errorf("%s is listed under --committed-only", name)
		}
	}
	if _, ok := without["Raised"]; !ok {
		t.Error("a committed change is missing under --committed-only")
	}
}

func TestReviewDiffFailsClosedWithoutAReportForThisHead(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(*repo)
		err   string
	}{
		{"no report", func(r *repo) {}, "missing"},
		{"a report that is not JSON", func(r *repo) { r.write(".go-tops/report.json", "oops\n") }, "not a JSON report"},
		{"a report for another head", func(r *repo) {
			r.writeReport(reviewViolations())
			b, err := os.ReadFile(filepath.Join(r.dir, ".go-tops", "report.json"))
			if err != nil {
				t.Fatal(err)
			}
			r.write(".go-tops/report.json", strings.Replace(string(b), r.git("rev-parse", "HEAD"), "0123456789abcdef0123456789abcdef01234567", 1))
		}, "run go-tops-gate again"},
		{"a report with no crap layer", func(r *repo) {
			r.write(".go-tops/report.json", `{"module":"`+reviewModule+`","git":{"head":"`+r.git("rev-parse", "HEAD")+`"},"layers":[]}`)
		}, "no crap layer"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := reviewRepo(t)
			c.setup(r)
			got := runSkill(t, "go-tops-review", r.dir, nil, "diff")
			if got.OK || !strings.Contains(got.Error, c.err) {
				t.Fatalf("ok = %v, error %q; want a failure naming %q", got.OK, got.Error, c.err)
			}
		})
	}
}

func TestReviewDiffFailsWithoutAForkPoint(t *testing.T) {
	r := reviewRepo(t)
	r.writeReport(reviewViolations())
	got := runSkill(t, "go-tops-review", r.dir, nil, "diff", "--base", "origin/nowhere")
	if got.OK || !strings.Contains(got.Error, "merge-base") {
		t.Fatalf("ok = %v, error %q", got.OK, got.Error)
	}
}

func TestReviewFilesListsTheChangesAndTheProtocolLabel(t *testing.T) {
	r := reviewRepo(t)
	got := runSkill(t, "go-tops-review", r.dir, nil, "files")
	if !got.OK {
		t.Fatal(got.Error)
	}
	srcs := asList(got.Data["go_sources"])
	if len(srcs) != 2 {
		t.Errorf("go_sources = %v, want pkg/a.go and pkg/b.go and no test file", srcs)
	}
	pc := asObj(got.Data["protocol_change"])
	if pc["label"] != false {
		t.Errorf("protocol_change = %v for a diff that does not touch the protocol", pc)
	}
	r.write("schema/x.schema.json", "{}\n")
	r.write("docs/note.md", "# note\n")
	r.commit("schema")
	got = runSkill(t, "go-tops-review", r.dir, nil, "files")
	pc = asObj(got.Data["protocol_change"])
	paths := asList(pc["paths"])
	if pc["label"] != true || len(paths) != 1 || paths[0] != "schema/x.schema.json" {
		t.Errorf("protocol_change = %v, want the label for schema/x.schema.json alone", pc)
	}
}

func TestReviewFilesFailsWithoutABase(t *testing.T) {
	r := reviewRepo(t)
	got := runSkill(t, "go-tops-review", r.dir, nil, "files", "--base", "origin/nowhere")
	if got.OK {
		t.Fatal("a base that does not exist gave ok: true")
	}
}

func TestReviewAnswersChecksEveryRuleIsAnswered(t *testing.T) {
	all := func(skip string, over map[string]string) string {
		var b strings.Builder
		b.WriteString("[answers]\n")
		for _, id := range []string{"ORACLE", "SCAR", "WEAKEN", "GENERATED", "DELETE", "RECORD", "SUPPRESS", "WRITE-CLOSE", "GOROUTINES", "INTERFACES", "SYNC", "TESTS"} {
			if id == skip {
				continue
			}
			line := `"` + id + `" = { status = "ok", note = "" }`
			if o, ok := over[id]; ok {
				line = `"` + id + `" = ` + o
			}
			b.WriteString(line + "\n")
		}
		return b.String()
	}
	for _, c := range []struct {
		name, toml string
		ok         bool
		err        string
		findings   int
	}{
		{"every rule answered", all("", nil), true, "", 0},
		{"a finding with a note", all("", map[string]string{"SCAR": `{ status = "finding", note = "pkg/a.go:4 splits parse for its score" }`}), true, "", 1},
		{"n/a is an answer", all("", map[string]string{"TESTS": `{ status = "n/a", note = "" }`}), true, "", 0},
		{"a missing rule", all("SYNC", nil), false, "no answer for SYNC", 0},
		{"an unknown rule", all("", nil) + `"MAGIC" = { status = "ok", note = "" }` + "\n", false, "unknown rule MAGIC", 0},
		{"a finding with no note", all("", map[string]string{"SCAR": `{ status = "finding", note = "" }`}), false, "needs a note", 0},
		{"a status that is not one", all("", map[string]string{"SCAR": `{ status = "fine", note = "" }`}), false, "status must be", 0},
		{"no answers table", "title = \"x\"\n", false, "no [answers] table", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "answers.toml")
			if err := os.WriteFile(file, []byte(c.toml), 0o644); err != nil {
				t.Fatal(err)
			}
			got := runSkill(t, "go-tops-review", dir, nil, "answers", file)
			if got.OK != c.ok || !strings.Contains(got.Error, c.err) {
				t.Fatalf("ok = %v, error %q; want ok %v with %q", got.OK, got.Error, c.ok, c.err)
			}
			if c.ok {
				if fs := asList(got.Data["findings"]); len(fs) != c.findings {
					t.Errorf("findings = %v, want %d", fs, c.findings)
				}
			}
		})
	}
}

func TestReviewAnswersFailsOnAMissingFile(t *testing.T) {
	got := runSkill(t, "go-tops-review", t.TempDir(), nil, "answers", "no-such-file.toml")
	if got.OK || !strings.Contains(got.Error, "does not exist") {
		t.Fatalf("ok = %v, error %q", got.OK, got.Error)
	}
}

func TestReviewWithNoCommandNamesTheThree(t *testing.T) {
	needNu(t)
	out, err := exec.Command("nu", skillScript(t, "go-tops-review")).Output()
	if err != nil {
		t.Fatal(err)
	}
	got := decodeEnvelope(t, out)
	if got.OK || !strings.Contains(got.Error, "files, diff or answers") {
		t.Fatalf("ok = %v, error %q", got.OK, got.Error)
	}
}
