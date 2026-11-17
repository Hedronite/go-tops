package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	gotops "github.com/Hedronite/go-tops"
)

// asMain re-runs this test binary as the gotops process, so the tests below
// observe the real exit status of main and not a function it calls.
const asMain = "GOTOPS_TEST_AS_MAIN"

func TestMain(m *testing.M) {
	if os.Getenv(asMain) == "1" {
		main()
		return
	}
	os.Exit(m.Run())
}

func gotopsCmd(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], args...)
	cmd.Env = append(os.Environ(), asMain+"=1")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		code = 0
	case errors.As(err, &exit):
		code = exit.ExitCode()
	default:
		t.Fatalf("run gotops %v: %v", args, err)
	}
	return code, out.String(), errb.String()
}

func TestVersionPrintsProtocolAndPackage(t *testing.T) {
	code, stdout, stderr := gotopsCmd(t, "version")
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, stderr)
	}
	protocol, err := gotops.Protocol()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(protocol) {
		t.Fatalf("embedded protocol %q is not MAJOR.MINOR.PATCH", protocol)
	}
	want := "protocol " + protocol + "\npackage 0.0.0-dev\n"
	if stdout != want {
		t.Errorf("version output\n%q\nwant\n%q", stdout, want)
	}
}

func TestVersionRefusesArguments(t *testing.T) {
	code, stdout, stderr := gotopsCmd(t, "version", "extra")
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if stdout != "" || stderr == "" {
		t.Errorf("stdout %q, stderr %q: want the refusal on stderr only", stdout, stderr)
	}
}

func TestUnknownCommandExits2(t *testing.T) {
	code, _, stderr := gotopsCmd(t, "no-such-command")
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, "no-such-command") {
		t.Errorf("stderr %q does not name the command", stderr)
	}
}

func TestHelpListsVersion(t *testing.T) {
	code, stdout, _ := gotopsCmd(t, "--help")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(stdout, "version") {
		t.Errorf("--help does not list version:\n%s", stdout)
	}
}

func TestBareGotopsExits2(t *testing.T) {
	if code, _, _ := gotopsCmd(t); code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
}
