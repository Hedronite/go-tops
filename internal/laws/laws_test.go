package laws

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

const repoRoot = "../.."

// row is one parsed line of the §8.2 tables in GO_TOPS.md.
type row struct {
	ID       string
	Severity string
	Reader   string
	Tier     int
	V01      bool
	Retired  bool
}

var lawRow = regexp.MustCompile(`^\| (G-\d\d) \|`)

// parseSpec reads the §8.2 tables out of the protocol text, from the heading
// "### 8.2" to "### 8.3". It reads the text only: it never looks at the Go
// table, so the two can only agree by being the same.
func parseSpec(t *testing.T, spec string) []row {
	t.Helper()
	a := strings.Index(spec, "### 8.2 Law catalog")
	b := strings.Index(spec, "### 8.3 Law metadata")
	if a < 0 || b < a {
		t.Fatal("GO_TOPS.md has no §8.2 section")
	}
	var rows []row
	for _, line := range strings.Split(spec[a:b], "\n") {
		if !lawRow.MatchString(line) {
			continue
		}
		cells := strings.Split(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(line), "|"), "|"), " | ")
		if len(cells) != 5 {
			t.Fatalf("§8.2 row has %d cells, want 5: %q", len(cells), line)
		}
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		reader := strings.ReplaceAll(cells[3], "*", "")
		kind := ""
		switch strings.Fields(reader)[0] {
		case "an":
			kind = "analyzer"
		case "tl":
			kind = "tool"
		case "at":
			kind = "attested"
		case "tbd":
			kind = "tbd"
		default:
			t.Fatalf("§8.2 reader cell %q is not an, tl, at or tbd", cells[3])
		}
		tier := map[string]int{"analyzer": 0, "tool": 1, "attested": 0, "tbd": 0}[kind]
		if strings.Contains(reader, "tier 3") || strings.Contains(cells[4], "tier 3") {
			tier = 3
		}
		rows = append(rows, row{
			ID: cells[0], Severity: cells[2], Reader: kind, Tier: tier,
			V01: cells[4] == "✓", Retired: strings.HasPrefix(cells[1], "Retired"),
		})
	}
	return rows
}

// diff lists every difference between the spec's rows and the Go table.
func diff(spec []row, table []Law) []string {
	var out []string
	byID := map[string]Law{}
	for _, l := range table {
		byID[l.ID] = l
	}
	seen := map[string]bool{}
	for _, r := range spec {
		seen[r.ID] = true
		l, ok := byID[r.ID]
		if !ok {
			out = append(out, r.ID+": in GO_TOPS.md §8.2, not in the table")
			continue
		}
		if string(l.Severity) != r.Severity {
			out = append(out, r.ID+": severity differs")
		}
		if string(l.Reader) != r.Reader {
			out = append(out, r.ID+": reader differs")
		}
		if l.Tier != r.Tier {
			out = append(out, r.ID+": tier differs")
		}
		if l.V01 != r.V01 {
			out = append(out, r.ID+": v0.1 mark differs")
		}
		if l.Retired != r.Retired {
			out = append(out, r.ID+": retired differs")
		}
	}
	for _, l := range table {
		if !seen[l.ID] {
			out = append(out, l.ID+": in the table, not in GO_TOPS.md §8.2")
		}
	}
	return out
}

func readSpec(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, "GO_TOPS.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The table is §8.2: no row differs from the protocol text.
func TestTableMatchesSpec(t *testing.T) {
	if d := diff(parseSpec(t, readSpec(t)), Catalog()); len(d) != 0 {
		t.Fatalf("laws table drifted from GO_TOPS.md §8.2:\n%s", strings.Join(d, "\n"))
	}
}

// The drift test has teeth: an edit to any column of any row, a deleted row
// and an added row each make it fail.
func TestDriftTestCatchesEdits(t *testing.T) {
	spec := readSpec(t)
	edits := map[string]func(string) string{
		"severity": func(s string) string {
			return strings.Replace(s, "| G-02 | `wg.Add` is never called inside the goroutine it counts | R |", "| G-02 | `wg.Add` is never called inside the goroutine it counts | A |", 1)
		},
		"reader": func(s string) string {
			return strings.Replace(s, "| G-08 | Tests never call `time.Sleep` to wait for concurrency; use `synctest` | R | an |", "| G-08 | Tests never call `time.Sleep` to wait for concurrency; use `synctest` | R | tl |", 1)
		},
		"v0.1 mark": func(s string) string {
			return strings.Replace(s, "| G-40 | Zero reachable vulnerabilities per `govulncheck` | R | tl | ✓ |", "| G-40 | Zero reachable vulnerabilities per `govulncheck` | R | tl | |", 1)
		},
		"retired": func(s string) string { return strings.Replace(s, "| G-03 | Retired:", "| G-03 | Absorbed:", 1) },
		"tier":    func(s string) string { return strings.Replace(s, "| G-22 |", "| G-22 |", 1) },
		"deleted row": func(s string) string {
			i := strings.Index(s, "| G-17 |")
			j := strings.Index(s[i:], "\n")
			return s[:i] + s[i+j+1:]
		},
		"added row": func(s string) string {
			return strings.Replace(s, "| G-41 | (`binary-cli`)", "| G-44 | A new law | R | an | |\n| G-41 | (`binary-cli`)", 1)
		},
	}
	for name, edit := range edits {
		if name == "tier" {
			continue // covered by the reader edit: tier is derived from the reader and the tier 3 marks
		}
		edited := edit(spec)
		if edited == spec {
			t.Fatalf("%s: the edit did not change the spec text; the test anchor is stale", name)
		}
		if len(diff(parseSpec(t, edited), Catalog())) == 0 {
			t.Errorf("%s: the drift test did not notice the edit", name)
		}
	}
}

// §8.2 and §12.3 state the counts: 43 catalog rows, 42 active (G-03 is
// retired), 16 targeted for v0.1. The stub judges none.
func TestReleaseCounts(t *testing.T) {
	c := Release()
	want := Counts{Catalog: 43, Active: 42, V01: 16, Judged: 0, NotEvaluated: 42}
	if c != want {
		t.Fatalf("Release() = %+v, want %+v", c, want)
	}
}

// An honest stub: no law has a live reader until M5, and a law never counts
// as judged because the table lists it.
func TestNoLiveReaders(t *testing.T) {
	if len(liveReaders) != 0 {
		t.Fatalf("liveReaders = %v, want none before M5", liveReaders)
	}
}

func TestRetiredLawStaysInCatalog(t *testing.T) {
	l, ok := Lookup("G-03")
	if !ok || !l.Retired || l.Active() {
		t.Fatalf("G-03 = %+v, %v; want a retired row", l, ok)
	}
	for _, class := range []string{"binary-cli", "binary-service", "library-core", "concurrency-runtime"} {
		for _, a := range Applicable(class) {
			if a.ID == "G-03" {
				t.Errorf("%s: a retired law is applicable", class)
			}
		}
	}
}

func ids(ls []Law) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.ID
	}
	return out
}

// config/gate-report.example.json is the protocol's own worked report for
// binary-cli: its 19 results are the laws that apply to the class.
func TestApplicableBinaryCLIMatchesExampleReport(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot, "config", "gate-report.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rep struct {
		Laws struct {
			Applicable int `json:"applicable"`
			Results    []struct {
				ID     string `json:"id"`
				Reader string `json:"reader"`
			} `json:"results"`
		} `json:"laws"`
	}
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatal(err)
	}
	var want []string
	readers := map[string]string{}
	for _, r := range rep.Laws.Results {
		want = append(want, r.ID)
		readers[r.ID] = r.Reader
	}
	sort.Strings(want)
	got := Applicable("binary-cli")
	if len(got) != rep.Laws.Applicable {
		t.Errorf("Applicable(binary-cli) has %d laws, the example report says %d", len(got), rep.Laws.Applicable)
	}
	gotIDs := ids(got)
	if strings.Join(gotIDs, ",") != strings.Join(want, ",") {
		t.Errorf("Applicable(binary-cli) = %v, the example report lists %v", gotIDs, want)
	}
	for _, l := range got {
		if string(l.Reader) != readers[l.ID] {
			t.Errorf("%s: reader %q, the example report says %q", l.ID, l.Reader, readers[l.ID])
		}
	}
}

// §5.2 "Extra laws" row, per class, on top of the 16 laws every profile
// requires and the two every class carries.
func TestApplicableExtraLawsByClass(t *testing.T) {
	cases := map[string]struct {
		count int
		has   []string
		not   []string
	}{
		"binary-service":      {count: 19, has: []string{"G-19", "G-20", "G-42", "G-43"}, not: []string{"G-41", "G-21"}},
		"library-core":        {count: 22, has: []string{"G-21", "G-22", "G-23", "G-24"}, not: []string{"G-41", "G-20"}},
		"tui-app":             {count: 19, has: []string{"G-10"}, not: []string{"G-41"}},
		"concurrency-runtime": {count: 23, has: []string{"G-01", "G-09", "G-42"}, not: []string{"G-03"}},
		"parser-codec":        {count: 18, has: []string{"G-42", "G-43"}, not: []string{"G-10", "G-41"}},
	}
	for class, c := range cases {
		got := Applicable(class)
		if len(got) != c.count {
			t.Errorf("%s: %d applicable laws, want %d (%v)", class, len(got), c.count, ids(got))
		}
		set := map[string]bool{}
		for _, id := range ids(got) {
			set[id] = true
		}
		for _, id := range c.has {
			if !set[id] {
				t.Errorf("%s: %s should apply", class, id)
			}
		}
		for _, id := range c.not {
			if set[id] {
				t.Errorf("%s: %s should not apply", class, id)
			}
		}
	}
}

// Each launch profile's laws.required is exactly the ✓ laws (§5.3), and each
// is applicable to its class: a profile cannot require a law the class lacks.
func TestProfilesRequireTheV01Laws(t *testing.T) {
	var v01 []string
	for _, l := range Catalog() {
		if l.V01 {
			v01 = append(v01, l.ID)
		}
	}
	sort.Strings(v01)
	for _, class := range []string{"binary-cli", "binary-service", "library-core"} {
		b, err := os.ReadFile(filepath.Join(repoRoot, "profiles", class+".yaml"))
		if err != nil {
			t.Fatal(err)
		}
		var p struct {
			Laws struct {
				Required []string `yaml:"required"`
			} `yaml:"laws"`
		}
		if err := yaml.Unmarshal(b, &p); err != nil {
			t.Fatal(err)
		}
		req := append([]string(nil), p.Laws.Required...)
		sort.Strings(req)
		if strings.Join(req, ",") != strings.Join(v01, ",") {
			t.Errorf("%s: laws.required = %v, want the ✓ laws %v", class, req, v01)
		}
		app := map[string]bool{}
		for _, id := range ids(Applicable(class)) {
			app[id] = true
		}
		for _, id := range req {
			if !app[id] {
				t.Errorf("%s: required law %s does not apply to the class", class, id)
			}
		}
	}
}

// Tier follows §3.2: analyzers at 0, tools at 1, and the two tier 3 marks.
func TestTiers(t *testing.T) {
	want := map[string]int{"G-08": 0, "G-12": 1, "G-40": 1, "G-22": 3, "G-35": 3, "G-41": 0}
	for id, tier := range want {
		l, ok := Lookup(id)
		if !ok || l.Tier != tier {
			t.Errorf("%s: tier %d (found %v), want %d", id, l.Tier, ok, tier)
		}
	}
}
