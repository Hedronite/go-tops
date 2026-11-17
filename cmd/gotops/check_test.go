package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gotops "github.com/Hedronite/go-tops"
)

// §12.1 `check` and §12.2: a bad configuration prints its JSON pointers and
// exits 2; a good one exits 0; a repository that cannot be read exits 3.

func gotopsIn(t *testing.T, dir string, args ...string) (code int, stdout, stderr string) {
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

func module(t *testing.T, config string) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/app\n\ngo 1.26\n")
	if config != "" {
		write("go-tops.yaml", config)
	}
	return dir
}

func goodConfig(t *testing.T) string {
	t.Helper()
	p, err := gotops.Protocol()
	if err != nil {
		t.Fatal(err)
	}
	return "protocol: \"" + p + "\"\nversion: \"0.1.0\"\nmodule: example.com/app\nclass: binary-cli\nenforce: hard\n"
}

func TestCheckExits0OnAGoodConfig(t *testing.T) {
	code, stdout, stderr := gotopsIn(t, module(t, goodConfig(t)), "check")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "ok") {
		t.Errorf("stdout %q", stdout)
	}
}

func TestCheckExits2WithAPointerOnABadConfig(t *testing.T) {
	cfg := goodConfig(t) + "gates:\n  crap: {cap: 11}\n"
	code, stdout, stderr := gotopsIn(t, module(t, cfg), "check")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("stdout %q, want the issues on stderr only", stdout)
	}
	if !strings.Contains(stderr, "/gates/crap/cap") {
		t.Errorf("stderr %q does not carry the JSON pointer", stderr)
	}
}

func TestCheckExits2OnAnUnknownKeyAtThePointerOfTheKey(t *testing.T) {
	code, _, stderr := gotopsIn(t, module(t, goodConfig(t)+"bogus: 1\n"), "check")
	if code != 2 || !strings.Contains(stderr, "/bogus") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

func TestCheckExits2WithNoConfig(t *testing.T) {
	code, _, stderr := gotopsIn(t, module(t, ""), "check")
	if code != 2 || !strings.Contains(stderr, "go-tops.yaml") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

func TestCheckExits3WhenRatchetCannotReadAGitRepository(t *testing.T) {
	dir := module(t, strings.Replace(goodConfig(t), "enforce: hard", "enforce: ratchet", 1))
	if err := os.MkdirAll(filepath.Join(dir, ".go-tops"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".go-tops", "baseline.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	code, _, stderr := gotopsIn(t, dir, "check")
	if code != 3 {
		t.Errorf("exit = %d, want 3; stderr %q", code, stderr)
	}
}

func TestCheckRefusesArguments(t *testing.T) {
	if code, _, _ := gotopsIn(t, module(t, goodConfig(t)), "check", "extra"); code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
}

func TestCheckIsListedInHelp(t *testing.T) {
	_, stdout, _ := gotopsIn(t, t.TempDir(), "--help")
	if !strings.Contains(stdout, "check") {
		t.Errorf("--help lacks check:\n%s", stdout)
	}
	if code, out, _ := gotopsIn(t, t.TempDir(), "check", "--help"); code != 0 || !strings.Contains(out, "Usage: gotops check") {
		t.Errorf("check --help: exit %d, %q", code, out)
	}
}
