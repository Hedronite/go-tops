package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The oracle is GO_TOPS.md §6.8: an exemption takes the functions it covers out
// of the CRAP cap check, in the open: they are scored and listed, every
// exemption in force is printed with its expiry, and an entry that matches
// nothing fails the gate, naming it.

// heavy has complexity 12 and its test runs every branch, so it is fully
// covered: it fails the cap of 10 at every coverage.
const heavySrc = `package app

func Heavy(a int) int {
	n := 0
` + `	if a > 0 {
		n++
	}
` + "\tif a > 1 {\n\t\tn++\n\t}\n\tif a > 2 {\n\t\tn++\n\t}\n\tif a > 3 {\n\t\tn++\n\t}\n\tif a > 4 {\n\t\tn++\n\t}\n" +
	"\tif a > 5 {\n\t\tn++\n\t}\n\tif a > 6 {\n\t\tn++\n\t}\n\tif a > 7 {\n\t\tn++\n\t}\n\tif a > 8 {\n\t\tn++\n\t}\n" +
	"\tif a > 9 {\n\t\tn++\n\t}\n\tif a > 10 {\n\t\tn++\n\t}\n\treturn n\n}\n"

const heavyTest = `package app

import "testing"

func TestHeavy(t *testing.T) {
	if Heavy(20) != 11 {
		t.Fatal("Heavy")
	}
}
`

func exemptionBlock(path string) string {
	return "exemptions:\n  - path: " + path + "\n    symbol: Heavy\n    layer: crap\n" +
		"    reason: generated dispatch, one test per arm\n    owner: platform\n    expires: \"2999-01-01\"\n"
}

func heavyGate(t *testing.T, extra string) (code int, stdout string, rep map[string]any) {
	t.Helper()
	dir := gitModule(t, goodConfig(t)+extra)
	writeFile(t, dir, "heavy.go", heavySrc)
	writeFile(t, dir, "heavy_test.go", heavyTest)
	code, stdout, _ = gotopsEnv(t, dir, []string{"CGO_ENABLED=1"}, "gate", "--only", "crap")
	b, err := os.ReadFile(filepath.Join(dir, ".go-tops", "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatal(err)
	}
	return code, stdout, rep
}

// array and object read a decoded JSON value, and fail the test when it has
// another type; a missing key reads as empty.
func array(t *testing.T, v any) []any {
	t.Helper()
	if v == nil {
		return nil
	}
	a, ok := v.([]any)
	if !ok {
		t.Fatalf("%v is %T, want an array", v, v)
	}
	return a
}

func object(t *testing.T, v any) map[string]any {
	t.Helper()
	o, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%v is %T, want an object", v, v)
	}
	return o
}

func crapDetails(t *testing.T, rep map[string]any) map[string]any {
	t.Helper()
	for _, l := range array(t, rep["layers"]) {
		if m := object(t, l); m["id"] == "crap" {
			return object(t, m["details"])
		}
	}
	t.Fatal("no crap layer in the report")
	return nil
}

func TestGateExemptionTakesAFunctionOffTheCapAndIsPrinted(t *testing.T) {
	code, stdout, rep := heavyGate(t, "")
	if code != 1 || !strings.Contains(stdout, "Heavy") {
		t.Fatalf("without an exemption: exit %d, want 1 naming Heavy\n%s", code, stdout)
	}
	if v := array(t, crapDetails(t, rep)["violations"]); len(v) != 1 {
		t.Fatalf("violations %v, want Heavy", v)
	}

	code, stdout, rep = heavyGate(t, exemptionBlock("heavy.go"))
	if code != 0 {
		t.Fatalf("with an exemption: exit %d, want 0\n%s", code, stdout)
	}
	for _, want := range []string{"exemption in force", "heavy.go", "Heavy", "2999-01-01", "platform"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the text output lacks %q:\n%s", want, stdout)
		}
	}
	d := crapDetails(t, rep)
	ex := array(t, d["exempt"])
	if v := array(t, d["violations"]); len(v) != 0 || len(ex) != 1 {
		t.Fatalf("violations %v, exempt %v; want Heavy scored and listed under exempt only", v, ex)
	}
	if row := object(t, ex[0]); row["cc"] != float64(12) || !strings.HasSuffix(fmt.Sprint(row["func"]), ".Heavy") {
		t.Errorf("exempt row %v, want Heavy with complexity 12", row)
	}
}

func TestGateUnmatchedExemptionFailsNamingItsPath(t *testing.T) {
	code, stdout, _ := heavyGate(t, exemptionBlock("gone/**"))
	if code != 1 || !strings.Contains(stdout, "gone/**") || !strings.Contains(stdout, "no production file") {
		t.Fatalf("exit %d, want 1 naming gone/**\n%s", code, stdout)
	}
}
