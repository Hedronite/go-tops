package baseline

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// nolintAll is the name a bare //nolint counts under: it silences every linter.
const nolintAll = "all"

var ackRE = regexp.MustCompile(`^tops:ack\s+(G-[0-9]{2})(\s|$)`)

// skipDir says whether the walk leaves a directory out: a hidden one, vendored
// code, and test fixtures are not the module's own code.
func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata"
}

// walkGo calls fn with the module-relative slash path of every .go file under
// dir that the module owns.
func walkGo(dir string, fn func(rel, abs string) error) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != dir && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		return fn(filepath.ToSlash(rel), p)
	})
}

// scanSuppressions counts the suppressions of the module (§6.7): //nolint
// comments per linter and file, and //tops:ack comments and acks.yaml entries
// per law and file. A comment that names two linters counts once for each. A
// file that does not parse cannot be shown to hold no suppression, so it is an
// error and not a zero.
func scanSuppressions(dir, acks string) (nolint, ack Counts, err error) {
	bump := func(c *Counts, name, file string) {
		if *c == nil {
			*c = Counts{}
		}
		if (*c)[name] == nil {
			(*c)[name] = map[string]int{}
		}
		(*c)[name][file]++
	}
	err = walkGo(dir, func(rel, abs string) error {
		f, perr := parser.ParseFile(token.NewFileSet(), abs, nil, parser.ParseComments|parser.SkipObjectResolution)
		if perr != nil {
			return fmt.Errorf("cannot read the suppressions of %s: %w", rel, perr)
		}
		for _, g := range f.Comments {
			for _, c := range g.List {
				text, ok := strings.CutPrefix(c.Text, "//")
				if !ok {
					continue
				}
				text = strings.TrimLeft(text, " \t")
				if names, ok := nolintNames(text); ok {
					for _, n := range names {
						bump(&nolint, n, rel)
					}
				}
				if m := ackRE.FindStringSubmatch(text); m != nil {
					bump(&ack, m[1], rel)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if acks == "" {
		return nolint, ack, nil
	}
	raw, rerr := os.ReadFile(filepath.Join(dir, filepath.FromSlash(acks)))
	if errors.Is(rerr, fs.ErrNotExist) {
		return nolint, ack, nil
	}
	if rerr != nil {
		return nil, nil, rerr
	}
	var doc struct {
		Acks []struct {
			Law  string `yaml:"law"`
			File string `yaml:"file"`
		} `yaml:"acks"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", acks, err)
	}
	for _, a := range doc.Acks {
		bump(&ack, a.Law, filepath.ToSlash(filepath.Clean(a.File)))
	}
	return nolint, ack, nil
}

// nolintNames reads the text of a comment, after its slashes: `nolint`,
// `nolint:a,b` or `nolint:a // why`. It returns the linters named, or "all" for
// a bare one, and false for any other comment, including `nolintlint`.
func nolintNames(text string) ([]string, bool) {
	rest, ok := strings.CutPrefix(text, "nolint")
	if !ok {
		return nil, false
	}
	if rest == "" || rest[0] == ' ' || rest[0] == '\t' {
		return []string{nolintAll}, true
	}
	list, ok := strings.CutPrefix(rest, ":")
	if !ok {
		return nil, false
	}
	if i := strings.IndexAny(list, " \t"); i >= 0 {
		list = list[:i]
	}
	var out []string
	for _, n := range strings.Split(list, ",") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return []string{nolintAll}, true
	}
	return out, true
}
