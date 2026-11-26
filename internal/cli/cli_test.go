package cli_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/cli"
)

// The oracle is GO_TOPS.md §12.1 (the subcommands) and §12.2 (exit codes): an
// unknown command is refused with code 2 before anything runs, --help lists the
// commands, and a command's own exit code is the process exit code.

func run(t *testing.T, r *cli.Registry, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = r.Run(cli.NewEnv(&out, &errb), args)
	return code, out.String(), errb.String()
}

func echo(code int) cli.Func {
	return func(env cli.Env, args []string) int {
		env.Stdout.Printf("%s", strings.Join(args, ","))
		return code
	}
}

func TestUnknownCommandExits2(t *testing.T) {
	r := cli.NewRegistry()
	r.Register(cli.Command{Name: "known", Summary: "s", Run: echo(0)})
	code, stdout, stderr := run(t, r, "bogus")
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "bogus") {
		t.Errorf("stderr %q does not name the command", stderr)
	}
}

func TestNoCommandExits2WithUsageOnStderr(t *testing.T) {
	r := cli.NewRegistry()
	r.Register(cli.Command{Name: "known", Summary: "does a thing", Run: echo(0)})
	code, stdout, stderr := run(t, r)
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "known") {
		t.Errorf("stderr %q does not list the commands", stderr)
	}
}

func TestHelpListsEveryCommandSortedAndExits0(t *testing.T) {
	for _, flag := range []string{"--help", "-h", "help"} {
		t.Run(flag, func(t *testing.T) {
			r := cli.NewRegistry()
			r.Register(cli.Command{Name: "zeta", Summary: "last one", Run: echo(0)})
			r.Register(cli.Command{Name: "alpha", Summary: "first one", Run: echo(0)})
			code, stdout, stderr := run(t, r, flag)
			if code != 0 {
				t.Errorf("exit = %d, want 0", code)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
			a, z := strings.Index(stdout, "alpha"), strings.Index(stdout, "zeta")
			if a < 0 || z < 0 || a > z {
				t.Errorf("help lists alpha at %d and zeta at %d, want both, alpha first:\n%s", a, z, stdout)
			}
			for _, summary := range []string{"first one", "last one"} {
				if !strings.Contains(stdout, summary) {
					t.Errorf("help lacks summary %q:\n%s", summary, stdout)
				}
			}
		})
	}
}

func TestCommandReceivesItsArgsAndItsExitCodeIsReturned(t *testing.T) {
	for _, code := range []int{0, 1, 2, 3, 4} {
		r := cli.NewRegistry()
		r.Register(cli.Command{Name: "c", Summary: "s", Run: echo(code)})
		got, stdout, _ := run(t, r, "c", "x", "--y")
		if got != code {
			t.Errorf("command exits %d, Run returned %d", code, got)
		}
		if stdout != "x,--y" {
			t.Errorf("command got args %q, want x,--y", stdout)
		}
	}
}

func TestHelpFlagAfterACommandBelongsToTheCommand(t *testing.T) {
	r := cli.NewRegistry()
	r.Register(cli.Command{Name: "c", Summary: "s", Run: echo(0)})
	_, stdout, _ := run(t, r, "c", "--help")
	if stdout != "--help" {
		t.Errorf("command got %q, want --help passed through", stdout)
	}
}

func TestRegisterRefusesBadCommands(t *testing.T) {
	ok := echo(0)
	tests := []struct {
		name string
		cmd  cli.Command
	}{
		{"empty name", cli.Command{Name: "", Summary: "s", Run: ok}},
		{"flag-shaped name", cli.Command{Name: "-x", Summary: "s", Run: ok}},
		{"name with space", cli.Command{Name: "a b", Summary: "s", Run: ok}},
		{"reserved help", cli.Command{Name: "help", Summary: "s", Run: ok}},
		{"nil Run", cli.Command{Name: "c", Summary: "s", Run: nil}},
		{"no summary", cli.Command{Name: "c", Summary: "", Run: ok}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("Register did not panic")
				}
			}()
			cli.NewRegistry().Register(tt.cmd)
		})
	}
}

func TestRegisterRefusesADuplicateName(t *testing.T) {
	r := cli.NewRegistry()
	r.Register(cli.Command{Name: "c", Summary: "s", Run: echo(0)})
	defer func() {
		if recover() == nil {
			t.Error("a second Register of the same name did not panic")
		}
	}()
	r.Register(cli.Command{Name: "c", Summary: "t", Run: echo(0)})
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

// A command that succeeded but whose output could not be written did not
// succeed: §12.2 code 3 is an environment error that is not a missing tool.
func TestLostStdoutOnSuccessIsAnEnvironmentError(t *testing.T) {
	r := cli.NewRegistry()
	r.Register(cli.Command{Name: "c", Summary: "s", Run: echo(0)})
	var errb bytes.Buffer
	if got := r.Run(cli.NewEnv(brokenWriter{}, &errb), []string{"c"}); got != 3 {
		t.Errorf("exit = %d, want 3", got)
	}
	if !strings.Contains(errb.String(), "broken pipe") {
		t.Errorf("stderr %q does not report the write error", errb.String())
	}
}

func TestLostStdoutDoesNotHideACommandFailure(t *testing.T) {
	r := cli.NewRegistry()
	r.Register(cli.Command{Name: "c", Summary: "s", Run: echo(1)})
	var errb bytes.Buffer
	if got := r.Run(cli.NewEnv(brokenWriter{}, &errb), []string{"c"}); got != 1 {
		t.Errorf("exit = %d, want the command's own 1", got)
	}
}

func TestDefaultRegistryIsTheOneMainUses(t *testing.T) {
	var out, errb bytes.Buffer
	cli.Register(cli.Command{Name: "default-registry-probe", Summary: "s", Run: echo(4)})
	if got := cli.Main([]string{"default-registry-probe"}, &out, &errb); got != 4 {
		t.Errorf("Main exit = %d, want 4 from the command registered through Register", got)
	}
}

func TestBelowLimitWeak(t *testing.T) {
	if !cli.BelowLimit(1, 5) {
		t.Fatal("BelowLimit(1, 5) = false, want true")
	}
	if cli.BelowLimit(6, 5) {
		t.Fatal("BelowLimit(6, 5) = true, want false")
	}
}
