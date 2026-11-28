package lock

import (
	"context"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// This file is `gotops lock --verify` (GO_TOPS.md §10, §12.1): it re-checks
// every pin. The toolchain is compared with `go env GOVERSION`; a downloaded
// binary by its SHA-256, through VerifyBinary, the check the gate shares; a
// module tool by building it from tools/go.mod and reading the module version
// from `go version -m`. A `go install` build reports its own version as dev,
// so a --version text is never compared.

// Status is the verdict on one pin.
type Status int

// The statuses of a pin. A pin present at another version or hash is a
// mismatch (exit 2, a lock mismatch); an absent tool is missing (exit 1,
// tool-missing), as §12.2 has it.
const (
	StatusOK Status = iota
	StatusMissing
	StatusMismatch
)

// Finding is the result of checking one pin.
type Finding struct {
	Subject string
	Status  Status
	Detail  string
}

// Runner runs a command in dir and returns its standard output.
type Runner func(ctx context.Context, dir string, argv ...string) (string, error)

// VerifyOptions are the inputs of Verify; the zero value of each seam uses the
// real thing.
type VerifyOptions struct {
	Ctx      context.Context
	Dir      string
	Platform string // such as "darwin/arm64"; default HostPlatform()
	Run      Runner
	LookPath func(name string) (string, error)
}

func (o VerifyOptions) withDefaults() VerifyOptions {
	if o.Platform == "" {
		o.Platform = HostPlatform()
	}
	if o.Run == nil {
		o.Run = func(ctx context.Context, dir string, argv ...string) (string, error) {
			return execRunner(ctx, dir, argv...)
		}
	}
	if o.LookPath == nil {
		o.LookPath = exec.LookPath
	}
	return o
}

func execRunner(ctx context.Context, dir string, argv ...string) (string, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("%s: %w: %s", strings.Join(argv, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// Verify checks every pin of the repository at o.Dir.
func Verify(ctx context.Context, o VerifyOptions) []Finding {
	o.Ctx = ctx
	o = o.withDefaults()
	l, err := LoadDir(o.Dir)
	if err != nil {
		return []Finding{{FileName, StatusMismatch, err.Error()}}
	}
	out := []Finding{verifyToolchain(o, l)}
	out = append(out, verifyBinaries(o, l)...)
	return append(out, verifyModuleTools(o)...)
}

// ExitCode maps findings to the exit code of §12.2: 2 for any mismatch, else 1
// for any missing tool, else 0.
func ExitCode(fs []Finding) int {
	code := 0
	for _, f := range fs {
		switch f.Status {
		case StatusMismatch:
			return 2
		case StatusMissing:
			code = 1
		case StatusOK:
		}
	}
	return code
}

func verifyToolchain(o VerifyOptions, l *Lock) Finding {
	out, err := o.Run(o.Ctx, o.Dir, "go", "env", "GOVERSION")
	if err != nil {
		return Finding{"toolchain", StatusMissing, err.Error()}
	}
	if got := strings.TrimSpace(out); got != l.Toolchain {
		return Finding{"toolchain", StatusMismatch, fmt.Sprintf("go env GOVERSION is %s, the lock pins %s", got, l.Toolchain)}
	}
	return Finding{"toolchain", StatusOK, l.Toolchain}
}

// BinaryPath resolves a downloaded binary: .go-tops/bin/<name>, then PATH.
func BinaryPath(o VerifyOptions, name string) (string, bool) {
	local := filepath.Join(o.Dir, ".go-tops", "bin", name)
	if st, err := os.Stat(local); err == nil && !st.IsDir() {
		return local, true
	}
	if p, err := o.withDefaults().LookPath(name); err == nil {
		return p, true
	}
	return "", false
}

func verifyBinaries(o VerifyOptions, l *Lock) []Finding {
	var out []Finding
	for _, b := range l.Binaries {
		path, ok := BinaryPath(o, b.Name)
		if !ok {
			out = append(out, Finding{b.Name, StatusMissing, "neither in .go-tops/bin nor on PATH"})
			continue
		}
		if err := l.VerifyBinary(b.Name, path, o.Platform); err != nil {
			out = append(out, Finding{b.Name, StatusMismatch, err.Error()})
			continue
		}
		out = append(out, Finding{b.Name, StatusOK, "SHA-256 matches the lock for " + o.Platform})
	}
	return out
}

// ModuleTool is a tool pinned in tools/go.mod.
type ModuleTool struct {
	Name, Package, Module, Version string
}

const toolsModfile = "tools/go.mod"

var (
	requireLineRe = regexp.MustCompile(`^(?:require\s+)?(\S+)\s+(v\S+)`)
	majorSuffixRe = regexp.MustCompile(`^v\d+$`)
)

// ParseToolsMod reads the tool lines and the pinned module versions of a
// tools/go.mod. A tool is matched to the requirement with the longest module
// path that prefixes its package path.
func ParseToolsMod(data []byte) ([]ModuleTool, error) {
	tools, reqs := scanToolsMod(string(data))
	out := make([]ModuleTool, 0, len(tools))
	for _, pkg := range tools {
		mod := ""
		for m := range reqs {
			if (pkg == m || strings.HasPrefix(pkg, m+"/")) && len(m) > len(mod) {
				mod = m
			}
		}
		if mod == "" {
			return nil, fmt.Errorf("%s: tool %s has no require line", toolsModfile, pkg)
		}
		out = append(out, ModuleTool{Name: toolName(pkg), Package: pkg, Module: mod, Version: reqs[mod]})
	}
	return out, nil
}

// scanToolsMod reads the tool packages and the require versions, whether each
// is a single-line directive or a parenthesised block.
func scanToolsMod(text string) (tools []string, reqs map[string]string) {
	reqs = map[string]string{}
	block := ""
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.SplitN(raw, "//", 2)[0])
		switch {
		case line == "":
		case line == ")":
			block = ""
		case strings.HasSuffix(line, "("):
			block = strings.TrimSpace(strings.TrimSuffix(line, "("))
		default:
			tools, reqs = scanLine(line, block, tools, reqs)
		}
	}
	return tools, reqs
}

func scanLine(line, block string, tools []string, reqs map[string]string) ([]string, map[string]string) {
	kind := block
	if kind == "" {
		kind, _, _ = strings.Cut(line, " ")
		line = strings.TrimSpace(strings.TrimPrefix(line, kind))
	}
	switch kind {
	case "tool":
		tools = append(tools, strings.Fields(line)[0])
	case "require":
		if m := requireLineRe.FindStringSubmatch(line); m != nil {
			reqs[m[1]] = m[2]
		}
	}
	return tools, reqs
}

// toolName is the command a tool package builds: the last path element, or its
// parent for a major-version suffix.
func toolName(pkg string) string {
	parts := strings.Split(pkg, "/")
	last := parts[len(parts)-1]
	if majorSuffixRe.MatchString(last) && len(parts) > 1 {
		return parts[len(parts)-2]
	}
	return last
}

func verifyModuleTools(o VerifyOptions) []Finding {
	data, err := os.ReadFile(filepath.Join(o.Dir, filepath.FromSlash(toolsModfile)))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return []Finding{{toolsModfile, StatusMismatch, err.Error()}}
	}
	tools, err := ParseToolsMod(data)
	if err != nil {
		return []Finding{{toolsModfile, StatusMismatch, err.Error()}}
	}
	tmp, err := os.MkdirTemp("", "gotops-lock-*")
	if err != nil {
		return []Finding{{toolsModfile, StatusMissing, err.Error()}}
	}
	defer os.RemoveAll(tmp) //nolint:errcheck // a leftover temp directory is not a verification result
	var out []Finding
	for _, t := range tools {
		out = append(out, verifyModuleTool(o, t, tmp)...)
	}
	return out
}

// verifyModuleTool builds the tool from tools/go.mod and compares the module
// version in the binary with the pin. A binary of the same name in
// .go-tops/bin is read the same way, so a stale copy is a mismatch too.
func verifyModuleTool(o VerifyOptions, t ModuleTool, tmp string) []Finding {
	built := filepath.Join(tmp, t.Name)
	if _, err := o.Run(o.Ctx, o.Dir, "go", "build", "-modfile="+toolsModfile, "-o", built, t.Package); err != nil {
		if custodyRe.MatchString(err.Error()) {
			return []Finding{{t.Name, StatusMismatch, "the module does not match its checksum record (" + toolsModfile + ", tools/go.sum): " + err.Error()}}
		}
		return []Finding{{t.Name, StatusMissing, "cannot build from " + toolsModfile + ": " + err.Error()}}
	}
	out := []Finding{moduleVersionFinding(o, t, built, "built from "+toolsModfile)}
	if local := filepath.Join(o.Dir, ".go-tops", "bin", t.Name); fileExists(local) {
		f := moduleVersionFinding(o, t, local, ".go-tops/bin/"+t.Name)
		f.Subject = t.Name + " (.go-tops/bin)"
		out = append(out, f)
	}
	return out
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// custodyRe matches a build that failed because what Go found disagrees with
// the pins: a checksum that differs, a go.sum entry that is absent, or a go.mod
// that would need an update. Network and compile failures are not custody.
var custodyRe = regexp.MustCompile(`(?i)checksum mismatch|missing go\.sum entry|SECURITY ERROR|go\.sum|updates to go\.mod needed`)

var buildInfoRe = regexp.MustCompile(`(?m)^\s*(?:mod|dep)\s+(\S+)\s+(\S+)`)

func moduleVersionFinding(o VerifyOptions, t ModuleTool, binary, what string) Finding {
	out, err := o.Run(o.Ctx, o.Dir, "go", "version", "-m", binary)
	if err != nil {
		return Finding{t.Name, StatusMismatch, "go version -m: " + err.Error()}
	}
	got := ""
	for _, m := range buildInfoRe.FindAllStringSubmatch(out, -1) {
		if m[1] == t.Module {
			got = m[2]
			break
		}
	}
	switch got {
	case t.Version:
		return Finding{t.Name, StatusOK, fmt.Sprintf("%s %s (%s)", t.Module, got, what)}
	case "":
		return Finding{t.Name, StatusMismatch, fmt.Sprintf("%s: go version -m shows no %s", what, t.Module)}
	}
	return Finding{t.Name, StatusMismatch, fmt.Sprintf("%s has %s %s, %s pins %s", what, t.Module, got, toolsModfile, t.Version)}
}

// Pin is one pinned tool for `gotops version`.
type Pin struct {
	Name, Version, Source string
}

// Pins lists the toolchain and the tools pinned in the repository at dir, or
// nothing where the files are absent or do not read.
func Pins(dir string) []Pin {
	var out []Pin
	if l, err := LoadDir(dir); err == nil {
		out = append(out, Pin{"toolchain", l.Toolchain, FileName})
		for _, b := range l.Binaries {
			out = append(out, Pin{b.Name, b.Version, FileName})
		}
	}
	if data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(toolsModfile))); err == nil {
		if tools, err := ParseToolsMod(data); err == nil {
			for _, t := range tools {
				out = append(out, Pin{t.Name, t.Version, toolsModfile})
			}
		}
	}
	return out
}
