package config_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/config"
)

// The oracle is GO_TOPS.md §12.1 `check`: the schema, then each semantic rule it
// lists. Every rule has a planted configuration that must fail with the rule's
// name and the JSON pointer of the offending value, and the neighbouring
// configuration that must pass.

func TestBaseConfigPasses(t *testing.T) {
	requirePass(t, writeModule(t, baseDoc(t), nil))
}

func TestMissingConfigFileIsAnIssue(t *testing.T) {
	dir := writeModule(t, nil, nil)
	res := check(t, dir)
	expect(t, res.Issues, want{rule: "config", contains: "not found"})
}

// bad is one planted configuration.
type bad struct {
	name  string
	mut   func(doc)
	files map[string]string
	want  want
}

func set(path string, v any) func(doc) { return func(d doc) { setPath(d, path, v) } }

func TestPlantedConfigsFail(t *testing.T) {
	shaOK := strings.Repeat("a", 64)
	exemption := func(mod map[string]any) func(doc) {
		return func(d doc) {
			e := doc{"path": "internal/x/**", "layer": "mutation", "reason": "frozen vendor port", "owner": "platform", "expires": "2027-03-01"}
			for k, v := range mod {
				if _, del := v.(deletedKey); del {
					delete(e, k)
				} else {
					e[k] = v
				}
			}
			d["exemptions"] = []any{e}
		}
	}
	offCrap := func(extra doc) func(doc) {
		return func(d doc) {
			c := doc{"enabled": false}
			for k, v := range extra {
				c[k] = v
			}
			setPath(d, "gates.crap", c)
		}
	}
	tests := []bad{
		// Schema (JSON Schema 2020-12, strict objects).
		{name: "unknown top-level key", mut: set("bogus", 1), want: want{rule: "schema", pointer: ptr("/bogus")}},
		{name: "unknown nested key", mut: set("gates.hygiene.foo", true), want: want{rule: "schema", pointer: ptr("/gates/hygiene/foo")}},
		{name: "wrong type", mut: set("gates.crap.cap", "ten"), want: want{rule: "schema", pointer: ptr("/gates/crap/cap")}},
		{name: "unknown class", mut: set("class", "web-app"), want: want{rule: "schema", pointer: ptr("/class")}},
		{name: "removed key laws.enable", mut: set("laws.enable", []any{"G-02"}), want: want{rule: "schema", pointer: ptr("/laws/enable")}},
		{name: "mutation survivors above 0", mut: set("gates.mutation.unexplained_survivors_max", 1), want: want{rule: "schema", pointer: ptr("/gates/mutation/unexplained_survivors_max")}},
		{name: "tests race off", mut: set("gates.tests.race", false), want: want{rule: "schema", pointer: ptr("/gates/tests/race")}},

		// Protocol id.
		{name: "protocol id", mut: set("protocol", "0.0.1"), want: want{rule: "schema", pointer: ptr("/protocol")}},

		// Non-empty version.
		{name: "version blank", mut: set("version", "  "), want: want{rule: "version", pointer: ptr("/version")}},
		{name: "version missing", mut: set("version", deleted), want: want{rule: "version", pointer: ptr("/version")}},

		// Module name equals go.mod.
		{name: "module missing", mut: set("module", deleted), want: want{rule: "module", pointer: ptr("/module")}},
		{name: "module differs from go.mod", mut: set("module", "other.example/app"), want: want{rule: "module", pointer: ptr("/module"), contains: "example.com/app"}},

		// Language floor <= current.
		{name: "go floor above current", mut: set("toolchain", doc{"go_floor": "1.28", "go_current": "1.27"}), want: want{rule: "toolchain", pointer: ptr("/toolchain/go_floor")}},

		// Cap not above the class column; floor not below it.
		{name: "cap above class column", mut: set("gates.crap.cap", 11), want: want{rule: "cap-class", pointer: ptr("/gates/crap/cap"), contains: "10"}},
		{name: "floor below class column", mut: set("gates.coverage.statements_floor", 60), want: want{rule: "floor-class", pointer: ptr("/gates/coverage/statements_floor"), contains: "70"}},
		{name: "no-column class cap above 8", mut: func(d doc) { d["class"] = "library-util"; setPath(d, "gates.crap.cap", 9) }, want: want{rule: "cap-class", pointer: ptr("/gates/crap/cap"), contains: "8"}},
		{name: "no-column class floor below 90", mut: func(d doc) { d["class"] = "library-util"; setPath(d, "gates.coverage.statements_floor", 89) }, want: want{rule: "floor-class", pointer: ptr("/gates/coverage/statements_floor"), contains: "90"}},
		{name: "library-core cap above 8", mut: func(d doc) { d["class"] = "library-core"; setPath(d, "gates.crap.cap", 10) }, want: want{rule: "cap-class", pointer: ptr("/gates/crap/cap")}},

		// enabled: false needs a reason.
		{name: "crap off without reason", mut: offCrap(doc{"expires": "2027-03-01"}), want: want{rule: "schema", pointer: ptr("/gates/crap")}},
		{name: "crap off with blank reason", mut: offCrap(doc{"reason": "   ", "expires": "2027-03-01"}), want: want{rule: "reason", pointer: ptr("/gates/crap/reason")}},
		{name: "bench off without reason", mut: set("layers.bench", doc{"enabled": false}), want: want{rule: "schema", pointer: ptr("/layers/bench")}},
		{name: "fuzz off with blank reason", mut: set("layers.fuzz", doc{"enabled": false, "reason": " "}), want: want{rule: "reason", pointer: ptr("/layers/fuzz/reason")}},

		// A disabled crap or mutation needs an expires that has not passed.
		{name: "crap off without expires", mut: offCrap(doc{"reason": "lands at M2"}), want: want{rule: "schema", pointer: ptr("/gates/crap")}},
		{name: "crap off expired yesterday", mut: offCrap(doc{"reason": "lands at M2", "expires": "2026-11-16"}), want: want{rule: "expiry", pointer: ptr("/gates/crap/expires")}},
		{name: "crap off expiring today is expired", mut: offCrap(doc{"reason": "lands at M2", "expires": "2026-11-17"}), want: want{rule: "expiry", pointer: ptr("/gates/crap/expires")}},
		{name: "crap off impossible day", mut: offCrap(doc{"reason": "lands at M2", "expires": "2027-02-30"}), want: want{rule: "expiry", pointer: ptr("/gates/crap/expires"), contains: "calendar"}},
		{name: "mutation off expired", mut: set("gates.mutation", doc{"enabled": false, "reason": "lands at M4", "expires": "2026-01-01"}), want: want{rule: "expiry", pointer: ptr("/gates/mutation/expires")}},

		// No enabled key on gates 1-3.
		{name: "enabled on hygiene", mut: set("gates.hygiene.enabled", false), want: want{rule: "schema", pointer: ptr("/gates/hygiene/enabled")}},
		{name: "enabled on tests", mut: set("gates.tests.enabled", false), want: want{rule: "schema", pointer: ptr("/gates/tests/enabled")}},
		{name: "enabled on coverage", mut: set("gates.coverage.enabled", false), want: want{rule: "schema", pointer: ptr("/gates/coverage/enabled")}},

		// laws.required: whatever the profile requires stays.
		{name: "required removes a profile law", mut: set("laws.required", []any{"G-02"}), want: want{rule: "loosens-profile", pointer: ptr("/laws/required"), contains: "G-07"}},
		{name: "required emptied", mut: set("laws.required", []any{}), want: want{rule: "loosens-profile", pointer: ptr("/laws/required")}},
		{name: "required law not in the catalog", mut: func(d doc) { setPath(d, "laws.required", append(profileLaws(t), "G-99")) }, want: want{rule: "laws-required", pointer: ptr("/laws/required/16"), contains: "catalog"}},
		{name: "required duplicate", mut: func(d doc) { setPath(d, "laws.required", append(profileLaws(t), "G-02")) }, want: want{rule: "schema", pointer: ptr("/laws/required")}},

		// Exemptions: well formed and unexpired (§6.8).
		{name: "exemption expired", mut: exemption(map[string]any{"expires": "2026-06-01"}), want: want{rule: "expiry", pointer: ptr("/exemptions/0/expires")}},
		{name: "exemption impossible day", mut: exemption(map[string]any{"expires": "2027-02-29"}), want: want{rule: "expiry", pointer: ptr("/exemptions/0/expires")}},
		{name: "exemption expires missing", mut: exemption(map[string]any{"expires": deleted}), want: want{rule: "schema", pointer: ptr("/exemptions/0")}},
		{name: "exemption unknown field", mut: exemption(map[string]any{"colour": "red"}), want: want{rule: "schema", pointer: ptr("/exemptions/0/colour")}},
		{name: "exemption layer outside the table", mut: exemption(map[string]any{"layer": "coverage"}), want: want{rule: "schema", pointer: ptr("/exemptions/0/layer")}},
		{name: "exemption shape on mutation", mut: exemption(map[string]any{"shape": "table-switch", "symbol": "F"}), want: want{rule: "schema", pointer: ptr("/exemptions/0/layer")}},
		{name: "exemption shape without symbol", mut: exemption(map[string]any{"shape": "table-switch", "layer": "crap"}), want: want{rule: "schema", pointer: ptr("/exemptions/0")}},
		{name: "exemption symbol without path", mut: exemption(map[string]any{"symbol": "F", "path": deleted}), want: want{rule: "schema", pointer: ptr("/exemptions/0")}},
		{name: "exemption two-word reason", mut: exemption(map[string]any{"reason": "too short"}), want: want{rule: "schema", pointer: ptr("/exemptions/0/reason")}},
		{name: "exemption bad glob", mut: exemption(map[string]any{"path": "internal/[x"}), want: want{rule: "exemption", pointer: ptr("/exemptions/0/path")}},
		{name: "exemption backslash path", mut: exemption(map[string]any{"path": `internal\x`}), want: want{rule: "exemption", pointer: ptr("/exemptions/0/path")}},
		{name: "exemption absolute path", mut: exemption(map[string]any{"path": "/etc/passwd"}), want: want{rule: "exemption", pointer: ptr("/exemptions/0/path")}},
		{name: "exemption dot-dot path", mut: exemption(map[string]any{"path": "../x/**"}), want: want{rule: "exemption", pointer: ptr("/exemptions/0/path")}},

		// Ledger files validate against core.schema.json.
		{
			name: "survivors entry with a short hash", mut: set("gates.mutation.ledger", ".go-tops/survivors.yaml"),
			files: map[string]string{".go-tops/survivors.yaml": "survivors:\n  - {file: a.go, function: F, mutator: ARITHMETIC_BASE, line_hash: abc, reason: three words here, owner: x, expires: \"2027-01-01\"}\n"},
			want:  want{file: ".go-tops/survivors.yaml", rule: "schema", pointer: ptr("/survivors/0/line_hash")},
		},
		{
			name: "survivors entry with an impossible day", mut: set("gates.mutation.base", "origin/main"),
			files: map[string]string{".go-tops/survivors.yaml": "survivors:\n  - {file: a.go, function: F, mutator: ARITHMETIC_BASE, line_hash: " + shaOK + ", reason: three words here, owner: x, expires: \"2027-02-30\"}\n"},
			want:  want{file: ".go-tops/survivors.yaml", rule: "expiry", pointer: ptr("/survivors/0/expires")},
		},
		{
			name: "survivors file empty", mut: set("gates.mutation.base", "origin/main"),
			files: map[string]string{".go-tops/survivors.yaml": ""},
			want:  want{file: ".go-tops/survivors.yaml", rule: "yaml"},
		},
		{
			name: "acks entry with a bad law id", mut: set("laws.ack_ledger", ".go-tops/acks.yaml"),
			files: map[string]string{".go-tops/acks.yaml": "acks:\n  - {law: G-1, file: a.go, reason: three words here, owner: x}\n"},
			want:  want{file: ".go-tops/acks.yaml", rule: "schema", pointer: ptr("/acks/0/law")},
		},
		{
			name: "acks file with an unknown key", mut: set("report.path", ".go-tops/report.json"),
			files: map[string]string{".go-tops/acks.yaml": "acks: []\nextra: 1\n"},
			want:  want{file: ".go-tops/acks.yaml", rule: "schema", pointer: ptr("/extra")},
		},
		{name: "ledger path leaves the module", mut: set("gates.mutation.ledger", "../survivors.yaml"), want: want{rule: "ledger-file", pointer: ptr("/gates/mutation/ledger")}},
		{name: "ack ledger path absolute", mut: set("laws.ack_ledger", "/tmp/acks.yaml"), want: want{rule: "ledger-file", pointer: ptr("/laws/ack_ledger")}},

		// enforce: advisory carries the advisory key with an unexpired expires.
		{name: "advisory without the key", mut: set("enforce", "advisory"), want: want{rule: "schema", pointer: ptr("")}},
		{name: "advisory key without advisory enforce", mut: set("advisory", doc{"reason": "the laws layer lands at M5", "owner": "x", "expires": "2027-06-01"}), want: want{rule: "schema", pointer: ptr("/enforce")}},
		{name: "advisory expired", mut: func(d doc) {
			d["enforce"] = "advisory"
			d["advisory"] = doc{"reason": "the laws layer lands at M5", "owner": "x", "expires": "2026-11-01"}
		}, want: want{rule: "expiry", pointer: ptr("/advisory/expires")}},
		{name: "advisory blank reason", mut: func(d doc) {
			d["enforce"] = "advisory"
			d["advisory"] = doc{"reason": "   ", "owner": "x", "expires": "2027-06-01"}
		}, want: want{rule: "schema", pointer: ptr("/advisory/reason")}},
		{name: "advisory two-word reason", mut: func(d doc) {
			d["enforce"] = "advisory"
			d["advisory"] = doc{"reason": "no laws", "owner": "x", "expires": "2027-06-01"}
		}, want: want{rule: "schema", pointer: ptr("/advisory/reason")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := baseDoc(t)
			tt.mut(d)
			dir := writeModule(t, d, tt.files)
			res := check(t, dir)
			expect(t, res.Issues, tt.want)
		})
	}
}

func profileLaws(t *testing.T) []any {
	t.Helper()
	p, err := config.ProfileFor("binary-cli")
	if err != nil {
		t.Fatal(err)
	}
	out := make([]any, 0, len(p.Laws.Required)+1)
	for _, id := range p.Laws.Required {
		out = append(out, id)
	}
	return out
}

func TestNeighbouringConfigsPass(t *testing.T) {
	shaOK := strings.Repeat("b", 64)
	tests := []struct {
		name  string
		mut   func(doc)
		files map[string]string
	}{
		{"tighter floor and cap", func(d doc) {
			setPath(d, "gates.coverage.statements_floor", 80)
			setPath(d, "gates.crap.cap", 8)
		}, nil},
		{"cap and floor exactly at the class column", func(d doc) {
			setPath(d, "gates.coverage.statements_floor", 70)
			setPath(d, "gates.crap.cap", 10)
		}, nil},
		{"no-column class at 8 and 90", func(d doc) {
			d["class"] = "library-util"
			setPath(d, "gates.coverage.statements_floor", 90)
			setPath(d, "gates.crap.cap", 8)
		}, nil},
		{"crap off with reason and an unexpired date", set("gates.crap", doc{"enabled": false, "reason": "lands at M2", "expires": "2026-11-18"}), nil},
		{"mutation off with reason and an unexpired date", set("gates.mutation", doc{"enabled": false, "reason": "lands at M4", "expires": "2027-03-01"}), nil},
		{"bench off with a reason alone", set("layers.bench", doc{"enabled": false, "reason": "binary-cli does not gate benchmarks"}), nil},
		{"floor equal to current", set("toolchain", doc{"go_floor": "1.27", "go_current": "1.27"}), nil},
		{"floor with a patch release", set("toolchain", doc{"go_floor": "1.26.4", "go_current": "1.27"}), nil},
		{"required is the profile list", func(d doc) { setPath(d, "laws.required", profileLaws(t)) }, nil},
		{"exemption in force", func(d doc) {
			d["exemptions"] = []any{
				doc{"path": "internal/wire/dispatch.go", "symbol": "Server.handleOp", "layer": "crap", "shape": "table-switch", "reason": "opcode dispatch, every arm one call", "owner": "platform", "expires": "2026-11-18"},
				doc{"path": "internal/legacy/**", "layer": "mutation", "reason": "frozen vendor port", "owner": "protocol", "expires": "2027-01-31"},
			}
		}, nil},
		{"advisory in force", func(d doc) {
			d["enforce"] = "advisory"
			d["advisory"] = doc{"reason": "the laws layer lands at M5", "owner": "x", "expires": "2026-11-18"}
		}, nil},
		{"valid ledgers", set("gates.mutation.base", "origin/main"), map[string]string{
			".go-tops/survivors.yaml": "survivors:\n  - {file: a.go, function: F, mutator: ARITHMETIC_BASE, line_hash: " + shaOK + ", reason: three words here, owner: x, expires: \"2027-01-01\"}\n",
			".go-tops/acks.yaml":      "acks:\n  - {law: G-12, file: a.go, reason: three words here, owner: x}\n",
		}},
		{"empty ledgers", set("gates.mutation.base", "origin/main"), map[string]string{
			".go-tops/survivors.yaml": "survivors: []\n",
			".go-tops/acks.yaml":      "acks: []\n",
		}},
		{"ledger at a custom path", func(d doc) { setPath(d, "gates.mutation.ledger", "ledgers/s.yaml") }, map[string]string{"ledgers/s.yaml": "survivors: []\n"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := baseDoc(t)
			tt.mut(d)
			requirePass(t, writeModule(t, d, tt.files))
		})
	}
}

func TestEveryClassHasALimitAndAGoodConfig(t *testing.T) {
	classes := []string{
		"library-core", "library-util", "parser-codec", "embedded-tinygo", "unsafe-kernel", "binary-cli",
		"tui-app", "binary-service", "concurrency-runtime", "cgo-bridge", "codegen", "workspace-root",
	}
	for _, class := range classes {
		t.Run(class, func(t *testing.T) {
			lim := config.LimitsFor(class)
			d := baseDoc(t)
			d["class"] = class
			setPath(d, "gates.crap.cap", lim.Cap)
			setPath(d, "gates.coverage.statements_floor", lim.Floor)
			requirePass(t, writeModule(t, d, nil))
		})
	}
}

// §5.2 is the oracle for the class table: one cap and one floor per class, 8 and
// 90 for a class with no column.
func TestLimitsAreTheTableOfSection52(t *testing.T) {
	want := map[string]config.Limits{
		"library-core":        {Cap: 8, Floor: 85},
		"binary-cli":          {Cap: 10, Floor: 70},
		"tui-app":             {Cap: 8, Floor: 65},
		"binary-service":      {Cap: 10, Floor: 75},
		"concurrency-runtime": {Cap: 8, Floor: 90},
		"library-util":        {Cap: 8, Floor: 90},
		"parser-codec":        {Cap: 8, Floor: 90},
		"unsafe-kernel":       {Cap: 8, Floor: 90},
		"cgo-bridge":          {Cap: 8, Floor: 90},
		"codegen":             {Cap: 8, Floor: 90},
		"embedded-tinygo":     {Cap: 8, Floor: 90},
		"workspace-root":      {Cap: 8, Floor: 90},
	}
	for class, w := range want {
		if got := config.LimitsFor(class); got != w {
			t.Errorf("LimitsFor(%s) = %+v, want %+v", class, got, w)
		}
	}
}

// A profile's numbers are the class column: the two sources must agree.
func TestLaunchProfilesAgreeWithTheClassTable(t *testing.T) {
	for _, class := range []string{"binary-cli", "binary-service", "library-core"} {
		t.Run(class, func(t *testing.T) {
			p, err := config.ProfileFor(class)
			if err != nil {
				t.Fatal(err)
			}
			lim := config.LimitsFor(class)
			if p.Gates.Crap.Cap == nil || *p.Gates.Crap.Cap != lim.Cap {
				t.Errorf("profile cap %v, table %v", p.Gates.Crap.Cap, lim.Cap)
			}
			if p.Gates.Coverage.StatementsFloor == nil || *p.Gates.Coverage.StatementsFloor != lim.Floor {
				t.Errorf("profile floor %v, table %v", p.Gates.Coverage.StatementsFloor, lim.Floor)
			}
			if len(p.Laws.Required) != 16 {
				t.Errorf("profile requires %d laws, want the 16 of the v0.1 set", len(p.Laws.Required))
			}
		})
	}
}

func TestNoProfileForAClassWithNone(t *testing.T) {
	if _, err := config.ProfileFor("tui-app"); err == nil {
		t.Error("ProfileFor(tui-app) succeeded, want ErrNoProfile")
	}
}

// A profile is not a config: it omits module and version, and check requires both.
func TestAProfileFileIsNotAConfig(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "profiles", "binary-cli.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	dir := writeModule(t, nil, map[string]string{"go-tops.yaml": string(data)})
	res := check(t, dir)
	expect(t, res.Issues, want{rule: "version", pointer: ptr("/version")})
	expect(t, res.Issues, want{rule: "module", pointer: ptr("/module")})
}

// The profile supplies the class defaults; a config tightens it and never loosens it.
func TestConfigCannotLoosenTheProfile(t *testing.T) {
	d := baseDoc(t)
	d["class"] = "binary-service"
	setPath(d, "gates.coverage.statements_floor", 75)
	setPath(d, "layers.bench.regression_pct", 10)
	res := check(t, writeModule(t, d, nil))
	expect(t, res.Issues, want{rule: "loosens-profile", pointer: ptr("/layers/bench/regression_pct"), contains: "5"})

	setPath(d, "layers.bench.regression_pct", 3)
	requirePass(t, writeModule(t, d, nil))
}

// The real catalog (internal/laws) accepts the profile's laws; an injected
// catalog adds each way a law can be refused.
type catalog map[string]config.LawInfo

func (c catalog) Lookup(id, class string) (config.LawInfo, bool) {
	if info, ok := c[id]; ok {
		return info, true
	}
	return config.DefaultLaws().Lookup(id, class)
}

func TestRequiredLawsAreJudgedAgainstTheCatalog(t *testing.T) {
	cat := catalog{
		"G-41": {Applies: true, HasReader: false, Tier: 1},
		"G-42": {Applies: true, HasReader: true, Tier: 2},
		"G-01": {Applies: false, HasReader: true, Tier: 1},
		"G-50": {Applies: true, HasReader: true, Tier: 0},
	}
	tests := []struct {
		id      string
		wantErr string
	}{
		{"G-41", "no reader"},
		{"G-42", "tier 2"},
		{"G-01", "does not apply"},
		{"G-99", "catalog"},
		{"G-50", ""},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			d := baseDoc(t)
			setPath(d, "laws.required", append(profileLaws(t), tt.id))
			dir := writeModule(t, d, nil)
			res, err := config.Check(context.Background(), dir, config.Options{Now: fixedNow, Laws: cat})
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantErr == "" {
				if len(res.Issues) != 0 {
					t.Fatalf("want a pass, got %v", res.Issues)
				}
				return
			}
			expect(t, res.Issues, want{rule: "laws-required", pointer: ptr("/laws/required/16"), contains: tt.wantErr})
		})
	}
}

func TestGoModMissingIsAnIssue(t *testing.T) {
	dir := writeModule(t, baseDoc(t), nil)
	if err := os.Remove(filepath.Join(dir, "go.mod")); err != nil {
		t.Fatal(err)
	}
	expect(t, check(t, dir).Issues, want{rule: "module", pointer: ptr("/module"), contains: "go.mod"})
}

func TestGoModModuleLineForms(t *testing.T) {
	forms := map[string]string{
		"plain":      "module example.com/app\n\ngo 1.26\n",
		"quoted":     "module \"example.com/app\"\n",
		"backquoted": "module `example.com/app`\n",
		"comment":    "// a comment\nmodule example.com/app // trailing\n",
		"crlf":       "module example.com/app\r\ngo 1.26\r\n",
	}
	for name, content := range forms {
		t.Run(name, func(t *testing.T) {
			dir := writeModule(t, baseDoc(t), nil)
			writeFile(t, dir, "go.mod", []byte(content))
			requirePass(t, dir)
		})
	}
	t.Run("no module line", func(t *testing.T) {
		dir := writeModule(t, baseDoc(t), nil)
		writeFile(t, dir, "go.mod", []byte("go 1.26\n"))
		expect(t, check(t, dir).Issues, want{rule: "module", pointer: ptr("/module"), contains: "module"})
	})
}

func TestIssuesAreSortedAndPrinted(t *testing.T) {
	d := baseDoc(t)
	setPath(d, "gates.crap.cap", 11)
	setPath(d, "gates.coverage.statements_floor", 60)
	res := check(t, writeModule(t, d, nil))
	if len(res.Issues) != 2 {
		t.Fatalf("issues = %v, want 2", res.Issues)
	}
	if res.Issues[0].Pointer > res.Issues[1].Pointer {
		t.Errorf("issues not sorted by pointer: %v", res.Issues)
	}
	line := res.Issues[0].String()
	for _, part := range []string{"go-tops.yaml", "/gates/coverage/statements_floor", "floor-class"} {
		if !strings.Contains(line, part) {
			t.Errorf("issue line %q lacks %q", line, part)
		}
	}
}
