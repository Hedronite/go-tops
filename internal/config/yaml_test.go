package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/config"
)

// §6.8 wants a quoted date and the schema wants strict objects; the loader also
// refuses the YAML features that make a document mean something other than it
// reads as: aliases, merge keys, repeated keys, a second document.

func TestLoadRefusesWhatYAMLWouldAccept(t *testing.T) {
	tests := []struct {
		name, text string
		rule       string
		pointer    *string
		contains   string
	}{
		{"repeated key", "protocol: a\nprotocol: b\n", "yaml", ptr("/protocol"), "twice"},
		{"repeated nested key", "gates:\n  crap: {cap: 1, cap: 2}\n", "yaml", ptr("/gates/crap/cap"), "twice"},
		{"anchor", "a: &x 1\n", "yaml", ptr("/a"), "anchors"},
		{"alias", "a: &x 1\nb: *x\n", "yaml", ptr("/b"), "anchors"},
		{"merge key", "base: {a: 1}\ngates:\n  <<: {a: 1}\n", "yaml", ptr("/gates"), "merge"},
		{"non-string key", "1: a\n", "yaml", ptr(""), "not a string"},
		{"two documents", "class: binary-cli\n---\nclass: library-core\n", "yaml", nil, "more than one"},
		{"empty", "", "yaml", nil, "empty"},
		{"only a comment", "# nothing\n", "yaml", nil, "empty"},
		{"tab indentation", "gates:\n\tcrap: {}\n", "yaml", nil, "invalid YAML"},
		{"unquoted date", "gates:\n  crap:\n    enabled: false\n    reason: lands at M2\n    expires: 2027-03-01\n", "yaml", ptr("/gates/crap/expires"), "quoted"},
		{"not a number", "gates:\n  crap:\n    cap: .nan\n", "yaml", ptr("/gates/crap/cap"), "finite"},
		{"oversized", "version: " + strings.Repeat("x", 1<<20) + "\n", "yaml", nil, "larger than"},
		{"root is a list", "- a\n", "schema", ptr(""), ""},
		{"root is a scalar", "just text\n", "schema", ptr(""), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, issues, err := config.Load([]byte(tt.text))
			if err != nil {
				t.Fatal(err)
			}
			if cfg != nil {
				t.Fatalf("Load accepted %q", tt.text)
			}
			expect(t, issues, want{rule: tt.rule, pointer: tt.pointer, contains: tt.contains})
		})
	}
}

func TestLoadDecodesTheExampleIntoTypedFields(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "go-tops.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, issues, err := config.Load(data)
	if err != nil || len(issues) > 0 {
		t.Fatalf("Load: %v %v", err, issues)
	}
	if cfg.Class != "binary-cli" || cfg.Enforce != "ratchet" || cfg.Module != "github.com/acme/myapp" {
		t.Errorf("identity fields: %+v", cfg)
	}
	if cp := cfg.Gates.Crap.Cap; cp == nil || *cp != 10 {
		t.Errorf("crap cap = %v, want 10", cp)
	}
	if fl := cfg.Gates.Coverage.StatementsFloor; fl == nil || *fl != 70 {
		t.Errorf("floor = %v, want 70", fl)
	}
	if n := cfg.Layers.Stress.Count; n == nil || *n != 20 {
		t.Errorf("stress count = %v, want 20", n)
	}
	if cfg.Gates.Mutation.Engine != "overlay" || cfg.Layers.Fuzz.Timed.Duration != "5m" {
		t.Errorf("mutation engine %q, fuzz duration %q", cfg.Gates.Mutation.Engine, cfg.Layers.Fuzz.Timed.Duration)
	}
	if len(cfg.Exemptions) != 1 || cfg.Exemptions[0].Symbol != "Server.handleOp" || cfg.Exemptions[0].Expires != "2027-03-01" {
		t.Errorf("exemptions: %+v", cfg.Exemptions)
	}
	if cfg.Laws.Required != nil {
		t.Errorf("laws.required = %v, want nil when the key is absent", cfg.Laws.Required)
	}
}

func TestEveryEmbeddedProfileIsAValidDocument(t *testing.T) {
	for _, class := range []string{"binary-cli", "binary-service", "library-core"} {
		if _, err := config.ProfileFor(class); err != nil {
			t.Errorf("ProfileFor(%s): %v", class, err)
		}
	}
}
