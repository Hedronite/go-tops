package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gotops "github.com/Hedronite/go-tops"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// validate checks a report document against the embedded
// schema/gate-report.schema.json, with its two $ref targets registered.
func validate(t *testing.T, doc []byte) error {
	t.Helper()
	c := jsonschema.NewCompiler()
	for _, name := range []string{"core.schema.json", "go-tops.schema.json", "gate-report.schema.json"} {
		b, err := gotops.FS.ReadFile("schema/" + name)
		if err != nil {
			t.Fatal(err)
		}
		v, err := jsonschema.UnmarshalJSON(strings.NewReader(string(b)))
		if err != nil {
			t.Fatal(err)
		}
		if err := c.AddResource("mem:///schema/"+name, v); err != nil {
			t.Fatal(err)
		}
	}
	sch, err := c.Compile("mem:///schema/gate-report.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	inst, err := jsonschema.UnmarshalJSON(strings.NewReader(string(doc)))
	if err != nil {
		t.Fatal(err)
	}
	return sch.Validate(inst)
}

func minimal() Report {
	return Report{
		Protocol: Protocol, ToolVersion: "0.0.0-dev", Module: "example.com/m", Class: "binary-cli",
		Enforce: "hard", Git: Git{Head: "abc1234", Base: "origin/main"}, MaxTier: 1,
		StartedAt: "2026-11-17T18:00:00Z",
		Summary:   Summary{Outcome: "pass", Enforced: true},
	}
}

// §12.3: the file written for a report with empty collections still carries
// every required array, so it validates.
func TestMarshalEmptyCollectionsValidate(t *testing.T) {
	b, err := Marshal(minimal())
	if err != nil {
		t.Fatal(err)
	}
	if err := validate(t, b); err != nil {
		t.Fatalf("minimal report invalid: %v\n%s", err, b)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if v, ok := m["vuln_db_timestamp"]; !ok || v != nil {
		t.Errorf("vuln_db_timestamp = %v, present %v; want an explicit null when the layer did not run", v, ok)
	}
}

// The oracle is the protocol's own example: the typed report must read and
// rewrite config/gate-report.example.json into a document that still validates.
func TestExampleReportRoundTrips(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "config", "gate-report.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validate(t, b); err != nil {
		t.Fatalf("the example report does not validate against its schema: %v", err)
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	var r Report
	if err := dec.Decode(&r); err != nil {
		t.Fatalf("the typed report cannot hold the example (a field is missing): %v", err)
	}
	out, err := Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := validate(t, out); err != nil {
		t.Fatalf("rewritten example invalid: %v", err)
	}
}

// A schema-invalid report is a failure, never a shipped file: the validator
// the tests rely on must reject one.
func TestValidatorRejectsInvalidReport(t *testing.T) {
	r := minimal()
	r.Summary.Outcome = "passed"
	b, err := Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if validate(t, b) == nil {
		t.Fatal("an outcome outside the enum validated")
	}
	r = minimal()
	r.Layers = []Layer{{ID: "tests", Tier: 1, Outcome: "pass"}} // details are required once a layer ran
	b, _ = Marshal(r)
	if validate(t, b) == nil {
		t.Fatal("a tests layer that ran with no details validated")
	}
}

func TestWriteCreatesDirAndFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".go-tops", "report.json")
	if err := Write(p, minimal()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := validate(t, b); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(b), "\n") {
		t.Error("no trailing newline")
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Errorf("%d files left in the report directory, want 1 (no temp file)", len(entries))
	}
}

func TestWriteReplacesAnExistingReport(t *testing.T) {
	p := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(p, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(p, minimal()); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), "stale") {
		t.Error("old content survived")
	}
}

func TestWriteFailsWhereItCannotWrite(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(filepath.Join(blocker, "report.json"), minimal()); err == nil {
		t.Fatal("writing under a regular file succeeded")
	}
}

// A failed layer that has no figures says why with details {error: ...}.
func TestFailedLayerMayCarryAnError(t *testing.T) {
	r := minimal()
	r.Layers = []Layer{{ID: "tests", Tier: 1, Outcome: "fail", Details: map[string]any{"error": "go test printed no JSON"}}}
	b, err := Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := validate(t, b); err != nil {
		t.Fatalf("a failed tests layer with details.error is invalid: %v", err)
	}
	r.Layers[0].Details = map[string]any{"error": ""}
	b, _ = Marshal(r)
	if validate(t, b) == nil {
		t.Fatal("an empty error validated")
	}
}
