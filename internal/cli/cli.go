// Package cli is the command registry behind cmd/gotops. A command lives in its
// own file under cmd/gotops and joins the registry from an init function, so a
// step that adds one never edits main.go.
package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Exit codes the shell itself decides, before any command runs (GO_TOPS.md
// §12.2). A command returns its own codes.
const (
	// ExitUsage is an invocation gotops cannot act on: no command, or an
	// unknown one.
	ExitUsage = 2
	// ExitEnvironment is an environment error that is not a missing tool, such
	// as standard output that cannot be written.
	ExitEnvironment = 3
)

// Out is a writer that remembers its first error, so a command prints without
// checking every line and the registry still sees a lost write.
type Out struct {
	w   io.Writer
	err error
}

// Printf writes formatted text. After the first failed write it does nothing.
func (o *Out) Printf(format string, a ...any) {
	if o.err != nil {
		return
	}
	_, o.err = fmt.Fprintf(o.w, format, a...)
}

// Env is what a command may touch: its two output streams.
type Env struct {
	Stdout, Stderr *Out
}

// NewEnv wraps the two streams.
func NewEnv(stdout, stderr io.Writer) Env {
	return Env{Stdout: &Out{w: stdout}, Stderr: &Out{w: stderr}}
}

// Func runs a command with the arguments after its name and returns the exit
// code.
type Func func(env Env, args []string) int

// Command is one subcommand: the word that selects it, a one-line summary for
// --help, and its function.
type Command struct {
	Name    string
	Summary string
	Run     Func
}

// Registry holds the commands by name.
type Registry struct {
	cmds map[string]Command
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{cmds: map[string]Command{}} }

// Default is the registry cmd/gotops runs. Each command file registers itself
// in it from init.
var Default = NewRegistry()

// Register adds a command to the Default registry.
func Register(c Command) { Default.Register(c) }

// Main runs the Default registry on the arguments after the program name and
// returns the process exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	return Default.Run(NewEnv(stdout, stderr), args)
}

// Register adds a command. It panics on a malformed or duplicate command,
// because a registration is code and a mistake in it is a bug, found by the
// first test run.
func (r *Registry) Register(c Command) {
	switch {
	case c.Name == "" || strings.HasPrefix(c.Name, "-") || strings.ContainsAny(c.Name, " \t\n"):
		panic(fmt.Sprintf("cli: bad command name %q", c.Name))
	case c.Name == "help":
		panic("cli: help is reserved")
	case c.Run == nil:
		panic(fmt.Sprintf("cli: command %q has no Run", c.Name))
	case c.Summary == "":
		panic(fmt.Sprintf("cli: command %q has no summary", c.Name))
	}
	if _, dup := r.cmds[c.Name]; dup {
		panic(fmt.Sprintf("cli: command %q registered twice", c.Name))
	}
	r.cmds[c.Name] = c
}

// Run dispatches args[0] to its command and returns the exit code. A command
// that returned 0 but whose output was lost exits 3.
func (r *Registry) Run(env Env, args []string) int {
	code := r.dispatch(env, args)
	if code == 0 && env.Stdout.err != nil {
		env.Stderr.Printf("gotops: write standard output: %v\n", env.Stdout.err)
		return ExitEnvironment
	}
	return code
}

func (r *Registry) dispatch(env Env, args []string) int {
	if len(args) == 0 {
		r.usage(env.Stderr)
		return ExitUsage
	}
	switch args[0] {
	case "--help", "-h", "help":
		r.usage(env.Stdout)
		return 0
	}
	cmd, ok := r.cmds[args[0]]
	if !ok {
		env.Stderr.Printf("gotops: unknown command %q\nRun 'gotops --help' for the commands.\n", args[0])
		return ExitUsage
	}
	return cmd.Run(env, args[1:])
}

func (r *Registry) usage(out *Out) {
	names := make([]string, 0, len(r.cmds))
	width := 0
	for name := range r.cmds {
		names = append(names, name)
		width = max(width, len(name))
	}
	sort.Strings(names)
	out.Printf("gotops: the Go-TOPS gate\n\nUsage: gotops <command> [flags]\n\nCommands:\n")
	for _, name := range names {
		out.Printf("  %-*s  %s\n", width, name, r.cmds[name].Summary)
	}
	out.Printf("\nRun 'gotops <command> --help' for a command's flags.\n")
}
