package baseline

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Hedronite/go-tops/internal/acks"
)

// nolintAll is the name a bare //nolint counts under: it silences every linter.
const nolintAll = "all"


// skipDir says whether the walk leaves a directory out. It is the scope of the
// CRAP layer (internal/crap): hidden and underscore directories, vendored code,
// test fixtures and nested modules are not the module's own code.
func skipDir(root, p, name string) bool {
	if p == root {
		return false
	}
	if name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return true
	}
	_, err := os.Stat(filepath.Join(p, "go.mod"))
	return err == nil
}

// walkGo calls fn with the module-relative slash path of every .go file under
// dir that the module owns.
func walkGo(dir string, fn func(rel, abs string) error) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDir(dir, p, d.Name()) {
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

// tally counts suppressions as they are read.
type tally struct{ nolint, ack Counts }

func bump(c *Counts, name, file string) {
	if *c == nil {
		*c = Counts{}
	}
	if (*c)[name] == nil {
		(*c)[name] = map[string]int{}
	}
	(*c)[name][file]++
}

// scanSuppressions counts the suppressions of the module (§6.7): //nolint
// comments per linter and file, and //tops:ack comments and acks.yaml entries
// per law and file. A comment that names two linters counts once for each. A
// file that does not parse cannot be shown to hold no suppression, so it is an
// error and not a zero.
func scanSuppressions(dir, ackPath string) (nolint, ack Counts, err error) {
	var t tally
	if err := walkGo(dir, t.scanFile); err != nil {
		return nil, nil, err
	}
	if err := t.readAcks(dir, ackPath); err != nil {
		return nil, nil, err
	}
	return t.nolint, t.ack, nil
}

// scanFile counts the suppression comments of one source file.
func (t *tally) scanFile(rel, abs string) error {
	f, err := parser.ParseFile(token.NewFileSet(), abs, nil, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("cannot read the suppressions of %s: %w", rel, err)
	}
	for _, g := range f.Comments {
		for _, c := range g.List {
			t.comment(rel, c.Text)
		}
	}
	return nil
}

// comment counts one comment: a line comment that is a //nolint directive or a
// //tops:ack, and nothing else.
func (t *tally) comment(rel, raw string) {
	text, ok := strings.CutPrefix(raw, "//")
	if !ok {
		return
	}
	text = strings.TrimLeft(text, " \t")
	if names, ok := nolintNames(text); ok {
		for _, n := range names {
			bump(&t.nolint, n, rel)
		}
	}
	if law, _, ok := acks.Comment("//" + text); ok {
		bump(&t.ack, law, rel)
	}
}

// readAcks counts the entries of acks.yaml, if the file is there.
func (t *tally) readAcks(dir, ackPath string) error {
	if ackPath == "" {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(ackPath)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	doc, err := acks.ParseYAML(ackPath, raw)
	if err != nil {
		return err
	}
	for _, a := range doc.Acks {
		bump(&t.ack, a.Law, filepath.ToSlash(filepath.Clean(a.File)))
	}
	return nil
}

// ScanSuppressions counts //nolint and //tops:ack in the module's Go sources.
func ScanSuppressions(dir, acksPath string) (nolint, ack Counts, err error) {
	return scanSuppressions(dir, acksPath)
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
	return linterList(list), true
}

// linterList is the comma-separated linters of a directive, up to the first
// space. A list with no name silences every linter, as a bare directive does.
func linterList(list string) []string {
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
		return []string{nolintAll}
	}
	return out
}
