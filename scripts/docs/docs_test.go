package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture builds a repository root in a temp dir from testdata/base, lays the
// files of testdata/<overlay> over it, and deletes the paths in remove.
func fixture(t *testing.T, overlay string, remove ...string) string {
	t.Helper()
	dst := t.TempDir()
	copyTree(t, filepath.Join("testdata", "base"), dst)
	if overlay != "" {
		copyTree(t, filepath.Join("testdata", overlay), dst)
	}
	for _, r := range remove {
		if err := os.Remove(filepath.Join(dst, r)); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(out, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func runDocs(root string, args ...string) (int, string) {
	var out, errb bytes.Buffer
	code := run(append([]string{"-root", root}, args...), &out, &errb)
	return code, out.String() + errb.String()
}

func failedChecks(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) > 1 && (f[0] == "FAIL" || f[0] == "VACUOUS") {
			names = append(names, f[1])
		}
	}
	return names
}

func TestBasePasses(t *testing.T) {
	code, out := runDocs(fixture(t, ""))
	if code != 0 {
		t.Fatalf("base fixture must pass, got exit %d:\n%s", code, out)
	}
	for _, want := range []string{"catalog 3 (active 2, retired 1), v0.1 1", "3 examples", "2 balanced pairs", "docs: PASS"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

// Each planted defect must fail exactly the named check, with the named reason.
func TestPlantedDefectsFail(t *testing.T) {
	cases := []struct{ overlay, check, reason string }{
		{"count-mismatch", "counts", `"2 of 3 laws judged" states v01 = 2; the catalog has v01 = 1`},
		{"bad-key", "examples", "additional properties 'tierr' not allowed"},
		{"bad-lock-key", "examples", "additional properties 'bogus_key' not allowed"},
		{"bad-type", "examples", "minimum"},
		{"drifted-block", "examples", `first difference at line 2: block "tier: 2", file "tier: 1"`},
		{"invalid-yaml-example", "examples", "invalid YAML"},
		{"marker-missing-schema", "examples", "needs exactly path=<file> and schema=<file>"},
		{"unbalanced-marker", "markers", "tops-core:begin verdicts is never closed"},
		{"unknown-marker", "markers", `names "verdict"`},
		{"nested-marker", "markers", "inside verdicts"},
		{"bad-link", "links", "../missing.md"},
		{"case-link", "links", "does not exist (case-sensitive)"},
		{"bad-anchor", "links", `no heading or id "no-such-heading"`},
		{"lintcfg-no-reason", "lintconfig", "rule MD033 is disabled with no reason"},
		{"tag-pinned-action", "workflow", "actions/checkout@v4"},
		{"missing-need", "workflow", "job docs does not need b"},
	}
	for _, c := range cases {
		t.Run(c.overlay, func(t *testing.T) {
			code, out := runDocs(fixture(t, c.overlay))
			if code != 1 {
				t.Fatalf("exit %d, want 1:\n%s", code, out)
			}
			if got := failedChecks(out); len(got) != 1 || got[0] != c.check {
				t.Errorf("failed checks %v, want only [%s]:\n%s", got, c.check, out)
			}
			if !strings.Contains(out, c.reason) {
				t.Errorf("output lacks %q:\n%s", c.reason, out)
			}
		})
	}
}

// Each plant changes one stated count that the first version of the counts
// check skipped or never read: a clause beside a sibling-suite name on the same
// line, the same in a table cell, and the report example's not_evaluated.
func TestPlantedCountsAlongsideSiblingNamesFail(t *testing.T) {
	cases := []struct{ name, old, new, reason string }{
		{"line-names-sibling-suite", "judge 1 law mechanically", "judge 2 law mechanically", "states v01 = 2; the catalog has v01 = 1"},
		{"cell-names-sibling-suite", "native multichecker (3 laws);", "native multichecker (2 laws);", "states catalog = 2; the catalog has catalog = 3"},
		{"report-not-evaluated", `"not_evaluated": 1`, `"not_evaluated": 2`, "states not_evaluated = 2; the catalog has not_evaluated = 1"},
		{"catalog-rows-phrase", "out of 3 catalog rows", "out of 4 catalog rows", "states catalog = 4; the catalog has catalog = 3"},
		{"singular-active-law", "1 active v0.1 law", "2 active v0.1 law", "states v01 = 2; the catalog has v01 = 1"},
		{"field-bullet-catalog", "retired ones included (3);", "retired ones included (4);", "states catalog = 4; the catalog has catalog = 3"},
		{"field-bullet-active", "laws not retired (2);", "laws not retired (3);", "states active = 3; the catalog has active = 2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := fixture(t, "")
			editFile(t, root, "GO_TOPS.md", c.old, c.new)
			code, out := runDocs(root)
			if code != 1 || len(failedChecks(out)) != 1 || failedChecks(out)[0] != "counts" || !strings.Contains(out, c.reason) {
				t.Fatalf("exit %d, want only counts failing with %q:\n%s", code, c.reason, out)
			}
		})
	}
}

// A count that belongs to the sibling suite is still not read.
func TestSiblingSuiteCountsStayIgnored(t *testing.T) {
	root := fixture(t, "")
	editFile(t, root, "GO_TOPS.md", "Bend2 (53 laws, 15 judged)", "Bend2 (54 laws, 16 judged)")
	editFile(t, root, "GO_TOPS.md", "Rust-TOPS has a sibling suite with 53 laws and 15 judged.", "Rust-TOPS has a sibling suite with 54 laws and 16 judged.")
	if code, out := runDocs(root); code != 0 {
		t.Fatalf("sibling counts must not be read, exit %d:\n%s", code, out)
	}
}

func editFile(t *testing.T, root, rel, old, new string) {
	t.Helper()
	p := filepath.Join(root, rel)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), old) != 1 {
		t.Fatalf("%s: want exactly one %q", rel, old)
	}
	if err := os.WriteFile(p, []byte(strings.Replace(string(data), old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Every YAML or JSON block in sections 5.3, 9, 10 and 12.3 must carry a marker.
func TestUnmarkedBlocksInExampleSectionsFail(t *testing.T) {
	const prose = "Prose only: no example block here."
	cases := []struct{ name, old, new, reason string }{
		{"yaml-in-section-9", "## 9. Configuration\n\n" + prose, "## 9. Configuration\n\n```yaml\nprotocol: \"0.4.0\"\n```", "fenced yaml block in section 9 has no go-tops:example marker"},
		{"yaml-in-section-10", "## 10. Tool pins\n\n" + prose, "## 10. Tool pins\n\n```yaml\nprotocol: \"0.4.0\"\n```", "fenced yaml block in section 10 has no go-tops:example marker"},
		{"section-10-renumbered", "## 10. Tool pins", "## 12. Tool pins", "section 10 not found"},
		{"json-in-section-12.3", "### 12.3 Report\n\n" + prose, "### 12.3 Report\n\n```json\n{}\n```", "fenced json block in section 12.3 has no go-tops:example marker"},
		{"yml-in-section-5.3", "### 5.3 Profile file shape\n\n" + prose, "### 5.3 Profile file shape\n\n```yml\na: 1\n```", "fenced yml block in section 5.3 has no go-tops:example marker"},
		{"subsection-of-9", "## 9. Configuration\n\n" + prose, "## 9. Configuration\n\n### 9.1 Keys\n\n```yaml\na: 1\n```", "fenced yaml block in section 9 has no go-tops:example marker"},
		{"section-renumbered", "### 12.3 Report", "### 12.4 Report", "section 12.3 not found"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := fixture(t, "")
			editFile(t, root, "GO_TOPS.md", c.old, c.new)
			code, out := runDocs(root)
			if got := failedChecks(out); code != 1 || len(got) != 1 || got[0] != "examples" || !strings.Contains(out, c.reason) {
				t.Fatalf("exit %d, want only examples failing with %q:\n%s", code, c.reason, out)
			}
		})
	}
}

func TestMarkedAndOutOfSectionBlocksPass(t *testing.T) {
	root := fixture(t, "")
	editFile(t, root, "GO_TOPS.md", "## 9. Configuration\n\nProse only: no example block here.",
		"## 9. Configuration\n\n<!-- go-tops:example path=examples/cfg.yaml schema=schema/cfg.schema.json -->\n\n```yaml\nprotocol: \"0.4.0\"\ntier: 1\n```")
	editFile(t, root, "GO_TOPS.md", "## 11. After the example sections\n\nProse only.",
		"## 11. After the example sections\n\n```yaml\nnot: an example section\n```")
	code, out := runDocs(root)
	if code != 0 || !strings.Contains(out, "4 examples") {
		t.Fatalf("a marked block in section 9 and an unmarked one in section 11 must pass, exit %d:\n%s", code, out)
	}
}

// The protocol version is one value in the header, the section 2 row and each
// schema constant; changing any one fails only the version check.
func TestVersionDisagreementFails(t *testing.T) {
	cases := []struct{ name, file, old, new, reason string }{
		{"header-dev", "GO_TOPS.md", "0.4.0 (fixture) \u00b7", "0.4.0-dev (fixture) \u00b7", `says "0.4.0-dev"`},
		{"section-2-row", "GO_TOPS.md", "**0.4.0** (fixture)", "**0.3.0** (fixture)", `section 2 row says "0.3.0"`},
		{"go-tops-schema", "schema/go-tops.schema.json", `"0.4.0"`, `"0.5.0"`, `schema/go-tops.schema.json protocol const says "0.5.0"`},
		{"lock-schema", "schema/lock.schema.json", `"0.4.0"`, `"0.9.9"`, `schema/lock.schema.json protocol const says "0.9.9"`},
		{"gate-report-schema", "schema/gate-report.schema.json", `"0.4.0"`, `"0.3.0"`, `schema/gate-report.schema.json protocol const says "0.3.0"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := fixture(t, "")
			editFile(t, root, c.file, c.old, c.new)
			code, out := runDocs(root)
			if got := failedChecks(out); code != 1 || len(got) != 1 || got[0] != "version" || !strings.Contains(out, c.reason) {
				t.Fatalf("exit %d, want only version failing with %q:\n%s", code, c.reason, out)
			}
		})
	}
}

func TestMissingLockSchemaFailsVersion(t *testing.T) {
	root := fixture(t, "", "schema/lock.schema.json")
	if code, out := runDocs(root); code != 1 || !strings.Contains(out, "schema/lock.schema.json: not found") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
}

func TestMissingSchemaFailsVersion(t *testing.T) {
	root := fixture(t, "", "schema/gate-report.schema.json")
	if code, out := runDocs(root); code != 1 || !strings.Contains(out, "schema/gate-report.schema.json: not found") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
}

func TestMissingExampleFileFails(t *testing.T) {
	root := fixture(t, "")
	if err := os.Remove(filepath.Join(root, "examples", "cfg.yaml")); err != nil {
		t.Fatal(err)
	}
	code, out := runDocs(root)
	if code != 1 || !strings.Contains(out, "does not exist") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
}

func TestVacuousExamples(t *testing.T) {
	root := fixture(t, "", "docs/example.md")
	code, out := runDocs(root)
	if code != 1 || !strings.Contains(out, "VACUOUS  examples") {
		t.Fatalf("zero examples must fail by default, exit %d:\n%s", code, out)
	}
	code, out = runDocs(root, "--allow-vacuous")
	if code != 0 || !strings.Contains(out, "VACUOUS  examples") || !strings.Contains(out, "(allowed)") {
		t.Fatalf("zero examples must pass, and say so, under --allow-vacuous; exit %d:\n%s", code, out)
	}
}

func TestVacuousDoesNotHideAFailure(t *testing.T) {
	root := fixture(t, "count-mismatch", "docs/example.md")
	if code, out := runDocs(root, "--allow-vacuous"); code != 1 {
		t.Fatalf("a real failure must still fail, exit %d:\n%s", code, out)
	}
}

func TestChecksSelection(t *testing.T) {
	root := fixture(t, "count-mismatch")
	if code, out := runDocs(root, "-checks", "parse,links"); code != 0 {
		t.Fatalf("links alone must pass, exit %d:\n%s", code, out)
	}
	if code, _ := runDocs(root, "-checks", "nonsense"); code != 2 {
		t.Fatalf("unknown check must exit 2, got %d", code)
	}
	if code, _ := runDocs(filepath.Join(root, "absent")); code != 2 {
		t.Fatalf("missing root must exit 2, got %d", code)
	}
}

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"19. Roadmap":                               "19-roadmap",
		"4.2 Or install the orchestrator":           "42-or-install-the-orchestrator",
		"Appendix A: Gate order at a glance":        "appendix-a-gate-order-at-a-glance",
		"1. Catalog (draft: 3 laws, `G-01`…`G-03`)": "1-catalog-draft-3-laws-g-01g-03",
		"The [`gotops`](#x) CLI":                    "the-gotops-cli",
		"Snake_case and *emphasis*":                 "snake_case-and-emphasis",
	} {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMaskCode(t *testing.T) {
	for in, want := range map[string]string{
		"a `b` c":           "a     c",
		"a ``b ` c`` d":     "a           d",
		"unclosed ` tick":   "unclosed ` tick",
		"`x` and `y` and z": "    and     and z",
	} {
		if got := maskCode(in); got != want {
			t.Errorf("maskCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReadStatements(t *testing.T) {
	type s = statement
	for line, want := range map[string][]s{
		"The report states \"18 of 41 laws judged.\"":                                                              {{"v01", 18, ""}, {"catalog", 41, ""}},
		"judge 18 laws mechanically (the rows)":                                                                    {{"v01", 18, ""}},
		"| **M3** | 18 laws judged (native) | \"18 of 41 judged\" |":                                               {{"v01", 18, ""}, {"v01", 18, ""}, {"catalog", 41, ""}},
		"17 active v0.1 laws and 40 active laws":                                                                   {{"v01", 17, ""}, {"active", 40, ""}},
		"Law catalog (draft: 41 laws, `G-01`…`G-41`)":                                                              {{"catalog", 41, ""}, {"catalog", 41, ""}},
		"Rust-TOPS 1.1.0 defines 53 laws":                                                                          nil,
		"| Laws engine | Bend2 (53 laws, 15 judged) | native (41 laws) |":                                          {{"catalog", 41, ""}},
		"**v0.1 target:** judge 18 laws mechanically (the rows), mirroring Rust-TOPS's":                            {{"v01", 18, ""}},
		"| Laws engine | Bend2 (53 laws, 15 judged) | Native multichecker (41 laws); no Bend or Lean layer |":      {{"catalog", 41, ""}},
		`"catalog": 41, "judged": 18, "not_evaluated": 23`:                                                         {{"catalog", 41, ""}, {"v01", 18, ""}, {"not_evaluated", 23, ""}},
		"Rust-TOPS defines 53 laws, this defines 41 laws":                                                          {{"catalog", 41, ""}},
		"- `catalog`: every law row, retired ones included (41);":                                                  {{"catalog", 41, ""}},
		"- `active`: laws not retired (40);":                                                                       {{"active", 40, ""}},
		"- `judged`: applicable laws with a verdict (not-evaluated is none)":                                       nil,
		`report states "16 of 43 laws judged": 16 active v0.1 laws out of 43 catalog rows, retired rows included.`: {{"v01", 16, ""}, {"catalog", 43, ""}, {"v01", 16, ""}, {"catalog", 43, ""}},
		"1 active v0.1 law out of 3 catalog rows, retired rows included.":                                          {{"v01", 1, ""}, {"catalog", 3, ""}},
		"no counts here, only 3 of 5 things":                                                                       nil,
	} {
		got := readStatements(line)
		if len(got) != len(want) {
			t.Errorf("%q: got %v, want %v", line, got, want)
			continue
		}
		for i := range got {
			if got[i].Qty != want[i].Qty || got[i].N != want[i].N {
				t.Errorf("%q: statement %d = %s %d, want %s %d", line, i, got[i].Qty, got[i].N, want[i].Qty, want[i].N)
			}
		}
	}
}

func TestIsRetired(t *testing.T) {
	for in, want := range map[string]bool{
		"Retired into G-33: x": true, "~~Retired~~": true, "**retired** x": true,
		"Module language": false, "Not retired": false, "": false,
	} {
		if got := isRetired(in); got != want {
			t.Errorf("isRetired(%q) = %v, want %v", in, got, want)
		}
	}
}
