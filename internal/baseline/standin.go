package baseline

// STAND-IN. Function identity (§6.7) is defined once, in internal/crap, by G1.
// Until that lands this file gives `baseline --rename` something to ask whether
// a key exists. It is deleted when G1 merges: the Inventory then comes from
// internal/crap, and nothing here is kept.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"sort"
	"strconv"
	"strings"
)

// SourceInventory is the set of function and package keys found in the
// module's production files.
type SourceInventory struct{ fns, pkgs map[string]bool }

// Has implements Inventory.
func (s *SourceInventory) Has(k Kind, key string) bool {
	if k == KindPackage {
		return s.pkgs[key]
	}
	return s.fns[key]
}

type standinFn struct{ base, file string }

// ScanInventory parses the production files under dir. A key is the import
// path, the receiver type if any, and the name; a key that repeats in a package
// adds the file's name, and one that still repeats in a file adds its source
// order.
func ScanInventory(dir, module string) (*SourceInventory, error) {
	inv := &SourceInventory{fns: map[string]bool{}, pkgs: map[string]bool{}}
	var found []standinFn
	err := walkGo(dir, func(rel, abs string) error {
		if strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), abs, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		pkg := module
		if d := path.Dir(rel); d != "." {
			pkg = module + "/" + d
		}
		inv.pkgs[pkg] = true
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			base := pkg + "."
			if fd.Recv != nil && len(fd.Recv.List) == 1 {
				base += recvName(fd.Recv.List[0].Type) + "."
			}
			found = append(found, standinFn{base + fd.Name.Name, rel})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	perBase := map[string][]standinFn{}
	for _, fn := range found {
		perBase[fn.base] = append(perBase[fn.base], fn)
	}
	for base, list := range perBase {
		if len(list) == 1 {
			inv.fns[base] = true
			continue
		}
		sort.SliceStable(list, func(i, j int) bool { return list[i].file < list[j].file })
		seen := map[string]int{}
		for _, fn := range list {
			key := base + "@" + path.Base(fn.file)
			seen[key]++
			if seen[key] > 1 {
				key += "#" + strconv.Itoa(seen[key])
			}
			inv.fns[key] = true
		}
	}
	return inv, nil
}

func recvName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return recvName(t.X)
	case *ast.IndexExpr:
		return recvName(t.X)
	case *ast.IndexListExpr:
		return recvName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return "?"
}
