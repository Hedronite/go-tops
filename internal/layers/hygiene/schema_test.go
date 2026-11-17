package hygiene

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	gotops "github.com/Hedronite/go-tops"
	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/report"
)

// The details the layer emits must be exactly what schema/gate-report.schema.json
// (hygieneDetails, hygieneCheck) accepts, for every outcome the layer can reach:
// a report that fails its own schema is a defect (GO_TOPS.md §3.4, §12.3).

func validateLayerReport(t *testing.T, r gate.Result) error {
	t.Helper()
	rep := report.Report{
		Protocol: report.Protocol, ToolVersion: "x", Module: "m", Class: "binary-cli", Enforce: "hard",
		Git: report.Git{Head: "a", Base: "b"}, MaxTier: 1, StartedAt: "2026-11-17T18:00:00Z",
		Layers:  []report.Layer{{ID: "hygiene", Tier: 1, Outcome: string(r.Outcome), Reason: r.Reason, Details: r.Details}},
		Summary: report.Summary{Outcome: "fail", ExitCode: 1, Enforced: true},
	}
	if r.VulnDBTimestamp != "" {
		ts := r.VulnDBTimestamp
		rep.VulnDBTimestamp = &ts
	}
	doc, err := report.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	for _, n := range []string{"core.schema.json", "go-tops.schema.json", "gate-report.schema.json"} {
		b, err := fs.ReadFile(gotops.Files(), "schema/"+n)
		if err != nil {
			t.Fatal(err)
		}
		v, err := jsonschema.UnmarshalJSON(strings.NewReader(string(b)))
		if err != nil {
			t.Fatal(err)
		}
		if err := c.AddResource("mem:///schema/"+n, v); err != nil {
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

func TestEmittedDetailsValidateAgainstTheSchema(t *testing.T) {
	const withDepguard = "version: \"2\"\nlinters:\n  default: none\n  enable: [errcheck, depguard]\n  settings:\n    depguard:\n      rules:\n        main:\n          deny:\n            - pkg: math/rand\n              desc: use crypto/rand\nformatters:\n  enable: [gofmt]\n"
	type tc struct {
		res  gate.Result
		want gate.Outcome
	}
	cases := map[string]tc{
		"clean":                {plant(t, nil, gate.HygieneConfig{}), gate.Pass},
		"unformatted":          {plant(t, module{"main.go": "package main\n\nfunc main()  {}\n"}, gate.HygieneConfig{}), gate.Fail},
		"lint finding":         {plant(t, module{"main.go": "package main\n\nimport \"os\"\n\nfunc main() {\n\tos.Remove(\"x\")\n}\n"}, gate.HygieneConfig{}), gate.Fail},
		"reachable":            {plant(t, module{"main.go": "package main\n\nimport \"net/http\"\n\nfunc main() {\n\tif err := http.ListenAndServe(\":0\", nil); err != nil {\n\t\tpanic(err)\n\t}\n}\n"}, gate.HygieneConfig{}), gate.Fail},
		"unreached":            {plant(t, module{"main.go": "package main\n\nimport \"net/http\"\n\nfunc main() { _ = http.StatusText(200) }\n"}, gate.HygieneConfig{FailOn: "reachable"}), gate.Pass},
		"toolchain":            {plant(t, module{"go-tops.lock": strings.Replace(lockText, "go1.27.4", "go1.20.1", 1)}, gate.HygieneConfig{}), gate.Fail},
		"depguard not enabled": {plant(t, nil, gate.HygieneConfig{Depguard: true}), gate.Fail},
		"depguard enabled":     {plant(t, module{".golangci.yml": withDepguard}, gate.HygieneConfig{Depguard: true}), gate.Pass},
	}
	f := newFake(t)
	f.missing["govulncheck"] = true
	cases["tool missing"] = tc{runLayer(t, f, gate.Config{Module: "m"}), gate.ToolMissing}
	f = newFake(t)
	f.on("go mod verify", gate.Output{ExitCode: 1}) // fails and prints nothing
	cases["verify fails silently"] = tc{runLayer(t, f, gate.Config{}), gate.Fail}
	f = newFake(t)
	f.missing["golangci-lint"] = true
	cases["no linter"] = tc{runLayer(t, f, gate.Config{Hygiene: gate.HygieneConfig{Depguard: true}}), gate.ToolMissing}

	for name, c := range cases {
		if c.res.Outcome != c.want {
			t.Errorf("%s: outcome %s (%s), want %s", name, c.res.Outcome, c.res.Reason, c.want)
			continue
		}
		if _, onlyError := c.res.Details["error"]; onlyError {
			t.Errorf("%s: details are the {error} form, so the run never reached its checks: %s", name, c.res.Reason)
			continue
		}
		if err := validateLayerReport(t, c.res); err != nil {
			t.Errorf("%s: details do not validate: %s", name, strings.ReplaceAll(err.Error(), "\n", " | "))
		}
	}
}
