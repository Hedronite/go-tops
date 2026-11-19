package initcmd

import (
	"strings"
	"testing"
)

// GO_TOPS.md section 9: deps.depguard true means the import linter runs. init
// writes deps.depguard true, so the .golangci.yml it writes enables depguard
// with rules; the two files say the same thing (SPEC-m1 B13).
func TestLintConfigEnablesDepguardWhenTheConfigDoes(t *testing.T) {
	for _, class := range launchClasses {
		t.Run(class, func(t *testing.T) {
			dir := newModule(t, "github.com/acme/demo", "1.26")
			if code, _, errs := run(opts(dir, class)); code != ExitOK {
				t.Fatal(errs)
			}
			cfg := asMap(t, decodeYAML(t, read(t, dir, "go-tops.yaml")))
			deps := asMap(t, asMap(t, asMap(t, cfg["gates"])["hygiene"])["deps"])
			if deps["depguard"] != true {
				t.Fatalf("go-tops.yaml deps.depguard = %v, want true", deps["depguard"])
			}
			lint := asMap(t, decodeYAML(t, read(t, dir, ".go-tops/golangci.yml")))
			linters := asMap(t, lint["linters"])
			enabled, ok := linters["enable"].([]any)
			if !ok || len(enabled) != 1 || enabled[0] != "depguard" {
				t.Fatalf("linters.enable = %v, want [depguard]", linters["enable"])
			}
			rules := asMap(t, asMap(t, asMap(t, linters["settings"])["depguard"])["rules"])
			deny, ok := asMap(t, rules["main"])["deny"].([]any)
			if !ok || len(deny) == 0 {
				t.Fatalf("depguard has no deny rules: %v", rules)
			}
			for _, d := range deny {
				if m := asMap(t, d); m["pkg"] == nil || m["desc"] == nil {
					t.Errorf("a deny rule needs a pkg and a desc: %v", m)
				}
			}
		})
	}
}

// With deps.depguard false the lint config does not mention the linter, and
// still carries the errcheck settings every config has.
func TestLintConfigOmitsDepguardWhenTheConfigTurnsItOff(t *testing.T) {
	in := &inputs{o: Options{Class: "binary-cli"}, module: "github.com/acme/demo", goFloor: "1.26", owner: "@acme", depguard: false}
	text, err := in.fill(`linters:
  default: standard
{DEPGUARD_ENABLE}  settings:
{DEPGUARD_SETTINGS}    errcheck:
      check-blank: true
`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "depguard") || strings.Contains(text, "enable:") {
		t.Errorf("depguard off must leave no trace:\n%s", text)
	}
	m := asMap(t, decodeYAML(t, text))
	if asMap(t, asMap(t, m["linters"])["settings"])["errcheck"] == nil {
		t.Errorf("the errcheck settings must survive:\n%s", text)
	}
}

// The floor leg proves which toolchain it runs: the first thing it does after
// setup-go is compare go env GOVERSION with the floor release (SPEC-m1 B13).
func TestWorkflowFloorLegGuardsTheToolchain(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26.4")
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	text := read(t, dir, ".github/workflows/go-tops.yml")
	for _, want := range []string{
		"name: toolchain is the floor release",
		"if: matrix.leg == 'floor'",
		`got="$(go env GOVERSION)"`,
		"go1.26.4|go1.26.4.*) ;;",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the workflow lacks %q", want)
		}
	}
	guard, gate, build := strings.Index(text, "toolchain is the floor release"), strings.Index(text, "name: gate\n"), strings.Index(text, "name: build and test on the floor")
	if guard < 0 || gate < 0 || build < 0 || guard > build || guard > gate {
		t.Errorf("the guard must come before the gate and the floor build (%d, %d, %d)", guard, gate, build)
	}
}
