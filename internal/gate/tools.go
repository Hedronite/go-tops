package gate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ErrToolMissing is wrapped by Resolve when a tool cannot be found. The layer
// that asked reports tool-missing, and the gate exits 1 (§12.2).
var ErrToolMissing = errors.New("tool missing")

// ToolsModfile is the module file that pins module tools (§10).
const ToolsModfile = "tools/go.mod"

// Resolver turns a tool name into the argv prefix that runs it.
type Resolver interface {
	Resolve(ctx context.Context, name string) ([]string, error)
}

// ToolResolver resolves the two kinds of tool of §10:
//
//   - a module tool (govulncheck, gotestsum, go-licenses, the gotops
//     analyzers) runs as `go tool -modfile=tools/go.mod <name>`;
//   - a binary the lock downloads (golangci-lint) is `.go-tops/bin/<name>`
//     when present, else the first <name> on PATH.
//
// Resolve asks Go whether a module tool exists (`go tool -n`), so a tool
// missing from tools/go.mod, or a missing tools/go.mod, is ErrToolMissing.
// Whether a found tool is the pinned version is `gotops lock --verify`.
type ToolResolver struct {
	Dir    string
	Exec   Executor
	Binary map[string]bool // names resolved as downloaded binaries
	cache  map[string][]string
}

// DefaultBinaries are the tools §10 gives to go-tops.lock rather than tools/go.mod.
func DefaultBinaries() map[string]bool { return map[string]bool{"golangci-lint": true} }

// Resolve implements Resolver.
func (r *ToolResolver) Resolve(ctx context.Context, name string) ([]string, error) {
	if argv, ok := r.cache[name]; ok {
		return argv, nil
	}
	var argv []string
	var err error
	if r.Binary[name] {
		argv, err = r.binary(name)
	} else {
		argv, err = r.module(ctx, name)
	}
	if err != nil {
		return nil, err
	}
	if r.cache == nil {
		r.cache = map[string][]string{}
	}
	r.cache[name] = argv
	return argv, nil
}

func (r *ToolResolver) binary(name string) ([]string, error) {
	local := filepath.Join(r.Dir, ".go-tops", "bin", name)
	if st, err := os.Stat(local); err == nil && !st.IsDir() {
		return []string{local}, nil
	}
	p, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %s is neither in .go-tops/bin nor on PATH", ErrToolMissing, name)
	}
	return []string{p}, nil
}

func (r *ToolResolver) module(ctx context.Context, name string) ([]string, error) {
	prefix := []string{"go", "tool", "-modfile=" + ToolsModfile, name}
	probe := []string{"go", "tool", "-modfile=" + ToolsModfile, "-n", name}
	out, err := r.Exec.Run(ctx, Cmd{Dir: r.Dir, Argv: probe})
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrToolMissing, name, err)
	}
	if out.ExitCode != 0 {
		return nil, fmt.Errorf("%w: %s is not a tool of %s", ErrToolMissing, name, ToolsModfile)
	}
	return prefix, nil
}
