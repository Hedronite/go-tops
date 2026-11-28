package laws

import (
	"testing"

	"github.com/Hedronite/go-tops/internal/report"
)

func TestHygieneFromLayersAbsent(t *testing.T) {
	if h := HygieneFromLayers(nil); h.Ran {
		t.Fatal("want Ran false when no layers")
	}
}

func TestHygieneFromLayersPassFail(t *testing.T) {
	pass := report.Layer{
		ID: "hygiene", Outcome: "pass",
		Details: map[string]any{
			"tidy":            map[string]any{"outcome": "pass"},
			"verify":          map[string]any{"outcome": "pass"},
			"vulnerabilities": map[string]any{"outcome": "pass"},
		},
	}
	h := HygieneFromLayers([]report.Layer{pass})
	if !h.Ran || h.HygieneLayerFail {
		t.Fatalf("pass layer: %+v", h)
	}
	if !h.TidyPass || !h.VerifyPass || !h.VulnsPass {
		t.Fatalf("sub-outcomes: %+v", h)
	}

	fail := report.Layer{ID: "hygiene", Outcome: "fail", Details: map[string]any{
		"tidy": map[string]any{"outcome": "fail"},
	}}
	h = HygieneFromLayers([]report.Layer{fail})
	if !h.Ran || !h.HygieneLayerFail || h.TidyPass {
		t.Fatalf("fail layer: %+v", h)
	}
}

func TestSubOutcomePassShapes(t *testing.T) {
	if subOutcomePass(nil, "tidy") {
		t.Fatal("nil details")
	}
	if subOutcomePass(map[string]any{"tidy": "not-a-map"}, "tidy") {
		t.Fatal("wrong type")
	}
	if subOutcomePass(map[string]any{"tidy": map[string]any{"outcome": "skip"}}, "tidy") {
		t.Fatal("non-pass outcome")
	}
}

func TestSubOutcomePassReturnsTrueOnPass(t *testing.T) {
	d := map[string]any{"verify": map[string]any{"outcome": "pass"}}
	if !subOutcomePass(d, "verify") {
		t.Fatal("want true for pass outcome")
	}
	inner, ok := d["verify"].(map[string]any)
	if !ok || inner["outcome"] != "pass" {
		t.Fatalf("verify block %v", d["verify"])
	}
}

func TestSubOutcomePassRejectsFail(t *testing.T) {
	if subOutcomePass(map[string]any{"tidy": map[string]any{"outcome": "fail"}}, "tidy") {
		t.Fatal("fail outcome must not pass")
	}
}

func TestSubOutcomePassOnlyPassString(t *testing.T) {
	pass := map[string]any{"outcome": "pass"}
	for _, key := range []string{"pass", "ack", "", "fail"} {
		pass["outcome"] = key
		got := subOutcomePass(map[string]any{"step": pass}, "step")
		want := key == "pass"
		if got != want {
			t.Fatalf("outcome %q => %v want %v", key, got, want)
		}
	}
}

func TestSubOutcomePassMissingKeyOrNil(t *testing.T) {
	if subOutcomePass(map[string]any{}, "tidy") {
		t.Fatal("missing key must not pass")
	}
	if subOutcomePass(map[string]any{"tidy": nil}, "tidy") {
		t.Fatal("nil raw must not pass")
	}
}
