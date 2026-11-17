package gotops

import (
	"encoding/json"
	"io/fs"
	"path"
	"regexp"
	"testing"
	"testing/fstest"
)

// The embedded files are the contract the binary validates against, so these
// tests pin what must be present and that the protocol id is one id everywhere.

func TestFilesServeSchemasProfilesAndTemplate(t *testing.T) {
	want := []string{
		"schema/core.schema.json",
		"schema/go-tops.schema.json",
		"schema/gate-report.schema.json",
		"profiles/binary-cli.yaml",
		"profiles/binary-service.yaml",
		"profiles/library-core.yaml",
		"config/AGENTS.template.md",
	}
	for _, name := range want {
		data, err := fs.ReadFile(Files(), name)
		if err != nil {
			t.Errorf("embedded file %s: %v", name, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("embedded file %s is empty", name)
		}
	}
}

func TestEmbeddedSchemasAreJSON(t *testing.T) {
	names, err := fs.Glob(Files(), "schema/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) < 3 {
		t.Fatalf("embedded schemas: got %v, want at least core, go-tops and gate-report", names)
	}
	for _, name := range names {
		data, err := fs.ReadFile(Files(), name)
		if err != nil {
			t.Fatal(err)
		}
		if !json.Valid(data) {
			t.Errorf("%s is not valid JSON", name)
		}
	}
}

var semver = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// One protocol id: the one Protocol reports is the constant in every embedded
// schema that has one and the protocol line of every embedded profile.
func TestProtocolIsOneIDAcrossTheEmbeddedFiles(t *testing.T) {
	got, err := Protocol()
	if err != nil {
		t.Fatal(err)
	}
	if !semver.MatchString(got) {
		t.Fatalf("Protocol() = %q, want MAJOR.MINOR.PATCH", got)
	}

	schemas, err := fs.Glob(Files(), "schema/*.json")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, name := range schemas {
		id, ok := schemaProtocol(t, name)
		if !ok {
			continue // core.schema.json carries shared definitions, no protocol
		}
		seen++
		if id != got {
			t.Errorf("%s protocol const %q, Protocol() %q", name, id, got)
		}
	}
	if seen < 2 {
		t.Errorf("only %d embedded schemas carry a protocol const, want the config and report schemas", seen)
	}

	profiles, err := fs.Glob(Files(), "profiles/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	line := regexp.MustCompile(`(?m)^protocol:\s*"?([0-9.]+)"?`)
	for _, name := range profiles {
		data, err := fs.ReadFile(Files(), name)
		if err != nil {
			t.Fatal(err)
		}
		m := line.FindSubmatch(data)
		if m == nil {
			t.Errorf("%s has no protocol line", name)
			continue
		}
		if string(m[1]) != got {
			t.Errorf("%s protocol %q, Protocol() %q", path.Base(name), m[1], got)
		}
	}
}

func schemaProtocol(t *testing.T, name string) (string, bool) {
	t.Helper()
	data, err := fs.ReadFile(Files(), name)
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Properties map[string]struct {
			Const string `json:"const"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	p, ok := s.Properties["protocol"]
	return p.Const, ok && p.Const != ""
}

func TestProtocolOfReportsEachFailure(t *testing.T) {
	const file = "schema/go-tops.schema.json"
	tests := []struct {
		name    string
		fsys    fs.FS
		want    string
		wantErr bool
	}{
		{"const present", fstest.MapFS{file: {Data: []byte(`{"properties":{"protocol":{"const":"9.8.7"}}}`)}}, "9.8.7", false},
		{"file missing", fstest.MapFS{}, "", true},
		{"not JSON", fstest.MapFS{file: {Data: []byte(`{`)}}, "", true},
		{"no protocol property", fstest.MapFS{file: {Data: []byte(`{"properties":{}}`)}}, "", true},
		{"empty const", fstest.MapFS{file: {Data: []byte(`{"properties":{"protocol":{"const":""}}}`)}}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := protocolOf(tt.fsys)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
