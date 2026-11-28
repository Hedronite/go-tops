// crapprobe: read-only calibration probe for the Go-TOPS assessment.
// Computes gocyclo-rule complexity, the 0.3.0 error-guard discount, and CRAP
// three ways from a go test cover profile. Stdlib only.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type block struct{ sl, sc, el, ec, n, cnt int }

type span struct{ sl, sc, el, ec int }

type fn struct {
	File, Name         string
	Line               int
	CC, Guards, TableX int
	Stmts, Cov         int
	GStmts, GCov       int
	MaxCases           int
}

func before(al, ac, bl, bc int) bool { return al < bl || (al == bl && ac <= bc) }
func inside(b block, s span) bool {
	return before(s.sl, s.sc, b.sl, b.sc) && before(b.el, b.ec, s.el, s.ec)
}

func readProfile(path, module string) map[string][]block {
	out := map[string]map[[4]int]*block{}
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "profile:", err)
		return nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		ln := sc.Text()
		if strings.HasPrefix(ln, "mode:") {
			continue
		}
		c := strings.LastIndex(ln, ":")
		if c < 0 {
			continue
		}
		file := strings.TrimPrefix(strings.TrimPrefix(ln[:c], module), "/")
		parts := strings.Fields(ln[c+1:])
		if len(parts) != 3 {
			continue
		}
		rng := strings.Split(parts[0], ",")
		a := strings.Split(rng[0], ".")
		b := strings.Split(rng[1], ".")
		var v [4]int
		v[0], _ = strconv.Atoi(a[0])
		v[1], _ = strconv.Atoi(a[1])
		v[2], _ = strconv.Atoi(b[0])
		v[3], _ = strconv.Atoi(b[1])
		n, _ := strconv.Atoi(parts[1])
		cnt, _ := strconv.Atoi(parts[2])
		if out[file] == nil {
			out[file] = map[[4]int]*block{}
		}
		if e := out[file][v]; e != nil {
			e.cnt += cnt
		} else {
			out[file][v] = &block{v[0], v[1], v[2], v[3], n, cnt}
		}
	}
	res := map[string][]block{}
	for f, m := range out {
		for _, b := range m {
			res[f] = append(res[f], *b)
		}
	}
	return res
}

func hasIdent(exprs []ast.Expr, name string) bool {
	for _, e := range exprs {
		if id, ok := e.(*ast.Ident); ok && id.Name == name {
			return true
		}
	}
	return false
}

func assigns(s ast.Stmt, name string) bool {
	switch s := s.(type) {
	case *ast.AssignStmt:
		return hasIdent(s.Lhs, name)
	case *ast.DeclStmt:
		if gd, ok := s.Decl.(*ast.GenDecl); ok {
			for _, sp := range gd.Specs {
				if vs, ok := sp.(*ast.ValueSpec); ok {
					for _, n := range vs.Names {
						if n.Name == name {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

// isGuard implements GO_TOPS.md 0.3.0 section 6.3, syntactically.
func isGuard(s *ast.IfStmt, prev ast.Stmt) bool {
	if s.Else != nil || len(s.Body.List) != 1 {
		return false
	}
	be, ok := s.Cond.(*ast.BinaryExpr)
	if !ok || be.Op != token.NEQ {
		return false
	}
	x, ok := be.X.(*ast.Ident)
	y, ok2 := be.Y.(*ast.Ident)
	if !ok || !ok2 || y.Name != "nil" {
		return false
	}
	ret, ok := s.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) == 0 {
		return false
	}
	last := ret.Results[len(ret.Results)-1]
	okRet := false
	switch l := last.(type) {
	case *ast.Ident:
		okRet = l.Name == x.Name
	case *ast.CallExpr:
		okRet = hasIdent(l.Args, x.Name)
	}
	if !okRet {
		return false
	}
	if s.Init != nil && assigns(s.Init, x.Name) {
		return true
	}
	return prev != nil && assigns(prev, x.Name)
}

func tableSwitch(body *ast.BlockStmt) (cases int, table bool) {
	table = true
	for _, c := range body.List {
		cc, ok := c.(*ast.CaseClause)
		if !ok {
			continue
		}
		if cc.List != nil {
			cases++
		}
		if len(cc.Body) > 1 {
			table = false
		}
		for _, st := range cc.Body {
			switch st.(type) {
			case *ast.ReturnStmt, *ast.ExprStmt, *ast.AssignStmt, *ast.BranchStmt:
			default:
				table = false
			}
		}
	}
	return
}

func analyze(fset *token.FileSet, rel string, fd *ast.FuncDecl, blocks []block) fn {
	r := fn{File: rel, Name: fd.Name.Name, Line: fset.Position(fd.Pos()).Line, CC: 1}
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		t := fd.Recv.List[0].Type
		if st, ok := t.(*ast.StarExpr); ok {
			t = st.X
		}
		if ix, ok := t.(*ast.IndexExpr); ok {
			t = ix.X
		}
		if id, ok := t.(*ast.Ident); ok {
			r.Name = id.Name + "." + r.Name
		}
	}
	var guards []span
	lists := func(list []ast.Stmt) {
		for i, s := range list {
			ifs, ok := s.(*ast.IfStmt)
			if !ok {
				continue
			}
			var prev ast.Stmt
			if i > 0 {
				prev = list[i-1]
			}
			if isGuard(ifs, prev) {
				r.Guards++
				a, b := fset.Position(ifs.Body.Lbrace), fset.Position(ifs.Body.Rbrace)
				guards = append(guards, span{a.Line, a.Column, b.Line, b.Column + 1})
			}
		}
	}
	ast.Inspect(fd, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
			r.CC++
		case *ast.CaseClause:
			if n.List != nil {
				r.CC++
			}
			lists(n.Body)
		case *ast.CommClause:
			if n.Comm != nil {
				r.CC++
			}
			lists(n.Body)
		case *ast.BinaryExpr:
			if n.Op == token.LAND || n.Op == token.LOR {
				r.CC++
			}
		case *ast.BlockStmt:
			lists(n.List)
		case *ast.SwitchStmt:
			c, t := tableSwitch(n.Body)
			if c > r.MaxCases {
				r.MaxCases = c
			}
			if t && c > 1 {
				r.TableX += c - 1
			}
		case *ast.TypeSwitchStmt:
			c, t := tableSwitch(n.Body)
			if c > r.MaxCases {
				r.MaxCases = c
			}
			if t && c > 1 {
				r.TableX += c - 1
			}
		}
		return true
	})
	if fd.Body == nil {
		return r
	}
	a, b := fset.Position(fd.Pos()), fset.Position(fd.End())
	fs := span{a.Line, a.Column, b.Line, b.Column}
	for _, bl := range blocks {
		if !inside(bl, fs) {
			continue
		}
		r.Stmts += bl.n
		c := 0
		if bl.cnt > 0 {
			c = bl.n
		}
		r.Cov += c
		for _, g := range guards {
			if inside(bl, g) {
				r.GStmts += bl.n
				r.GCov += c
				break
			}
		}
	}
	return r
}

func crap(cc int, cov float64) float64 {
	c := float64(cc)
	return c*c*math.Pow(1-cov, 3) + c
}

func frac(a, b int) float64 {
	if b == 0 {
		return 1
	}
	return float64(a) / float64(b)
}

// result is one repo's numbers, as printed below and as written by -json.
type result struct {
	Repo             string           `json:"repo"`
	Functions        int              `json:"functions"`
	Statements       int              `json:"statements"`
	StmtCovPct       float64          `json:"stmt_cov_pct"`
	Guards           int              `json:"guards"`
	GuardBodyStmts   int              `json:"guard_body_stmts"`
	GuardBodyCovPct  float64          `json:"guard_body_cov_pct"`
	NoProfileBlocks  int              `json:"no_profile_blocks"`
	CCRawSum         int              `json:"cc_raw_sum"`
	CCDiscountedSum  int              `json:"cc_discounted_sum"`
	GuardShareOfCCPc float64          `json:"guard_share_of_decisions_pct"`
	Caps             []float64        `json:"caps"`
	OverCap          map[string][]int `json:"functions_over_cap"`
	Top              []topFn          `json:"top_by_neutral_crap"`
}

type topFn struct {
	Name         string  `json:"name"`
	File         string  `json:"file"`
	Line         int     `json:"line"`
	CCRaw        int     `json:"cc_raw"`
	Guards       int     `json:"guards"`
	CCDisc       int     `json:"cc_disc"`
	TableX       int     `json:"table_x"`
	MaxCases     int     `json:"max_cases"`
	CovPct       float64 `json:"cov_pct"`
	NeutralCovPc float64 `json:"neutral_cov_pct"`
	CrapRaw      float64 `json:"crap_raw"`
	CrapDisc     float64 `json:"crap_disc"`
	CrapNeutral  float64 `json:"crap_neutral"`
}

func main() {
	jsonPath := flag.String("json", "", "also write the results to this file as JSON")
	flag.Parse()
	var results []result
	caps := []float64{4, 5, 6, 8, 10, 15, 30}
	for _, repo := range flag.Args() {
		modBytes, _ := os.ReadFile(filepath.Join(repo, "go.mod"))
		module := strings.Fields(strings.SplitN(string(modBytes), "\n", 2)[0])[1]
		prof := readProfile(repo+".cover", module)
		var fns []fn
		fset := token.NewFileSet()
		filepath.Walk(repo, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				if n := info.Name(); n == "testdata" || n == "vendor" || n == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			f, err := parser.ParseFile(fset, p, nil, parser.ParseComments)
			if err != nil {
				return nil
			}
			for _, cg := range f.Comments {
				if cg.Pos() < f.Package && strings.Contains(cg.Text(), "Code generated") {
					return nil
				}
			}
			rel, _ := filepath.Rel(repo, p)
			for _, d := range f.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok {
					fns = append(fns, analyze(fset, rel, fd, prof[rel]))
				}
			}
			return nil
		})
		var st, cv, gst, gcv, guards, ccSum, ccdSum, unmeasured int
		over := map[string][]int{}
		for _, k := range []string{"raw", "disc", "neutral", "ccdisc_only", "cctable_only", "neutral_table"} {
			over[k] = make([]int, len(caps))
		}
		for _, f := range fns {
			st += f.Stmts
			cv += f.Cov
			gst += f.GStmts
			gcv += f.GCov
			guards += f.Guards
			ccSum += f.CC
			ccd := f.CC - f.Guards
			ccdSum += ccd
			if f.Stmts == 0 {
				unmeasured++
			}
			vals := map[string]float64{
				"raw":           crap(f.CC, frac(f.Cov, f.Stmts)),
				"disc":          crap(ccd, frac(f.Cov, f.Stmts)),
				"neutral":       crap(ccd, frac(f.Cov-f.GCov, f.Stmts-f.GStmts)),
				"ccdisc_only":   float64(ccd),
				"cctable_only":  float64(ccd - f.TableX),
				"neutral_table": crap(ccd-f.TableX, frac(f.Cov-f.GCov, f.Stmts-f.GStmts)),
			}
			for k, v := range vals {
				for i, c := range caps {
					if v > c+1e-9 {
						over[k][i]++
					}
				}
			}
		}
		res := result{Repo: repo, Functions: len(fns), Statements: st, StmtCovPct: 100 * frac(cv, st),
			Guards: guards, GuardBodyStmts: gst, GuardBodyCovPct: 100 * frac(gcv, gst), NoProfileBlocks: unmeasured,
			CCRawSum: ccSum, CCDiscountedSum: ccdSum, GuardShareOfCCPc: 100 * float64(guards) / float64(ccSum-len(fns)+1),
			Caps: caps, OverCap: over}
		fmt.Printf("\n=== %s  functions=%d  stmts=%d  stmt_cov=%.1f%%  guards=%d  guard_body_stmts=%d  guard_body_cov=%.1f%%  no_profile_blocks=%d\n",
			repo, len(fns), st, 100*frac(cv, st), guards, gst, 100*frac(gcv, gst), unmeasured)
		fmt.Printf("    sum CC raw=%d  discounted=%d  (guards are %.0f%% of all decisions)\n", ccSum, ccdSum, 100*float64(guards)/float64(ccSum-len(fns)+1))
		fmt.Printf("    functions over cap:        %v\n", caps)
		for _, k := range []string{"raw", "disc", "neutral", "neutral_table", "ccdisc_only", "cctable_only"} {
			fmt.Printf("    %-14s %v\n", k, over[k])
		}
		sort.Slice(fns, func(i, j int) bool {
			a := crap(fns[i].CC-fns[i].Guards, frac(fns[i].Cov-fns[i].GCov, fns[i].Stmts-fns[i].GStmts))
			b := crap(fns[j].CC-fns[j].Guards, frac(fns[j].Cov-fns[j].GCov, fns[j].Stmts-fns[j].GStmts))
			return a > b
		})
		fmt.Println("    top 12 by neutral CRAP: name file:line cc_raw guards cc_disc tableX maxcases cov% neutralcov% crap_raw crap_disc crap_neutral")
		for i, f := range fns {
			if i >= 12 {
				break
			}
			ccd := f.CC - f.Guards
			res.Top = append(res.Top, topFn{f.Name, f.File, f.Line, f.CC, f.Guards, ccd, f.TableX, f.MaxCases,
				100 * frac(f.Cov, f.Stmts), 100 * frac(f.Cov-f.GCov, f.Stmts-f.GStmts),
				crap(f.CC, frac(f.Cov, f.Stmts)), crap(ccd, frac(f.Cov, f.Stmts)), crap(ccd, frac(f.Cov-f.GCov, f.Stmts-f.GStmts))})
			fmt.Printf("      %-34s %s:%d  %d %d %d %d %d  %.0f %.0f  %.1f %.1f %.1f\n", f.Name, f.File, f.Line, f.CC, f.Guards, ccd, f.TableX, f.MaxCases,
				100*frac(f.Cov, f.Stmts), 100*frac(f.Cov-f.GCov, f.Stmts-f.GStmts),
				crap(f.CC, frac(f.Cov, f.Stmts)), crap(ccd, frac(f.Cov, f.Stmts)), crap(ccd, frac(f.Cov-f.GCov, f.Stmts-f.GStmts)))
		}
		results = append(results, res)
	}
	if *jsonPath != "" {
		out, err := json.MarshalIndent(results, "", "  ")
		if err == nil {
			err = os.WriteFile(*jsonPath, append(out, '\n'), 0o644)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "json:", err)
			os.Exit(1)
		}
	}
}
