package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const lawsDir = "laws"

var lawFileRe = regexp.MustCompile(`^G-\d\d\.yaml$`)

// checkLaws validates laws/G-NN.yaml against schema/law.schema.json, matches
// §8.2 on the fields the drift test covers, and ensures each active law's
// fixture path exists. Paths under testdata/laws/pending/ are catalog
// placeholders only (existence is checked; contents are not enforced).
func checkLaws(r *repo) checkResult {
	res := checkResult{}
	dir := filepath.Join(r.Root, lawsDir)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			res.Summary = "no laws/ directory (skipped for minimal doc fixtures)"
			return res
		}
		res.Findings = append(res.Findings, finding{lawsDir + "/", 0, fmt.Sprintf("laws directory: %v", err)})
		res.Summary = "laws directory unreadable"
		return res
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		res.Findings = append(res.Findings, finding{lawsDir + "/", 0, err.Error()})
		res.Summary = "laws directory unreadable"
		return res
	}
	specRows, specFind := parseLawSpec(r)
	res.Findings = append(res.Findings, specFind...)
	if len(specFind) > 0 {
		res.Summary = "§8.2 parse failed"
		return res
	}
	cache := map[string]*jsonschema.Schema{}
	var files []string
	pending := 0
	var v01Pending []string
	activeFixtures := 0
	for _, e := range entries {
		if e.IsDir() || !lawFileRe.MatchString(e.Name()) {
			continue
		}
		files = append(files, e.Name())
		rel := lawsDir + "/" + e.Name()
		data, err := os.ReadFile(filepath.Join(r.Root, rel))
		if err != nil {
			res.Findings = append(res.Findings, finding{rel, 0, err.Error()})
			continue
		}
		inst, err := decodeInstance(rel, data)
		if err != nil {
			res.Findings = append(res.Findings, finding{rel, 0, err.Error()})
			continue
		}
		sch, err := schemaAt(r, "schema/law.schema.json", cache)
		if err != nil {
			res.Findings = append(res.Findings, finding{rel, 0, err.Error()})
			break
		}
		if err := sch.Validate(inst); err != nil {
			res.Findings = append(res.Findings, finding{rel, 0, fmt.Sprintf("schema: %v", err)})
		}
		row, err := lawRowFromYAML(inst)
		if err != nil {
			res.Findings = append(res.Findings, finding{rel, 0, err.Error()})
			continue
		}
		wantID := strings.TrimSuffix(e.Name(), ".yaml")
		if row.ID != wantID {
			res.Findings = append(res.Findings, finding{rel, 0, fmt.Sprintf("laws/%s: id %q does not match file name", e.Name(), row.ID)})
		}
		if diffs := diffLawRow(specRows, row); len(diffs) > 0 {
			for _, d := range diffs {
				res.Findings = append(res.Findings, finding{rel, 0, d})
			}
		}
		if row.Status == "active" && row.Fixture != "" {
			activeFixtures++
			if !pathInside(r.Root, row.Fixture) {
				res.Findings = append(res.Findings, finding{rel, 0, "fixture path escapes the repository root"})
				continue
			}
			abs := filepath.Join(r.Root, filepath.FromSlash(row.Fixture))
			if _, err := os.Stat(abs); err != nil {
				res.Findings = append(res.Findings, finding{rel, 0, fmt.Sprintf("fixture %s: %v", row.Fixture, err)})
			}
			if strings.HasPrefix(filepath.ToSlash(row.Fixture), "testdata/laws/pending/") {
				pending++
				if row.V01 && row.Status == "active" {
					v01Pending = append(v01Pending, row.ID)
					res.Findings = append(res.Findings, finding{
						rel, 0,
						fmt.Sprintf("v0.1 active law %s still points at placeholder fixture %s (L15)", row.ID, row.Fixture),
					})
				}
			}
		}
	}
	sort.Strings(files)
	if len(files) != len(specRows) {
		res.Findings = append(res.Findings, finding{
			catalogFile, 0,
			fmt.Sprintf("laws: %d files, §8.2 has %d rows", len(files), len(specRows)),
		})
	}
	for id := range specRows {
		want := id + ".yaml"
		if !contains(files, want) {
			res.Findings = append(res.Findings, finding{lawsDir, 0, id + ": in §8.2, no laws/" + want})
		}
	}
	sort.Strings(v01Pending)
	summary := fmt.Sprintf("%d law files; %d active fixture paths; %d under testdata/laws/pending/ (catalog placeholders until G3/G4)", len(files), activeFixtures, pending)
	if len(v01Pending) > 0 {
		summary += "; v0.1 laws still on pending fixtures (G4 fails until real): " + strings.Join(v01Pending, ", ")
	}
	res.Summary = summary
	if len(files) == 0 {
		res.Vacuous = true
	}
	return res
}

func schemaAt(r *repo, rel string, cache map[string]*jsonschema.Schema) (*jsonschema.Schema, error) {
	if s, ok := cache[rel]; ok {
		return s, nil
	}
	if _, err := r.readInside(rel); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(filepath.Join(r.Root, filepath.FromSlash(rel)))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	s, err := c.Compile("file://" + filepath.ToSlash(abs))
	if err != nil {
		return nil, err
	}
	cache[rel] = s
	return s, nil
}

var launchProfileClasses = []string{"library-core", "binary-service", "binary-cli"}

type lawRow struct {
	ID       string
	Severity string
	Reader   string
	Tier     int
	V01      bool
	Retired  bool
	Status   string
	Fixture  string
	Classes  []string

	ExactClasses []string // class-tagged rows in §8.2; exact match required
	NeedLaunch   bool     // v0.1 active rows must include launchProfileClasses
}

func lawRowFromYAML(inst any) (lawRow, error) {
	m, ok := inst.(map[string]any)
	if !ok {
		return lawRow{}, fmt.Errorf("law file is not a mapping")
	}
	id, _ := m["id"].(string)
	sev, _ := m["severity"].(string)
	reader, _ := m["reader"].(string)
	status, _ := m["status"].(string)
	fixture, _ := m["fixture"].(string)
	v01, _ := m["v01"].(bool)
	sevR := map[string]string{"refuse": "R", "ack": "A"}[sev]
	tier := map[string]int{"analyzer": 0, "tool": 1, "attested": 0, "tbd": 0}[reader]
	if id == "G-22" || id == "G-35" {
		tier = 3
	}
	classes := stringSliceField(m, "classes")
	return lawRow{
		ID: id, Severity: sevR, Reader: reader, Tier: tier, V01: v01,
		Retired: status == "retired", Status: status, Fixture: fixture, Classes: classes,
	}, nil
}

func stringSliceField(m map[string]any, key string) []string {
	raw, ok := m[key].([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func diffLawRow(spec map[string]lawRow, got lawRow) []string {
	want, ok := spec[got.ID]
	if !ok {
		return []string{got.ID + ": not in GO_TOPS.md §8.2"}
	}
	var out []string
	if want.Severity != got.Severity {
		out = append(out, "severity differs from §8.2")
	}
	if want.Reader != got.Reader {
		out = append(out, "reader differs from §8.2")
	}
	if want.Tier != got.Tier {
		out = append(out, "tier differs from §8.2")
	}
	if want.V01 != got.V01 {
		out = append(out, "v0.1 mark differs from §8.2")
	}
	if want.Retired != got.Retired {
		out = append(out, "retired differs from §8.2")
	}
	if want.ExactClasses != nil {
		if !sameStringSet(want.ExactClasses, got.Classes) {
			out = append(out, fmt.Sprintf("classes differ from §8.2 (want %v, got %v)", want.ExactClasses, got.Classes))
		}
	}
	if want.NeedLaunch {
		for _, c := range launchProfileClasses {
			if !containsString(got.Classes, c) {
				out = append(out, fmt.Sprintf("v0.1 law missing launch class %q", c))
			}
		}
	}
	return out
}

func sameStringSet(a, b []string) bool {
	a = append([]string(nil), a...)
	b = append([]string(nil), b...)
	sort.Strings(a)
	sort.Strings(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsString(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

var specLawRow = regexp.MustCompile(`^\| (G-\d\d) \|`)

func parseLawSpec(r *repo) (map[string]lawRow, []finding) {
	f, ok := r.byRel[catalogFile]
	if !ok {
		return nil, []finding{{catalogFile, 0, "GO_TOPS.md not loaded"}}
	}
	text := strings.Join(f.Lines, "\n")
	a := strings.Index(text, "### 8.2 Law catalog")
	b := strings.Index(text, "### 8.3 Law metadata")
	if a < 0 || b < a {
		return nil, []finding{{catalogFile, 0, "GO_TOPS.md has no §8.2 section"}}
	}
	out := map[string]lawRow{}
	var findings []finding
	for i, line := range f.Lines {
		if !specLawRow.MatchString(line) {
			continue
		}
		cells := splitRow(line)
		if len(cells) != 5 {
			continue
		}
		for j := range cells {
			cells[j] = strings.TrimSpace(cells[j])
		}
		reader := strings.ReplaceAll(cells[3], "*", "")
		kind := strings.Fields(reader)[0]
		rk, ok := map[string]string{"an": "analyzer", "tl": "tool", "at": "attested", "tbd": "tbd"}[kind]
		if !ok {
			findings = append(findings, finding{catalogFile, i + 1, fmt.Sprintf("reader cell %q invalid", cells[3])})
			continue
		}
		tier := map[string]int{"analyzer": 0, "tool": 1, "attested": 0, "tbd": 0}[rk]
		if strings.Contains(reader, "tier 3") || strings.Contains(cells[4], "tier 3") {
			tier = 3
		}
		id := cells[0]
		if id == "G-22" || id == "G-35" {
			tier = 3
		}
		v01 := cells[4] == "✓"
		retired := strings.HasPrefix(cells[1], "Retired")
		row := lawRow{
			ID: id, Severity: cells[2], Reader: rk, Tier: tier,
			V01: v01, Retired: retired,
		}
		row.ExactClasses = classTaggedExactClasses(id)
		if v01 && !retired {
			row.NeedLaunch = true
		}
		out[id] = row
	}
	return out, findings
}

func classTaggedExactClasses(id string) []string {
	switch id {
	case "G-10":
		return []string{"tui-app"}
	case "G-24":
		return []string{"library-core"}
	case "G-29":
		return []string{"parser-codec"}
	case "G-41":
		return []string{"binary-cli"}
	default:
		return nil
	}
}

func pathInside(root, rel string) bool {
	abs, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return false
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	relTo, err := filepath.Rel(rootAbs, abs)
	return err == nil && relTo != ".." && !strings.HasPrefix(relTo, ".."+string(filepath.Separator))
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
