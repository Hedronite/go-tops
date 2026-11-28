package config_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Hedronite/go-tops/internal/config"
)

// GO_TOPS.md §12.1: under ratchet the baseline file exists and is clean against
// HEAD. §6.7: adopters default to ratchet. §12.2: a missing or dirty baseline
// is a configuration failure (2); a repository that cannot be read is an
// environment failure (3).

const baselineJSON = `{"build":{"goos":"linux","goarch":"amd64","tags":[],"cgo_enabled":true},"ledger":{},"judged_laws":0}` + "\n"

func ratchetDoc(t *testing.T) doc {
	t.Helper()
	d := baseDoc(t)
	d["enforce"] = "ratchet"
	return d
}

// ratchetRepo writes a module, makes it a git repository with every file
// committed, and returns its directory.
func ratchetRepo(t *testing.T, d doc, files map[string]string) string {
	t.Helper()
	dir := writeModule(t, d, files)
	git(t, dir, "init", "-q")
	commitAll(t, dir)
	return dir
}

func baselineWant(pointer string) want {
	return want{rule: "baseline", pointer: ptr(pointer)}
}

func TestRatchetPassesWithACleanCommittedBaseline(t *testing.T) {
	requirePass(t, ratchetRepo(t, ratchetDoc(t), map[string]string{".go-tops/baseline.json": baselineJSON}))
}

func TestRatchetIsTheDefaultEnforce(t *testing.T) {
	d := baseDoc(t)
	delete(d, "enforce")
	dir := ratchetRepo(t, d, nil)
	expect(t, check(t, dir).Issues, want{rule: "baseline", pointer: ptr("/enforce"), contains: "does not exist"})
}

func TestHardAndAdvisoryNeedNoBaselineAndNoGit(t *testing.T) {
	requirePass(t, writeModule(t, baseDoc(t), nil)) // hard, not a git repository
	d := baseDoc(t)
	d["enforce"] = "advisory"
	d["advisory"] = doc{"reason": "the laws layer lands at M5", "owner": "x", "expires": "2027-06-01"}
	requirePass(t, writeModule(t, d, nil))
}

func TestRatchetBaselineProblems(t *testing.T) {
	tests := []struct {
		name     string
		doc      func(*testing.T) doc
		files    map[string]string
		after    func(t *testing.T, dir string)
		wantText string
		pointer  string
	}{
		{name: "missing", wantText: "does not exist", pointer: "/enforce"},
		{
			name: "present but untracked", wantText: "not tracked",
			pointer: "/enforce",
			after: func(t *testing.T, dir string) {
				writeFile(t, dir, ".go-tops/baseline.json", []byte(baselineJSON))
			},
		},
		{
			name: "tracked then modified", files: map[string]string{".go-tops/baseline.json": baselineJSON}, wantText: "differs from HEAD",
			pointer: "/enforce",
			after: func(t *testing.T, dir string) {
				writeFile(t, dir, ".go-tops/baseline.json", []byte(`{"build":{"goos":"linux","goarch":"amd64","tags":[],"cgo_enabled":true},"ledger":{},"judged_laws":1}`+"\n"))
			},
		},
		{
			name: "staged but not committed", wantText: "differs from HEAD", pointer: "/enforce",
			after: func(t *testing.T, dir string) {
				writeFile(t, dir, ".go-tops/baseline.json", []byte(baselineJSON))
				git(t, dir, "add", ".go-tops/baseline.json")
			},
		},
		{
			name: "tracked then deleted", files: map[string]string{".go-tops/baseline.json": baselineJSON}, wantText: "does not exist", pointer: "/enforce",
			after: func(t *testing.T, dir string) {
				if err := os.Remove(filepath.Join(dir, ".go-tops", "baseline.json")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{name: "named by the baseline key", doc: func(t *testing.T) doc {
			d := ratchetDoc(t)
			d["baseline"] = "ledgers/base.json"
			return d
		}, wantText: "ledgers/base.json", pointer: "/baseline"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mk := tt.doc
			if mk == nil {
				mk = ratchetDoc
			}
			dir := ratchetRepo(t, mk(t), tt.files)
			if tt.after != nil {
				tt.after(t, dir)
			}
			expect(t, check(t, dir).Issues, want{rule: "baseline", pointer: ptr(tt.pointer), contains: tt.wantText})
		})
	}
}

// §6.7: the baseline is a debt ledger. Its content is validated against the
// baseline definition of core.schema.json, clean or not.
func TestRatchetBaselineContent(t *testing.T) {
	const file = ".go-tops/baseline.json"
	bw := func(rule, pointer string) want { return want{file: file, rule: rule, pointer: ptr(pointer)} }
	tests := []struct {
		name, content string
		want          want
	}{
		{"empty object", "{}", bw("schema", "")},
		{"build missing", `{"ledger":{},"judged_laws":0}`, bw("schema", "")},
		{"build without cgo_enabled", `{"build":{"goos":"linux","goarch":"amd64","tags":[]},"ledger":{},"judged_laws":0}`, bw("schema", "/build")},
		{"build goos empty", `{"build":{"goos":"","goarch":"amd64","tags":[],"cgo_enabled":true},"ledger":{},"judged_laws":0}`, bw("schema", "/build/goos")},
		{"build goos uppercase", `{"build":{"goos":"Linux","goarch":"amd64","tags":[],"cgo_enabled":true},"ledger":{},"judged_laws":0}`, bw("schema", "/build/goos")},
		{"build goarch missing", `{"build":{"goos":"linux","tags":[],"cgo_enabled":true},"ledger":{},"judged_laws":0}`, bw("schema", "/build")},
		{"build tags not an array", `{"build":{"goos":"linux","goarch":"amd64","tags":"a","cgo_enabled":true},"ledger":{},"judged_laws":0}`, bw("schema", "/build/tags")},
		{"build tag with a space", `{"build":{"goos":"linux","goarch":"amd64","tags":["a b"],"cgo_enabled":true},"ledger":{},"judged_laws":0}`, bw("schema", "/build/tags/0")},
		{"build tags repeated", `{"build":{"goos":"linux","goarch":"amd64","tags":["a","a"],"cgo_enabled":true},"ledger":{},"judged_laws":0}`, bw("schema", "/build/tags")},
		{"build cgo not a boolean", `{"build":{"goos":"linux","goarch":"amd64","tags":[],"cgo_enabled":1},"ledger":{},"judged_laws":0}`, bw("schema", "/build/cgo_enabled")},
		{"build unknown key", `{"build":{"goos":"linux","goarch":"amd64","tags":[],"cgo_enabled":true,"extra":1},"ledger":{},"judged_laws":0}`, bw("schema", "/build/extra")},
		{"not JSON", "{", bw("json", "")},
		{"empty file", "", bw("json", "")},
		{"two values", baselineJSON + baselineJSON, bw("json", "")},
		{"judged_laws negative", `{"build":{"goos":"linux","goarch":"amd64","tags":[],"cgo_enabled":true},"ledger":{},"judged_laws":-1}`, bw("schema", "/judged_laws")},
		{"judged_laws not an integer", `{"build":{"goos":"linux","goarch":"amd64","tags":[],"cgo_enabled":true},"ledger":{},"judged_laws":"many"}`, bw("schema", "/judged_laws")},
		{"unknown ledger kind", `{"build":{"goos":"linux","goarch":"amd64","tags":[],"cgo_enabled":true},"ledger":{"bogus":1},"judged_laws":0}`, bw("schema", "/ledger/bogus")},
		{"unknown top-level key", `{"build":{"goos":"linux","goarch":"amd64","tags":[],"cgo_enabled":true},"ledger":{},"judged_laws":0,"extra":1}`, bw("schema", "/extra")},
		{"function score negative", `{"build":{"goos":"linux","goarch":"amd64","tags":[],"cgo_enabled":true},"ledger":{"functions":{"a.F":-1}},"judged_laws":0}`, bw("schema", "/ledger/functions/a.F")},
		{"package percentage above 100", `{"build":{"goos":"linux","goarch":"amd64","tags":[],"cgo_enabled":true},"ledger":{"packages":{"a":{"statements_pct":101,"uncovered":1}}},"judged_laws":0}`, bw("schema", "/ledger/packages/a/statements_pct")},
		{"exemption count zero", `{"build":{"goos":"linux","goarch":"amd64","tags":[],"cgo_enabled":true},"ledger":{"exemptions":0},"judged_laws":0}`, bw("schema", "/ledger/exemptions")},
		{"regression without an owner", `{"build":{"goos":"linux","goarch":"amd64","tags":[],"cgo_enabled":true},"ledger":{},"judged_laws":0,"regressions":[{"kind":"functions","key":"a.F","from":1,"to":2,"reason":"three words here"}]}`, bw("schema", "/regressions/0")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := ratchetRepo(t, ratchetDoc(t), map[string]string{file: tt.content})
			expect(t, check(t, dir).Issues, tt.want)
		})
	}
}

func TestRatchetBaselineContentIsReportedEvenWhenDirty(t *testing.T) {
	const file = ".go-tops/baseline.json"
	dir := ratchetRepo(t, ratchetDoc(t), map[string]string{file: baselineJSON})
	writeFile(t, dir, file, []byte("{}"))
	issues := check(t, dir).Issues
	expect(t, issues, want{rule: "baseline", pointer: ptr("/enforce"), contains: "differs from HEAD"})
	expect(t, issues, want{file: file, rule: "schema", pointer: ptr("")})
}

func TestBaselineContentIsNotCheckedOutsideRatchet(t *testing.T) {
	dir := writeModule(t, baseDoc(t), map[string]string{".go-tops/baseline.json": "{"})
	requirePass(t, dir)
}

func TestARichBaselinePasses(t *testing.T) {
	rich := `{"build":{"goos":"linux","goarch":"amd64","tags":[],"cgo_enabled":true},"ledger":{"functions":{"internal/a.F":14.2},"packages":{"example.com/app/a":{"statements_pct":61.5,"uncovered":30}},` +
		`"findings":{"revive":{"a.go":2}},"laws":{"G-12":{"a.go":1}},"suppressions":{"nolint":{"errcheck":{"a.go":1}},"ack":{"G-12":{"a.go":1}}},` +
		`"exemptions":2,"survivors":{"a.go":1}},"judged_laws":16,` +
		`"regressions":[{"kind":"functions","key":"internal/a.F","from":null,"to":14.2,"reason":"accepted for the migration","owner":"x"}]}`
	requirePass(t, ratchetRepo(t, ratchetDoc(t), map[string]string{".go-tops/baseline.json": rich}))
}

// §6.7: the baseline records the build it was measured under; tags and a
// build without cgo are ordinary values.
func TestABaselineRecordsAnyBuild(t *testing.T) {
	b := `{"build":{"goos":"darwin","goarch":"arm64","tags":["integration","netgo"],"cgo_enabled":false},"ledger":{},"judged_laws":0}`
	requirePass(t, ratchetRepo(t, ratchetDoc(t), map[string]string{".go-tops/baseline.json": b}))
}

func TestRatchetBaselineKeyPathIsContained(t *testing.T) {
	d := ratchetDoc(t)
	d["baseline"] = "../baseline.json"
	expect(t, check(t, ratchetRepo(t, d, nil)).Issues, baselineWant("/baseline"))
}

func TestRatchetBaselineInARepositoryWithNoCommit(t *testing.T) {
	dir := writeModule(t, ratchetDoc(t), map[string]string{".go-tops/baseline.json": baselineJSON})
	git(t, dir, "init", "-q")
	expect(t, check(t, dir).Issues, want{rule: "baseline", pointer: ptr("/enforce"), contains: "no commit"})
}

func TestRatchetOutsideARepositoryIsAnEnvironmentError(t *testing.T) {
	dir := writeModule(t, ratchetDoc(t), map[string]string{".go-tops/baseline.json": baselineJSON})
	// Stop git looking above the module for a work tree the temp root may sit in.
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	if _, err := config.Check(context.Background(), dir, opts); err == nil {
		t.Fatal("Check succeeded outside a git repository under ratchet, want an environment error")
	}
}

func TestRatchetBaselineMissingFileNeedsNoGit(t *testing.T) {
	// The file is absent, so the answer is known without asking git.
	dir := writeModule(t, ratchetDoc(t), nil)
	res, err := config.Check(context.Background(), dir, opts)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	expect(t, res.Issues, want{rule: "baseline", contains: "does not exist"})
}

// The §9 example is a ratchet config: with its module, a committed baseline and
// the lock the repo ships, it passes.
func TestTheShippedExampleConfigPasses(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "go-tops.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := os.ReadFile(filepath.Join("..", "..", "config", "go-tops.example.lock"))
	if err != nil {
		t.Fatal(err)
	}
	dir := writeModule(t, nil, map[string]string{
		"go.mod":                 "module github.com/acme/myapp\n\ngo 1.26\n",
		"go-tops.yaml":           string(data),
		"go-tops.lock":           string(lock),
		".go-tops/baseline.json": baselineJSON,
	})
	git(t, dir, "init", "-q")
	commitAll(t, dir)
	requirePass(t, dir)
}
