package mutate

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hedronite/go-tops/internal/cover"
	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/gobuild"
)

func gitSetupOverlayRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, string(out))
		}
	}
	runGit("init", "-b", "main")
	runGit("config", "user.name", "Test Author")
	runGit("config", "user.email", "author@example.com")
	runGit("config", "commit.gpgsign", "false")
	writeTreeFiles(t, dir, files)
	runGit("add", ".")
	runGit("commit", "-m", "base")
	runGit("checkout", "-b", "feat")
	return dir
}

func writeTreeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func runOverlayOnDir(t *testing.T, dir string, build gobuild.Build, opts OverlayOptions) (Evaluation, error) {
	t.Helper()
	diff, err := ComputeDiff(t.Context(), gate.OS{}, dir, "main", build)
	if err != nil {
		t.Fatalf("ComputeDiff: %v", err)
	}
	env := gate.Env{
		Dir:    dir,
		Base:   "main",
		Exec:   gate.OS{},
		Config: gate.Config{Base: "main", Mutation: gate.MutationConfig{Engine: "overlay"}},
	}
	return RunOverlay(t.Context(), env, diff, build, opts)
}

func TestRunOverlayAllSixMutatorsAndNotViable(t *testing.T) {
	dir := gitSetupOverlayRepo(t, map[string]string{
		"go.mod": "module example.com/allmuts\n\ngo 1.26\n",
		"ops.go": "package allmuts\n\nfunc Stub() int { return 0 }\n",
		"ops_test.go": `package allmuts

import "testing"

func TestStub(t *testing.T) {
	if Stub() != 0 {
		t.Fatal("bad")
	}
}
`,
	})
	writeTreeFiles(t, dir, map[string]string{
		"ops.go": `package allmuts

func Compute(a, b int, ok bool) int {
	x := a + b
	if x < 10 && ok {
		x++
	}
	return -x
}

func Label(s string) string {
	return s + "!"
}
`,
		"ops_test.go": `package allmuts

import "testing"

func TestAllOps(t *testing.T) {
	if got := Compute(2, 3, true); got != -6 {
		t.Fatalf("Compute(2,3,true) = %d, want -6", got)
	}
	if got := Compute(5, 5, true); got != -10 {
		t.Fatalf("Compute(5,5,true) = %d, want -10", got)
	}
	if got := Compute(2, 3, false); got != -5 {
		t.Fatalf("Compute(2,3,false) = %d, want -5", got)
	}
	if got := Label("hi"); got != "hi!" {
		t.Fatalf("Label = %q", got)
	}
}
`,
	})
	ev, err := runOverlayOnDir(t, dir, gobuild.Build{CGOEnabled: true}, OverlayOptions{})
	if err != nil {
		t.Fatalf("RunOverlay: %v", err)
	}
	// Label string `+` mutates to `-`, failing Go type checking => NOT VIABLE (dropped=1, NOT killed!).
	if ev.Dropped != 1 {
		t.Fatalf("Dropped = %d, want 1 (NOT VIABLE string minus); ev=%+v", ev.Dropped, ev)
	}
	if ev.Mutants != 6 || ev.Killed != 6 || ev.SurvivedUnexplained != 0 {
		t.Fatalf("unexpected counts: %+v", ev)
	}
}

// V1: -vet=off must be passed on both baseline coverage and mutant compile so
// INVERT_LOGICAL on `x != 1 && x != 2` (forming `x != 1 || x != 2`) compiles and
// reports LIVED with a no-assertion test instead of failing vet as KILLED or NOT VIABLE.
func TestRunOverlayVetOffAllowsRedundantBoolLived(t *testing.T) {
	dir := gitSetupOverlayRepo(t, map[string]string{
		"go.mod": "module example.com/vetoff\n\ngo 1.26\n",
		"v.go":   "package vetoff\n\nfunc Valid(x int) bool { return false }\n",
		"v_test.go": `package vetoff

import "testing"

func TestBase(t *testing.T) { _ = Valid(1) }
`,
	})
	writeTreeFiles(t, dir, map[string]string{
		"v.go": `package vetoff

func Valid(x int) bool {
	return x != 1 && x != 2
}
`,
		"v_test.go": `package vetoff

import "testing"

func TestNoAssertion(t *testing.T) {
	_ = Valid(3)
}
`,
	})
	ev, err := runOverlayOnDir(t, dir, gobuild.Build{CGOEnabled: true}, OverlayOptions{})
	if err != nil {
		t.Fatalf("RunOverlay: %v", err)
	}
	if ev.Dropped != 0 || ev.Mutants != 3 || ev.SurvivedUnexplained != 3 {
		t.Fatalf("expected 3 LIVED mutants (0 dropped/not-viable), got %+v", ev)
	}
	foundInvertLogical := false
	for _, s := range ev.Survivors {
		if s.Status != "LIVED" {
			t.Errorf("expected LIVED (vet must be off), got %+v", s)
		}
		if s.Key.Mutator == "INVERT_LOGICAL" {
			foundInvertLogical = true
		}
	}
	if !foundInvertLogical {
		t.Fatalf("expected INVERT_LOGICAL survivor in %+v", ev.Survivors)
	}
}

func TestRunOverlaySwitchCasesTaggedAndTagless(t *testing.T) {
	baseFiles := map[string]string{
		"go.mod": "module example.com/swtest\n\ngo 1.26\n",
		"sw.go":  "package swtest\n\nfunc Classify(n int) string { return \"other\" }\n",
		"sw_test.go": `package swtest

import "testing"

func TestBase(t *testing.T) { _ = Classify(0) }
`,
	}
	prodTagless := `package swtest

func Classify(n int) string {
	switch {
	case n < 0:
		return "neg"
	case n < 10:
		return "small"
	default:
		return "big"
	}
}
`
	t.Run("tagless_weak_lived_never_not_covered", func(t *testing.T) {
		dir := gitSetupOverlayRepo(t, baseFiles)
		writeTreeFiles(t, dir, map[string]string{
			"sw.go": prodTagless,
			"sw_test.go": `package swtest

import "testing"

func TestWeak(t *testing.T) {
	if Classify(-1) != "neg" || Classify(5) != "small" || Classify(20) != "big" {
		t.Fatal("wrong")
	}
}
`,
		})
		ev, err := runOverlayOnDir(t, dir, gobuild.Build{CGOEnabled: true}, OverlayOptions{})
		if err != nil {
			t.Fatalf("RunOverlay: %v", err)
		}
		if ev.Mutants != 4 || ev.Killed != 2 || ev.SurvivedUnexplained != 2 {
			t.Fatalf("expected 4 mutants (2 killed, 2 LIVED), got %+v", ev)
		}
		for _, s := range ev.Survivors {
			if s.Status != "LIVED" || s.Key.Mutator != "CONDITIONALS_BOUNDARY" {
				t.Errorf("expected LIVED CONDITIONALS_BOUNDARY, got %+v", s)
			}
		}
	})

	t.Run("tagless_strong_kills_all", func(t *testing.T) {
		dir := gitSetupOverlayRepo(t, baseFiles)
		writeTreeFiles(t, dir, map[string]string{
			"sw.go": prodTagless,
			"sw_test.go": `package swtest

import "testing"

func TestStrong(t *testing.T) {
	for tc, want := range map[int]string{-1: "neg", 0: "small", 9: "small", 10: "big"} {
		if got := Classify(tc); got != want {
			t.Fatalf("Classify(%d) = %q, want %q", tc, got, want)
		}
	}
}
`,
		})
		ev, err := runOverlayOnDir(t, dir, gobuild.Build{CGOEnabled: true}, OverlayOptions{})
		if err != nil {
			t.Fatalf("RunOverlay: %v", err)
		}
		if ev.Mutants != 4 || ev.Killed != 4 || ev.SurvivedUnexplained != 0 {
			t.Fatalf("expected 4 killed, got %+v", ev)
		}
	})

	t.Run("tagged_switch_weak_and_strong", func(t *testing.T) {
		prodTagged := `package swtest

func Classify(n int) string {
	switch true {
	case n < 0:
		return "neg"
	default:
		return "nonneg"
	}
}
`
		dir := gitSetupOverlayRepo(t, baseFiles)
		writeTreeFiles(t, dir, map[string]string{
			"sw.go": prodTagged,
			"sw_test.go": `package swtest

import "testing"

func TestWeakTagged(t *testing.T) {
	if Classify(-1) != "neg" || Classify(2) != "nonneg" {
		t.Fatal("wrong")
	}
}
`,
		})
		ev, err := runOverlayOnDir(t, dir, gobuild.Build{CGOEnabled: true}, OverlayOptions{})
		if err != nil {
			t.Fatalf("RunOverlay tagged weak: %v", err)
		}
		if ev.Mutants != 2 || ev.Killed != 1 || ev.SurvivedUnexplained != 1 || ev.Survivors[0].Status != "LIVED" {
			t.Fatalf("tagged switch weak expected 1 killed + 1 LIVED, got %+v", ev)
		}
	})

	t.Run("uncovered_switch_reports_not_covered", func(t *testing.T) {
		dir := gitSetupOverlayRepo(t, baseFiles)
		writeTreeFiles(t, dir, map[string]string{
			"sw.go":      prodTagless,
			"sw_test.go": "package swtest\n\nimport \"testing\"\n\nfunc TestEmpty(t *testing.T) {}\n",
		})
		ev, err := runOverlayOnDir(t, dir, gobuild.Build{CGOEnabled: true}, OverlayOptions{})
		if err != nil {
			t.Fatalf("RunOverlay: %v", err)
		}
		if ev.Mutants != 4 || ev.SurvivedUnexplained != 4 {
			t.Fatalf("expected 4 NOT COVERED, got %+v", ev)
		}
		for _, s := range ev.Survivors {
			if s.Status != "NOT COVERED" {
				t.Errorf("expected NOT COVERED, got %s", s.Status)
			}
		}
	})
}

// G2: package-level const/var/type mutants always run (never NOT COVERED), while
// expressions inside uncalled package-level FuncLit use normal function cover blocks.
func TestRunOverlayPkgLevelDeclsAlwaysRun(t *testing.T) {
	dir := gitSetupOverlayRepo(t, map[string]string{
		"go.mod":  "module example.com/pkgdecl\n\ngo 1.26\n",
		"decl.go": "package pkgdecl\n\nconst Zero = 0\n",
		"d_test.go": `package pkgdecl

import "testing"

func TestBase(t *testing.T) {
	if Zero != 0 {
		t.Fatal("bad")
	}
}
`,
	})
	writeTreeFiles(t, dir, map[string]string{
		"decl.go": `package pkgdecl

const Sum = 2 + 3
const BadConst = "a" + "b"

var Double = 4 * 2

type Buf [1 + 1]byte

var UntestedLit = func(x int) bool { return x < 5 }
`,
		"d_test.go": `package pkgdecl

import "testing"

func TestDecls(t *testing.T) {
	if Sum != 5 {
		t.Fatalf("Sum = %d", Sum)
	}
	if len(Buf{}) != 2 {
		t.Fatal("bad buf")
	}
	// Double (var 4*2 -> 4/2) is not asserted => must run and report LIVED, never NOT COVERED.
	_ = Double
}
`,
	})
	ev, err := runOverlayOnDir(t, dir, gobuild.Build{CGOEnabled: true}, OverlayOptions{})
	if err != nil {
		t.Fatalf("RunOverlay pkg decls: %v", err)
	}
	// BadConst string + -> - is NOT VIABLE (Dropped=1).
	// Sum (killed), Double (LIVED), Buf (killed), plus UntestedLit has 2 NOT COVERED mutants (< -> <=, < -> >=).
	if ev.Dropped != 1 {
		t.Fatalf("expected 1 NOT VIABLE (BadConst), got %+v", ev)
	}
	if ev.Mutants != 5 || ev.Killed != 2 || ev.SurvivedUnexplained != 3 {
		t.Fatalf("expected 5 mutants (Sum+Buf killed, Double LIVED, 2x UntestedLit NOT COVERED), got %+v", ev)
	}
	statusByFunc := map[string]string{}
	for _, s := range ev.Survivors {
		statusByFunc[s.Key.Function] = s.Status
	}
	if statusByFunc["Double"] != "LIVED" {
		t.Errorf("package var Double must report LIVED (never NOT COVERED), got %+v", ev.Survivors)
	}
	if statusByFunc["UntestedLit"] != "NOT COVERED" {
		t.Errorf("uncalled FuncLit UntestedLit must report NOT COVERED, got %+v", ev.Survivors)
	}
}

// G6 / O6: per-package coverage means cross-package-only tests leave target NOT COVERED.
func TestRunOverlaySamePackageCoverageOnly(t *testing.T) {
	dir := gitSetupOverlayRepo(t, map[string]string{
		"go.mod":      "module example.com/crosspkg\n\ngo 1.26\n",
		"a/a.go":      "package a\n\nfunc Lt(x, y int) bool { return x == y }\n",
		"b/b_test.go": "package b\n\nimport (\n\t\"testing\"\n\t\"example.com/crosspkg/a\"\n)\n\nfunc TestViaB(t *testing.T) {\n\tif !a.Lt(1, 5) || a.Lt(5, 5) || a.Lt(6, 5) {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n",
	})
	writeTreeFiles(t, dir, map[string]string{
		"a/a.go": "package a\n\nfunc Lt(x, y int) bool { return x < y }\n",
	})
	ev, err := runOverlayOnDir(t, dir, gobuild.Build{CGOEnabled: true}, OverlayOptions{})
	if err != nil {
		t.Fatalf("RunOverlay crosspkg: %v", err)
	}
	if ev.Mutants != 2 || ev.SurvivedUnexplained != 2 {
		t.Fatalf("expected 2 NOT COVERED (no tests in pkg a), got %+v", ev)
	}
	for _, s := range ev.Survivors {
		if s.Status != "NOT COVERED" {
			t.Errorf("expected NOT COVERED without same-package test, got %+v", s)
		}
	}
}

func TestRunOverlayPackageMainTestdataDirAndBuildTags(t *testing.T) {
	dir := gitSetupOverlayRepo(t, map[string]string{
		"go.mod":                   "module example.com/pkgmain\n\ngo 1.26\n",
		"cmd/app/main.go":          "package main\n\nfunc main() {}\nfunc Check(n int) bool { return n == 0 }\n",
		"cmd/app/main_test.go":     "package main\n\nimport \"testing\"\n\nfunc TestBase(t *testing.T) { _ = Check(0) }\n",
		"cmd/app/testdata/cfg.txt": "limit=5\n",
	})
	writeTreeFiles(t, dir, map[string]string{
		"cmd/app/main.go": `//go:build customtag

package main

func main() {}

func Check(n int) bool {
	return n < 5
}
`,
		"cmd/app/main_test.go": `//go:build customtag

package main

import (
	"os"
	"strings"
	"testing"
)

func TestCheckWithRelativeTestdata(t *testing.T) {
	b, err := os.ReadFile("testdata/cfg.txt")
	if err != nil || !strings.Contains(string(b), "5") {
		t.Fatalf("relative testdata read failed (wrong cmd.Dir?): %v", err)
	}
	if !Check(1) || Check(6) {
		t.Fatal("wrong")
	}
}
`,
	})
	ev, err := runOverlayOnDir(t, dir, gobuild.Build{CGOEnabled: true, Tags: []string{"customtag"}}, OverlayOptions{})
	if err != nil {
		t.Fatalf("RunOverlay with build tags and package main: %v", err)
	}
	if ev.Mutants != 2 || ev.Killed != 1 || ev.SurvivedUnexplained != 1 {
		t.Fatalf("expected 1 killed + 1 lived under package main + customtag, got %+v", ev)
	}
	if ev.Survivors[0].Status != "LIVED" || ev.Survivors[0].Key.Function != "Check" {
		t.Fatalf("expected Check LIVED survivor, got %+v", ev.Survivors[0])
	}
}

func TestRunOverlayZeroWritesOnReadOnlyTreeWithSymlink(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "val.txt")
	if err := os.WriteFile(outside, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := gitSetupOverlayRepo(t, map[string]string{
		"go.mod":     "module example.com/ro\n\ngo 1.26\n",
		"calc.go":    "package ro\n\nfunc Lt(a, b int) bool { return a == b }\n",
		"ro_test.go": "package ro\n\nimport \"testing\"\n\nfunc TestBase(t *testing.T) { _ = Lt(1, 2) }\n",
	})
	if err := os.Symlink(outside, filepath.Join(dir, "secret.txt")); err != nil {
		t.Fatal(err)
	}
	writeTreeFiles(t, dir, map[string]string{
		"calc.go": "package ro\n\nfunc Lt(a, b int) bool { return a < b }\n",
		"ro_test.go": `package ro

import (
	"os"
	"testing"
)

func TestWithSymlink(t *testing.T) {
	b, err := os.ReadFile("secret.txt")
	if err != nil || string(b) != "ok" {
		t.Fatalf("symlink broken: %v %q", err, string(b))
	}
	if !Lt(1, 5) || Lt(6, 5) {
		t.Fatal("wrong")
	}
}
`,
	})
	build := gobuild.Build{CGOEnabled: true}
	diff, err := ComputeDiff(t.Context(), gate.OS{}, dir, "main", build)
	if err != nil {
		t.Fatalf("ComputeDiff: %v", err)
	}
	snapshot := func() map[string]int64 {
		m := map[string]int64{}
		if err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, werr error) error {
			if werr != nil {
				return werr
			}
			if d.IsDir() && p == filepath.Join(dir, ".git") {
				return filepath.SkipDir
			}
			info, err := os.Lstat(p)
			if err == nil {
				m[p] = info.ModTime().UnixNano()
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return m
	}
	before := snapshot()
	chmodTree(t, dir, false)
	defer chmodTree(t, dir, true)

	env := gate.Env{Dir: dir, Base: "main", Exec: gate.OS{}, Config: gate.Config{Base: "main", Mutation: gate.MutationConfig{Engine: "overlay"}}}
	ev, err := RunOverlay(t.Context(), env, diff, build, OverlayOptions{})
	if err != nil {
		t.Fatalf("RunOverlay on read-only tree: %v", err)
	}
	if ev.Mutants != 2 || ev.Killed != 1 || ev.SurvivedUnexplained != 1 {
		t.Fatalf("unexpected counts on read-only symlink tree: %+v", ev)
	}
	after := snapshot()
	for p, mt := range before {
		if after[p] != mt {
			t.Errorf("mtime changed for %s: before %d, after %d", p, mt, after[p])
		}
	}
	if len(after) != len(before) {
		t.Errorf("file count changed on read-only tree: before %d, after %d", len(before), len(after))
	}
}

func chmodTree(t *testing.T, root string, writable bool) {
	t.Helper()
	var dirs []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			dirs = append(dirs, p)
			if writable {
				return os.Chmod(p, 0o755)
			}
			return nil
		}
		if writable {
			return os.Chmod(p, 0o644)
		}
		return os.Chmod(p, 0o444)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !writable {
		slices.Reverse(dirs)
		for _, d := range dirs {
			if err := os.Chmod(d, 0o555); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestRunOverlayRedBaselineAndLineDirectiveFailClosed(t *testing.T) {
	t.Run("red_baseline_test_fails_closed", func(t *testing.T) {
		dir := gitSetupOverlayRepo(t, map[string]string{
			"go.mod":  "module example.com/redbase\n\ngo 1.26\n",
			"calc.go": "package redbase\n\nfunc Lt(a, b int) bool { return a == b }\n",
			"c_test.go": `package redbase

import "testing"

func TestOk(t *testing.T) {}
`,
		})
		writeTreeFiles(t, dir, map[string]string{
			"calc.go":   "package redbase\n\nfunc Lt(a, b int) bool { return a < b }\n",
			"c_test.go": "package redbase\n\nimport \"testing\"\n\nfunc TestRed(t *testing.T) { t.Fatal(\"boom\") }\n",
		})
		if _, err := runOverlayOnDir(t, dir, gobuild.Build{CGOEnabled: true}, OverlayOptions{}); err == nil || !strings.Contains(err.Error(), "baseline") {
			t.Fatalf("expected baseline failure error, got %v", err)
		}
	})

	t.Run("line_directive_fails_closed", func(t *testing.T) {
		dir := gitSetupOverlayRepo(t, map[string]string{
			"go.mod":  "module example.com/ldir\n\ngo 1.26\n",
			"calc.go": "package ldir\n\nfunc Add(a, b int) int { return a - b }\n",
		})
		writeTreeFiles(t, dir, map[string]string{
			"calc.go": "package ldir\n//line fx.go:1:1\nfunc Add(a, b int) int { return a + b }\n",
		})
		if _, err := runOverlayOnDir(t, dir, gobuild.Build{CGOEnabled: true}, OverlayOptions{}); err == nil || !strings.Contains(err.Error(), "line directive") {
			t.Fatalf("expected line directive error, got %v", err)
		}
	})
}

type spyTimeoutExec struct {
	coverArgvs [][]string
}

func (s *spyTimeoutExec) Run(ctx context.Context, c gate.Cmd) (gate.Output, error) {
	if len(c.Argv) >= 2 && c.Argv[0] == "go" && c.Argv[1] == "test" && !slices.Contains(c.Argv, "-c=true") {
		s.coverArgvs = append(s.coverArgvs, slices.Clone(c.Argv))
	}
	if len(c.Argv) > 0 && strings.HasPrefix(filepath.Base(c.Argv[0]), "mut-") {
		<-ctx.Done()
		return gate.Output{ExitCode: -1}, ctx.Err()
	}
	return gate.OS{}.Run(ctx, c)
}

func TestRunOverlayTargetedCoverAndTimeout(t *testing.T) {
	dir := gitSetupOverlayRepo(t, map[string]string{
		"go.mod":  "module example.com/timeout\n\ngo 1.26\n",
		"calc.go": "package timeout\n\nfunc Add(a, b int) int { return a - b }\n",
		"c_test.go": `package timeout

import "testing"

func TestAdd(t *testing.T) {
	if Add(1, 2) == 99 {
		t.Fatal("no")
	}
}
`,
	})
	writeTreeFiles(t, dir, map[string]string{
		"calc.go":   "package timeout\n\nfunc Add(a, b int) int { return a + b }\n",
		"c_test.go": "package timeout\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { if Add(2, 3) != 5 { t.Fatal(\"bad\") } }\n",
	})
	build := gobuild.Build{CGOEnabled: true}
	diff, err := ComputeDiff(t.Context(), gate.OS{}, dir, "main", build)
	if err != nil {
		t.Fatalf("ComputeDiff: %v", err)
	}
	spy := &spyTimeoutExec{}
	env := gate.Env{Dir: dir, Base: "main", Exec: spy, Config: gate.Config{Base: "main", Mutation: gate.MutationConfig{Engine: "overlay"}}}
	ev, err := RunOverlay(t.Context(), env, diff, build, OverlayOptions{
		MinTimeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("RunOverlay timeout: %v", err)
	}
	if len(spy.coverArgvs) != 1 || !slices.Contains(spy.coverArgvs[0], "-vet=off") || slices.Contains(spy.coverArgvs[0], "-coverpkg=./...") {
		t.Fatalf("expected 1 targeted per-pkg -vet=off cover call without -coverpkg, got %v", spy.coverArgvs)
	}
	if ev.TimedOut != 1 || ev.Killed != 1 || len(ev.TimedOutKeys) != 1 {
		t.Fatalf("expected 1 TIMED OUT counted in Killed & TimedOutKeys, got %+v", ev)
	}
}

func TestValidateEngineSupport(t *testing.T) {
	for _, engine := range []string{"", "overlay"} {
		if err := validateEngine(engine); err != nil {
			t.Errorf("%q: %v", engine, err)
		}
	}
	for _, engine := range []string{"mutest", "overlays"} {
		if err := validateEngine(engine); err == nil {
			t.Errorf("%q: expected an error", engine)
		}
	}
}

func TestRunOverlayBinaryParityGorootPathAndPanicOnExit0(t *testing.T) {
	t.Run("testBinaryEnv_prepends_goroot_before_existing_path", func(t *testing.T) {
		t.Setenv("PATH", "/poison/bin:/usr/bin")
		strip, env := testBinaryEnv(gobuild.Build{CGOEnabled: true}, "/fake/goroot/bin")
		if !slices.Contains(strip, "PATH=") {
			t.Fatalf("unexpected strip env %v", strip)
		}
		if !slices.Contains(env, "PATH=/fake/goroot/bin:/poison/bin:/usr/bin") {
			t.Fatalf("expected GOROOT/bin prepended in env, got %v", env)
		}
	})

	t.Run("goroot_bin_prepended_to_compiled_binary_path", func(t *testing.T) {
		gorootOut, err := exec.Command("go", "env", "GOROOT").Output()
		if err != nil {
			t.Fatal(err)
		}
		gorootBin := filepath.Join(strings.TrimSpace(string(gorootOut)), "bin")
		extraDir := t.TempDir()
		t.Setenv("PATH", extraDir+string(os.PathListSeparator)+os.Getenv("PATH"))
		wantPrefix := gorootBin + string(os.PathListSeparator)
		tmpl := `package gorootpath

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func %s(t *testing.T) {
	if got := os.Getenv("PATH"); !strings.HasPrefix(got, %q) {
		t.Fatalf("PATH=%%q does not start with %%q", got, %q)
	}
	if err := exec.Command("go", "version").Run(); err != nil {
		t.Fatalf("go failed: %%v", err)
	}%s
}
`
		baseTest := fmt.Sprintf(tmpl, "TestUsesGo", wantPrefix, wantPrefix, "")
		weakBody := `
	if !Lt(1, 5) || Lt(6, 5) {
		t.Fatal("wrong")
	}`
		weakTest := fmt.Sprintf(tmpl, "TestWeakWithGoExec", wantPrefix, wantPrefix, weakBody)
		dir := gitSetupOverlayRepo(t, map[string]string{
			"go.mod":    "module example.com/gorootpath\n\ngo 1.26\n",
			"calc.go":   "package gorootpath\n\nfunc Lt(a, b int) bool { return a == b }\n",
			"c_test.go": baseTest,
		})
		writeTreeFiles(t, dir, map[string]string{
			"calc.go":   "package gorootpath\n\nfunc Lt(a, b int) bool { return a < b }\n",
			"c_test.go": weakTest,
		})
		ev, err := runOverlayOnDir(t, dir, gobuild.Build{CGOEnabled: true}, OverlayOptions{})
		if err != nil {
			t.Fatalf("RunOverlay with GOROOT/bin PATH check: %v", err)
		}
		if ev.Mutants != 2 || ev.Killed != 1 || ev.SurvivedUnexplained != 1 {
			t.Fatalf("expected 1 KILLED + 1 LIVED, got %+v", ev)
		}
	})
	t.Run("paniconexit0_catches_os_exit_zero_in_mutant", func(t *testing.T) {
		dir := gitSetupOverlayRepo(t, map[string]string{
			"go.mod":  "module example.com/exit0\n\ngo 1.26\n",
			"calc.go": "package exit0\n\nfunc IsZero(n int) bool { return n > 0 }\n",
			"c_test.go": `package exit0

import "testing"

func TestBase(t *testing.T) {
	if !IsZero(0) {
		t.Fatal("bad")
	}
}
`,
		})
		writeTreeFiles(t, dir, map[string]string{
			"calc.go": "package exit0\n\nfunc IsZero(n int) bool { return n == 0 }\n",
			"c_test.go": `package exit0

import (
	"os"
	"testing"
)

func TestExit0OnBranch(t *testing.T) {
	if !IsZero(0) {
		// Under mutation == -> !=, IsZero(0) is false; calling os.Exit(0) must still FAIL via -test.paniconexit0.
		os.Exit(0)
	}
}
`,
		})
		ev, err := runOverlayOnDir(t, dir, gobuild.Build{CGOEnabled: true}, OverlayOptions{})
		if err != nil {
			t.Fatalf("RunOverlay paniconexit0: %v", err)
		}
		if ev.Mutants != 1 || ev.Killed != 1 || ev.SurvivedUnexplained != 0 {
			t.Fatalf("expected KILLED via -test.paniconexit0, got %+v", ev)
		}
	})
}

func TestRunOverlayNoTestFilesPackage(t *testing.T) {
	dir := gitSetupOverlayRepo(t, map[string]string{
		"go.mod":    "module example.com/notests\n\ngo 1.26\n",
		"notest.go": "package notests\n\nfunc Add(a, b int) int { return a - b }\n",
	})
	writeTreeFiles(t, dir, map[string]string{
		"notest.go": `package notests

const Bad = "a" + "b"

var Value = 1 + 2

func Lt(a, b int) bool {
	return a < b
}
`,
	})
	ev, err := runOverlayOnDir(t, dir, gobuild.Build{CGOEnabled: true}, OverlayOptions{})
	if err != nil {
		t.Fatalf("RunOverlay on package with no test files: %v", err)
	}
	// Bad string `+` fails compile => NOT VIABLE (Dropped=1); Value (`+`) and Lt (`<`) have no test binary => NOT COVERED (3 survivors).
	if ev.Dropped != 1 || ev.Mutants != 3 || ev.SurvivedUnexplained != 3 {
		t.Fatalf("expected Dropped=1, 3 NOT COVERED survivors, got %+v", ev)
	}
	for _, s := range ev.Survivors {
		if s.Status != "NOT COVERED" {
			t.Errorf("expected NOT COVERED without test files, got %+v", s)
		}
	}
}

type spyCompileTimeoutExec struct{}

func (spyCompileTimeoutExec) Run(ctx context.Context, c gate.Cmd) (gate.Output, error) {
	if len(c.Argv) >= 2 && c.Argv[0] == "go" && c.Argv[1] == "test" && slices.Contains(c.Argv, "-c=true") && anyOverlayArg(c.Argv) {
		<-ctx.Done()
		return gate.Output{ExitCode: -1}, ctx.Err()
	}
	return gate.OS{}.Run(ctx, c)
}

func anyOverlayArg(argv []string) bool {
	for _, a := range argv {
		if strings.HasPrefix(a, "-overlay=") {
			return true
		}
	}
	return false
}

func TestRunOverlayCompileTimeoutFailsLayer(t *testing.T) {
	dir := gitSetupOverlayRepo(t, map[string]string{
		"go.mod":    "module example.com/compto\n\ngo 1.26\n",
		"calc.go":   "package compto\n\nfunc Add(a, b int) int { return a - b }\n",
		"c_test.go": "package compto\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { if Add(2, 3) != 5 { t.Fatal(\"bad\") } }\n",
	})
	writeTreeFiles(t, dir, map[string]string{
		"calc.go": "package compto\n\nfunc Add(a, b int) int { return a + b }\n",
	})
	build := gobuild.Build{CGOEnabled: true}
	diff, err := ComputeDiff(t.Context(), gate.OS{}, dir, "main", build)
	if err != nil {
		t.Fatalf("ComputeDiff: %v", err)
	}
	env := gate.Env{Dir: dir, Base: "main", Exec: spyCompileTimeoutExec{}, Config: gate.Config{Base: "main", Mutation: gate.MutationConfig{Engine: "overlay"}}}
	_, err = RunOverlay(t.Context(), env, diff, build, OverlayOptions{MinCompileTimeout: 40 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected compile timeout to fail layer (not count as NOT VIABLE/KILLED), got %v", err)
	}
}

type scriptedCmdExec struct {
	fn func(ctx context.Context, c gate.Cmd) (gate.Output, error)
}

func (s scriptedCmdExec) Run(ctx context.Context, c gate.Cmd) (gate.Output, error) {
	return s.fn(ctx, c)
}

func TestOverlayOptsAndScaledTimeout(t *testing.T) {
	got := normalizeOverlayOpts(OverlayOptions{})
	if got.Workers != boundWorkers(runtime.GOMAXPROCS(0)) {
		t.Errorf("Workers = %d, want boundWorkers", got.Workers)
	}
	if got.TimeoutCoeff != 5 {
		t.Errorf("TimeoutCoeff = %d, want 5", got.TimeoutCoeff)
	}
	if got.MinTimeout != 10*time.Second {
		t.Errorf("MinTimeout = %v, want 10s", got.MinTimeout)
	}
	if got.MinCompileTimeout != 60*time.Second {
		t.Errorf("MinCompileTimeout = %v, want 60s", got.MinCompileTimeout)
	}
	if got.Today.IsZero() {
		t.Error("expected Today populated")
	}

	// Boundaries: Workers==1, TimeoutCoeff==1, MinTimeout==1ns, MinCompileTimeout==1ns must all be preserved.
	custom := normalizeOverlayOpts(OverlayOptions{
		Workers:           1,
		TimeoutCoeff:      1,
		MinTimeout:        time.Nanosecond,
		MinCompileTimeout: time.Nanosecond,
	})
	if custom.Workers != 1 || custom.TimeoutCoeff != 1 || custom.MinTimeout != time.Nanosecond || custom.MinCompileTimeout != time.Nanosecond {
		t.Fatalf("boundary custom opts overwritten: %+v", custom)
	}
	if d := scaledTimeout(2*time.Second, 5, 10*time.Second); d != 10*time.Second {
		t.Errorf("scaledTimeout boundary == floor got %v", d)
	}
	if d := scaledTimeout(3*time.Second, 5, 10*time.Second); d != 15*time.Second {
		t.Errorf("scaledTimeout > floor got %v", d)
	}
	if d := scaledTimeout(time.Second, 5, 10*time.Second); d != 10*time.Second {
		t.Errorf("scaledTimeout < floor got %v", d)
	}
}

func TestRunOverlayOutOfBuildOnlyReturnsVacuous(t *testing.T) {
	dir := gitSetupOverlayRepo(t, map[string]string{
		"go.mod":  "module example.com/oob\n\ngo 1.26\n",
		"calc.go": "//go:build ignore\n\npackage oob\n\nfunc F() int { return 1 + 2 }\n",
	})
	writeTreeFiles(t, dir, map[string]string{
		"calc.go": "//go:build ignore\n\npackage oob\n\nfunc F() int { return 1 - 2 }\n",
	})
	build := gobuild.Build{CGOEnabled: true}
	diff, err := ComputeDiff(t.Context(), gate.OS{}, dir, "main", build)
	if err != nil {
		t.Fatalf("ComputeDiff: %v", err)
	}
	if !diff.HasChangedProdLines() || len(diff.InBuildFiles) != 0 {
		t.Fatalf("expected changed prod lines with 0 InBuildFiles, got %+v", diff)
	}
	// Executor that fails if anything invokes go list; out-of-build diff must return early before readOverlayPkgMeta.
	env := gate.Env{
		Dir:  dir,
		Base: "main",
		Exec: scriptedCmdExec{fn: func(context.Context, gate.Cmd) (gate.Output, error) {
			return gate.Output{}, errors.New("should not execute go tools")
		}},
		Config: gate.Config{Base: "main", Mutation: gate.MutationConfig{Engine: "overlay"}},
	}
	ev, err := RunOverlay(t.Context(), env, diff, build, OverlayOptions{})
	if err != nil {
		t.Fatalf("RunOverlay out-of-build only: %v", err)
	}
	if ev.Mutants != 0 {
		t.Errorf("expected 0 mutants for out-of-build only diff, got %+v", ev)
	}
}

func TestReadOverlayPkgMetaAndCompileVerifyErrors(t *testing.T) {
	for step := range 6 {
		ex := scriptedCmdExec{fn: func(_ context.Context, c gate.Cmd) (gate.Output, error) {
			cmdKey := strings.Join(c.Argv, " ")
			switch step {
			case 0:
				return gate.Output{}, errors.New("mod exec err")
			case 1:
				return gate.Output{ExitCode: 2, Stderr: []byte("mod exit")}, nil
			case 2:
				if strings.HasPrefix(cmdKey, "go env GOROOT") {
					return gate.Output{}, errors.New("goroot exec err")
				}
			case 3:
				if strings.HasPrefix(cmdKey, "go env GOROOT") {
					return gate.Output{ExitCode: 1, Stderr: []byte("goroot exit")}, nil
				}
			case 4:
				if strings.HasPrefix(cmdKey, "go list -f") {
					return gate.Output{}, errors.New("list exec err")
				}
			case 5:
				if strings.HasPrefix(cmdKey, "go list -f") {
					return gate.Output{ExitCode: 1, Stderr: []byte("list exit")}, nil
				}
			}
			return gate.Output{ExitCode: 0, Stdout: []byte("example.com/m\n")}, nil
		}}
		if _, err := readOverlayPkgMeta(t.Context(), ex, t.TempDir(), gobuild.Build{}); err == nil {
			t.Errorf("step %d: expected error from readOverlayPkgMeta", step)
		}
	}

	// checkCompileOutcome & verifyUnmutatedOverlayBinaries branches:
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := checkCompileOutcome(canceled, t.Context(), "lbl", gate.Output{}, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("expected parentCtx canceled, got %v", err)
	}
	if err := checkCompileOutcome(t.Context(), t.Context(), "lbl", gate.Output{ExitCode: 0}, errors.New("exec boom")); err == nil || !strings.Contains(err.Error(), "exec boom") {
		t.Errorf("expected wrapped exec err, got %v", err)
	}
	errOut := checkCompileOutcome(t.Context(), t.Context(), "lbl", gate.Output{ExitCode: 1, Stdout: []byte("OUT"), Stderr: []byte("ERR")}, nil)
	if errOut == nil || !strings.Contains(errOut.Error(), "OUT\nERR") {
		t.Errorf("expected stdout+stderr joined in compile error, got %v", errOut)
	}

	tmpDir := t.TempDir()
	meta := overlayPkgMeta{pkgDirs: map[string]string{"example.com/p": tmpDir}}
	opts := normalizeOverlayOpts(OverlayOptions{TimeoutCoeff: 1, MinTimeout: time.Second, MinCompileTimeout: time.Second})
	// 1. unmutated compile fails -> verifyUnmutatedOverlayBinaries propagates error.
	compFailEx := scriptedCmdExec{fn: func(context.Context, gate.Cmd) (gate.Output, error) {
		return gate.Output{ExitCode: 1, Stdout: []byte("bad"), Stderr: []byte("compile")}, nil
	}}
	if _, err := verifyUnmutatedOverlayBinaries(t.Context(), compFailEx, tmpDir, tmpDir, meta, []string{"example.com/p"}, gobuild.Build{}, opts, map[string]time.Duration{}); err == nil || !strings.Contains(err.Error(), "bad\ncompile") {
		t.Errorf("expected compile failure propagation, got %v", err)
	}
	// 2. unmutated compile writes binary, then binary run returns exec error -> error propagated.
	execErrEx := scriptedCmdExec{fn: func(_ context.Context, c gate.Cmd) (gate.Output, error) {
		if len(c.Argv) >= 2 && c.Argv[0] == "go" {
			for i, a := range c.Argv {
				if a == "-o" && i+1 < len(c.Argv) {
					if werr := os.WriteFile(c.Argv[i+1], []byte("bin"), 0o700); werr != nil {
						return gate.Output{}, werr
					}
				}
			}
			return gate.Output{ExitCode: 0}, nil
		}
		return gate.Output{}, errors.New("binary spawn failed")
	}}
	if _, err := verifyUnmutatedOverlayBinaries(t.Context(), execErrEx, tmpDir, tmpDir, meta, []string{"example.com/p"}, gobuild.Build{}, opts, map[string]time.Duration{}); err == nil || !strings.Contains(err.Error(), "binary spawn failed") {
		t.Errorf("expected unmutated run err propagation, got %v", err)
	}
}

func TestOverlayLowLevelHelpers(t *testing.T) {
	// replaceTargets: empty pkgDir + dedup check.
	rtEmpty := replaceTargets("", "/dir/a.go", "a.go")
	if len(rtEmpty) == 0 || slices.Contains(rtEmpty, "") {
		t.Fatalf("unexpected replaceTargets with empty pkgDir: %v", rtEmpty)
	}
	rtPkg := replaceTargets("/pkg", "/dir/a.go", "a.go")
	if rtPkg[0] != "/pkg/a.go" {
		t.Fatalf("expected /pkg/a.go first, got %v", rtPkg)
	}
	resDir := resolvedPath(t.TempDir())
	rtDedup := replaceTargets(resDir, filepath.Join(resDir, "a.go"), "a.go")
	if len(rtDedup) != 1 || rtDedup[0] != filepath.Join(resDir, "a.go") {
		t.Fatalf("expected deduplicated replaceTargets of len 1, got %v", rtDedup)
	}

	// inspectDeclNodes: FuncLit resets alwaysRun=false and error stops inspection cleanly.
	fset := token.NewFileSet()
	pf, err := parser.ParseFile(fset, "f.go", "package p\nvar x = 1 + 2\nvar f = func() int { return (3 + 4) + 5 }\nvar y = (5 + 6) + 7\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("stop inside funclit")
	var visited []bool
	calls := 0
	err = inspectOverlayNodes(pf.Decls[1], true, func(n ast.Node, always bool, _ token.Pos) error {
		if n == nil {
			t.Fatal("visit called with nil node")
		}
		calls++
		if _, ok := n.(*ast.BinaryExpr); ok {
			visited = append(visited, always)
			return sentinel
		}
		return nil
	})
	if !errors.Is(err, sentinel) || len(visited) != 1 || visited[0] || calls == 0 {
		t.Fatalf("expected FuncLit alwaysRun=false + error stop, got err=%v visited=%v calls=%d", err, visited, calls)
	}
	// Sibling stop on package-level decl and n == nil guard:
	visited = nil
	err = inspectOverlayNodes(pf.Decls[2], true, func(n ast.Node, always bool, _ token.Pos) error {
		if n == nil {
			t.Fatal("visit called with nil node")
		}
		if _, ok := n.(*ast.BinaryExpr); ok {
			visited = append(visited, always)
			return sentinel
		}
		return nil
	})
	if !errors.Is(err, sentinel) || len(visited) != 1 || !visited[0] {
		t.Fatalf("expected sibling stop with alwaysRun=true, got err=%v visited=%v", err, visited)
	}
	// Package-level non-FuncLit completes normally with alwaysRun=true and n == nil skipped:
	visited = nil
	pfErr := inspectOverlayNodes(pf.Decls[0], true, func(n ast.Node, always bool, _ token.Pos) error {
		if n == nil {
			t.Fatal("visit called with nil node")
		}
		if _, ok := n.(*ast.BinaryExpr); ok {
			visited = append(visited, always)
		}
		return nil
	})
	if pfErr != nil || len(visited) != 1 || !visited[0] {
		t.Fatalf("expected alwaysRun=true on pkg var, got err=%v visited=%v", pfErr, visited)
	}

	// overlayMutatorsForNode: non-mutable BinaryExpr (SHL) must return ok=false.
	if _, _, _, ok := overlayMutatorsForNode(&ast.BinaryExpr{Op: token.SHL}); ok {
		t.Error("expected SHL BinaryExpr to have ok=false")
	}

	// appendNodeJobs: nextSeq increments monotonically for covered nodes.
	var plan fileMutantPlan
	tmp := t.TempDir()
	src := []byte("package p\nvar a = 1 + 2\n")
	if err := appendNodeJobs(&plan, tmp, "a.go", "example.com/p", tmp, []string{filepath.Join(tmp, "a.go")}, src, []string{"ARITHMETIC_BASE"}, token.ADD, len("package p\nvar a = 1 "), token.Position{Line: 2, Column: 11}, true); err != nil {
		t.Fatal(err)
	}
	if err := appendNodeJobs(&plan, tmp, "a.go", "example.com/p", tmp, []string{filepath.Join(tmp, "a.go")}, src, []string{"ARITHMETIC_BASE"}, token.ADD, len("package p\nvar a = 1 "), token.Position{Line: 2, Column: 11}, true); err != nil {
		t.Fatal(err)
	}
	if plan.nextSeq != 2 || len(plan.jobs) != 2 {
		t.Fatalf("expected nextSeq=2, jobs=2, got nextSeq=%d jobs=%d", plan.nextSeq, len(plan.jobs))
	}

	// spliceTokenBytes boundaries:
	if out, err := spliceTokenBytes([]byte("+"), 0, token.ADD, token.SUB); err != nil || string(out) != "-" {
		t.Fatalf("off=0 failed: out=%q err=%v", string(out), err)
	}
	for _, off := range []int{-1, 1} {
		if _, err := spliceTokenBytes([]byte("+"), off, token.ADD, token.SUB); err == nil {
			t.Errorf("expected error for off=%d", off)
		}
	}
	if _, err := spliceTokenBytes([]byte("++"), 1, token.INC, token.DEC); err == nil {
		t.Error("expected error when token extends past len(src)")
	}
	if _, err := spliceTokenBytes([]byte("-"), 0, token.ADD, token.SUB); err == nil {
		t.Error("expected error on token mismatch")
	}

	// innermostSwitchCaseCovered boundaries & outer/inner disambiguation:
	if cov, ok := innermostSwitchCaseCovered([]switchCaseSpan{
		{startOff: 0, endOff: 0, covered: true},
		{startOff: 0, endOff: 10, covered: false},
	}, 0); !ok || !cov {
		t.Errorf("zero-len span at off=0 before wider span got (%v,%v)", cov, ok)
	}
	if cov, ok := innermostSwitchCaseCovered([]switchCaseSpan{{startOff: 10, endOff: 20, covered: true}}, 10); !ok || !cov {
		t.Errorf("start boundary got (%v,%v)", cov, ok)
	}
	if cov, ok := innermostSwitchCaseCovered([]switchCaseSpan{{startOff: 10, endOff: 20, covered: true}}, 20); !ok || !cov {
		t.Errorf("end boundary got (%v,%v)", cov, ok)
	}
	if _, ok := innermostSwitchCaseCovered([]switchCaseSpan{{startOff: 10, endOff: 20, covered: true}}, 9); ok {
		t.Error("expected false below startOff")
	}
	if _, ok := innermostSwitchCaseCovered([]switchCaseSpan{{startOff: 10, endOff: 20, covered: true}}, 21); ok {
		t.Error("expected false above endOff")
	}
	nested := []switchCaseSpan{
		{startOff: 10, endOff: 100, covered: false},
		{startOff: 80, endOff: 90, covered: true},
	}
	if cov, ok := innermostSwitchCaseCovered(nested, 85); !ok || !cov {
		t.Errorf("innermost [80,90] must win over [10,100], got (%v,%v)", cov, ok)
	}
	// Reverse order with equal inner length must preserve earlier match (strict <).
	eq := []switchCaseSpan{
		{startOff: 80, endOff: 90, covered: true},
		{startOff: 80, endOff: 90, covered: false},
	}
	if cov, ok := innermostSwitchCaseCovered(eq, 85); !ok || !cov {
		t.Errorf("equal len must keep first span, got (%v,%v)", cov, ok)
	}

	// blockContainsPos boundaries:
	blk := cover.Block{StartLine: 10, StartCol: 5, EndLine: 12, EndCol: 20}
	for _, tc := range []struct {
		l, c int
		want bool
	}{
		{9, 100, false},
		{10, 4, false},
		{10, 5, true},
		{11, 1, true},
		{12, 20, true},
		{12, 21, false},
		{13, 1, false},
	} {
		if got := blockContainsPos(blk, tc.l, tc.c); got != tc.want {
			t.Errorf("blockContainsPos(%d,%d) = %v, want %v", tc.l, tc.c, got, tc.want)
		}
	}

	// testBinExt:
	if ext := testBinExt(gobuild.Build{GOOS: "linux"}); ext != ".test" {
		t.Errorf("linux ext = %q", ext)
	}
	if ext := testBinExt(gobuild.Build{GOOS: "windows"}); ext != ".exe" {
		t.Errorf("windows ext = %q", ext)
	}
	if ext := testBinExt(gobuild.Build{GOOS: ""}); (runtime.GOOS == "windows" && ext != ".exe") || (runtime.GOOS != "windows" && ext != ".test") {
		t.Errorf("default GOOS ext = %q", ext)
	}
}

// F1: same-package mutant execution must be serialized across workers while different packages run in parallel.
func TestRunOverlaySerializesSamePkgAndRunsDifferentPkgsConcurrently(t *testing.T) {
	t.Run("o_excl_shared_file_with_4_workers_has_zero_false_kills", func(t *testing.T) {
		dir := gitSetupOverlayRepo(t, map[string]string{
			"go.mod":  "module example.com/oexcl\n\ngo 1.26\n",
			"calc.go": "package oexcl\n\nfunc F() int { return 0 }\n",
			"c_test.go": `package oexcl

import "testing"

func TestBase(t *testing.T) {
	if F() < 0 {
		t.Fatal("bad")
	}
}
`,
		})
		writeTreeFiles(t, dir, map[string]string{
			"calc.go": `package oexcl

func F(a, b int) int {
	n := 0
	if a < b {
		n += 1
	}
	if a+1 < b+2 {
		n += 1
	}
	return n
}
`,
			"c_test.go": `package oexcl

import (
	"os"
	"testing"
)

func TestOExclInPkgDir(t *testing.T) {
	f, err := os.OpenFile("shared-oexcl.lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("O_EXCL collision: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove("shared-oexcl.lock"); err != nil {
		t.Fatal(err)
	}
	// Assert-nothing after setup: without per-package serialization, 4 workers collide on O_EXCL and false-kill.
	_ = F(1, 2)
}
`,
		})
		ev, err := runOverlayOnDir(t, dir, gobuild.Build{CGOEnabled: true}, OverlayOptions{Workers: 4})
		if err != nil {
			t.Fatalf("RunOverlay F1 O_EXCL: %v", err)
		}
		if ev.Mutants == 0 || ev.Killed != 0 || ev.SurvivedUnexplained != ev.Mutants {
			t.Fatalf("expected all %d mutants to LIVE (0 O_EXCL false kills), got %+v", ev.Mutants, ev)
		}
	})

	t.Run("different_packages_still_execute_concurrently", func(t *testing.T) {
		dir := gitSetupOverlayRepo(t, map[string]string{
			"go.mod":      "module example.com/multipkg\n\ngo 1.26\n",
			"a/a.go":      "package a\n\nfunc A(x int) int { return x }\n",
			"a/a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) { _ = A(1) }\n",
			"b/b.go":      "package b\n\nfunc B(x int) int { return x }\n",
			"b/b_test.go": "package b\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) { _ = B(1) }\n",
		})
		writeTreeFiles(t, dir, map[string]string{
			"a/a.go":      "package a\n\nfunc A(x int) int { return x + 1 + 2 }\n",
			"a/a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) { if A(0) != 3 { t.Fatal(\"bad\") } }\n",
			"b/b.go":      "package b\n\nfunc B(x int) int { return x + 1 + 2 }\n",
			"b/b_test.go": "package b\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) { if B(0) != 3 { t.Fatal(\"bad\") } }\n",
		})
		build := gobuild.Build{CGOEnabled: true}
		diff, err := ComputeDiff(t.Context(), gate.OS{}, dir, "main", build)
		if err != nil {
			t.Fatalf("ComputeDiff: %v", err)
		}
		var (
			mu         sync.Mutex
			activeBy   = map[string]int{}
			maxBy      = map[string]int{}
			totalAct   int
			maxTotal   int
			sawBothPkg sync.WaitGroup
			enteredA   atomic.Bool
			enteredB   atomic.Bool
		)
		sawBothPkg.Add(2)
		spy := scriptedCmdExec{fn: func(ctx context.Context, c gate.Cmd) (gate.Output, error) {
			if len(c.Argv) > 0 && strings.HasPrefix(filepath.Base(c.Argv[0]), "mut-") {
				pkgKey := filepath.Base(c.Dir)
				firstOfPkg := false
				if pkgKey == "a" && enteredA.CompareAndSwap(false, true) {
					firstOfPkg = true
				}
				if pkgKey == "b" && enteredB.CompareAndSwap(false, true) {
					firstOfPkg = true
				}
				mu.Lock()
				activeBy[pkgKey]++
				if activeBy[pkgKey] > maxBy[pkgKey] {
					maxBy[pkgKey] = activeBy[pkgKey]
				}
				totalAct++
				if totalAct > maxTotal {
					maxTotal = totalAct
				}
				mu.Unlock()
				if firstOfPkg {
					sawBothPkg.Done()
					doneCh := make(chan struct{})
					go func() {
						sawBothPkg.Wait()
						close(doneCh)
					}()
					select {
					case <-doneCh:
					case <-ctx.Done():
					}
				}
				res, runErr := (gate.OS{}).Run(ctx, c)
				mu.Lock()
				activeBy[pkgKey]--
				totalAct--
				mu.Unlock()
				return res, runErr
			}
			return (gate.OS{}).Run(ctx, c)
		}}
		env := gate.Env{Dir: dir, Base: "main", Exec: spy, Config: gate.Config{Base: "main", Mutation: gate.MutationConfig{Engine: "overlay"}}}
		ev, err := RunOverlay(t.Context(), env, diff, build, OverlayOptions{Workers: 4})
		if err != nil {
			t.Fatalf("RunOverlay multi-pkg concurrency: %v", err)
		}
		if ev.Mutants != 4 || ev.Killed != 4 {
			t.Fatalf("expected 4 killed mutants, got %+v", ev)
		}
		if maxBy["a"] != 1 || maxBy["b"] != 1 {
			t.Fatalf("expected max active per pkg == 1, got %v", maxBy)
		}
		if maxTotal < 2 {
			t.Fatalf("expected concurrent execution across packages (maxTotal>=2), got %d", maxTotal)
		}
	})
}

// F2: child processes holding stdout must be killed promptly with process-group SIGKILL / WaitDelay.
func TestRunOverlayChildProcessTimeoutDoesNotHang(t *testing.T) {
	dir := gitSetupOverlayRepo(t, map[string]string{
		"go.mod":  "module example.com/childto\n\ngo 1.26\n",
		"calc.go": "package childto\n\nfunc IsZero(n int) bool { return n > 0 }\n",
		"c_test.go": `package childto

import "testing"

func TestBase(t *testing.T) {
	if !IsZero(0) {
		t.Fatal("bad")
	}
}
`,
	})
	writeTreeFiles(t, dir, map[string]string{
		"calc.go": "package childto\n\nfunc IsZero(n int) bool { return n == 0 }\n",
		"c_test.go": `package childto

import (
	"os"
	"os/exec"
	"testing"
)

func TestChildSleepOnMutant(t *testing.T) {
	if !IsZero(0) {
		cmd := exec.Command("sleep", "30")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
		t.Fatal("mutated")
	}
}
`,
	})
	start := time.Now()
	ev, err := runOverlayOnDir(t, dir, gobuild.Build{CGOEnabled: true}, OverlayOptions{
		TimeoutCoeff: 1,
		MinTimeout:   2500 * time.Millisecond,
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("RunOverlay child timeout: %v", err)
	}
	if ev.TimedOut != 1 || ev.Killed != 1 {
		t.Fatalf("expected 1 TIMED OUT mutant, got %+v", ev)
	}
	if elapsed >= 10*time.Second {
		t.Fatalf("expected fast process-group timeout well under sleep 30s, elapsed=%v", elapsed)
	}
}

// F3: select comm clauses and type-switch case clauses inherit enclosing statement cover block (G1).
func TestRunOverlaySelectAndTypeSwitchClauses(t *testing.T) {
	t.Run("select_and_type_switch_reached_mutate_and_kill", func(t *testing.T) {
		dir := gitSetupOverlayRepo(t, map[string]string{
			"go.mod": "module example.com/selts\n\ngo 1.26\n",
			"sel.go": "package selts\n\nfunc Recv(chs []chan int, i int) int { return 0 }\n",
			"sel_test.go": `package selts

import "testing"

func TestBase(t *testing.T) { _ = Recv(nil, 0) }
`,
		})
		writeTreeFiles(t, dir, map[string]string{
			"sel.go": `package selts

func Recv(chs []chan int, i int) int {
	select {
	case v := <-chs[i+1]:
		return v
	case chs[i+1] <- i + 10:
		return 1
	default:
		return 0
	}
}

func TypeWidth(v any) int {
	switch v.(type) {
	case [1 + 1]int:
		return 2
	default:
		return 0
	}
}
`,
			"sel_test.go": `package selts

import "testing"

func TestSelectAndTypeSwitchStrong(t *testing.T) {
	ch := make(chan int, 1)
	ch <- 9
	if got := Recv([]chan int{nil, ch}, 0); got != 9 {
		t.Fatalf("recv got %d, want 9", got)
	}
	outCh := make(chan int, 1)
	if got := Recv([]chan int{nil, outCh}, 0); got != 1 {
		t.Fatalf("send got %d, want 1", got)
	}
	if got := <-outCh; got != 10 {
		t.Fatalf("sent val got %d, want 10", got)
	}
	blocked := make(chan int)
	if got := Recv([]chan int{nil, blocked}, 0); got != 0 {
		t.Fatalf("default got %d, want 0", got)
	}
	var arr [2]int
	if got := TypeWidth(arr); got != 2 {
		t.Fatalf("TypeWidth([2]int) = %d, want 2", got)
	}
	if got := TypeWidth(42); got != 0 {
		t.Fatalf("TypeWidth(int) = %d, want 0", got)
	}
}
`,
		})
		ev, err := runOverlayOnDir(t, dir, gobuild.Build{CGOEnabled: true}, OverlayOptions{})
		if err != nil {
			t.Fatalf("RunOverlay select/typeswitch: %v", err)
		}
		// 4 '+' tokens (3 in select clauses, 1 in type-switch case clause): all 4 are reached and killed (0 NOT COVERED!).
		if ev.Mutants != 4 || ev.Killed != 4 || ev.SurvivedUnexplained != 0 {
			t.Fatalf("expected 4/4 KILLED (0 NOT COVERED), got %+v", ev)
		}
	})

	t.Run("unreached_select_and_typeswitch_are_not_covered", func(t *testing.T) {
		dir := gitSetupOverlayRepo(t, map[string]string{
			"go.mod": "module example.com/seluncov\n\ngo 1.26\n",
			"sel.go": "package seluncov\n\nfunc Dead(ch chan int, v any, take bool) int { return 0 }\n",
			"sel_test.go": `package seluncov

import "testing"

func TestNeverTake(t *testing.T) { _ = Dead(nil, 1, false) }
`,
		})
		writeTreeFiles(t, dir, map[string]string{
			"sel.go": `package seluncov

func Dead(ch chan int, v any, take bool) int {
	if take {
		select {
		case x := <-[]chan int{ch}[0+0]:
			return x
		default:
			return 1
		}
	}
	if take {
		switch v.(type) {
		case [1 + 1]int:
			return 2
		}
	}
	return 0
}
`,
		})
		ev, err := runOverlayOnDir(t, dir, gobuild.Build{CGOEnabled: true}, OverlayOptions{})
		if err != nil {
			t.Fatalf("RunOverlay unreached select: %v", err)
		}
		if ev.Mutants != 2 || ev.SurvivedUnexplained != 2 {
			t.Fatalf("expected 2 NOT COVERED survivors, got %+v", ev)
		}
		for _, s := range ev.Survivors {
			if s.Status != "NOT COVERED" {
				t.Errorf("expected NOT COVERED, got %+v", s)
			}
		}
	})
}

// F4: code after a FuncLit in the same statement falls in no cover block and must inherit stmt.Pos().
func TestRunOverlayPostFuncLitSameStatement(t *testing.T) {
	t.Run("reached_post_funclit_runs_while_uninvoked_funclit_body_stays_not_covered", func(t *testing.T) {
		dir := gitSetupOverlayRepo(t, map[string]string{
			"go.mod": "module example.com/postfl\n\ngo 1.26\n",
			"fl.go":  "package postfl\n\nfunc Check(apply func(func(int) int) int, a, b int) int { return 0 }\n",
			"fl_test.go": `package postfl

import "testing"

func TestBase(t *testing.T) { _ = Check(func(func(int) int) int { return 1 }, 1, 2) }
`,
		})
		writeTreeFiles(t, dir, map[string]string{
			"fl.go": `package postfl

func Check(apply func(func(int) int) int, a, b int) int {
	if apply(func(x int) int { return x + 1 }) > 0 && b > a {
		return 1
	}
	return 0
}
`,
			// Test calls Check with an apply that does NOT invoke the FuncLit (so `x + 1` inside FuncLit is NOT COVERED),
			// while the reached `if ... && b > a` post-FuncLit expressions DO run and are KILLED!
			"fl_test.go": `package postfl

import "testing"

func TestReachedPostFuncLitStrong(t *testing.T) {
	noCall := func(func(int) int) int { return 1 }
	if Check(noCall, 1, 2) != 1 {
		t.Fatal("1 < 2 should return 1")
	}
	if Check(noCall, 2, 2) != 0 {
		t.Fatal("2 == 2 should return 0")
	}
	if Check(noCall, 3, 2) != 0 {
		t.Fatal("3 > 2 should return 0")
	}
	zeroCall := func(func(int) int) int { return 0 }
	if Check(zeroCall, 1, 2) != 0 {
		t.Fatal("apply == 0 should return 0")
	}
	if Check(func(func(int) int) int { return -1 }, 1, 2) != 0 {
		t.Fatal("apply < 0 should return 0")
	}
}
`,
		})
		ev, err := runOverlayOnDir(t, dir, gobuild.Build{CGOEnabled: true}, OverlayOptions{})
		if err != nil {
			t.Fatalf("RunOverlay reached post-FuncLit: %v", err)
		}
		// Inside FuncLit (`x + 1` line 4): NOT COVERED (1 survivor).
		// Post-FuncLit (`> 0` has 2 muts, `&&` has 1 mut, `> a` has 2 muts = 5 muts): all 5 run and are KILLED!
		if ev.Mutants != 6 || ev.Killed != 5 || ev.SurvivedUnexplained != 1 {
			t.Fatalf("expected 5 KILLED + 1 NOT COVERED (inside uninvoked FuncLit), got %+v", ev)
		}
		if ev.Survivors[0].Status != "NOT COVERED" || ev.Survivors[0].Line != 4 {
			t.Fatalf("expected survivor to be NOT COVERED inside FuncLit, got %+v", ev.Survivors[0])
		}
	})

	t.Run("unreached_post_funclit_reports_not_covered", func(t *testing.T) {
		dir := gitSetupOverlayRepo(t, map[string]string{
			"go.mod": "module example.com/postfluncov\n\ngo 1.26\n",
			"fl.go":  "package postfluncov\n\nfunc Check(apply func(func(int) int) int, a, b int, take bool) int { return 0 }\n",
			"fl_test.go": `package postfluncov

import "testing"

func TestBase(t *testing.T) { _ = Check(nil, 1, 2, false) }
`,
		})
		writeTreeFiles(t, dir, map[string]string{
			"fl.go": `package postfluncov

func Check(apply func(func(int) int) int, a, b int, take bool) int {
	if take {
		if apply(func(x int) int { return x + 1 }) > 0 && b > a {
			return 1
		}
	}
	return 0
}
`,
		})
		ev, err := runOverlayOnDir(t, dir, gobuild.Build{CGOEnabled: true}, OverlayOptions{})
		if err != nil {
			t.Fatalf("RunOverlay unreached post-FuncLit: %v", err)
		}
		if ev.Mutants != 6 || ev.SurvivedUnexplained != 6 {
			t.Fatalf("expected all 6 mutants NOT COVERED, got %+v", ev)
		}
		for _, s := range ev.Survivors {
			if s.Status != "NOT COVERED" {
				t.Errorf("expected NOT COVERED, got %+v", s)
			}
		}
	})

	t.Run("isMutantCovered_explicit_zero_block_not_overridden_by_stmtPos", func(t *testing.T) {
		src := "package p\nfunc f() int {\n\tif true {\n\t\treturn 1 + 2\n\t}\n\treturn 0\n}\n"
		fset := token.NewFileSet()
		pf, err := parser.ParseFile(fset, "p.go", src, 0)
		if err != nil {
			t.Fatal(err)
		}
		fn, ok1 := pf.Decls[0].(*ast.FuncDecl)
		if !ok1 {
			t.Fatal("expected FuncDecl")
		}
		ifStmt, ok2 := fn.Body.List[0].(*ast.IfStmt)
		if !ok2 {
			t.Fatal("expected IfStmt")
		}
		retStmt, ok3 := ifStmt.Body.List[0].(*ast.ReturnStmt)
		if !ok3 {
			t.Fatal("expected ReturnStmt")
		}
		addExpr, ok4 := retStmt.Results[0].(*ast.BinaryExpr)
		if !ok4 {
			t.Fatal("expected BinaryExpr")
		}
		addPos := fset.PositionFor(addExpr.OpPos, false)
		// outer `if` block at line 3 is covered (Count=1), inner `return 1 + 2` block at line 4 has Count=0:
		// explicit Count=0 block must win over stmtPos fallback, and NoPos outside any block must be false.
		blocks := []cover.Block{
			{File: "p.go", StartLine: 2, StartCol: 14, EndLine: 3, EndCol: 10, Count: 1},
			{File: "p.go", StartLine: 4, StartCol: 3, EndLine: 4, EndCol: 16, Count: 0},
		}
		if isMutantCovered(fset, blocks, nil, ifStmt.Pos(), addPos.Line, addPos.Column, 0) {
			t.Fatal("expected explicit Count=0 block to stay NOT COVERED")
		}
		if isMutantCovered(fset, blocks, nil, token.NoPos, 50, 1, 0) {
			t.Fatal("expected invalid stmtPos outside blocks to return false")
		}
	})
}

// Ruling (A): per-mutant timeout starts AFTER acquiring the per-package lock, not while waiting in queue.
func TestRunOverlayTimeoutStartsAfterPerPkgLock(t *testing.T) {
	dir := gitSetupOverlayRepo(t, map[string]string{
		"go.mod":    "module example.com/queueto\n\ngo 1.26\n",
		"calc.go":   "package queueto\n\nfunc Triple(a, b, c int) int { return 0 }\n",
		"c_test.go": "package queueto\n\nimport \"testing\"\n\nfunc TestBase(t *testing.T) { _ = Triple(1, 2, 3) }\n",
	})
	writeTreeFiles(t, dir, map[string]string{
		"calc.go": "package queueto\n\nfunc Triple(a, b, c int) int { return a + b + c }\n",
	})
	build := gobuild.Build{CGOEnabled: true}
	diff, err := ComputeDiff(t.Context(), gate.OS{}, dir, "main", build)
	if err != nil {
		t.Fatalf("ComputeDiff: %v", err)
	}
	const minTO = 1200 * time.Millisecond
	var (
		mu       sync.Mutex
		covDur   time.Duration
		minRem   time.Duration
		mutCalls int
	)
	spy := scriptedCmdExec{fn: func(ctx context.Context, c gate.Cmd) (gate.Output, error) {
		for _, a := range c.Argv {
			if strings.HasPrefix(a, "-coverprofile=") {
				t0 := time.Now()
				out, rerr := (gate.OS{}).Run(ctx, c)
				mu.Lock()
				covDur = time.Since(t0)
				mu.Unlock()
				return out, rerr
			}
		}
		if len(c.Argv) > 0 && strings.HasPrefix(filepath.Base(c.Argv[0]), "mut-") {
			dl, ok := ctx.Deadline()
			if !ok {
				return gate.Output{ExitCode: 1}, nil
			}
			rem := time.Until(dl)
			mu.Lock()
			mutCalls++
			if minRem == 0 || rem < minRem {
				minRem = rem
			}
			hold := max(covDur, minTO) * 2 / 3
			mu.Unlock()
			select {
			case <-time.After(hold):
				return gate.Output{ExitCode: 0}, nil
			case <-ctx.Done():
				return gate.Output{ExitCode: -1}, ctx.Err()
			}
		}
		return (gate.OS{}).Run(ctx, c)
	}}
	env := gate.Env{Dir: dir, Base: "main", Exec: spy, Config: gate.Config{Base: "main", Mutation: gate.MutationConfig{Engine: "overlay"}}}
	ev, err := RunOverlay(t.Context(), env, diff, build, OverlayOptions{
		Workers:      3,
		TimeoutCoeff: 1,
		MinTimeout:   minTO,
	})
	if err != nil {
		t.Fatalf("RunOverlay queued timeout: %v", err)
	}
	if mutCalls != 2 || ev.TimedOut != 0 || ev.Mutants != 2 || ev.SurvivedUnexplained != 2 {
		t.Fatalf("expected 2 LIVED with 0 TIMED OUT (calls=%d), got %+v", mutCalls, ev)
	}
	if minRem < minTO-100*time.Millisecond {
		t.Fatalf("expected full post-lock timeout budget (>= %v), got min remaining %v", minTO-100*time.Millisecond, minRem)
	}
}

// F8: timed-out mutant leaving a file in pkgDir must fail postflight instead of turning later mutants into false kills.
func TestRunOverlayF8Postflight(t *testing.T) {
	t.Run("timeout_leftover_o_excl_fails_postflight", func(t *testing.T) {
		dir := gitSetupOverlayRepo(t, map[string]string{
			"go.mod":  "module example.com/f8post\n\ngo 1.26\n",
			"calc.go": "package f8post\n\nfunc IsZero(n int) bool { return n > 0 }\n",
			"c_test.go": `package f8post

import "testing"

func TestBase(t *testing.T) {
	if !IsZero(0) {
		t.Fatal("bad")
	}
}
`,
		})
		writeTreeFiles(t, dir, map[string]string{
			"calc.go": "package f8post\n\nfunc IsZero(n int) bool { return n == 0 }\n\nfunc Alive(n int) bool { return n == 1 }\n",
			"c_test.go": `package f8post

import (
	"os"
	"os/exec"
	"testing"
)

func TestPostflightPollutedPkgDir(t *testing.T) {
	f, err := os.OpenFile("leaked.lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("O_EXCL leaked artifact present: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if !IsZero(0) {
		// Timed-out mutant leaves leaked.lock behind via SIGKILL before cleanup!
		_ = exec.Command("sleep", "30").Run()
	}
	if err := os.Remove("leaked.lock"); err != nil {
		t.Fatal(err)
	}
	_ = Alive(1)
}
`,
		})
		_, err := runOverlayOnDir(t, dir, gobuild.Build{CGOEnabled: true}, OverlayOptions{
			Workers:      2,
			TimeoutCoeff: 1,
			MinTimeout:   2500 * time.Millisecond,
		})
		if err == nil || !strings.Contains(err.Error(), "postflight") {
			t.Fatalf("expected postflight failure from leftover O_EXCL lock, got %v", err)
		}
	})

	t.Run("skipped_when_no_mutant_binary_executed", func(t *testing.T) {
		dir := gitSetupOverlayRepo(t, map[string]string{
			"go.mod":    "module example.com/f8skip\n\ngo 1.26\n",
			"calc.go":   "package f8skip\n\nfunc Dead(take bool) int { return 0 }\n",
			"c_test.go": "package f8skip\n\nimport \"testing\"\n\nfunc TestDead(t *testing.T) { _ = Dead(false) }\n",
		})
		writeTreeFiles(t, dir, map[string]string{
			"calc.go": "package f8skip\n\nfunc Dead(take bool) int {\n\tif take {\n\t\treturn 1 + 2\n\t}\n\treturn 0\n}\n",
		})
		var baseRuns int
		var mu sync.Mutex
		spy := scriptedCmdExec{fn: func(ctx context.Context, c gate.Cmd) (gate.Output, error) {
			if len(c.Argv) > 0 && strings.HasPrefix(filepath.Base(c.Argv[0]), "base-") {
				mu.Lock()
				baseRuns++
				mu.Unlock()
			}
			return (gate.OS{}).Run(ctx, c)
		}}
		build := gobuild.Build{CGOEnabled: true}
		diff, err := ComputeDiff(t.Context(), gate.OS{}, dir, "main", build)
		if err != nil {
			t.Fatal(err)
		}
		env := gate.Env{Dir: dir, Base: "main", Exec: spy, Config: gate.Config{Base: "main", Mutation: gate.MutationConfig{Engine: "overlay"}}}
		ev, err := RunOverlay(t.Context(), env, diff, build, OverlayOptions{})
		if err != nil {
			t.Fatalf("RunOverlay unreached: %v", err)
		}
		if ev.Mutants != 1 || ev.SurvivedUnexplained != 1 {
			t.Fatalf("expected 1 NOT COVERED, got %+v", ev)
		}
		if baseRuns != 1 {
			t.Fatalf("expected 1 unmutated run (preflight only, no postflight when 0 mutant binaries ran), got %d", baseRuns)
		}
	})
}

// Q1(b): every compiled test binary receives -test.timeout=<execTO> alongside -test.failfast and -test.paniconexit0.
func TestRunOverlayPassesTestTimeoutFlag(t *testing.T) {
	dir := gitSetupOverlayRepo(t, map[string]string{
		"go.mod":    "module example.com/toflag\n\ngo 1.26\n",
		"calc.go":   "package toflag\n\nfunc Add(a, b int) int { return a - b }\n",
		"c_test.go": "package toflag\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { if Add(3, 2) != 1 { t.Fatal(\"bad\") } }\n",
	})
	writeTreeFiles(t, dir, map[string]string{
		"calc.go":   "package toflag\n\nfunc Add(a, b int) int { return a + b }\n",
		"c_test.go": "package toflag\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { if Add(2, 3) != 5 { t.Fatal(\"bad\") } }\n",
	})
	var seenFlags []string
	var mu sync.Mutex
	spy := scriptedCmdExec{fn: func(ctx context.Context, c gate.Cmd) (gate.Output, error) {
		if len(c.Argv) > 0 && strings.HasPrefix(filepath.Base(c.Argv[0]), "mut-") {
			mu.Lock()
			seenFlags = slices.Clone(c.Argv)
			mu.Unlock()
		}
		return (gate.OS{}).Run(ctx, c)
	}}
	build := gobuild.Build{CGOEnabled: true}
	diff, err := ComputeDiff(t.Context(), gate.OS{}, dir, "main", build)
	if err != nil {
		t.Fatal(err)
	}
	env := gate.Env{Dir: dir, Base: "main", Exec: spy, Config: gate.Config{Base: "main", Mutation: gate.MutationConfig{Engine: "overlay"}}}
	ev, err := RunOverlay(t.Context(), env, diff, build, OverlayOptions{MinTimeout: 10 * time.Second})
	if err != nil || ev.Killed != 1 {
		t.Fatalf("RunOverlay got ev=%+v err=%v", ev, err)
	}
	for _, want := range []string{"-test.failfast", "-test.paniconexit0", "-test.timeout=10s"} {
		if !slices.Contains(seenFlags, want) {
			t.Fatalf("expected %q in mutant argv %v", want, seenFlags)
		}
	}
}

// O1: running ComputeDiff / RunOverlay via a symlinked module path must classify in-build files properly (not not_measured).
func TestComputeDiffAndRunOverlaySymlinkedDir(t *testing.T) {
	realDir := gitSetupOverlayRepo(t, map[string]string{
		"go.mod":  "module example.com/symmod\n\ngo 1.26\n",
		"calc.go": "package symmod\n\nfunc Lt(a, b int) bool { return a == b }\n",
		"c_test.go": `package symmod

import "testing"

func TestBase(t *testing.T) { _ = Lt(1, 2) }
`,
	})
	writeTreeFiles(t, realDir, map[string]string{
		"calc.go": "package symmod\n\nfunc Lt(a, b int) bool { return a < b }\n",
		"c_test.go": `package symmod

import "testing"

func TestWeak(t *testing.T) {
	if !Lt(1, 5) || Lt(6, 5) {
		t.Fatal("wrong")
	}
}
`,
	})
	linkParent := t.TempDir()
	symDir := filepath.Join(linkParent, "link-to-mod")
	if err := os.Symlink(realDir, symDir); err != nil {
		t.Fatal(err)
	}
	build := gobuild.Build{CGOEnabled: true}
	diff, err := ComputeDiff(t.Context(), gate.OS{}, symDir, "main", build)
	if err != nil {
		t.Fatalf("ComputeDiff through symlink: %v", err)
	}
	if len(diff.InBuildFiles) != 1 || len(diff.OutOfBuildFiles) != 0 {
		t.Fatalf("expected calc.go in InBuildFiles and 0 OutOfBuildFiles via symlink, got in=%v out=%v", diff.InBuildFiles, diff.OutOfBuildFiles)
	}
	env := gate.Env{Dir: symDir, Base: "main", Exec: gate.OS{}, Config: gate.Config{Base: "main", Mutation: gate.MutationConfig{Engine: "overlay"}}}
	ev, err := RunOverlay(t.Context(), env, diff, build, OverlayOptions{})
	if err != nil {
		t.Fatalf("RunOverlay through symlink: %v", err)
	}
	if ev.NotMeasured != 0 || ev.Mutants != 2 || ev.Killed != 1 || ev.SurvivedUnexplained != 1 {
		t.Fatalf("expected NotMeasured=0, 2 mutants (1 KILLED, 1 LIVED) via symlink, got %+v", ev)
	}
	if ev.Survivors[0].Key.Function != "Lt" || !strings.HasPrefix(ev.Survivors[0].Key.String(), "calc.go:Lt:CONDITIONALS_BOUNDARY:") {
		t.Fatalf("expected resolved Function name Lt in survivor key via symlink, got %+v", ev.Survivors[0].Key)
	}
}
