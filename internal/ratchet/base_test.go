package ratchet_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/crap"
	"github.com/Hedronite/go-tops/internal/ratchet"
)

// The diff base (GO_TOPS.md §6.7): the baseline is read at the merge base of
// --base through git. No merge base, or a shallow clone, fails.

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	return dir
}

func commit(t *testing.T, dir, msg string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", msg)
}

// ledgerFile is a baseline file for a ledger with one function entry at score.
func ledgerFile(t *testing.T, score float64) string {
	t.Helper()
	doc := map[string]any{
		"build":       map[string]any{"goos": "linux", "goarch": "amd64", "tags": []string{}, "cgo_enabled": true},
		"ledger":      map[string]any{"functions": map[string]float64{"example.com/m/p.F": score}},
		"judged_laws": 3,
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

const baselinePath = ".go-tops/baseline.json"

func TestBaseWithoutALedgerIsAFirstLedger(t *testing.T) {
	dir := repo(t)
	commit(t, dir, "base", map[string]string{"a.go": "package a\n"})
	git(t, dir, "checkout", "-q", "-b", "feature")
	commit(t, dir, "adopt", map[string]string{baselinePath: ledgerFile(t, 12)})
	b := ratchet.ReadBase(dir, "main", baselinePath)
	if b.Err != nil || b.Ledger != nil {
		t.Fatalf("base %+v, want no error and no ledger: a first ledger", b)
	}
}

func TestBaseLedgerIsReadAtTheMergeBase(t *testing.T) {
	dir := repo(t)
	commit(t, dir, "base", map[string]string{baselinePath: ledgerFile(t, 12)})
	git(t, dir, "checkout", "-q", "-b", "feature")
	commit(t, dir, "feature work", map[string]string{"a.go": "package a\n"})
	git(t, dir, "checkout", "-q", "main")
	// main moves on and lowers its ledger after the branch point.
	commit(t, dir, "main moves", map[string]string{baselinePath: ledgerFile(t, 9)})
	git(t, dir, "checkout", "-q", "feature")

	b := ratchet.ReadBase(dir, "main", baselinePath)
	if b.Err != nil || b.Ledger == nil {
		t.Fatalf("base %+v", b)
	}
	if got := b.Ledger.Functions["example.com/m/p.F"]; got != 12 {
		t.Errorf("the base ledger holds %v, want 12: the copy at the merge base, not at the tip of main", got)
	}
}

// The working tree's file is not the base: a hand edit in the tree changes the
// file the judge compares, never the base it is compared with.
func TestBaseIgnoresTheWorkingTree(t *testing.T) {
	dir := repo(t)
	commit(t, dir, "base", map[string]string{baselinePath: ledgerFile(t, 12)})
	git(t, dir, "checkout", "-q", "-b", "feature")
	commit(t, dir, "work", map[string]string{"a.go": "package a\n"})
	if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(baselinePath)), []byte(ledgerFile(t, 99)), 0o600); err != nil {
		t.Fatal(err)
	}
	b := ratchet.ReadBase(dir, "main", baselinePath)
	if b.Err != nil || b.Ledger == nil || b.Ledger.Functions["example.com/m/p.F"] != 12 {
		t.Fatalf("base %+v, want the committed 12", b)
	}
}

func TestNoMergeBaseFails(t *testing.T) {
	dir := repo(t)
	commit(t, dir, "base", map[string]string{"a.go": "package a\n"})
	git(t, dir, "checkout", "-q", "--orphan", "unrelated")
	commit(t, dir, "an unrelated history", map[string]string{"b.go": "package b\n"})
	b := ratchet.ReadBase(dir, "main", baselinePath)
	if b.Err == nil || !strings.Contains(b.Err.Error(), "merge base") {
		t.Fatalf("base %+v, want an error naming the merge base", b)
	}
}

func TestShallowCloneFails(t *testing.T) {
	src := repo(t)
	commit(t, src, "one", map[string]string{baselinePath: ledgerFile(t, 12)})
	commit(t, src, "two", map[string]string{"a.go": "package a\n"})
	clone := filepath.Join(t.TempDir(), "clone")
	git(t, filepath.Dir(clone), "clone", "-q", "--depth", "1", "file://"+src, clone)
	b := ratchet.ReadBase(clone, "origin/main", baselinePath)
	if b.Err == nil || !strings.Contains(b.Err.Error(), "shallow") {
		t.Fatalf("base %+v, want an error naming the shallow clone", b)
	}
}

func TestUnknownBaseFails(t *testing.T) {
	dir := repo(t)
	commit(t, dir, "base", map[string]string{"a.go": "package a\n"})
	if b := ratchet.ReadBase(dir, "origin/main", baselinePath); b.Err == nil {
		t.Fatal("a base that does not exist was accepted")
	}
}

func TestBaseLedgerThatCannotBeReadFails(t *testing.T) {
	dir := repo(t)
	commit(t, dir, "base", map[string]string{baselinePath: "{not json"})
	git(t, dir, "checkout", "-q", "-b", "feature")
	commit(t, dir, "work", map[string]string{"a.go": "package a\n"})
	if b := ratchet.ReadBase(dir, "main", baselinePath); b.Err == nil {
		t.Fatal("an unreadable base ledger was treated as absent")
	}
}

// A failed base read fails the judgment: no guess is made about what the base held.
func TestJudgeFailsOnABaseError(t *testing.T) {
	m := legacy()
	l := initLedger(m, linux)
	j := ratchet.Judge(input(m, l, ratchet.Base{Err: os.ErrNotExist}))
	wantOutcome(t, j, ratchet.Fail)
	wantFailure(t, j, "diff base", "")
}

func TestParseRefusesWhatTheSchemaRefuses(t *testing.T) {
	good := ledgerFile(t, 12)
	if _, err := ratchet.Parse([]byte(good)); err != nil {
		t.Fatalf("a good ledger was refused: %v", err)
	}
	for name, raw := range map[string]string{
		"not json":      "{",
		"unknown field": strings.Replace(good, `"judged_laws"`, `"judged_law"`, 1),
		"no build":      `{"ledger":{},"judged_laws":0}`,
	} {
		if _, err := ratchet.Parse([]byte(raw)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

// The base copy is read at the configured path too.
func TestBaseLedgerIsReadAtTheConfiguredPath(t *testing.T) {
	dir := repo(t)
	commit(t, dir, "base", map[string]string{
		"go-tops.yaml": "baseline: ledgers/main.json\n", "ledgers/main.json": ledgerFile(t, 12), baselinePath: ledgerFile(t, 99),
	})
	git(t, dir, "checkout", "-q", "-b", "feature")
	commit(t, dir, "work", map[string]string{"a.go": "package a\n"})
	b := ratchet.ReadBase(dir, "main", "ledgers/main.json")
	if b.Err != nil || b.Ledger == nil || b.Ledger.Functions["example.com/m/p.F"] != 12 {
		t.Fatalf("base %+v, want the ledger at the configured path, 12", b)
	}
	// The base is read where the base commit keeps it, whatever path this run asks for.
	if other := ratchet.ReadBase(dir, "main", "ledgers/other.json"); other.Err != nil || other.Ledger == nil || other.Moved == nil {
		t.Errorf("base %+v, want the base's own copy and a move", other)
	}
}

// The base copy is read where the base commit's own go-tops.yaml keeps it. A run
// whose configuration names another path judges against that copy and says so.
func TestBaseIsReadAtThePathTheBaseCommitNames(t *testing.T) {
	dir := repo(t)
	commit(t, dir, "base", map[string]string{
		"go-tops.yaml":    "protocol: \"" + protocolID + "\"\nbaseline: old/ledger.json\n",
		"old/ledger.json": ledgerFile(t, 12),
	})
	git(t, dir, "checkout", "-q", "-b", "feature")
	commit(t, dir, "move the baseline", map[string]string{
		"go-tops.yaml":    "protocol: \"" + protocolID + "\"\nbaseline: new/ledger.json\n",
		"new/ledger.json": ledgerFile(t, 99),
	})
	moved := ratchet.ReadBase(dir, "main", "new/ledger.json")
	if moved.Err != nil || moved.Ledger == nil || moved.Ledger.Functions["example.com/m/p.F"] != 12 {
		t.Fatalf("base %+v, want the copy at old/ledger.json, 12", moved)
	}
	if moved.Moved == nil || moved.Moved.From != "old/ledger.json" || moved.Moved.To != "new/ledger.json" {
		t.Errorf("moved %+v, want old/ledger.json to new/ledger.json", moved.Moved)
	}
	same := ratchet.ReadBase(dir, "main", "old/ledger.json")
	if same.Moved != nil || same.Err != nil {
		t.Errorf("same path: %+v, want no move", same)
	}
	// A path written another way is the same path.
	if again := ratchet.ReadBase(dir, "main", "./old//ledger.json"); again.Moved != nil {
		t.Errorf("an equivalent path read as a move: %+v", again.Moved)
	}
}

func TestBaseCommitWithoutAConfigurationKeepsTheLedgerAtTheDefaultPath(t *testing.T) {
	dir := repo(t)
	commit(t, dir, "base", map[string]string{baselinePath: ledgerFile(t, 12)})
	git(t, dir, "checkout", "-q", "-b", "feature")
	commit(t, dir, "adopt a path", map[string]string{"go-tops.yaml": "baseline: elsewhere.json\n", "elsewhere.json": ledgerFile(t, 50)})
	b := ratchet.ReadBase(dir, "main", "elsewhere.json")
	if b.Err != nil || b.Ledger == nil || b.Ledger.Functions["example.com/m/p.F"] != 12 || b.Moved == nil || b.Moved.From != baselinePath {
		t.Errorf("base %+v moved %+v, want the default path's copy and a move from it", b, b.Moved)
	}
}

func TestBaseConfigurationThatDoesNotParseFails(t *testing.T) {
	dir := repo(t)
	commit(t, dir, "base", map[string]string{"go-tops.yaml": "baseline: [unclosed\n", baselinePath: ledgerFile(t, 12)})
	git(t, dir, "checkout", "-q", "-b", "feature")
	commit(t, dir, "work", map[string]string{"a.go": "package a\n"})
	if b := ratchet.ReadBase(dir, "main", baselinePath); b.Err == nil {
		t.Error("a base go-tops.yaml that does not parse was read as naming the default path")
	}
}

// A module in a subdirectory of the repository. `rev:path` is read from the root of the
// repository, so the base copy of such a module was "not found" and the judge applied the
// first-ledger rule, which a hand-raised entry equal to the measure passes. The base is
// read relative to the module, for the ledger and for the base go-tops.yaml.
func TestBaseIsReadForAModuleInASubdirectory(t *testing.T) {
	root := repo(t)
	commit(t, root, "base", map[string]string{
		"sub/go-tops.yaml":      "baseline: ledgers/main.json\n",
		"sub/ledgers/main.json": ledgerFile(t, 12),
		"root-only.txt":         "x\n",
	})
	git(t, root, "checkout", "-q", "-b", "feature")
	commit(t, root, "raise an entry by hand", map[string]string{"sub/ledgers/main.json": ledgerFile(t, 17)})
	sub := filepath.Join(root, "sub")

	b := ratchet.ReadBase(sub, "main", "ledgers/main.json")
	if b.Err != nil || b.Ledger == nil || b.Ledger.Functions["example.com/m/p.F"] != 12 {
		t.Fatalf("base %+v, want the copy at sub/ledgers/main.json, 12", b)
	}
	if b.Moved != nil {
		t.Errorf("moved %+v: the base go-tops.yaml of the module names the same path", b.Moved)
	}

	// The hand-raised entry equals what the run measures, and it must still fail: it is
	// raised against the base copy, which the judge now finds.
	tree, err := ratchet.Parse([]byte(ledgerFile(t, 17)))
	if err != nil {
		t.Fatal(err)
	}
	m := ratchet.Measure{Cap: 10, Floor: 90, Functions: []crap.Function{{Key: "example.com/m/p.F", CRAP: 17}}}
	j := ratchet.Judge(ratchet.Input{Ledger: tree, Base: b, Run: tree.Build, M: m})
	wantOutcome(t, j, ratchet.Fail)
	wantFailure(t, j, "regressions", "example.com/m/p.F")
}

// The same for the base go-tops.yaml: it is the module's, not the repository root's.
func TestBaseConfigurationIsTheModulesInASubdirectory(t *testing.T) {
	root := repo(t)
	commit(t, root, "base", map[string]string{
		"go-tops.yaml":        "baseline: wrong/path.json\n", // the repository root's, not the module's
		"sub/go-tops.yaml":    "baseline: old/ledger.json\n",
		"sub/old/ledger.json": ledgerFile(t, 12),
	})
	git(t, root, "checkout", "-q", "-b", "feature")
	commit(t, root, "move the baseline", map[string]string{"sub/go-tops.yaml": "baseline: new/ledger.json\n", "sub/new/ledger.json": ledgerFile(t, 99)})
	b := ratchet.ReadBase(filepath.Join(root, "sub"), "main", "new/ledger.json")
	if b.Err != nil || b.Ledger == nil || b.Ledger.Functions["example.com/m/p.F"] != 12 {
		t.Fatalf("base %+v, want the copy at the module's own old/ledger.json", b)
	}
	if b.Moved == nil || b.Moved.From != "old/ledger.json" {
		t.Errorf("moved %+v, want a move from the module's old/ledger.json", b.Moved)
	}
}

// The module may be in any directory of the repository: QA's plant has it in mod/.
func TestBaseIsReadForAModuleInModDirectory(t *testing.T) {
	root := repo(t)
	commit(t, root, "base", map[string]string{"mod/" + baselinePath: ledgerFile(t, 12), "README.md": "x\n"})
	git(t, root, "checkout", "-q", "-b", "feature")
	commit(t, root, "raise", map[string]string{"mod/" + baselinePath: ledgerFile(t, 17)})
	b := ratchet.ReadBase(filepath.Join(root, "mod"), "main", baselinePath)
	if b.Err != nil || b.Ledger == nil || b.Ledger.Functions["example.com/m/p.F"] != 12 {
		t.Fatalf("base %+v, want the copy at mod/%s, 12", b, baselinePath)
	}
}

// A base go-tops.yaml that does not parse fails, for a module in a subdirectory too. It
// was never read there: the bare path named another file, so the bypass also hid a
// broken base configuration.
func TestBaseConfigurationThatDoesNotParseFailsInASubdirectory(t *testing.T) {
	root := repo(t)
	commit(t, root, "base", map[string]string{"mod/go-tops.yaml": "baseline: [unclosed\n", "mod/" + baselinePath: ledgerFile(t, 12)})
	git(t, root, "checkout", "-q", "-b", "feature")
	commit(t, root, "work", map[string]string{"mod/a.go": "package a\n"})
	b := ratchet.ReadBase(filepath.Join(root, "mod"), "main", baselinePath)
	if b.Err == nil || !strings.Contains(b.Err.Error(), "go-tops.yaml at the diff base") {
		t.Errorf("base %+v, want an error naming the base go-tops.yaml", b)
	}
}
