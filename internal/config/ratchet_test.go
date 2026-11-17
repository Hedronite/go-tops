package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Hedronite/go-tops/internal/config"
)

// GO_TOPS.md §12.1: under ratchet the baseline file exists and is clean against
// HEAD. §6.7: adopters default to ratchet. §12.2: a missing or dirty baseline
// is a configuration failure (2); a repository that cannot be read is an
// environment failure (3).

const baselineJSON = `{"ledger":{},"judged_laws":0}` + "\n"

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
				writeFile(t, dir, ".go-tops/baseline.json", []byte(`{"ledger":{},"judged_laws":1}`+"\n"))
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
	if _, err := config.Check(dir, opts); err == nil {
		t.Fatal("Check succeeded outside a git repository under ratchet, want an environment error")
	}
}

func TestRatchetBaselineMissingFileNeedsNoGit(t *testing.T) {
	// The file is absent, so the answer is known without asking git.
	dir := writeModule(t, ratchetDoc(t), nil)
	res, err := config.Check(dir, opts)
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
