package baseline_test

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	gotops "github.com/Hedronite/go-tops"
	"github.com/Hedronite/go-tops/internal/baseline"
)

// GO_TOPS.md §6.7: .go-tops/baseline.json is written with keys sorted and one
// entry per line so that parallel branches merge without conflict. Its shape is
// core.schema.json#/$defs/baseline.

// testBuild is the build a ledger records (ruling C3).
var testBuild = baseline.Build{GOOS: "linux", GOARCH: "amd64", Tags: []string{"race"}, CGOEnabled: true}

// buildDef is the build object of ruling C3 as the ledger writes it: goos,
// goarch, tags and cgo_enabled, all required. Until the 0.4.3 schema (PR 26)
// carries it, validate adds it to the embedded definition.
const buildDef = `{"type":"object","additionalProperties":false,"required":["goos","goarch","tags","cgo_enabled"],
 "properties":{"goos":{"type":"string","minLength":1},"goarch":{"type":"string","minLength":1},
 "tags":{"type":"array","items":{"type":"string","minLength":1}},"cgo_enabled":{"type":"boolean"}}}`

// validate checks data against the embedded baseline definition.
func validate(t *testing.T, data []byte) error {
	t.Helper()
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	names, err := fs.Glob(gotops.Files(), "schema/*.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		raw, err := fs.ReadFile(gotops.Files(), name)
		if err != nil {
			t.Fatal(err)
		}
		if name == "schema/core.schema.json" && !bytes.Contains(raw, []byte(`"build"`)) {
			raw = withBuild(t, raw)
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		if err := c.AddResource("file:///"+name, doc); err != nil {
			t.Fatal(err)
		}
	}
	sch, err := c.Compile("file:///schema/core.schema.json#/$defs/baseline")
	if err != nil {
		t.Fatal(err)
	}
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("not JSON: %v\n%s", err, data)
	}
	return sch.Validate(v)
}

func withBuild(t *testing.T, core []byte) []byte {
	t.Helper()
	// The patch is written as a merge of JSON documents, so no type assertion is
	// needed on the schema's shape.
	patch := `{"$defs":{"baseline":{"properties":{"build":` + buildDef + `}}}}`
	var doc, over map[string]any
	if err := json.Unmarshal(core, &doc); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(patch), &over); err != nil {
		t.Fatal(err)
	}
	merge(doc, over)
	var req struct {
		Defs struct {
			Baseline struct {
				Required []string `json:"required"`
			} `json:"baseline"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(core, &req); err != nil {
		t.Fatal(err)
	}
	required, err := json.Marshal(append(req.Defs.Baseline.Required, "build"))
	if err != nil {
		t.Fatal(err)
	}
	var reqDoc map[string]any
	if err := json.Unmarshal([]byte(`{"$defs":{"baseline":{"required":`+string(required)+`}}}`), &reqDoc); err != nil {
		t.Fatal(err)
	}
	merge(doc, reqDoc)
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// merge copies over into dst, objects key by key and everything else whole.
func merge(dst, over map[string]any) {
	for k, v := range over {
		sub, isMap := v.(map[string]any)
		cur, curMap := dst[k].(map[string]any)
		if isMap && curMap {
			merge(cur, sub)
			continue
		}
		dst[k] = v
	}
}

func f64(v float64) *float64 { return &v }

// full holds every kind, written in no particular order.
func full() baseline.File {
	return baseline.File{
		Build: testBuild,
		Ledger: baseline.Ledger{
			Functions:  map[string]float64{"example.com/app/z.Zed": 12.5, "example.com/app/a.Alpha": 21, "example.com/app/a.T.Method": 10.25},
			Packages:   map[string]baseline.Package{"example.com/app/q": {StatementsPct: 71.5, Uncovered: 12}, "example.com/app/b": {StatementsPct: 0, Uncovered: 40}},
			Findings:   baseline.Counts{"revive": {"b.go": 1, "a.go": 3}, "errcheck": {"a.go": 2}},
			Laws:       baseline.Counts{"G-05": {"a.go": 1}},
			Nolint:     baseline.Counts{"gosec": {"x/y.go": 2}},
			Ack:        baseline.Counts{"G-12": {"x/y.go": 1}},
			Exemptions: 2,
			Survivors:  map[string]int{"m.go": 4},
		},
		JudgedLaws: 7,
		Regressions: []baseline.Regression{
			{Kind: "functions", Key: "example.com/app/z.Zed", From: nil, To: 12.5, Reason: "the parser grows by design", Owner: "maintainer"},
			{Kind: "functions", Key: "example.com/app/a.Alpha", From: f64(19), To: 21, Reason: "one more format arm", Owner: "maintainer"},
		},
	}
}

const fullJSON = `{
  "build": {
    "cgo_enabled": true,
    "goarch": "amd64",
    "goos": "linux",
    "tags": ["race"]
  },
  "judged_laws": 7,
  "ledger": {
    "exemptions": 2,
    "findings": {
      "errcheck": {
        "a.go": 2
      },
      "revive": {
        "a.go": 3,
        "b.go": 1
      }
    },
    "functions": {
      "example.com/app/a.Alpha": 21,
      "example.com/app/a.T.Method": 10.25,
      "example.com/app/z.Zed": 12.5
    },
    "laws": {
      "G-05": {
        "a.go": 1
      }
    },
    "packages": {
      "example.com/app/b": {"statements_pct": 0, "uncovered": 40},
      "example.com/app/q": {"statements_pct": 71.5, "uncovered": 12}
    },
    "suppressions": {
      "ack": {
        "G-12": {
          "x/y.go": 1
        }
      },
      "nolint": {
        "gosec": {
          "x/y.go": 2
        }
      }
    },
    "survivors": {
      "m.go": 4
    }
  },
  "regressions": [
    {"from": 19, "key": "example.com/app/a.Alpha", "kind": "functions", "owner": "maintainer", "reason": "one more format arm", "to": 21},
    {"from": null, "key": "example.com/app/z.Zed", "kind": "functions", "owner": "maintainer", "reason": "the parser grows by design", "to": 12.5}
  ]
}
`

func TestMarshalSortsKeysAndWritesOneEntryPerLine(t *testing.T) {
	got := string(baseline.Marshal(full()))
	if got != fullJSON {
		t.Errorf("Marshal:\n%s\nwant:\n%s", got, fullJSON)
	}
}

func TestMarshalIsDeterministic(t *testing.T) {
	a := baseline.Marshal(full())
	for range 20 {
		if b := baseline.Marshal(full()); !bytes.Equal(a, b) {
			t.Fatalf("two writes of the same ledger differ:\n%s\n%s", a, b)
		}
	}
}

func TestMarshalIsValidAgainstTheBaselineSchema(t *testing.T) {
	for name, f := range map[string]baseline.File{"full": full(), "empty": {Build: testBuild}, "no tags": {Build: baseline.Build{GOOS: "darwin", GOARCH: "arm64"}}} {
		if err := validate(t, baseline.Marshal(f)); err != nil {
			t.Errorf("%s: %v\n%s", name, err, baseline.Marshal(f))
		}
	}
}

func TestEmptyLedgerIsAValidFileWithAnEmptyLedger(t *testing.T) {
	// §6.7: a repo with nothing over the line gets a valid file with an empty ledger.
	want := "{\n  \"build\": {\n    \"cgo_enabled\": true,\n    \"goarch\": \"amd64\",\n    \"goos\": \"linux\",\n    \"tags\": [\"race\"]\n  },\n  \"judged_laws\": 0,\n  \"ledger\": {}\n}\n"
	if got := string(baseline.Marshal(baseline.File{Build: testBuild})); got != want {
		t.Errorf("empty ledger wrote %q, want %q", got, want)
	}
}

func TestParseRoundTrips(t *testing.T) {
	got, err := baseline.Parse([]byte(fullJSON))
	if err != nil {
		t.Fatal(err)
	}
	// Regressions come back in file order; the written order is the sort order.
	if again := baseline.Marshal(got); string(again) != fullJSON {
		t.Errorf("parse then marshal changed the file:\n%s", again)
	}
}

func TestParseRefusesWhatIsNotALedger(t *testing.T) {
	for name, doc := range map[string]string{
		"empty":             "",
		"not json":          "ledger:",
		"unknown key":       `{"build":{"goos":"linux","goarch":"amd64","tags":[],"cgo_enabled":true},"ledger":{},"judged_laws":0,"extra":1}`,
		"no build":          `{"ledger":{},"judged_laws":0}`,
		"build missing key": `{"build":{"goos":"linux"},"ledger":{},"judged_laws":0}`,
		"unknown kind":      `{"ledger":{"bogus":{}},"judged_laws":0}`,
		"text after json":   `{"build":{"goos":"linux","goarch":"amd64","tags":[],"cgo_enabled":true},"ledger":{},"judged_laws":0} x`,
		"no judged_laws":    `{"build":{"goos":"linux","goarch":"amd64","tags":[],"cgo_enabled":true},"ledger":{}}`,
		"count not number":  `{"build":{"goos":"linux","goarch":"amd64","tags":[],"cgo_enabled":true},"ledger":{"exemptions":"two"},"judged_laws":0}`,
	} {
		if _, err := baseline.Parse([]byte(doc)); err == nil {
			t.Errorf("%s: Parse accepted %q", name, doc)
		}
	}
}

func TestMarshalledStringsAreEscapedAsJSON(t *testing.T) {
	f := baseline.File{Regressions: []baseline.Regression{{Kind: "functions", Key: `a"b<c>.F`, To: 11, Reason: "quote \" and <angle> kept", Owner: "o"}}}
	data := baseline.Marshal(f)
	var back struct {
		Regressions []struct{ Key, Reason string } `json:"regressions"`
	}
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	if back.Regressions[0].Key != `a"b<c>.F` || back.Regressions[0].Reason != "quote \" and <angle> kept" {
		t.Errorf("strings did not survive: %+v", back)
	}
	if bytes.Contains(data, []byte("\\u003c")) {
		t.Errorf("angle brackets were HTML-escaped: %s", data)
	}
}

func TestDescribeBuildNamesTheOSArchTagsAndCgo(t *testing.T) {
	b := baseline.Build{GOOS: "linux", GOARCH: "amd64", Tags: []string{"b", "a", "a"}, CGOEnabled: true}
	if s := baseline.DescribeBuild(b); s != "linux/amd64 cgo=1 tags=a,b" {
		t.Errorf("DescribeBuild = %q", s)
	}
	if s := baseline.DescribeBuild(baseline.Build{GOOS: "darwin", GOARCH: "arm64"}); s != "darwin/arm64 cgo=0 tags=" {
		t.Errorf("DescribeBuild = %q", s)
	}
}

func TestMarshalWritesTagsSortedAndOnce(t *testing.T) {
	f := baseline.File{Build: baseline.Build{GOOS: "linux", GOARCH: "amd64", Tags: []string{"z", "a", "z"}}}
	if got := string(baseline.Marshal(f)); !strings.Contains(got, `"tags": ["a", "z"]`) || !strings.Contains(got, `"cgo_enabled": false`) {
		t.Errorf("%s", got)
	}
}
