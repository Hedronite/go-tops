package baseline_test

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	gotops "github.com/Hedronite/go-tops"
	"github.com/Hedronite/go-tops/internal/baseline"
)

// GO_TOPS.md §6.7: .go-tops/baseline.json is written with keys sorted and one
// entry per line so that parallel branches merge without conflict. Its shape is
// core.schema.json#/$defs/baseline.

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

func f64(v float64) *float64 { return &v }

// full holds every kind, written in no particular order.
func full() baseline.File {
	return baseline.File{
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
			{Kind: "functions", Key: "example.com/app/z.Zed", From: nil, To: 12.5, Reason: "the parser grows by design", Owner: "ali"},
			{Kind: "functions", Key: "example.com/app/a.Alpha", From: f64(19), To: 21, Reason: "one more format arm", Owner: "ali"},
		},
	}
}

const fullJSON = `{
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
    {"from": 19, "key": "example.com/app/a.Alpha", "kind": "functions", "owner": "ali", "reason": "one more format arm", "to": 21},
    {"from": null, "key": "example.com/app/z.Zed", "kind": "functions", "owner": "ali", "reason": "the parser grows by design", "to": 12.5}
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
	for name, f := range map[string]baseline.File{"full": full(), "empty": {}} {
		if err := validate(t, baseline.Marshal(f)); err != nil {
			t.Errorf("%s: %v\n%s", name, err, baseline.Marshal(f))
		}
	}
}

func TestEmptyLedgerIsAValidFileWithAnEmptyLedger(t *testing.T) {
	// §6.7: a repo with nothing over the line gets a valid file with an empty ledger.
	want := "{\n  \"judged_laws\": 0,\n  \"ledger\": {}\n}\n"
	if got := string(baseline.Marshal(baseline.File{})); got != want {
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
		"empty":            "",
		"not json":         "ledger:",
		"unknown key":      `{"ledger":{},"judged_laws":0,"extra":1}`,
		"unknown kind":     `{"ledger":{"bogus":{}},"judged_laws":0}`,
		"text after json":  `{"ledger":{},"judged_laws":0} x`,
		"no judged_laws":   `{"ledger":{}}`,
		"count not number": `{"ledger":{"exemptions":"two"},"judged_laws":0}`,
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
