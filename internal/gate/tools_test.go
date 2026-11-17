package gate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// §10, §3.3: a mapped tool that is missing is ErrToolMissing, which a layer
// reports as tool-missing (exit 1); a module tool goes through
// `go tool -modfile=tools/go.mod <name>`.
func TestResolveModuleTool(t *testing.T) {
	ex := newExec()
	ex.toolOK = map[string]bool{"govulncheck": true}
	r := &ToolResolver{Dir: ".", Exec: ex}
	argv, err := r.Resolve(context.Background(), "govulncheck")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(argv, " "); got != "go tool -modfile=tools/go.mod govulncheck" {
		t.Fatalf("argv %q", got)
	}
	// resolved once, then remembered
	n := len(ex.calls)
	if _, err := r.Resolve(context.Background(), "govulncheck"); err != nil || len(ex.calls) != n {
		t.Errorf("second Resolve ran %d more steps (err %v)", len(ex.calls)-n, err)
	}
}

func TestResolveMissingModuleToolIsToolMissing(t *testing.T) {
	r := &ToolResolver{Dir: ".", Exec: newExec()}
	_, err := r.Resolve(context.Background(), "go-licenses")
	if !errors.Is(err, ErrToolMissing) {
		t.Fatalf("err = %v, want ErrToolMissing", err)
	}
	res := MissingTool("go-licenses", err)
	if res.Outcome != ToolMissing || res.Reason == "" {
		t.Fatalf("%+v", res)
	}
}

// With a real Go: a tools/go.mod that lacks the tool, and no tools/go.mod at
// all, are both tool-missing.
func TestResolveWithRealGo(t *testing.T) {
	if _, err := os.Stat("/usr/bin/env"); err != nil {
		t.Skip("no env")
	}
	dir := t.TempDir()
	r := &ToolResolver{Dir: dir, Exec: OS{}}
	if _, err := r.Resolve(context.Background(), "govulncheck"); !errors.Is(err, ErrToolMissing) {
		t.Fatalf("no tools/go.mod: err = %v, want ErrToolMissing", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tools", "go.mod"), []byte("module example.com/tools\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r = &ToolResolver{Dir: dir, Exec: OS{}}
	if _, err := r.Resolve(context.Background(), "govulncheck"); !errors.Is(err, ErrToolMissing) {
		t.Fatalf("empty tools/go.mod: err = %v, want ErrToolMissing", err)
	}
}

func TestResolveDownloadedBinary(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", t.TempDir()) // nothing on PATH
	r := &ToolResolver{Dir: dir, Exec: newExec(), Binary: DefaultBinaries()}
	if _, err := r.Resolve(context.Background(), "golangci-lint"); !errors.Is(err, ErrToolMissing) {
		t.Fatalf("absent: err = %v, want ErrToolMissing", err)
	}
	bin := filepath.Join(dir, ".go-tops", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "golangci-lint"), []byte("#!/bin/true\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r = &ToolResolver{Dir: dir, Exec: newExec(), Binary: DefaultBinaries()}
	argv, err := r.Resolve(context.Background(), "golangci-lint")
	if err != nil || len(argv) != 1 || argv[0] != filepath.Join(bin, "golangci-lint") {
		t.Fatalf("argv %v err %v", argv, err)
	}
}

func TestResolveBinaryFromPATH(t *testing.T) {
	dir, path := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(path, "golangci-lint"), []byte("#!/bin/true\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", path)
	r := &ToolResolver{Dir: dir, Exec: newExec(), Binary: DefaultBinaries()}
	argv, err := r.Resolve(context.Background(), "golangci-lint")
	if err != nil || len(argv) != 1 || filepath.Base(argv[0]) != "golangci-lint" {
		t.Fatalf("argv %v err %v", argv, err)
	}
}

// ---- argv, never a shell ----

// A shell would split the argument at the semicolon and run the second command.
func TestOSExecPassesArgvLiterally(t *testing.T) {
	out, err := OS{}.Run(context.Background(), Cmd{Argv: []string{"printf", "%s", "a; echo injected $HOME `id`"}})
	if err != nil || out.ExitCode != 0 {
		t.Fatalf("%v %d", err, out.ExitCode)
	}
	if got := string(out.Stdout); got != "a; echo injected $HOME `id`" {
		t.Fatalf("output %q: the argument was interpreted", got)
	}
}

func TestOSExecReportsExitCodeAsOutput(t *testing.T) {
	out, err := OS{}.Run(context.Background(), Cmd{Argv: []string{"sh-never-used-because-this-is-a-test", "x"}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing program: err = %v, out %+v", err, out)
	}
	out, err = OS{}.Run(context.Background(), Cmd{Argv: []string{"false"}})
	if err != nil || out.ExitCode != 1 {
		t.Fatalf("false: err %v exit %d; a non-zero exit is an Output, not an error", err, out.ExitCode)
	}
}

func TestOSExecDirEnvAndStdin(t *testing.T) {
	dir := t.TempDir()
	out, err := OS{}.Run(context.Background(), Cmd{Dir: dir, Argv: []string{"pwd"}})
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(dir)
	got, _ := filepath.EvalSymlinks(strings.TrimSpace(string(out.Stdout)))
	if got != want {
		t.Errorf("pwd %q, want %q", got, want)
	}
	out, err = OS{}.Run(context.Background(), Cmd{Argv: []string{"env"}, Env: []string{"GOTOPS_PROBE=1"}})
	if err != nil || !strings.Contains(string(out.Stdout), "GOTOPS_PROBE=1") {
		t.Errorf("env not passed: %v", err)
	}
	out, err = OS{}.Run(context.Background(), Cmd{Argv: []string{"cat"}, Stdin: strings.NewReader("hi")})
	if err != nil || string(out.Stdout) != "hi" {
		t.Errorf("stdin: %q %v", out.Stdout, err)
	}
}

func TestOSExecEmptyArgv(t *testing.T) {
	if _, err := (OS{}).Run(context.Background(), Cmd{}); err == nil {
		t.Fatal("empty argv ran")
	}
}

// G3a: no step goes through a shell anywhere in the kit's non-test Go source.
func TestNoShellInTheSource(t *testing.T) {
	shell := regexpShell()
	root := filepath.Join("..", "..")
	for _, sub := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, sub), func(p string, de os.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil || de.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			if loc := shell.FindIndex(b); loc != nil {
				t.Errorf("%s names a shell: %q", p, b[loc[0]:loc[1]])
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
