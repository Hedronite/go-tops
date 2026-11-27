package mutate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Hedronite/go-tops/internal/cover"
	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/gobuild"
)

// OverlayOptions configures native go test -overlay mutation testing.
type OverlayOptions struct {
	Workers           int           // <= 0 defaults to boundWorkers(runtime.GOMAXPROCS(0))
	TimeoutCoeff      int           // <= 0 defaults to 5
	MinTimeout        time.Duration // <= 0 defaults to 10s for mutant test execution
	MinCompileTimeout time.Duration // <= 0 defaults to 60s for go test -c compile check
	Today             time.Time     // zero defaults to time.Now()
}

type overlayPkgMeta struct {
	modulePath string
	gorootBin  string
	byRelDir   map[string]string // slash-rel dir -> import path
	pkgDirs    map[string]string // import path -> on-disk package dir
}

type overlayJob struct {
	rel         string
	pkgImport   string
	pkgDir      string
	mutator     string
	line        int
	col         int
	overlayPath string
	src         []byte
}

type switchCaseSpan struct {
	startOff int
	endOff   int
	covered  bool
}

type fileMutantPlan struct {
	uncovered []overlayJob
	jobs      []overlayJob
	nextSeq   int
}

// RunOverlay runs diff-scoped mutation testing using go test -c -overlay without modifying env.Dir.
func RunOverlay(ctx context.Context, env gate.Env, diff DiffResult, build gobuild.Build, opts OverlayOptions) (Evaluation, error) {
	opts = normalizeOverlayOpts(opts)
	ev, ec, err := prepareEvaluation(env.Config.Mutation, env.Config.Exemptions, diff, env.Dir, opts.Today, true)
	if err != nil {
		return Evaluation{}, err
	}
	if !diff.HasChangedProdLines() || len(diff.InBuildFiles) == 0 {
		return ev, nil
	}
	ex := overlayExecutor(env.Exec)
	meta, err := readOverlayPkgMeta(ctx, ex, env.Dir, build)
	if err != nil {
		return Evaluation{}, err
	}
	tmpDir, err := os.MkdirTemp("", "gotops-overlay-*")
	if err != nil {
		return Evaluation{}, err
	}
	defer func() {
		_ = os.RemoveAll(tmpDir) //nolint:errcheck // best-effort cleanup of temp scratch directory
	}()
	return executeOverlayRun(ctx, ex, env.Dir, tmpDir, meta, diff, build, opts, ec, ev)
}

func normalizeOverlayOpts(opts OverlayOptions) OverlayOptions {
	if opts.Workers < 1 {
		opts.Workers = boundWorkers(runtime.GOMAXPROCS(0))
	}
	if opts.TimeoutCoeff < 1 {
		opts.TimeoutCoeff = 5
	}
	if opts.MinTimeout <= 0 {
		opts.MinTimeout = 10 * time.Second
	}
	if opts.MinCompileTimeout <= 0 {
		opts.MinCompileTimeout = 60 * time.Second
	}
	if opts.Today.IsZero() {
		opts.Today = time.Now()
	}
	return opts
}

func overlayExecutor(ex gate.Executor) gate.Executor {
	if ex != nil {
		return ex
	}
	return gate.OS{}
}

func executeOverlayRun(ctx context.Context, ex gate.Executor, dir, tmpDir string, meta overlayPkgMeta, diff DiffResult, build gobuild.Build, opts OverlayOptions, ec evalContext, ev Evaluation) (Evaluation, error) {
	pkgs, err := overlayTargetPackages(diff.InBuildFiles, meta.byRelDir)
	if err != nil {
		return Evaluation{}, err
	}
	blocks, pkgDurs, err := runOverlayPkgCoverage(ctx, ex, dir, tmpDir, meta.modulePath, pkgs, build)
	if err != nil {
		return Evaluation{}, err
	}
	baseBins, err := verifyUnmutatedOverlayBinaries(ctx, ex, dir, tmpDir, meta, pkgs, build, opts, pkgDurs)
	if err != nil {
		return Evaluation{}, err
	}
	plan, err := buildOverlayJobs(dir, tmpDir, meta, diff.InBuildFiles, blocks)
	if err != nil {
		return Evaluation{}, err
	}
	for _, u := range plan.uncovered {
		if err := recordMutant(&ev, u.rel, toMutation(u, "NOT COVERED"), u.src, ec.idx, ec.exRes, ec.ledger); err != nil {
			return Evaluation{}, err
		}
	}
	statuses, ranPkgs, runErr := runOverlayWorkers(ctx, ex, dir, meta.gorootBin, build, opts, pkgDurs, plan.jobs)
	if runErr != nil {
		return Evaluation{}, runErr
	}
	if err := verifyPostflightOverlayBinaries(ctx, ex, meta, pkgs, baseBins, ranPkgs, build, opts, pkgDurs); err != nil {
		return Evaluation{}, err
	}
	for i, job := range plan.jobs {
		if err := recordMutant(&ev, job.rel, toMutation(job, statuses[i]), job.src, ec.idx, ec.exRes, ec.ledger); err != nil {
			return Evaluation{}, err
		}
	}
	sortEvaluation(&ev)
	return ev, nil
}

func toMutation(j overlayJob, status string) MutantOutcome {
	return MutantOutcome{Type: j.mutator, Status: status, Line: j.line, Column: j.col}
}

func scaledTimeout(baseline time.Duration, coeff int, floor time.Duration) time.Duration {
	return max(baseline*time.Duration(coeff), floor)
}

func readOverlayPkgMeta(ctx context.Context, ex gate.Executor, dir string, build gobuild.Build) (overlayPkgMeta, error) {
	env := build.Env()
	modPath, err := runGoMetaCmd(ctx, ex, dir, "read module path", []string{"go", "list", "-m"}, env)
	if err != nil {
		return overlayPkgMeta{}, err
	}
	goroot, err := runGoMetaCmd(ctx, ex, dir, "read GOROOT", []string{"go", "env", "GOROOT"}, env)
	if err != nil {
		return overlayPkgMeta{}, err
	}
	listOut, err := runGoMetaCmd(ctx, ex, dir, "list packages for overlay", []string{"go", "list", "-f", "{{.ImportPath}}\t{{.Dir}}", "./..."}, env)
	if err != nil {
		return overlayPkgMeta{}, err
	}
	gorootBin := ""
	if goroot != "" {
		gorootBin = filepath.Join(goroot, "bin")
	}
	return parseOverlayPkgMeta(dir, modPath, gorootBin, listOut), nil
}

func runGoMetaCmd(ctx context.Context, ex gate.Executor, dir, label string, argv, env []string) (string, error) {
	res, err := ex.Run(ctx, gate.Cmd{Dir: dir, Argv: argv, Env: env})
	if err != nil {
		return "", fmt.Errorf("%s: %w", label, err)
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("%s: %s", label, strings.TrimSpace(string(res.Stderr)))
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

func parseOverlayPkgMeta(dir, modPath, gorootBin, listOut string) overlayPkgMeta {
	absRoot := resolvedPath(dir)
	meta := overlayPkgMeta{
		modulePath: modPath,
		gorootBin:  gorootBin,
		byRelDir:   map[string]string{},
		pkgDirs:    map[string]string{},
	}
	for _, line := range nonEmptyLines(listOut) {
		fields := strings.SplitN(line, "\t", 2)
		if len(fields) != 2 {
			continue
		}
		imp, pkgDir := fields[0], fields[1]
		meta.pkgDirs[imp] = pkgDir
		if rel, err := filepath.Rel(absRoot, resolvedPath(pkgDir)); err == nil {
			meta.byRelDir[filepath.ToSlash(rel)] = imp
		}
	}
	return meta
}

func overlayTargetPackages(inBuild map[string]map[int]bool, byRelDir map[string]string) ([]string, error) {
	seen := map[string]bool{}
	for _, rel := range sortedMapKeys(inBuild) {
		pkgRelDir := filepath.ToSlash(filepath.Dir(rel))
		imp, ok := byRelDir[pkgRelDir]
		if !ok {
			return nil, fmt.Errorf("no package import path found for directory %s", pkgRelDir)
		}
		seen[imp] = true
	}
	out := make([]string, 0, len(seen))
	for imp := range seen {
		out = append(out, imp)
	}
	sort.Strings(out)
	return out, nil
}

// runOverlayPkgCoverage runs targeted per-package baseline tests (without -coverpkg)
// so coverage reflects same-package tests only, verifies unmutated baseline pass,
// and measures each package's baseline duration.
func runOverlayPkgCoverage(ctx context.Context, ex gate.Executor, dir, tmpDir, modPath string, pkgs []string, build gobuild.Build) (map[string][]cover.Block, map[string]time.Duration, error) {
	byFile := map[string][]cover.Block{}
	pkgDurs := make(map[string]time.Duration, len(pkgs))
	for i, pkg := range pkgs {
		covPath := filepath.Join(tmpDir, fmt.Sprintf("cover-%d.out", i))
		argv := []string{"go", "test", "-vet=off", "-count=1", "-coverprofile=" + covPath, pkg}
		t0 := time.Now()
		res, err := ex.Run(ctx, gate.Cmd{Dir: dir, Argv: argv, Env: build.Env()})
		pkgDurs[pkg] = time.Since(t0)
		if err != nil {
			return nil, nil, fmt.Errorf("baseline coverage run failed for %s: %w", pkg, err)
		}
		if res.ExitCode != 0 {
			msg := strings.TrimSpace(string(res.Stdout) + "\n" + string(res.Stderr))
			return nil, nil, fmt.Errorf("baseline coverage run failed for %s (exit %d): %s", pkg, res.ExitCode, msg)
		}
		if _, statErr := os.Stat(covPath); errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if err := mergeCoverFile(covPath, modPath, byFile); err != nil {
			return nil, nil, err
		}
	}
	return byFile, pkgDurs, nil
}

func mergeCoverFile(path, modPath string, byFile map[string][]cover.Block) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open coverage profile %s: %w", path, err)
	}
	prof, parseErr := cover.Parse(f)
	closeErr := f.Close()
	if parseErr != nil {
		return fmt.Errorf("parse coverage profile %s: %w", path, parseErr)
	}
	if closeErr != nil {
		return closeErr
	}
	prefix := modPath + "/"
	for _, b := range prof.Blocks {
		rel := strings.TrimPrefix(b.File, prefix)
		byFile[rel] = append(byFile[rel], b)
	}
	return nil
}

// verifyUnmutatedOverlayBinaries builds and executes the unmutated compiled test binary
// with GOROOT/bin first on PATH and -test.paniconexit0 before judging any mutant (V2).
func verifyUnmutatedOverlayBinaries(ctx context.Context, ex gate.Executor, dir, tmpDir string, meta overlayPkgMeta, pkgs []string, build gobuild.Build, opts OverlayOptions, pkgDurs map[string]time.Duration) (map[string]string, error) {
	bins := make(map[string]string, len(pkgs))
	for i, pkg := range pkgs {
		binPath := filepath.Join(tmpDir, fmt.Sprintf("base-%d%s", i, testBinExt(build)))
		kept, err := verifyOneUnmutatedBinary(ctx, ex, dir, meta, pkg, binPath, build, opts, pkgDurs[pkg])
		if err != nil {
			return nil, err
		}
		bins[pkg] = kept
	}
	return bins, nil
}

func verifyOneUnmutatedBinary(ctx context.Context, ex gate.Executor, dir string, meta overlayPkgMeta, pkg, binPath string, build gobuild.Build, opts OverlayOptions, pkgDur time.Duration) (string, error) {
	compTO := scaledTimeout(pkgDur, opts.TimeoutCoeff, opts.MinCompileTimeout)
	cctx, cancelCompile := context.WithTimeout(ctx, compTO)
	defer cancelCompile()
	compRes, err := ex.Run(cctx, gate.Cmd{
		Dir:  dir,
		Argv: []string{"go", "test", "-vet=off", "-c=true", "-o", binPath, pkg},
		Env:  build.Env(),
	})
	if err := checkCompileOutcome(ctx, cctx, "unmutated baseline "+pkg, compRes, err); err != nil {
		return "", err
	}
	if _, statErr := os.Stat(binPath); errors.Is(statErr, os.ErrNotExist) {
		return "", nil // package has no test files
	}
	if err := runUnmutatedBinary(ctx, ex, meta, pkg, binPath, build, opts, pkgDur, "preflight"); err != nil {
		return "", err
	}
	return binPath, nil
}

// verifyPostflightOverlayBinaries re-runs the unmutated binary after mutant execution for any
// package that ran >=1 mutant binary so leftover test/artifact state fails the layer (F8).
func verifyPostflightOverlayBinaries(ctx context.Context, ex gate.Executor, meta overlayPkgMeta, pkgs []string, baseBins map[string]string, ranPkgs map[string]bool, build gobuild.Build, opts OverlayOptions, pkgDurs map[string]time.Duration) error {
	for _, pkg := range pkgs {
		if !ranPkgs[pkg] || baseBins[pkg] == "" {
			continue
		}
		if err := runUnmutatedBinary(ctx, ex, meta, pkg, baseBins[pkg], build, opts, pkgDurs[pkg], "postflight"); err != nil {
			return err
		}
	}
	return nil
}

func runUnmutatedBinary(ctx context.Context, ex gate.Executor, meta overlayPkgMeta, pkg, binPath string, build gobuild.Build, opts OverlayOptions, pkgDur time.Duration, stage string) error {
	execTO := scaledTimeout(pkgDur, opts.TimeoutCoeff, opts.MinTimeout)
	label := stage + " unmutated baseline " + pkg
	status, err := runCompiledTestBinary(ctx, ex, meta.pkgDirs[pkg], binPath, meta.gorootBin, build, execTO, label)
	if err != nil {
		return err
	}
	if status != "LIVED" {
		return fmt.Errorf("%s unmutated compiled test binary failed for %s (%s)", stage, pkg, status)
	}
	return nil
}

func checkCompileOutcome(parentCtx, cctx context.Context, label string, res gate.Output, err error) error {
	if parentCtx.Err() != nil {
		return parentCtx.Err()
	}
	if errors.Is(cctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("compile %s timed out", label)
	}
	if err != nil {
		return fmt.Errorf("compile %s: %w", label, err)
	}
	if res.ExitCode != 0 {
		msg := strings.TrimSpace(string(res.Stdout) + "\n" + string(res.Stderr))
		return fmt.Errorf("compile %s failed (exit %d): %s", label, res.ExitCode, msg)
	}
	return nil
}

func buildOverlayJobs(dir, tmpDir string, meta overlayPkgMeta, inBuild map[string]map[int]bool, blocks map[string][]cover.Block) (fileMutantPlan, error) {
	var all fileMutantPlan
	for _, rel := range sortedMapKeys(inBuild) {
		p, err := buildFileOverlayJobs(dir, tmpDir, rel, inBuild[rel], meta, blocks[rel], all.nextSeq)
		if err != nil {
			return fileMutantPlan{}, err
		}
		all.uncovered = append(all.uncovered, p.uncovered...)
		all.jobs = append(all.jobs, p.jobs...)
		all.nextSeq = p.nextSeq
	}
	return all, nil
}

func buildFileOverlayJobs(dir, tmpDir, rel string, lines map[int]bool, meta overlayPkgMeta, fileBlocks []cover.Block, seq int) (fileMutantPlan, error) {
	fullPath := filepath.Join(dir, filepath.FromSlash(rel))
	src, err := os.ReadFile(fullPath)
	if err != nil {
		return fileMutantPlan{}, fmt.Errorf("read in-build file %s: %w", rel, err)
	}
	fset := token.NewFileSet()
	pf, err := parser.ParseFile(fset, fullPath, src, parser.ParseComments)
	if err != nil {
		return fileMutantPlan{}, fmt.Errorf("parse %s: %w", rel, err)
	}
	pkgRelDir := filepath.ToSlash(filepath.Dir(rel))
	pkgImp := meta.byRelDir[pkgRelDir]
	pkgDir := meta.pkgDirs[pkgImp]
	targetPaths := replaceTargets(pkgDir, fullPath, filepath.Base(rel))
	spans := collectSwitchCaseSpans(fset, pf, fileBlocks)
	return collectFileOverlayJobs(tmpDir, rel, pkgImp, pkgDir, targetPaths, src, fset, pf, lines, fileBlocks, spans, seq)
}

func replaceTargets(pkgDir, fullPath, base string) []string {
	var targets []string
	add := func(p string) {
		if p != "" && !slices.Contains(targets, p) {
			targets = append(targets, p)
		}
	}
	if pkgDir != "" {
		cand := filepath.Join(pkgDir, base)
		add(cand)
		add(resolvedPath(cand))
	}
	add(resolvedPath(fullPath))
	add(fullPath)
	return targets
}

// collectSwitchCaseSpans maps switch, type-switch, and select clause-expression spans
// to the cover block holding the enclosing statement keyword (E25 / ruling G1 / F3).
func collectSwitchCaseSpans(fset *token.FileSet, pf *ast.File, blocks []cover.Block) []switchCaseSpan {
	tf := fset.File(pf.Pos())
	var spans []switchCaseSpan
	ast.Inspect(pf, func(n ast.Node) bool {
		pos, body := clauseHeaderTarget(n)
		if body == nil {
			return true
		}
		covered := isPosCovered(fset, blocks, pos)
		for _, stmt := range body.List {
			if first, last, hasExpr := clauseExprRange(stmt); hasExpr {
				spans = append(spans, switchCaseSpan{
					startOff: tf.Offset(first),
					endOff:   tf.Offset(last),
					covered:  covered,
				})
			}
		}
		return true
	})
	return spans
}

func clauseHeaderTarget(n ast.Node) (token.Pos, *ast.BlockStmt) {
	switch s := n.(type) {
	case *ast.SwitchStmt:
		return s.Switch, s.Body
	case *ast.TypeSwitchStmt:
		return s.Switch, s.Body
	case *ast.SelectStmt:
		return s.Select, s.Body
	default:
		return token.NoPos, nil
	}
}

func clauseExprRange(stmt ast.Stmt) (token.Pos, token.Pos, bool) {
	switch c := stmt.(type) {
	case *ast.CaseClause:
		if len(c.List) == 0 {
			return token.NoPos, token.NoPos, false
		}
		return c.List[0].Pos(), c.List[len(c.List)-1].End(), true
	case *ast.CommClause:
		if c.Comm == nil {
			return token.NoPos, token.NoPos, false
		}
		return c.Comm.Pos(), c.Comm.End(), true
	default:
		return token.NoPos, token.NoPos, false
	}
}

func isPosCovered(fset *token.FileSet, blocks []cover.Block, p token.Pos) bool {
	pos := fset.PositionFor(p, false)
	for _, b := range blocks {
		if b.Count > 0 && blockContainsPos(b, pos.Line, pos.Column) {
			return true
		}
	}
	return false
}

type overlayWalkFrame struct {
	alwaysRun bool
	stmtPos   token.Pos
}

func collectFileOverlayJobs(tmpDir, rel, pkgImp, pkgDir string, targetPaths []string, src []byte, fset *token.FileSet, pf *ast.File, lines map[int]bool, blocks []cover.Block, spans []switchCaseSpan, seq int) (fileMutantPlan, error) {
	tf := fset.File(pf.Pos())
	plan := fileMutantPlan{nextSeq: seq}
	for _, decl := range pf.Decls {
		inPkgDecl := isPkgLevelNonFuncDecl(decl)
		err := inspectOverlayNodes(decl, inPkgDecl, func(n ast.Node, alwaysRun bool, stmtPos token.Pos) error {
			return visitMutantNode(&plan, tmpDir, rel, pkgImp, pkgDir, targetPaths, src, tf, fset, lines, blocks, spans, n, alwaysRun, stmtPos)
		})
		if err != nil {
			return fileMutantPlan{}, err
		}
	}
	return plan, nil
}

func isPkgLevelNonFuncDecl(decl ast.Decl) bool {
	gd, ok := decl.(*ast.GenDecl)
	if !ok {
		return false
	}
	return gd.Tok == token.CONST || gd.Tok == token.VAR || gd.Tok == token.TYPE
}

func isContainerStmt(st ast.Stmt) bool {
	switch st.(type) {
	case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
		return true
	default:
		return false
	}
}

// inspectOverlayNodes tracks package-level G2 scope and enclosing executable statement starts (F4),
// resetting both inside *ast.FuncLit bodies.
func inspectOverlayNodes(root ast.Node, alwaysRun bool, visit func(ast.Node, bool, token.Pos) error) error {
	var visitErr error
	stack := []overlayWalkFrame{{alwaysRun: alwaysRun, stmtPos: token.NoPos}}
	ast.Inspect(root, func(n ast.Node) bool {
		if visitErr != nil {
			return false
		}
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		next := nextWalkFrame(stack[len(stack)-1], n)
		if err := visit(n, next.alwaysRun, next.stmtPos); err != nil {
			visitErr = err
			return false
		}
		stack = append(stack, next)
		return true
	})
	return visitErr
}

func nextWalkFrame(cur overlayWalkFrame, n ast.Node) overlayWalkFrame {
	if _, ok := n.(*ast.FuncLit); ok {
		return overlayWalkFrame{alwaysRun: false, stmtPos: token.NoPos}
	}
	if st, ok := n.(ast.Stmt); ok && !isContainerStmt(st) {
		cur.stmtPos = st.Pos()
	}
	return cur
}

func visitMutantNode(plan *fileMutantPlan, tmpDir, rel, pkgImp, pkgDir string, targetPaths []string, src []byte, tf *token.File, fset *token.FileSet, lines map[int]bool, blocks []cover.Block, spans []switchCaseSpan, n ast.Node, alwaysRun bool, stmtPos token.Pos) error {
	muts, tok, tokPos, ok := overlayMutatorsForNode(n)
	if !ok {
		return nil
	}
	pos := fset.PositionFor(tokPos, false)
	if !lines[pos.Line] {
		return nil
	}
	off := tf.Offset(tokPos)
	cov := alwaysRun || isMutantCovered(fset, blocks, spans, stmtPos, pos.Line, pos.Column, off)
	return appendNodeJobs(plan, tmpDir, rel, pkgImp, pkgDir, targetPaths, src, muts, tok, off, pos, cov)
}

func overlayMutatorsForNode(n ast.Node) ([]string, token.Token, token.Pos, bool) {
	switch x := n.(type) {
	case *ast.BinaryExpr:
		muts := binaryMutators(x.Op)
		return muts, x.Op, x.OpPos, len(muts) > 0
	case *ast.IncDecStmt:
		return []string{"INCREMENT_DECREMENT"}, x.Tok, x.TokPos, true
	case *ast.UnaryExpr:
		if x.Op == token.SUB {
			return []string{"INVERT_NEGATIVES"}, x.Op, x.OpPos, true
		}
		return nil, token.ILLEGAL, token.NoPos, false
	default:
		return nil, token.ILLEGAL, token.NoPos, false
	}
}

func binaryMutators(op token.Token) []string {
	var out []string
	for _, mt := range ProtocolMutatorsOn {
		if mt == "INCREMENT_DECREMENT" || mt == "INVERT_NEGATIVES" {
			continue
		}
		if _, ok := TokenReplacements[mt][op]; ok {
			out = append(out, mt)
		}
	}
	return out
}

func appendNodeJobs(plan *fileMutantPlan, tmpDir, rel, pkgImp, pkgDir string, targetPaths []string, src []byte, muts []string, orig token.Token, off int, pos token.Position, covered bool) error {
	for _, mt := range muts {
		rep, ok := TokenReplacements[mt][orig]
		if !ok {
			continue
		}
		baseJob := overlayJob{rel: rel, pkgImport: pkgImp, pkgDir: pkgDir, mutator: mt, line: pos.Line, col: pos.Column, src: src}
		if !covered {
			plan.uncovered = append(plan.uncovered, baseJob)
			continue
		}
		plan.nextSeq++
		ovPath, err := writeMutantOverlay(tmpDir, plan.nextSeq, targetPaths, src, off, orig, rep)
		if err != nil {
			return err
		}
		baseJob.overlayPath = ovPath
		plan.jobs = append(plan.jobs, baseJob)
	}
	return nil
}

func writeMutantOverlay(tmpDir string, seq int, targetPaths []string, src []byte, off int, orig, rep token.Token) (string, error) {
	mutSrc, err := spliceTokenBytes(src, off, orig, rep)
	if err != nil {
		return "", err
	}
	mutPath := filepath.Join(tmpDir, fmt.Sprintf("mut-%d.go", seq))
	if err := os.WriteFile(mutPath, mutSrc, 0o600); err != nil {
		return "", err
	}
	replaceMap := make(map[string]string, len(targetPaths))
	for _, tp := range targetPaths {
		replaceMap[tp] = mutPath
	}
	ovPath := filepath.Join(tmpDir, fmt.Sprintf("ov-%d.json", seq))
	payload, err := json.Marshal(struct {
		Replace map[string]string
	}{Replace: replaceMap})
	if err != nil {
		return "", err
	}
	return ovPath, os.WriteFile(ovPath, payload, 0o600)
}

func spliceTokenBytes(src []byte, off int, orig, rep token.Token) ([]byte, error) {
	origStr, repStr := orig.String(), rep.String()
	if off < 0 || off+len(origStr) > len(src) || string(src[off:off+len(origStr)]) != origStr {
		return nil, fmt.Errorf("token %q not found at offset %d", origStr, off)
	}
	return slices.Concat(src[:off], []byte(repStr), src[off+len(origStr):]), nil
}

func isMutantCovered(fset *token.FileSet, blocks []cover.Block, spans []switchCaseSpan, stmtPos token.Pos, line, col, off int) bool {
	if cov, matched := innermostSwitchCaseCovered(spans, off); matched {
		return cov
	}
	if cov, inBlock := coverBlockCovered(blocks, line, col); inBlock {
		return cov
	}
	if stmtPos.IsValid() {
		return isPosCovered(fset, blocks, stmtPos)
	}
	return false
}

func coverBlockCovered(blocks []cover.Block, line, col int) (bool, bool) {
	found := false
	for _, b := range blocks {
		if !blockContainsPos(b, line, col) {
			continue
		}
		if b.Count > 0 {
			return true, true
		}
		found = true
	}
	return false, found
}

func innermostSwitchCaseCovered(spans []switchCaseSpan, off int) (bool, bool) {
	bestLen := -1
	bestCov := false
	for _, s := range spans {
		if off < s.startOff || off > s.endOff {
			continue
		}
		spanLen := s.endOff - s.startOff
		if bestLen < 0 || spanLen < bestLen {
			bestLen = spanLen
			bestCov = s.covered
		}
	}
	return bestCov, bestLen >= 0
}

func blockContainsPos(b cover.Block, line, col int) bool {
	afterStart := line > b.StartLine || (line == b.StartLine && col >= b.StartCol)
	beforeEnd := line < b.EndLine || (line == b.EndLine && col <= b.EndCol)
	return afterStart && beforeEnd
}

func runOverlayWorkers(ctx context.Context, ex gate.Executor, dir, gorootBin string, build gobuild.Build, opts OverlayOptions, pkgDurs map[string]time.Duration, jobs []overlayJob) ([]string, map[string]bool, error) {
	statuses := make([]string, len(jobs))
	ranPkgs := map[string]bool{}
	if len(jobs) == 0 {
		return statuses, ranPkgs, nil
	}
	ranBins := make([]bool, len(jobs))
	pkgLocks := makePkgMutexes(jobs)
	ch := make(chan int, len(jobs))
	for i := range jobs {
		ch <- i
	}
	close(ch)
	var firstErr error
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range min(opts.Workers, len(jobs)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range ch {
				job := jobs[idx]
				pkgDur := pkgDurs[job.pkgImport]
				compTO := scaledTimeout(pkgDur, opts.TimeoutCoeff, opts.MinCompileTimeout)
				execTO := scaledTimeout(pkgDur, opts.TimeoutCoeff, opts.MinTimeout)
				status, ran, err := executeOneOverlayMutant(ctx, ex, dir, gorootBin, build, compTO, execTO, idx, job, pkgLocks[job.pkgImport])
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					return
				}
				statuses[idx] = status
				ranBins[idx] = ran
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, nil, firstErr
	}
	for i, ran := range ranBins {
		if ran {
			ranPkgs[jobs[i].pkgImport] = true
		}
	}
	return statuses, ranPkgs, nil
}

func makePkgMutexes(jobs []overlayJob) map[string]*sync.Mutex {
	locks := make(map[string]*sync.Mutex)
	for _, job := range jobs {
		if _, ok := locks[job.pkgImport]; !ok {
			locks[job.pkgImport] = &sync.Mutex{}
		}
	}
	return locks
}

func executeOneOverlayMutant(ctx context.Context, ex gate.Executor, dir, gorootBin string, build gobuild.Build, compTO, execTO time.Duration, seq int, job overlayJob, pkgMu *sync.Mutex) (string, bool, error) {
	cctx, cancelCompile := context.WithTimeout(ctx, compTO)
	defer cancelCompile()
	binPath := filepath.Join(filepath.Dir(job.overlayPath), fmt.Sprintf("mut-%d%s", seq, testBinExt(build)))
	defer func() {
		_ = os.Remove(binPath) //nolint:errcheck // best-effort cleanup in MkdirTemp removed on function exit
	}()
	compileRes, err := ex.Run(cctx, gate.Cmd{
		Dir:  dir,
		Argv: []string{"go", "test", "-vet=off", "-overlay=" + job.overlayPath, "-c=true", "-o", binPath, job.pkgImport},
		Env:  build.Env(),
	})
	if err := checkMutantCompileOutcome(ctx, cctx, job, compTO, compileRes, err); err != nil {
		return "", false, err
	}
	if compileRes.ExitCode != 0 {
		return "NOT VIABLE", false, nil
	}
	if _, statErr := os.Stat(binPath); errors.Is(statErr, os.ErrNotExist) {
		return "NOT COVERED", false, nil
	}
	runDir := job.pkgDir
	if runDir == "" {
		runDir = filepath.Dir(job.overlayPath)
	}
	status, err := runLockedCompiledBinary(ctx, pkgMu, ex, runDir, binPath, gorootBin, build, execTO, fmt.Sprintf("mutant %s:%d", job.rel, job.line))
	if err != nil {
		return "", true, err
	}
	return status, true, nil
}

func checkMutantCompileOutcome(ctx, cctx context.Context, job overlayJob, compTO time.Duration, _ gate.Output, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(cctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("compile mutant %s:%d timed out after %s", job.rel, job.line, compTO)
	}
	if err != nil {
		return fmt.Errorf("compile mutant %s:%d: %w", job.rel, job.line, err)
	}
	return nil
}

func runLockedCompiledBinary(ctx context.Context, mu *sync.Mutex, ex gate.Executor, runDir, binPath, gorootBin string, build gobuild.Build, execTO time.Duration, label string) (string, error) {
	mu.Lock()
	defer mu.Unlock()
	return runCompiledTestBinary(ctx, ex, runDir, binPath, gorootBin, build, execTO, label)
}

func runCompiledTestBinary(ctx context.Context, ex gate.Executor, runDir, binPath, gorootBin string, build gobuild.Build, execTO time.Duration, label string) (string, error) {
	mctx, cancel := context.WithTimeout(ctx, execTO)
	defer cancel()
	strip, env := testBinaryEnv(build, gorootBin)
	res, err := ex.Run(mctx, gate.Cmd{
		Dir:      runDir,
		Argv:     []string{binPath, "-test.failfast", "-test.paniconexit0"},
		Env:      env,
		StripEnv: strip,
	})
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if errors.Is(mctx.Err(), context.DeadlineExceeded) {
		return "TIMED OUT", nil
	}
	if err != nil {
		return "", fmt.Errorf("run %s: %w", label, err)
	}
	if res.ExitCode == 0 {
		return "LIVED", nil
	}
	return "KILLED", nil
}

func testBinaryEnv(build gobuild.Build, gorootBin string) ([]string, []string) {
	env := build.Env()
	if gorootBin == "" {
		return nil, env
	}
	pathVal := gorootBin
	if cur := os.Getenv("PATH"); cur != "" {
		pathVal += string(os.PathListSeparator) + cur
	}
	return []string{"PATH="}, append(env, "PATH="+pathVal)
}

func testBinExt(build gobuild.Build) string {
	goos := build.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos == "windows" {
		return ".exe"
	}
	return ".test"
}
