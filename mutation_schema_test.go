package gotops_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/config"
	"github.com/Hedronite/go-tops/internal/report"
)

// What protocol 0.4.6 fixes about the mutation layer in the schemas: the details
// and survivor rows of the report (GO_TOPS.md §12.3) and the one engine with an
// adapter (§6.4). Each case changes the published example in one way.

type object = map[string]any

func exampleReport(t *testing.T) (doc, details, row object) {
	t.Helper()
	data, err := os.ReadFile("config/gate-report.example.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	layers, ok := doc["layers"].([]any)
	if !ok {
		t.Fatal("the example has no layers")
	}
	for _, l := range layers {
		layer, ok := l.(object)
		if !ok || layer["id"] != "mutation" {
			continue
		}
		details, ok = layer["details"].(object)
		if !ok {
			t.Fatal("the mutation layer has no details")
		}
		rows, ok := details["survivors"].([]any)
		if !ok || len(rows) == 0 {
			t.Fatal("the example lists no survivor row")
		}
		row, ok = rows[0].(object)
		if !ok {
			t.Fatal("the survivor row is not an object")
		}
		return doc, details, row
	}
	t.Fatal("the example has no mutation layer")
	return nil, nil, nil
}

func loads(t *testing.T, doc object) error {
	t.Helper()
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = report.Load(path)
	return err
}

func TestTheReportSchemaHoldsTheMutationDetails(t *testing.T) {
	key := "a.go:Server.handle:INVERT_LOGICAL:" + strings.Repeat("0", 64)
	type change func(doc, details, row object)
	drop := func(m string) change { return func(_, d, _ object) { delete(d, m) } }
	dropRow := func(m string) change { return func(_, _, r object) { delete(r, m) } }
	setRow := func(m string, v any) change { return func(_, _, r object) { r[m] = v } }
	set := func(m string, v any) change { return func(_, d, _ object) { d[m] = v } }
	valid := map[string]change{
		"the example":                     func(object, object, object) {},
		"a survivor nothing explains":     setRow("explained", nil),
		"no survivors, empty lists":       set("survivors", []any{}),
		"a key listed twice as timed out": set("timed_out_keys", []any{key, key}),
		"an exempt survivor":              set("exempt", []any{key}),
	}
	invalid := map[string]change{
		"no base":                    drop("base"),
		"no base_sha":                drop("base_sha"),
		"a short base_sha":           set("base_sha", "9f3c1e0"),
		"no not_measured":            drop("not_measured"),
		"no survivors":               drop("survivors"),
		"survivors null":             set("survivors", nil),
		"exempt null":                set("exempt", nil),
		"an exempt item not a key":   set("exempt", []any{"internal/legacy"}),
		"a timed-out item not a key": set("timed_out_keys", []any{"a.go:F"}),
		"a row without explained":    dropRow("explained"),
		"a row without its line":     dropRow("line"),
		"a row without its key":      dropRow("key"),
		"explained by an exemption":  setRow("explained", "exemption"),
		"a killed row":               setRow("status", "KILLED"),
		"line zero":                  setRow("line", 0),
		"a short line hash":          setRow("line_hash", "abc"),
		"an unknown row field":       setRow("column", 3),
		"an unknown detail":          set("skipped", 2),
	}
	for name, c := range valid {
		doc, details, row := exampleReport(t)
		c(doc, details, row)
		if err := loads(t, doc); err != nil {
			t.Errorf("%s is refused: %v", name, err)
		}
	}
	for name, c := range invalid {
		doc, details, row := exampleReport(t)
		c(doc, details, row)
		if err := loads(t, doc); err == nil {
			t.Errorf("%s is accepted", name)
		}
	}
}

func TestOnlyAnEngineWithAnAdapterIsConfigurable(t *testing.T) {
	data, err := os.ReadFile("config/go-tops.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "engine: overlay ") {
		t.Fatal("the example config names no engine")
	}
	for engine, ok := range map[string]bool{"overlay": true, "gremlins": false, "go-mutesting": false, "mutest": false} {
		_, issues, err := config.Load([]byte(strings.Replace(string(data), "engine: overlay ", "engine: "+engine+" ", 1)))
		if err != nil {
			t.Fatal(err)
		}
		if got := len(issues) == 0; got != ok {
			t.Errorf("engine %s: accepted = %v, want %v (%v)", engine, got, ok, issues)
		}
	}
}
