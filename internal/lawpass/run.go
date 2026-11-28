package lawpass

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Hedronite/go-tops/internal/lawpass/embedded"
)

// Paths the law pass writes under the module root (§8.1).
const (
	LawsConfigRel = ".go-tops/laws.golangci.yml"
	LintLawsJSON  = ".go-tops/lint-laws.json"
)

// lintEnableOnly is the --enable-only list for the law pass (§8.1).
var lintEnableOnly = []string{
	"govet", "errorlint", "errcheck", "noctx", "contextcheck",
	"bodyclose", "sqlclosecheck", "rowserrcheck", "gosec",
}

// Output is the result of an external command.
type Output struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
}

// Executor runs argv in dir.
type Executor interface {
	Run(ctx context.Context, dir string, argv []string) (Output, error)
}

// Tools resolves a tool name to argv prefix.
type Tools interface {
	Resolve(ctx context.Context, name string) ([]string, error)
}

// LintIssue is one finding in the law-pass JSON.
type LintIssue struct {
	FromLinter string
	Text       string
	Pos        struct {
		Filename string
		Line     int
	}
}

// LintResult is the parsed law-pass output.
type LintResult struct {
	Issues []LintIssue
	ByLaw  map[string]int
}

// ErrLintPass is returned when the law-pass JSON is missing or invalid (§3.4 rule 5).
type ErrLintPass struct {
	Msg string
}

func (e ErrLintPass) Error() string { return e.Msg }

// WriteLawsConfig writes the kit-embedded config to dir.
func WriteLawsConfig(dir string) error {
	path := filepath.Join(dir, filepath.FromSlash(LawsConfigRel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, embedded.LawsGolangciYAML, 0o644)
}

// Run executes the isolated law lint pass into .go-tops/lint-laws.json (GO_TOPS §8.1).
func Run(ctx context.Context, dir string, exec Executor, tools Tools) (LintResult, error) {
	if err := WriteLawsConfig(dir); err != nil {
		return LintResult{}, ErrLintPass{Msg: fmt.Sprintf("write %s: %v", LawsConfigRel, err)}
	}
	return execLintPass(ctx, dir, exec, tools)
}

func execLintPass(ctx context.Context, dir string, exec Executor, tools Tools) (LintResult, error) {
	argv, err := tools.Resolve(ctx, "golangci-lint")
	if err != nil {
		return LintResult{}, ErrLintPass{Msg: err.Error()}
	}
	path, err := prepareLintJSON(dir)
	if err != nil {
		return LintResult{}, err
	}
	out, err := runDelegatedLint(ctx, dir, path, exec, argv)
	if err != nil {
		return LintResult{}, err
	}
	return parseLintOutput(dir, path, out)
}

func prepareLintJSON(dir string) (string, error) {
	path := filepath.Join(dir, filepath.FromSlash(LintLawsJSON))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", ErrLintPass{Msg: fmt.Sprintf("prepare %s: %v", LintLawsJSON, err)}
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", ErrLintPass{Msg: fmt.Sprintf("clear %s: %v", LintLawsJSON, err)}
	}
	return path, nil
}

func runDelegatedLint(ctx context.Context, dir, jsonPath string, exec Executor, argv []string) (Output, error) {
	cfg := filepath.Join(dir, filepath.FromSlash(LawsConfigRel))
	args := append(append(append([]string{}, argv...), "run"),
		"--config", cfg,
		"--enable-only", strings.Join(lintEnableOnly, ","),
		"--output.json.path="+jsonPath,
		"./...",
	)
	out, err := exec.Run(ctx, dir, args)
	if err != nil {
		return Output{}, ErrLintPass{Msg: fmt.Sprintf("start law pass: %v", err)}
	}
	return out, nil
}

func parseLintOutput(dir, path string, out Output) (LintResult, error) {
	doc, err := os.ReadFile(path)
	if err != nil {
		return LintResult{}, ErrLintPass{Msg: fmt.Sprintf("%s was not written or is empty (exit %d: %s)", LintLawsJSON, out.ExitCode, headErr(out))}
	}
	if len(strings.TrimSpace(string(doc))) == 0 {
		return LintResult{}, ErrLintPass{Msg: fmt.Sprintf("%s was not written or is empty (exit %d: %s)", LintLawsJSON, out.ExitCode, headErr(out))}
	}
	res, err := ParseLintLawsJSON(doc)
	if err != nil {
		return LintResult{}, ErrLintPass{Msg: fmt.Sprintf("%s: %v", LintLawsJSON, err)}
	}
	normalizeIssuePaths(dir, &res)
	res.ByLaw = CountByLaw(res.Issues)
	return res, nil
}

func normalizeIssuePaths(dir string, res *LintResult) {
	for i := range res.Issues {
		res.Issues[i].Pos.Filename = moduleRel(dir, res.Issues[i].Pos.Filename)
	}
}

func headErr(out Output) string {
	s := strings.TrimSpace(string(out.Stderr))
	if s == "" {
		return "(no stderr)"
	}
	lines := strings.Split(s, "\n")
	return strings.Join(lines[:min(len(lines), 2)], "\n")
}

// ParseLintLawsJSON reads golangci-lint JSON from doc.
func ParseLintLawsJSON(doc []byte) (LintResult, error) {
	var top map[string]any
	if err := json.Unmarshal(doc, &top); err != nil {
		return LintResult{}, fmt.Errorf("not JSON: %w", err)
	}
	if _, ok := top["Issues"]; !ok {
		return LintResult{}, errors.New("not a golangci-lint result: no Issues key")
	}
	var res LintResult
	if err := json.Unmarshal(doc, &res); err != nil {
		return LintResult{}, err
	}
	return res, nil
}

func moduleRel(dir, name string) string {
	name = filepath.ToSlash(name)
	if name == "" {
		return name
	}
	if filepath.IsAbs(name) {
		if rel, err := filepath.Rel(dir, name); err == nil {
			name = filepath.ToSlash(rel)
		}
	}
	return strings.TrimPrefix(name, "./")
}
