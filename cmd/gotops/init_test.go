package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initCmdIn runs the gotops process with dir as its working directory.
func initCmdIn(t *testing.T, dir string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), asMain+"=1")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		code = exit.ExitCode()
	default:
		t.Fatalf("run gotops %v: %v", args, err)
	}
	return code, out.String(), errb.String()
}

func goModule(t *testing.T, module string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+module+"\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestInitIsListedInHelp(t *testing.T) {
	_, stdout, _ := gotopsCmd(t, "--help")
	if !strings.Contains(stdout, "init ") {
		t.Errorf("--help does not list init:\n%s", stdout)
	}
}

func TestInitUsageErrorsExitTwo(t *testing.T) {
	cases := map[string][]string{
		"no-class":     {"init"},
		"unknown-flag": {"init", "--class", "binary-cli", "--bogus"},
		"argument":     {"init", "--class", "binary-cli", "extra"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			dir := goModule(t, "github.com/acme/demo")
			code, stdout, stderr := initCmdIn(t, dir, args...)
			if code != 2 || stdout != "" || !strings.Contains(stderr, "Usage: gotops init") {
				t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
			}
			if _, err := os.Stat(filepath.Join(dir, "go-tops.yaml")); err == nil {
				t.Error("a usage error wrote files")
			}
		})
	}
}

func TestInitHelpExitsZero(t *testing.T) {
	code, stdout, stderr := gotopsCmd(t, "init", "--help")
	if code != 0 || !strings.Contains(stdout, "--class") || stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestInitWritesIntoTheCurrentModule(t *testing.T) {
	dir := goModule(t, "github.com/acme/demo")
	code, stdout, stderr := initCmdIn(t, dir, "init", "--class", "binary-cli", "--offline")
	if code != 0 {
		t.Fatalf("exit %d\n%s%s", code, stdout, stderr)
	}
	for _, f := range []string{"go-tops.yaml", "go-tops.lock", "tools/go.mod", ".golangci.yml", "AGENTS.md", "GO-TOPS.md", "justfile"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); err != nil {
			t.Errorf("init did not write %s: %v", f, err)
		}
	}
	// a development build is not a release, so gotops is not pinned
	mod, err := os.ReadFile(filepath.Join(dir, "tools", "go.mod"))
	if err != nil || strings.Contains(string(mod), "Hedronite/go-tops") {
		t.Errorf("a development build must not pin gotops: %v\n%s", err, mod)
	}
	if code, _, _ := initCmdIn(t, dir, "init", "--class", "binary-cli", "--offline"); code != 2 {
		t.Errorf("a second init without --force exits %d, want 2", code)
	}
}
