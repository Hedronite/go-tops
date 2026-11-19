package gotops

import (
	"bytes"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// The skills live in .agents/skills. The harnesses read them from .claude/skills
// and .cursor/skills, which hold byte-identical copies. These tests need no
// Nushell: they run in every plain go test.

var skillNames = []string{"go-tops-adopt", "go-tops-gate", "go-tops-review"}

var skillMirrors = []string{".claude/skills", ".cursor/skills"}

// treeDiffs lists how b differs from a: a file only in one tree, or in both with
// other bytes. An empty list means the trees are identical.
func treeDiffs(a, b fs.FS) ([]string, error) {
	var diffs []string
	seen := map[string]bool{}
	err := fs.WalkDir(a, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		seen[p] = true
		want, err := fs.ReadFile(a, p)
		if err != nil {
			return err
		}
		got, err := fs.ReadFile(b, p)
		switch {
		case err != nil:
			diffs = append(diffs, p+": missing from the copy")
		case !bytes.Equal(want, got):
			diffs = append(diffs, p+": the bytes differ")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	err = fs.WalkDir(b, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !seen[p] {
			diffs = append(diffs, p+": only in the copy")
		}
		return err
	})
	return diffs, err
}

func TestSkillMirrorsAreByteIdentical(t *testing.T) {
	src := os.DirFS(".agents/skills")
	entries, err := fs.ReadDir(src, ".")
	if err != nil {
		t.Fatal(err)
	}
	var have []string
	for _, e := range entries {
		have = append(have, e.Name())
	}
	if !slices.Equal(have, skillNames) {
		t.Fatalf(".agents/skills holds %v, want %v", have, skillNames)
	}
	for _, mirror := range skillMirrors {
		diffs, err := treeDiffs(src, os.DirFS(mirror))
		if err != nil {
			t.Fatalf("%s: %v", mirror, err)
		}
		if len(diffs) != 0 {
			t.Errorf("%s differs from .agents/skills:\n  %s", mirror, strings.Join(diffs, "\n  "))
		}
	}
}

func TestTreeDiffsSeesAChangedByteAMissingFileAndAnExtraOne(t *testing.T) {
	base := fstest.MapFS{
		"s/SKILL.md": {Data: []byte("name: s\n")},
		"s/s.nu":     {Data: []byte("def main [] {}\n")},
	}
	for _, c := range []struct {
		name string
		copy fstest.MapFS
		want string
	}{
		{"identical", fstest.MapFS{"s/SKILL.md": base["s/SKILL.md"], "s/s.nu": base["s/s.nu"]}, ""},
		{"a changed byte", fstest.MapFS{"s/SKILL.md": base["s/SKILL.md"], "s/s.nu": {Data: []byte("def main [] {} \n")}}, "s/s.nu: the bytes differ"},
		{"a missing file", fstest.MapFS{"s/SKILL.md": base["s/SKILL.md"]}, "s/s.nu: missing from the copy"},
		{"an extra file", fstest.MapFS{"s/SKILL.md": base["s/SKILL.md"], "s/s.nu": base["s/s.nu"], "s/x.nu": {Data: []byte("x")}}, "s/x.nu: only in the copy"},
	} {
		t.Run(c.name, func(t *testing.T) {
			diffs, err := treeDiffs(base, c.copy)
			if err != nil {
				t.Fatal(err)
			}
			if c.want == "" && len(diffs) != 0 || c.want != "" && !slices.Equal(diffs, []string{c.want}) {
				t.Errorf("diffs = %v, want %q", diffs, c.want)
			}
		})
	}
}

var frontmatter = regexp.MustCompile(`(?s)\A---\nname: (\S+)\ndescription: ([^\n]+)\n---\n`)

// A skill is used from its SKILL.md alone: it names the script, says Nushell is
// required, shows the command, and says what a missing nu means.
func TestEverySkillStatesHowToRunItAndWhatAMissingNuMeans(t *testing.T) {
	for _, name := range skillNames {
		t.Run(name, func(t *testing.T) {
			b, err := os.ReadFile(path.Join(".agents/skills", name, "SKILL.md"))
			if err != nil {
				t.Fatal(err)
			}
			text := string(b)
			m := frontmatter.FindStringSubmatch(text)
			if m == nil {
				t.Fatal("SKILL.md has no name and description frontmatter")
			}
			if m[1] != name {
				t.Errorf("the frontmatter name is %q, want %q", m[1], name)
			}
			for _, want := range []string{
				"Nushell (`nu`) is required",
				"nu " + name + ".nu",
				"`nu: command not found`",
				`{"ok":false,"command":"nu ` + name + `.nu","error":"nu: command not found"}`,
			} {
				if !strings.Contains(text, want) {
					t.Errorf("SKILL.md lacks %q", want)
				}
			}
			if _, err := os.Stat(path.Join(".agents/skills", name, name+".nu")); err != nil {
				t.Errorf("the script is missing: %v", err)
			}
		})
	}
}
