// Package initcmd implements `gotops init --class <c>` (GO_TOPS.md §12.1,
// §4.1): it writes the drop-in file set of a Go-TOPS adopter. It runs no gate
// and never writes a baseline.
package initcmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

// Exit codes (§12.2). A refusal is a configuration or custody problem: exit 2.
const (
	ExitOK      = 0
	ExitFailure = 1
	ExitRefused = 2
)

// Options are the inputs of one init run.
type Options struct {
	Class      string
	Dir        string // the module root
	Owner      string // CODEOWNERS owner; empty derives it from a github.com module path
	Force      bool
	Offline    bool   // do not run go mod tidy for tools/go.sum
	Assets     fs.FS  // the kit's embedded files, in repo layout: profiles/, config/
	KitVersion string // the gotops release; empty or not a release leaves gotops out of tools/go.mod
	// Tidy fills tools/go.sum. Nil runs `go -C tools mod tidy` in Dir.
	Tidy func(dir string) error
}

// refusal is an error that exits 2.
type refusal struct{ msg string }

func (r refusal) Error() string { return r.msg }

func refuse(format string, a ...any) error { return refusal{fmt.Sprintf(format, a...)} }

// Printer is where init prints: cli.Out satisfies it and remembers a lost write.
type Printer interface {
	Printf(format string, a ...any)
}

// Run writes the files and returns the exit code.
func Run(o Options, stdout, stderr Printer) int {
	p, err := buildPlan(o)
	if err != nil {
		stderr.Printf("gotops init: %v\n", err)
		return codeOf(err)
	}
	if err := p.write(o.Dir); err != nil {
		stderr.Printf("gotops init: %v\n", err)
		return ExitFailure
	}
	for _, f := range p.files {
		stdout.Printf("wrote %s\n", f.path)
	}
	stdout.Printf("wrote %s\n", agentsFile)
	for _, n := range p.notes {
		stdout.Printf("note: %s\n", n)
	}
	if o.Offline {
		stdout.Printf("note: --offline: tools/go.sum is not written; run go -C tools mod tidy\n")
		return ExitOK
	}
	return runTidy(o, stderr)
}

func codeOf(err error) int {
	var r refusal
	if errors.As(err, &r) {
		return ExitRefused
	}
	return ExitFailure
}

func runTidy(o Options, stderr Printer) int {
	tidy := o.Tidy
	if tidy == nil {
		tidy = goModTidy
	}
	if err := tidy(o.Dir); err != nil {
		stderr.Printf("gotops init: go mod tidy for tools/go.mod: %v\n", err)
		return ExitFailure
	}
	return ExitOK
}

func goModTidy(dir string) error {
	// Inside tools/, so the tools module is tidied on its own. From the module root,
	// -modfile=tools/go.mod would read the adopter's packages as packages of the
	// tools module and fail as soon as one imports another.
	cmd := exec.Command("go", "-C", "tools", "mod", "tidy")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w\n%s", err, out)
	}
	return nil
}

type file struct {
	path string
	data []byte
}

// plan is everything init will write, built and checked before any write.
type plan struct {
	files  []file // every file except AGENTS.md
	agents []byte // the full new AGENTS.md
	notes  []string
}

const agentsFile = "AGENTS.md"

func buildPlan(o Options) (*plan, error) {
	in, err := gatherInputs(o)
	if err != nil {
		return nil, err
	}
	files, err := renderFiles(in)
	if err != nil {
		return nil, err
	}
	agents, err := renderAgents(o, in)
	if err != nil {
		return nil, err
	}
	if err := checkConflicts(o, files); err != nil {
		return nil, err
	}
	return &plan{files: files, agents: agents, notes: in.notes()}, nil
}

// checkConflicts refuses to overwrite an existing file without Force, listing
// every one so a single run says everything.
func checkConflicts(o Options, files []file) error {
	if o.Force {
		return nil
	}
	var existing []string
	for _, f := range files {
		if _, err := os.Lstat(filepath.Join(o.Dir, filepath.FromSlash(f.path))); err == nil {
			existing = append(existing, f.path)
		}
	}
	sort.Strings(existing)
	if len(existing) > 0 {
		return refuse("refusing to overwrite %v without --force; nothing was written", existing)
	}
	return nil
}

func (p *plan) write(dir string) error {
	all := append([]file{{agentsFile, p.agents}}, p.files...)
	for _, f := range all {
		path := filepath.Join(dir, filepath.FromSlash(f.path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, f.data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
