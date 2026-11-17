package config_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	gotops "github.com/Hedronite/go-tops"
	"github.com/Hedronite/go-tops/internal/config"
)

// FuzzLoad throws bytes at parse and validate. Whatever the input, Load returns
// either a config or issues, never both and never neither, never panics, and
// every issue names a rule and a well-formed pointer.
func FuzzLoad(f *testing.F) {
	example, err := os.ReadFile(filepath.Join("..", "..", "config", "go-tops.example.yaml"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(example)
	profiles, err := fs.Glob(gotops.Files(), "profiles/*.yaml")
	if err != nil {
		f.Fatal(err)
	}
	for _, name := range profiles {
		data, err := fs.ReadFile(gotops.Files(), name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	for _, s := range []string{
		"", "# only a comment\n", "- a\n", "just text", "{}", "null", "~", "---\n---\n",
		"a: &x 1\nb: *x\n", "<<: {a: 1}\n", "1: a\n", "a: 1\na: 2\n", "a: .nan\n", "a: 2027-03-01\n",
		"gates:\n  crap: {enabled: false}\n", "gates: [[[[[[[[[[[[]]]]]]]]]]]]\n",
		"protocol: \"0\"\nclass: binary-cli\nexemptions: [{path: a, layer: crap}]\n",
		"laws: {required: [G-1, G-02, G-02]}\n", "\xff\xfe", "a: \"\\u0000\"\n",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		cfg, issues, err := config.Load(data)
		if err != nil {
			t.Fatalf("Load returned an environment error for input: %v", err)
		}
		if (cfg == nil) == (len(issues) == 0) {
			t.Fatalf("config %v with %d issues: want exactly one of the two", cfg != nil, len(issues))
		}
		for _, is := range issues {
			if is.Rule == "" || is.Msg == "" || is.File != "go-tops.yaml" {
				t.Fatalf("malformed issue %+v", is)
			}
			if is.Pointer != "" && !strings.HasPrefix(is.Pointer, "/") {
				t.Fatalf("pointer %q is not an RFC 6901 pointer", is.Pointer)
			}
		}
		again, againIssues, againErr := config.Load(data)
		if againErr != nil {
			t.Fatalf("second Load: %v", againErr)
		}
		if !reflect.DeepEqual(cfg, again) || !reflect.DeepEqual(issues, againIssues) {
			t.Fatal("Load is not deterministic")
		}
	})
}
