// Package mutate implements Go-TOPS branch-diff mutation testing over `go test -overlay`.
package mutate

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/Hedronite/go-tops/internal/crap"
	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/gobuild"
)

// DiffResult describes the branch-diff scope computed against the target base ref.
type DiffResult struct {
	Base            string
	BaseSHA         string
	ModPrefix       string
	DiffPath        string
	RawDiff         string
	ProductionFiles map[string]map[int]bool
	InBuildFiles    map[string]map[int]bool
	OutOfBuildFiles map[string]map[int]bool
	AllChangedPaths []string
}

// HasChangedProdLines reports whether any non-excluded production Go file has changed lines.
func (d DiffResult) HasChangedProdLines() bool {
	return len(d.ProductionFiles) > 0
}

// ComputeDiff resolves merge-base against base, computes the relative branch diff, and splits lines by build leg.
func ComputeDiff(ctx context.Context, ex gate.Executor, dir, base string, build gobuild.Build) (DiffResult, error) {
	prefix, err := moduleRelPrefix(ctx, ex, dir)
	if err != nil {
		return DiffResult{}, err
	}
	baseSHA, err := resolveMergeBase(ctx, ex, dir, base)
	if err != nil {
		return DiffResult{}, err
	}
	raw, err := runGitDiff(ctx, ex, dir, baseSHA)
	if err != nil {
		return DiffResult{}, err
	}
	rawChanged, allPaths := parseUnifiedDiff(raw)
	untracked, err := listUntrackedGo(ctx, ex, dir)
	if err != nil {
		return DiffResult{}, err
	}
	allPaths = mergeUntracked(dir, rawChanged, allPaths, untracked)
	res, err := classifyDiff(dir, base, baseSHA, prefix, "", raw, rawChanged, allPaths, build)
	if err != nil {
		return DiffResult{}, err
	}
	filtered := buildFilteredDiff(dir, raw, untracked, res.ProductionFiles)
	diffPath, err := writeMutationDiff(dir, filtered)
	if err != nil {
		return DiffResult{}, err
	}
	res.DiffPath = diffPath
	res.RawDiff = filtered
	return res, nil
}

func moduleRelPrefix(ctx context.Context, ex gate.Executor, dir string) (string, error) {
	res, err := ex.Run(ctx, gate.Cmd{Dir: dir, Argv: []string{"git", "rev-parse", "--show-toplevel"}})
	if err != nil || res.ExitCode != 0 {
		return "", fmt.Errorf("resolve git repository root for %s: %s", dir, strings.TrimSpace(string(res.Stderr)))
	}
	top := resolvedPath(strings.TrimSpace(string(res.Stdout)))
	absDir := resolvedPath(dir)
	rel, err := filepath.Rel(top, absDir)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func resolvedPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	realPath, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs
	}
	return realPath
}

func resolveMergeBase(ctx context.Context, ex gate.Executor, dir, base string) (string, error) {
	if base == "" {
		base = "HEAD"
	}
	if err := requireFullHistory(ctx, ex, dir, base); err != nil {
		return "", err
	}
	if err := verifyBaseCommit(ctx, ex, dir, base); err != nil {
		return "", err
	}
	mb, err := ex.Run(ctx, gate.Cmd{Dir: dir, Argv: []string{"git", "merge-base", "HEAD", base}})
	sha := strings.TrimSpace(string(mb.Stdout))
	if err != nil || mb.ExitCode != 0 || sha == "" {
		return "", fmt.Errorf("git merge-base HEAD %s failed (in CI ensure fetch-depth: 0): %s", base, strings.TrimSpace(string(mb.Stderr)))
	}
	return sha, nil
}

func requireFullHistory(ctx context.Context, ex gate.Executor, dir, base string) error {
	sh, err := ex.Run(ctx, gate.Cmd{Dir: dir, Argv: []string{"git", "rev-parse", "--is-shallow-repository"}})
	if err == nil && sh.ExitCode == 0 && strings.TrimSpace(string(sh.Stdout)) == "true" {
		return fmt.Errorf("shallow clone cannot reliably compute merge-base against %s (set fetch-depth: 0)", base)
	}
	return nil
}

func verifyBaseCommit(ctx context.Context, ex gate.Executor, dir, base string) error {
	res, err := ex.Run(ctx, gate.Cmd{Dir: dir, Argv: []string{"git", "rev-parse", "--verify", base + "^{commit}"}})
	if err != nil || res.ExitCode != 0 || strings.TrimSpace(string(res.Stdout)) == "" {
		return fmt.Errorf("base ref %q not found in git history (in CI ensure fetch-depth: 0): %s", base, strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}

var gitDiffStripEnv = []string{"GIT_DIFF_OPTS=", "GIT_EXTERNAL_DIFF="}

func gitDiffEnv() []string {
	return []string{
		"GIT_CONFIG_COUNT=9",
		"GIT_CONFIG_KEY_0=diff.context", "GIT_CONFIG_VALUE_0=0",
		"GIT_CONFIG_KEY_1=diff.interHunkContext", "GIT_CONFIG_VALUE_1=0",
		"GIT_CONFIG_KEY_2=diff.mnemonicPrefix", "GIT_CONFIG_VALUE_2=false",
		"GIT_CONFIG_KEY_3=diff.noprefix", "GIT_CONFIG_VALUE_3=false",
		"GIT_CONFIG_KEY_4=diff.srcPrefix", "GIT_CONFIG_VALUE_4=a/",
		"GIT_CONFIG_KEY_5=diff.dstPrefix", "GIT_CONFIG_VALUE_5=b/",
		"GIT_CONFIG_KEY_6=core.quotePath", "GIT_CONFIG_VALUE_6=false",
		"GIT_CONFIG_KEY_7=color.ui", "GIT_CONFIG_VALUE_7=never",
		"GIT_CONFIG_KEY_8=color.diff", "GIT_CONFIG_VALUE_8=never",
	}
}

func runGitDiff(ctx context.Context, ex gate.Executor, dir, baseSHA string) (string, error) {
	argv := []string{
		"git", "diff", "--no-color", "--no-ext-diff", "--relative",
		"--src-prefix=a/", "--dst-prefix=b/", "--unified=0", "--inter-hunk-context=0",
		"-M", baseSHA, "--", ".",
	}
	res, err := ex.Run(ctx, gate.Cmd{Dir: dir, Argv: argv, Env: gitDiffEnv(), StripEnv: gitDiffStripEnv})
	if err != nil || res.ExitCode != 0 {
		return "", fmt.Errorf("git diff against %s failed: %s", baseSHA, strings.TrimSpace(string(res.Stderr)))
	}
	return string(res.Stdout), nil
}

func writeMutationDiff(dir, raw string) (string, error) {
	outDir := filepath.Join(dir, ".go-tops")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	diffPath := filepath.Join(outDir, "mutation.diff")
	if err := os.WriteFile(diffPath, []byte(raw), 0o644); err != nil {
		return "", err
	}
	return diffPath, nil
}

func parseUnifiedDiff(raw string) (map[string]map[int]bool, []string) {
	changed := map[string]map[int]bool{}
	seen := map[string]bool{}
	cur := ""
	for _, line := range strings.Split(raw, "\n") {
		if path, ok := parseDiffTarget(line); ok {
			cur = path
			recordPath(&seen, path)
			continue
		}
		if cur != "" {
			addHunkLines(line, cur, changed)
		}
	}
	return changed, sortedKeys(seen)
}

func parseDiffTarget(line string) (string, bool) {
	if !strings.HasPrefix(line, "+++ ") {
		return "", false
	}
	rest := strings.TrimPrefix(line, "+++ ")
	if rest == "/dev/null" {
		return "", true
	}
	return strings.TrimPrefix(rest, "b/"), true
}

func recordPath(seen *map[string]bool, p string) {
	if p != "" {
		(*seen)[p] = true
	}
}

func addHunkLines(line, cur string, changed map[string]map[int]bool) {
	start, count, ok := parseHunkHeader(line)
	if !ok || count == 0 {
		return
	}
	if changed[cur] == nil {
		changed[cur] = map[int]bool{}
	}
	for i := 0; i < count; i++ {
		changed[cur][start+i] = true
	}
}

func parseHunkHeader(line string) (int, int, bool) {
	if !strings.HasPrefix(line, "@@ ") {
		return 0, 0, false
	}
	plusIdx := strings.Index(line, " +")
	if plusIdx < 0 {
		return 0, 0, false
	}
	tail := line[plusIdx+2:]
	endIdx := strings.Index(tail, " @@")
	if endIdx < 0 {
		return 0, 0, false
	}
	return parseLineSpan(tail[:endIdx])
}

func parseLineSpan(span string) (int, int, bool) {
	startStr, countStr, hasComma := strings.Cut(span, ",")
	start, err := strconv.Atoi(startStr)
	if err != nil {
		return 0, 0, false
	}
	if !hasComma {
		return start, 1, true
	}
	count, err := strconv.Atoi(countStr)
	if err != nil {
		return 0, 0, false
	}
	return start, count, true
}

func listUntrackedGo(ctx context.Context, ex gate.Executor, dir string) ([]string, error) {
	res, err := ex.Run(ctx, gate.Cmd{
		Dir:      dir,
		Env:      gitDiffEnv(),
		StripEnv: gitDiffStripEnv,
		Argv:     []string{"git", "ls-files", "--others", "--exclude-standard", "--", "*.go"},
	})
	if err != nil || res.ExitCode != 0 {
		return nil, fmt.Errorf("list untracked Go files in %s: %s", dir, strings.TrimSpace(string(res.Stderr)))
	}
	return nonEmptyLines(string(res.Stdout)), nil
}

func mergeUntracked(dir string, changed map[string]map[int]bool, paths, untracked []string) []string {
	seen := map[string]bool{}
	for _, p := range paths {
		seen[p] = true
	}
	for _, rel := range untracked {
		rel = filepath.ToSlash(rel)
		seen[rel] = true
		addAllFileLines(dir, rel, changed)
	}
	return sortedKeys(seen)
}

func addAllFileLines(dir, rel string, changed map[string]map[int]bool) {
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		return
	}
	n := bytes.Count(data, []byte("\n"))
	if len(data) > 0 && data[len(data)-1] != '\n' {
		n++
	}
	if n == 0 {
		return
	}
	changed[rel] = map[int]bool{}
	for i := 1; i <= n; i++ {
		changed[rel][i] = true
	}
}

func classifyDiff(dir, base, baseSHA, prefix, diffPath, raw string, changed map[string]map[int]bool, paths []string, build gobuild.Build) (DiffResult, error) {
	sel, err := crap.Select(resolvedPath(dir), build)
	if err != nil {
		return DiffResult{}, err
	}
	out := DiffResult{
		Base:            base,
		BaseSHA:         baseSHA,
		ModPrefix:       prefix,
		DiffPath:        diffPath,
		RawDiff:         raw,
		ProductionFiles: map[string]map[int]bool{},
		InBuildFiles:    map[string]map[int]bool{},
		OutOfBuildFiles: map[string]map[int]bool{},
		AllChangedPaths: paths,
	}
	for rel, lines := range changed {
		if len(lines) == 0 || !isEligibleProdFile(dir, rel) {
			continue
		}
		out.ProductionFiles[rel] = lines
		assignByBuild(rel, lines, sel.Files[rel], out.InBuildFiles, out.OutOfBuildFiles)
	}
	return out, nil
}

func assignByBuild(rel string, lines map[int]bool, inBuild bool, inMap, outMap map[string]map[int]bool) {
	if inBuild {
		inMap[rel] = lines
		return
	}
	outMap[rel] = lines
}

func isEligibleProdFile(dir, rel string) bool {
	if !isProdGoFilename(rel) || hasExcludedSegment(rel) || insideSubmodule(dir, rel) {
		return false
	}
	full := filepath.Join(dir, filepath.FromSlash(rel))
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, full, nil, parser.ParseComments)
	return err == nil && !ast.IsGenerated(file)
}

func hasExcludedSegment(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, seg := range parts[:len(parts)-1] {
		if seg == "." || seg == "" {
			continue
		}
		if isSkippedDirName(seg) {
			return true
		}
	}
	return false
}

func insideSubmodule(dir, rel string) bool {
	cur := filepath.Dir(filepath.ToSlash(rel))
	for cur != "." && cur != "" {
		if hasNestedGoMod(filepath.Join(dir, filepath.FromSlash(cur))) {
			return true
		}
		cur = filepath.Dir(cur)
	}
	return false
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func buildFilteredDiff(dir, raw string, untracked []string, prod map[string]map[int]bool) string {
	var buf strings.Builder
	for _, sec := range splitDiffSections(raw) {
		if target := sectionTarget(sec); target != "" && len(prod[target]) > 0 {
			buf.WriteString(sec)
		}
	}
	appendUntrackedSections(&buf, dir, untracked, prod)
	return buf.String()
}

func splitDiffSections(raw string) []string {
	if raw == "" {
		return nil
	}
	var sections []string
	var cur strings.Builder
	for _, line := range strings.SplitAfter(raw, "\n") {
		if strings.HasPrefix(line, "diff --git ") && cur.Len() > 0 {
			sections = append(sections, cur.String())
			cur.Reset()
		}
		cur.WriteString(line)
	}
	if cur.Len() > 0 {
		sections = append(sections, cur.String())
	}
	return sections
}

func sectionTarget(sec string) string {
	for _, line := range strings.Split(sec, "\n") {
		if target, ok := parseDiffTarget(line); ok {
			return target
		}
	}
	return ""
}

func appendUntrackedSections(buf *strings.Builder, dir string, untracked []string, prod map[string]map[int]bool) {
	sorted := slices.Clone(untracked)
	sort.Strings(sorted)
	for _, rawRel := range sorted {
		rel := filepath.ToSlash(rawRel)
		if len(prod[rel]) == 0 {
			continue
		}
		writeSyntheticDiff(buf, dir, rel)
	}
}

func writeSyntheticDiff(buf *strings.Builder, dir, rel string) {
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil || len(data) == 0 {
		return
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	fmt.Fprintf(buf, "diff --git a/%s b/%s\nnew file mode 100644\n--- /dev/null\n+++ b/%s\n@@ -0,0 +1,%d @@\n",
		rel, rel, rel, len(lines))
	for _, line := range lines {
		buf.WriteString("+")
		buf.WriteString(line)
		buf.WriteString("\n")
	}
}
