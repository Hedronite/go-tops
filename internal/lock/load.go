// Package lock reads go-tops.lock (GO_TOPS.md §10): the Go toolchain and the
// downloaded binaries the gate depends on, each pinned by an exact version and
// a SHA-256 per platform. The file is YAML and schema/lock.schema.json says
// what it may hold. Module tools are pinned in tools/go.mod, not here.
//
// This file loads and validates the lock. sha.go checks a binary against it.
package lock

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"

	gotops "github.com/Hedronite/go-tops"
)

// FileName is the lock file at the module root.
const FileName = "go-tops.lock"

// Lock is the parsed go-tops.lock.
type Lock struct {
	Protocol  string   `yaml:"protocol" json:"protocol"`
	Toolchain string   `yaml:"toolchain" json:"toolchain"`
	Trust     Trust    `yaml:"trust" json:"trust"`
	Binaries  []Binary `yaml:"binaries" json:"binaries"`
}

// Trust says how the SHA-256 values were obtained.
type Trust struct {
	Mode string `yaml:"mode" json:"mode"` // checksum or tofu
	Note string `yaml:"note" json:"note"`
}

// Binary pins one downloaded tool.
type Binary struct {
	Name    string `yaml:"name" json:"name"`
	Version string `yaml:"version" json:"version"` // exact, no leading v
	URL     string `yaml:"url" json:"url"`         // https template: {version}, {os}, {arch}
	// Extract is where the executable sits inside a downloaded archive. Empty
	// when the download is the executable. SHA256 is then the hash of the
	// archive, not of the executable.
	Extract string `yaml:"extract,omitempty" json:"extract,omitempty"`
	// SHA256 holds the hash of the downloaded file per platform ("darwin/arm64").
	SHA256 map[string]string `yaml:"sha256" json:"sha256"`
}

// Error is a lock that does not load: the file is missing, is not valid YAML
// for the lock, or breaks the schema or a rule the schema cannot state. Path is
// the file; Problems lists what is wrong, one per line, each with the JSON
// pointer it is at where there is one.
type Error struct {
	Path     string
	Problems []string
	Err      error // set when the cause is not a content problem, such as a missing file
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Path, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Path, strings.Join(e.Problems, "; "))
}

// Unwrap exposes a missing file to errors.Is(err, fs.ErrNotExist).
func (e *Error) Unwrap() error { return e.Err }

// Load reads and validates the lock file at path.
func Load(path string) (*Lock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &Error{Path: path, Err: err}
	}
	l, probs := parse(data)
	if len(probs) > 0 {
		return nil, &Error{Path: path, Problems: probs}
	}
	return l, nil
}

// LoadDir reads go-tops.lock at the root of dir.
func LoadDir(dir string) (*Lock, error) { return Load(filepath.Join(dir, FileName)) }

// Parse validates lock text. It is Load without the file.
func Parse(data []byte) (*Lock, error) {
	l, probs := parse(data)
	if len(probs) > 0 {
		return nil, &Error{Path: FileName, Problems: probs}
	}
	return l, nil
}

func parse(data []byte) (*Lock, []string) {
	var l Lock
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // an unknown key at any depth is an error
	if err := dec.Decode(&l); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, []string{"the file is empty"}
		}
		var lines []string
		for _, ln := range strings.Split(err.Error(), "\n") {
			if ln = strings.TrimSpace(ln); ln != "" && !strings.HasSuffix(ln, "unmarshal errors:") {
				lines = append(lines, ln)
			}
		}
		if len(lines) == 0 {
			lines = []string{err.Error()}
		}
		return nil, []string{"not a valid lock: " + strings.Join(lines, "; ")}
	}
	if probs := validate(&l); len(probs) > 0 {
		return nil, probs
	}
	if probs := rules(&l); len(probs) > 0 {
		return nil, probs
	}
	return &l, nil
}

var atLine = regexp.MustCompile(`- at '([^']*)': (.*)`)

// validate checks the lock against the embedded schema/lock.schema.json.
func validate(l *Lock) []string {
	raw, err := fs.ReadFile(gotops.Files(), "schema/lock.schema.json")
	if err != nil {
		return []string{"cannot read the embedded lock schema: " + err.Error()}
	}
	sch, err := compile(raw)
	if err != nil {
		return []string{"cannot compile the embedded lock schema: " + err.Error()}
	}
	doc, err := json.Marshal(l)
	if err != nil {
		return []string{err.Error()}
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(doc))
	if err != nil {
		return []string{err.Error()}
	}
	verr := sch.Validate(inst)
	if verr == nil {
		return nil
	}
	var probs []string
	for _, line := range strings.Split(verr.Error(), "\n") {
		if m := atLine.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			at := m[1]
			if at == "" {
				at = "/"
			}
			probs = append(probs, at+": "+m[2])
		}
	}
	if len(probs) == 0 {
		probs = []string{firstLine(verr.Error())}
	}
	return probs
}

func compile(raw []byte) (*jsonschema.Schema, error) {
	c := jsonschema.NewCompiler()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	const url = "mem:///schema/lock.schema.json"
	if err := c.AddResource(url, doc); err != nil {
		return nil, err
	}
	return c.Compile(url)
}

// rules are the checks the schema cannot state.
func rules(l *Lock) []string {
	var probs []string
	first := map[string]int{}
	for i, b := range l.Binaries {
		if j, dup := first[b.Name]; dup {
			probs = append(probs, fmt.Sprintf("/binaries/%d/name: %q repeats /binaries/%d; a binary is pinned once", i, b.Name, j))
			continue
		}
		first[b.Name] = i
	}
	return probs
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
