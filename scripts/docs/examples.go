package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

// An example is a fenced block preceded by
//
//	<!-- go-tops:example path=<file> schema=<file> -->
//
// on its own line (blank lines may sit between the comment and the fence).
// Both paths are relative to the repository root. The block must be
// byte-identical to the file, and the file must validate against the schema.
var (
	exampleMarkerRe = regexp.MustCompile(`^\s*<!--\s*go-tops:example\b(.*?)-->\s*$`)
	exampleAnyRe    = regexp.MustCompile(`<!--\s*go-tops:example\b`)
	attrRe          = regexp.MustCompile(`(\S+)=(\S+)`)
)

type example struct {
	Doc          *mdFile
	Line         int // line of the marker
	Path, Schema string
	Block        *fence
}

func checkExamples(r *repo) checkResult {
	res := checkResult{}
	var all []example
	for _, f := range r.Files {
		exs, fnd := findExamples(f)
		all = append(all, exs...)
		res.Findings = append(res.Findings, fnd...)
	}
	schemas := map[string]*jsonschema.Schema{}
	marked := map[string]map[int]bool{} // file -> opening line of each marked block
	for _, ex := range all {
		res.Findings = append(res.Findings, r.verifyExample(ex, schemas)...)
		if marked[ex.Doc.Rel] == nil {
			marked[ex.Doc.Rel] = map[int]bool{}
		}
		marked[ex.Doc.Rel][ex.Block.Open] = true
	}
	res.Findings = append(res.Findings, r.checkUnmarkedBlocks(marked)...)
	res.Summary = fmt.Sprintf("%d examples", len(all))
	if len(all) == 0 {
		res.Summary = "0 examples found, nothing was compared or validated"
	}
	res.Vacuous = len(all) == 0 && len(res.Findings) == 0
	return res
}

func findExamples(f *mdFile) ([]example, []finding) {
	var exs []example
	var out []finding
	f.prose(func(n int, line string) {
		m := exampleMarkerRe.FindStringSubmatch(line)
		if m == nil {
			if exampleAnyRe.MatchString(line) {
				out = append(out, finding{f.Rel, n, "malformed go-tops:example marker: it must be alone on its line and end with -->"})
			}
			return
		}
		attrs := map[string]string{}
		for _, a := range attrRe.FindAllStringSubmatch(m[1], -1) {
			attrs[a[1]] = a[2]
		}
		ex := example{Doc: f, Line: n, Path: attrs["path"], Schema: attrs["schema"]}
		if ex.Path == "" || ex.Schema == "" || len(attrs) != 2 {
			out = append(out, finding{f.Rel, n, "go-tops:example needs exactly path=<file> and schema=<file>"})
			return
		}
		if b := nextFence(f, n); b != nil {
			ex.Block = b
			exs = append(exs, ex)
		} else {
			out = append(out, finding{f.Rel, n, "go-tops:example marker is not followed by a fenced block"})
		}
	})
	return exs, out
}

// nextFence returns the fence that opens on the first non-blank line after
// line n, or nil.
func nextFence(f *mdFile, n int) *fence {
	for i := n; i < len(f.Lines); i++ {
		if strings.TrimSpace(f.Lines[i]) == "" {
			continue
		}
		for k := range f.Fences {
			if f.Fences[k].Open == i+1 {
				return &f.Fences[k]
			}
		}
		return nil
	}
	return nil
}

func (r *repo) verifyExample(ex example, schemas map[string]*jsonschema.Schema) []finding {
	at := func(format string, a ...any) []finding {
		return []finding{{ex.Doc.Rel, ex.Line, fmt.Sprintf(format, a...)}}
	}
	file, err := r.readInside(ex.Path)
	if err != nil {
		return at("example file %s: %v", ex.Path, err)
	}
	var out []finding
	want := string(file)
	got := strings.Join(ex.Block.Body, "\n")
	if len(ex.Block.Body) > 0 {
		got += "\n"
	}
	if got != want {
		out = append(out, at("block differs from %s: %s", ex.Path, firstDiff(got, want))...)
	}
	inst, err := decodeInstance(ex.Path, file)
	if err != nil {
		return append(out, at("%s: %v", ex.Path, err)...)
	}
	sch, err := r.schema(ex.Schema, schemas)
	if err != nil {
		return append(out, at("schema %s: %v", ex.Schema, err)...)
	}
	if err := sch.Validate(inst); err != nil {
		out = append(out, at("%s does not validate against %s:\n%v", ex.Path, ex.Schema, err)...)
	}
	return out
}

// readInside reads a root-relative file, refusing paths that leave the root.
func (r *repo) readInside(rel string) ([]byte, error) {
	clean := path.Clean(rel)
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return nil, fmt.Errorf("path leaves the repository")
	}
	if _, ok := r.existsExact(clean); !ok {
		return nil, fmt.Errorf("does not exist (case-sensitive)")
	}
	return os.ReadFile(filepath.Join(r.Root, filepath.FromSlash(clean)))
}

func (r *repo) schema(rel string, cache map[string]*jsonschema.Schema) (*jsonschema.Schema, error) {
	if s, ok := cache[rel]; ok {
		return s, nil
	}
	if _, err := r.readInside(rel); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(filepath.Join(r.Root, filepath.FromSlash(path.Clean(rel))))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	s, err := c.Compile("file://" + filepath.ToSlash(abs))
	if err != nil {
		return nil, err
	}
	cache[rel] = s
	return s, nil
}

// decodeInstance parses a .json or .yaml/.yml file into the value the schema
// validator expects. YAML goes through JSON so numbers take JSON's types.
func decodeInstance(name string, data []byte) (any, error) {
	switch strings.ToLower(path.Ext(name)) {
	case ".json":
		return jsonschema.UnmarshalJSON(bytes.NewReader(data))
	case ".yaml", ".yml":
		dec := yaml.NewDecoder(bytes.NewReader(data))
		var v any
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("invalid YAML: %v", err)
		}
		var extra any
		if dec.Decode(&extra) == nil {
			return nil, fmt.Errorf("more than one YAML document")
		}
		j, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("YAML is not representable as JSON: %v", err)
		}
		return jsonschema.UnmarshalJSON(bytes.NewReader(j))
	}
	return nil, fmt.Errorf("unsupported example extension (want .json, .yaml or .yml)")
}

// firstDiff describes where two texts first differ, by line.
func firstDiff(block, file string) string {
	a, b := strings.Split(block, "\n"), strings.Split(file, "\n")
	for i := 0; i < len(a) || i < len(b); i++ {
		switch {
		case i >= len(a):
			return fmt.Sprintf("the block ends at line %d, the file continues", i)
		case i >= len(b):
			return fmt.Sprintf("the file ends at line %d, the block continues", i)
		case a[i] != b[i]:
			return fmt.Sprintf("first difference at line %d: block %q, file %q", i+1, a[i], b[i])
		}
	}
	return "identical lines but different bytes"
}
