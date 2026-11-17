package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"

	gotops "github.com/Hedronite/go-tops"
)

// The embedded schemas carry no $id, so they are registered under a made-up
// base and their relative $refs (core.schema.json#/$defs/...) resolve inside it.
const schemaBase = "file:///schema/"

// Schema names, as check uses them. A name is a schema file or a definition in
// core.schema.json.
const (
	schemaConfig    = "go-tops.schema.json"
	schemaLock      = "lock.schema.json"
	schemaSurvivors = "core.schema.json#/$defs/survivorsFile"
	schemaAcks      = "core.schema.json#/$defs/acksFile"
	schemaBaseline  = "core.schema.json#/$defs/baseline"
)

type compiledSchemas struct {
	once    sync.Once
	byName  map[string]*jsonschema.Schema
	compile func(name string) (*jsonschema.Schema, error)
	mu      sync.Mutex
}

var schemas = newSchemas()

func newSchemas() *compiledSchemas {
	s := &compiledSchemas{byName: map[string]*jsonschema.Schema{}}
	var (
		c       *jsonschema.Compiler
		initErr error
	)
	s.compile = func(name string) (*jsonschema.Schema, error) {
		s.once.Do(func() { c, initErr = newCompiler() })
		if initErr != nil {
			return nil, initErr
		}
		return c.Compile(schemaBase + name)
	}
	return s
}

func newCompiler() (*jsonschema.Compiler, error) {
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	names, err := fs.Glob(gotops.Files(), "schema/*.json")
	if err != nil {
		return nil, err
	}
	for _, path := range names {
		data, err := fs.ReadFile(gotops.Files(), path)
		if err != nil {
			return nil, err
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("embedded %s: %w", path, err)
		}
		if err := c.AddResource(schemaBase+path[len("schema/"):], doc); err != nil {
			return nil, fmt.Errorf("embedded %s: %w", path, err)
		}
	}
	return c, nil
}

func (s *compiledSchemas) get(name string) (*jsonschema.Schema, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sch, ok := s.byName[name]; ok {
		return sch, nil
	}
	sch, err := s.compile(name)
	if err != nil {
		return nil, fmt.Errorf("compile embedded schema %s: %w", name, err)
	}
	s.byName[name] = sch
	return sch, nil
}

// validate checks a parsed value against a named schema and returns one Issue
// per violated rule, each at the pointer of the value it concerns. The error is
// non-nil only when the embedded schema itself cannot be compiled.
func validate(name, file string, v any) ([]Issue, error) {
	sch, err := schemas.get(name)
	if err != nil {
		return nil, err
	}
	verr := sch.Validate(v)
	if verr == nil {
		return nil, nil
	}
	var ve *jsonschema.ValidationError
	if !errors.As(verr, &ve) {
		return []Issue{{File: file, Rule: "schema", Msg: verr.Error()}}, nil
	}
	var out []Issue
	collect(ve.DetailedOutput(), file, &out)
	return sortIssues(out), nil
}

// collect walks the validator's error tree and keeps the leaves: the rules that
// actually failed. An unknown key is reported at the key, not at its object.
func collect(u *jsonschema.OutputUnit, file string, out *[]Issue) {
	if len(u.Errors) == 0 && u.Error != nil {
		if ap, ok := u.Error.Kind.(*kind.AdditionalProperties); ok {
			for _, p := range ap.Properties {
				*out = append(*out, Issue{File: file, Rule: "schema", Pointer: u.InstanceLocation + "/" + escape(p), Msg: fmt.Sprintf("unknown key %q", p)})
			}
			return
		}
		*out = append(*out, Issue{File: file, Rule: "schema", Pointer: u.InstanceLocation, Msg: u.Error.String()})
		return
	}
	for i := range u.Errors {
		collect(&u.Errors[i], file, out)
	}
}

// parseJSON decodes one JSON document into the value the schema validator takes.
// The string is why it could not, or "".
func parseJSON(data []byte) (any, string) {
	if len(data) > maxBytes {
		return nil, fmt.Sprintf("larger than %d bytes", maxBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, "the file is empty"
		}
		return nil, "invalid JSON: " + err.Error()
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, "more than one JSON value, or text after the first"
	}
	return v, ""
}
