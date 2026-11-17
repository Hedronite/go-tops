package gate

import (
	"encoding/json"
	"sort"
	"testing"

	gotops "github.com/Hedronite/go-tops"
	"github.com/Hedronite/go-tops/internal/laws"
)

func enumOf(t *testing.T, def string) []string {
	t.Helper()
	b, err := gotops.FS.ReadFile("schema/core.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Defs map[string]struct {
			Enum []string `json:"enum"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	e := append([]string(nil), s.Defs[def].Enum...)
	sort.Strings(e)
	return e
}

func sameSet(t *testing.T, name string, got, want []string) {
	t.Helper()
	sort.Strings(got)
	if len(got) != len(want) {
		t.Errorf("%s: %v, want the schema's %v", name, got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s: %v, want the schema's %v", name, got, want)
			return
		}
	}
}

// §3.3: the outcomes, verdicts and reason codes are exactly the enums of
// schema/core.schema.json, so a value never differs between the report and the code.
func TestVocabularyIsTheCoreSchemaEnums(t *testing.T) {
	var out []string
	for _, o := range Outcomes() {
		out = append(out, string(o))
	}
	sameSet(t, "layer outcomes", out, enumOf(t, "outcome"))
	sameSet(t, "run outcomes", []string{string(RunPass), string(RunFail), string(RunPartial)}, enumOf(t, "runOutcome"))
	sameSet(t, "law verdicts", []string{string(laws.Allow), string(laws.Ack), string(laws.Refuse), string(laws.NotEvaluated)}, enumOf(t, "verdict"))
	sameSet(t, "reason codes", []string{string(laws.NoReader), string(laws.ConfigOff), string(laws.InputMissing)}, enumOf(t, "reasonCode"))
	sameSet(t, "enforcement modes", []string{"hard", "ratchet", "advisory"}, enumOf(t, "enforcementMode"))
}
