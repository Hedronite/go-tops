package mutate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner" //nolint:depguard // Go standard library package required by §6.4 line_hash
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Hedronite/go-tops/internal/exempt"
)

// TokenReplacements mirrors the exact token replacement table used by gremlins v0.6.0.
var TokenReplacements = map[string]map[token.Token]token.Token{
	"ARITHMETIC_BASE": {
		token.ADD: token.SUB,
		token.MUL: token.QUO,
		token.QUO: token.MUL,
		token.REM: token.MUL,
		token.SUB: token.ADD,
	},
	"CONDITIONALS_BOUNDARY": {
		token.GEQ: token.GTR,
		token.GTR: token.GEQ,
		token.LEQ: token.LSS,
		token.LSS: token.LEQ,
	},
	"CONDITIONALS_NEGATION": {
		token.EQL: token.NEQ,
		token.GEQ: token.LSS,
		token.GTR: token.LEQ,
		token.LEQ: token.GTR,
		token.LSS: token.GEQ,
		token.NEQ: token.EQL,
	},
	"INCREMENT_DECREMENT": {
		token.DEC: token.INC,
		token.INC: token.DEC,
	},
	"INVERT_NEGATIVES": {
		token.SUB: token.ADD,
	},
	"INVERT_LOGICAL": {
		token.LAND: token.LOR,
		token.LOR:  token.LAND,
	},
	"INVERT_ASSIGNMENTS": {
		token.ADD_ASSIGN: token.SUB_ASSIGN,
		token.MUL_ASSIGN: token.QUO_ASSIGN,
		token.QUO_ASSIGN: token.MUL_ASSIGN,
		token.REM_ASSIGN: token.REM_ASSIGN,
		token.SUB_ASSIGN: token.ADD_ASSIGN,
	},
	"INVERT_BITWISE": {
		token.AND:     token.OR,
		token.OR:      token.AND,
		token.XOR:     token.AND,
		token.AND_NOT: token.AND,
		token.SHL:     token.SHR,
		token.SHR:     token.SHL,
	},
	"INVERT_BWASSIGN": {
		token.AND_ASSIGN:     token.OR_ASSIGN,
		token.OR_ASSIGN:      token.AND_ASSIGN,
		token.XOR_ASSIGN:     token.AND_ASSIGN,
		token.AND_NOT_ASSIGN: token.AND_ASSIGN,
		token.SHL_ASSIGN:     token.SHR_ASSIGN,
		token.SHR_ASSIGN:     token.SHL_ASSIGN,
	},
	"INVERT_LOOPCTRL": {
		token.BREAK:    token.CONTINUE,
		token.CONTINUE: token.BREAK,
	},
	"REMOVE_SELF_ASSIGNMENTS": {
		token.ADD_ASSIGN:     token.ASSIGN,
		token.AND_ASSIGN:     token.ASSIGN,
		token.AND_NOT_ASSIGN: token.ASSIGN,
		token.MUL_ASSIGN:     token.ASSIGN,
		token.OR_ASSIGN:      token.ASSIGN,
		token.QUO_ASSIGN:     token.ASSIGN,
		token.REM_ASSIGN:     token.ASSIGN,
		token.SHL_ASSIGN:     token.ASSIGN,
		token.SHR_ASSIGN:     token.ASSIGN,
		token.SUB_ASSIGN:     token.ASSIGN,
		token.XOR_ASSIGN:     token.ASSIGN,
	},
}

var tokenMutantTypes = map[token.Token][]string{
	token.ADD:            {"ARITHMETIC_BASE"},
	token.ADD_ASSIGN:     {"INVERT_ASSIGNMENTS", "REMOVE_SELF_ASSIGNMENTS"},
	token.AND:            {"INVERT_BITWISE"},
	token.AND_ASSIGN:     {"REMOVE_SELF_ASSIGNMENTS", "INVERT_BWASSIGN"},
	token.AND_NOT:        {"INVERT_BITWISE"},
	token.AND_NOT_ASSIGN: {"REMOVE_SELF_ASSIGNMENTS", "INVERT_BWASSIGN"},
	token.BREAK:          {"INVERT_LOOPCTRL"},
	token.CONTINUE:       {"INVERT_LOOPCTRL"},
	token.DEC:            {"INCREMENT_DECREMENT"},
	token.EQL:            {"CONDITIONALS_NEGATION"},
	token.GEQ:            {"CONDITIONALS_BOUNDARY", "CONDITIONALS_NEGATION"},
	token.GTR:            {"CONDITIONALS_BOUNDARY", "CONDITIONALS_NEGATION"},
	token.INC:            {"INCREMENT_DECREMENT"},
	token.LAND:           {"INVERT_LOGICAL"},
	token.LEQ:            {"CONDITIONALS_BOUNDARY", "CONDITIONALS_NEGATION"},
	token.LOR:            {"INVERT_LOGICAL"},
	token.LSS:            {"CONDITIONALS_BOUNDARY", "CONDITIONALS_NEGATION"},
	token.MUL:            {"ARITHMETIC_BASE"},
	token.MUL_ASSIGN:     {"INVERT_ASSIGNMENTS", "REMOVE_SELF_ASSIGNMENTS"},
	token.NEQ:            {"CONDITIONALS_NEGATION"},
	token.OR:             {"INVERT_BITWISE"},
	token.OR_ASSIGN:      {"REMOVE_SELF_ASSIGNMENTS", "INVERT_BWASSIGN"},
	token.QUO:            {"ARITHMETIC_BASE"},
	token.QUO_ASSIGN:     {"INVERT_ASSIGNMENTS", "REMOVE_SELF_ASSIGNMENTS"},
	token.REM:            {"ARITHMETIC_BASE"},
	token.REM_ASSIGN:     {"INVERT_ASSIGNMENTS", "REMOVE_SELF_ASSIGNMENTS"},
	token.SHL:            {"INVERT_BITWISE"},
	token.SHL_ASSIGN:     {"REMOVE_SELF_ASSIGNMENTS", "INVERT_BWASSIGN"},
	token.SHR:            {"INVERT_BITWISE"},
	token.SHR_ASSIGN:     {"REMOVE_SELF_ASSIGNMENTS", "INVERT_BWASSIGN"},
	token.SUB:            {"INVERT_NEGATIVES", "ARITHMETIC_BASE"},
	token.SUB_ASSIGN:     {"INVERT_ASSIGNMENTS", "REMOVE_SELF_ASSIGNMENTS"},
	token.XOR:            {"INVERT_BITWISE"},
	token.XOR_ASSIGN:     {"REMOVE_SELF_ASSIGNMENTS", "INVERT_BWASSIGN"},
}

// MutatedLineHash computes the SHA-256 hex digest of the tokens on line after applying mutatorName at col.
func MutatedLineHash(src []byte, line, col int, mutatorName string) (string, error) {
	reps, ok := TokenReplacements[mutatorName]
	if !ok {
		return "", fmt.Errorf("unknown mutator %q", mutatorName)
	}
	parts, err := scanMutatedLine(src, line, col, reps, mutatorName)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, " ")))
	return hex.EncodeToString(sum[:]), nil
}

func scanMutatedLine(src []byte, line, col int, reps map[token.Token]token.Token, mutatorName string) ([]string, error) {
	fset := token.NewFileSet()
	tf := fset.AddFile("", fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(tf, src, nil, 0)
	var parts []string
	replaced := false
	for {
		part, didReplace, done, err := nextLineToken(fset, &s, line, col, reps, mutatorName)
		if err != nil {
			return nil, err
		}
		if done {
			break
		}
		if part != "" {
			replaced = replaced || didReplace
			parts = append(parts, part)
		}
	}
	if !replaced {
		return nil, fmt.Errorf("no %s token at %d:%d", mutatorName, line, col)
	}
	return parts, nil
}

func nextLineToken(fset *token.FileSet, s *scanner.Scanner, line, col int, reps map[token.Token]token.Token, mutatorName string) (string, bool, bool, error) {
	tpos, tok, lit := s.Scan()
	pos := fset.Position(tpos)
	if tok == token.EOF || pos.Line > line {
		return "", false, true, nil
	}
	if !keepLineToken(pos.Line, line, tok, lit) {
		return "", false, false, nil
	}
	part, didReplace, err := renderToken(tok, lit, pos.Column == col, reps, mutatorName)
	return part, didReplace, false, err
}

func keepLineToken(tokLine, targetLine int, tok token.Token, lit string) bool {
	return tokLine == targetLine && (tok != token.SEMICOLON || lit == ";")
}

func renderToken(tok token.Token, lit string, replace bool, reps map[token.Token]token.Token, mutatorName string) (string, bool, error) {
	if replace {
		rep, ok := reps[tok]
		if !ok {
			return "", false, fmt.Errorf("token %s cannot be mutated by %s", tok, mutatorName)
		}
		return rep.String(), true, nil
	}
	if lit != "" {
		return lit, false, nil
	}
	return tok.String(), false, nil
}

// CountMutantsOnLines counts mutants matching gremlins AST rules on selected lines.
func CountMutantsOnLines(src []byte, lines map[int]bool, enabled map[string]bool) (int, error) {
	if len(lines) == 0 {
		return 0, nil
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, 0)
	if err != nil {
		return 0, err
	}
	count := 0
	ast.Inspect(file, func(n ast.Node) bool {
		tok, pos, ok := mutableASTToken(n)
		if ok && lines[fset.Position(pos).Line] {
			count += countEnabledMutants(tok, enabled)
		}
		return true
	})
	return count, nil
}

func mutableASTToken(n ast.Node) (token.Token, token.Pos, bool) {
	switch x := n.(type) {
	case *ast.AssignStmt:
		return x.Tok, x.TokPos, true
	case *ast.BinaryExpr:
		return x.Op, x.OpPos, true
	case *ast.BranchStmt:
		return x.Tok, x.TokPos, true
	case *ast.IncDecStmt:
		return x.Tok, x.TokPos, true
	case *ast.UnaryExpr:
		return x.Op, x.OpPos, true
	default:
		return token.ILLEGAL, token.NoPos, false
	}
}

func countEnabledMutants(tok token.Token, enabled map[string]bool) int {
	total := 0
	for _, mt := range tokenMutantTypes[tok] {
		if enabled[mt] {
			total++
		}
	}
	return total
}

type declSpan struct {
	name      string
	startLine int
	startCol  int
	endLine   int
	endCol    int
}

func (s declSpan) contains(line, col int) bool {
	afterStart := line > s.startLine || (line == s.startLine && col >= s.startCol)
	beforeEnd := line < s.endLine || (line == s.endLine && col <= s.endCol)
	return afterStart && beforeEnd
}

// ModuleIndex holds source-position symbol mappings for non-generated production Go files.
type ModuleIndex struct {
	files map[string][]declSpan
	list  []string
	funcs []exempt.Func
}

// ScanModule parses all non-generated production Go files under dir for symbol resolution.
func ScanModule(dir string) (*ModuleIndex, error) {
	idx := &ModuleIndex{files: map[string][]declSpan{}}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		return idx.visitEntry(dir, p, d, walkErr)
	})
	if err != nil {
		return nil, err
	}
	return idx, nil
}

func (idx *ModuleIndex) visitEntry(root, p string, d fs.DirEntry, walkErr error) error {
	if walkErr != nil {
		return walkErr
	}
	if d.IsDir() {
		return skipExcludedDir(root, p, d.Name())
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)
	if !isProdGoFilename(rel) {
		return nil
	}
	return idx.indexFile(p, rel)
}

func skipExcludedDir(root, p, name string) error {
	if p == root {
		return nil
	}
	if isSkippedDirName(name) || hasNestedGoMod(p) {
		return fs.SkipDir
	}
	return nil
}

func isSkippedDirName(name string) bool {
	return name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

func hasNestedGoMod(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "go.mod"))
	return err == nil
}

func isProdGoFilename(rel string) bool {
	base := filepath.Base(rel)
	return strings.HasSuffix(base, ".go") && !strings.HasSuffix(base, "_test.go") && !strings.HasSuffix(base, ".gen.go")
}

func (idx *ModuleIndex) indexFile(path, rel string) error {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil || ast.IsGenerated(file) {
		return nil
	}
	idx.list = append(idx.list, rel)
	var spans []declSpan
	for _, decl := range file.Decls {
		spans = appendDeclSpans(spans, fset, decl, rel, &idx.funcs)
	}
	idx.files[rel] = spans
	return nil
}

func appendDeclSpans(spans []declSpan, fset *token.FileSet, decl ast.Decl, rel string, funcs *[]exempt.Func) []declSpan {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		recv := declRecvName(d)
		name := qualFuncName(recv, d.Name.Name)
		*funcs = append(*funcs, exempt.Func{File: rel, Recv: recv, Name: d.Name.Name})
		return append(spans, makeSpan(fset, name, d.Pos(), d.End()))
	case *ast.GenDecl:
		return appendGenSpans(spans, fset, d)
	default:
		return spans
	}
}

func appendGenSpans(spans []declSpan, fset *token.FileSet, d *ast.GenDecl) []declSpan {
	if d.Tok != token.VAR && d.Tok != token.CONST {
		return spans
	}
	for _, spec := range d.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok || len(vs.Names) == 0 {
			continue
		}
		spans = appendValueSpans(spans, fset, vs)
	}
	return spans
}

func appendValueSpans(spans []declSpan, fset *token.FileSet, vs *ast.ValueSpec) []declSpan {
	if len(vs.Values) == len(vs.Names) {
		for i, expr := range vs.Values {
			spans = append(spans, makeSpan(fset, vs.Names[i].Name, expr.Pos(), expr.End()))
		}
		return spans
	}
	return append(spans, makeSpan(fset, vs.Names[0].Name, vs.Pos(), vs.End()))
}

func makeSpan(fset *token.FileSet, name string, startPos, endPos token.Pos) declSpan {
	s, e := fset.Position(startPos), fset.Position(endPos)
	return declSpan{name: name, startLine: s.Line, startCol: s.Column, endLine: e.Line, endCol: e.Column}
}

func declRecvName(d *ast.FuncDecl) string {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return ""
	}
	return recvTypeName(d.Recv.List[0].Type)
}

func recvTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return recvTypeName(t.X)
	case *ast.IndexExpr:
		return recvTypeName(t.X)
	case *ast.IndexListExpr:
		return recvTypeName(t.X)
	case *ast.Ident:
		return t.Name
	default:
		return ""
	}
}

func qualFuncName(recv, name string) string {
	if recv == "" {
		return name
	}
	return recv + "." + name
}

// EnclosingFunc returns the Go declaration name containing line:col in rel, or "".
func (idx *ModuleIndex) EnclosingFunc(rel string, line, col int) string {
	for _, s := range idx.files[filepath.ToSlash(rel)] {
		if s.contains(line, col) {
			return s.name
		}
	}
	return ""
}

// Files returns the non-generated production Go file paths indexed in the module.
func (idx *ModuleIndex) Files() []string {
	return idx.list
}

// Funcs returns all functions and methods indexed in the module for exemption matching.
func (idx *ModuleIndex) Funcs() []exempt.Func {
	return idx.funcs
}
