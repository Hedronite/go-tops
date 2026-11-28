package lawpass

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFixture(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIndexIssuesByPosSkipsUnmapped(t *testing.T) {
	t.Parallel()
	got := indexIssuesByPos([]LintIssue{{FromLinter: "vet", Text: "x", Pos: struct {
		Filename string
		Line     int
	}{Filename: "a.go", Line: 1}}})
	if len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestIndexIssuesByPosMapsLaw(t *testing.T) {
	t.Parallel()
	got := indexIssuesByPos([]LintIssue{{
		FromLinter: "errcheck",
		Text:       "unchecked",
		Pos: struct {
			Filename string
			Line     int
		}{Filename: "pkg/a.go", Line: 3},
	}})
	if got["pkg/a.go:3"] != "G-12" {
		t.Fatalf("got %v", got)
	}
}

func TestDiffMarkedFindingMissed(t *testing.T) {
	t.Parallel()
	want := map[string][]Marker{"G-12": {{File: "a.go", Line: 2, ID: "G-12"}}}
	d := diffMarkedFindings(want, map[string]string{})
	if len(d) != 1 || d[0].Reason == "" {
		t.Fatalf("diff %v", d)
	}
}

func TestDiffMarkedWrongLaw(t *testing.T) {
	t.Parallel()
	want := map[string][]Marker{"G-12": {{File: "a.go", Line: 2, ID: "G-12"}}}
	got := map[string]string{"a.go:2": "G-16"}
	d := diffMarkedFindings(want, got)
	if len(d) != 1 || d[0].Law != "G-12" {
		t.Fatalf("diff %v", d)
	}
}

func TestDiffUnmarkedFinding(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "a.go", "package p\n\nfunc f() {}\n")
	issues := []LintIssue{{FromLinter: "errcheck", Text: "x", Pos: struct {
		Filename string
		Line     int
	}{Filename: "a.go", Line: 3}}}
	d := diffUnmarkedFindings(dir, issues)
	if len(d) != 1 || d[0].Law != "G-12" {
		t.Fatalf("diff %v", d)
	}
}

func TestLineHasWantLaw(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "a.go", "package p\nvar x = 1 // want G-12\n")
	if !lineHasWantLaw(dir, "a.go", 2, "G-12") {
		t.Fatal("want match")
	}
	if lineHasWantLaw(dir, "a.go", 2, "G-16") {
		t.Fatal("want mismatch")
	}
}

func TestScanMarkersIgnoresTests(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "a.go", "package p\n// want G-02\n")
	writeFixture(t, dir, "a_test.go", "package p\n// want G-99\n")
	m, err := ScanMarkers(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 || m[0].ID != "G-02" {
		t.Fatalf("markers %v", m)
	}
}

func TestRequiredToolLawMarkersCounts(t *testing.T) {
	t.Parallel()
	miss := RequiredToolLawMarkers([]Marker{{ID: "G-02"}})
	if len(miss) != len(ToolLawIDs)-1 {
		t.Fatalf("miss %v", miss)
	}
}

func TestReadLineMissingFile(t *testing.T) {
	t.Parallel()
	if s := readLine(t.TempDir(), "missing.go", 1); s != "" {
		t.Fatalf("got %q", s)
	}
}

func TestDiffFixtureMarkersIntegration(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "a.go", "package p\nos.Remove(\"x\") // want G-12\n")
	issues := []LintIssue{{FromLinter: "errcheck", Text: "x", Pos: struct {
		Filename string
		Line     int
	}{Filename: "a.go", Line: 2}}}
	d, err := DiffFixtureMarkers(dir, issues)
	if err != nil {
		t.Fatal(err)
	}
	if len(d) != 0 {
		t.Fatalf("diff %v", d)
	}
}

func TestDiffFixtureMarkersRejectsMissingRoot(t *testing.T) {
	t.Parallel()
	_, err := DiffFixtureMarkers(filepath.Join(t.TempDir(), "missing"), nil)
	if err == nil {
		t.Fatal("want error from ScanMarkers walk")
	}
}

func TestDiffMarkedFindingsMatch(t *testing.T) {
	t.Parallel()
	want := map[string][]Marker{"G-12": {{File: "a.go", Line: 2, ID: "G-12"}}}
	got := map[string]string{"a.go:2": "G-12"}
	if d := diffMarkedFindings(want, got); len(d) != 0 {
		t.Fatalf("diff %v", d)
	}
}

func TestMarkersInFileCollectsWants(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	writeFixture(t, dir, "a.go", "package p\n// want G-02\n// want G-07\n")
	got, err := markersInFile(path)
	if err != nil || len(got) != 2 || got[0].id != "G-02" || got[0].line != 2 || got[1].id != "G-07" || got[1].line != 3 {
		t.Fatalf("markers %v err=%v", got, err)
	}
}

func TestMarkerOrderKeyStrictLess(t *testing.T) {
	t.Parallel()
	if markerOrderKey(Marker{File: "a.go", Line: 2, ID: "G-02"}) <= markerOrderKey(Marker{File: "a.go", Line: 1, ID: "G-02"}) {
		t.Fatal("line order")
	}
	if markerOrderKey(Marker{File: "b.go", Line: 1, ID: "G-02"}) <= markerOrderKey(Marker{File: "a.go", Line: 9, ID: "G-02"}) {
		t.Fatal("file order")
	}
}

func TestSortMarkersNumericLineOrder(t *testing.T) {
	out := []Marker{
		{File: "a.go", Line: 10, ID: "G-02"},
		{File: "a.go", Line: 2, ID: "G-07"},
	}
	sortMarkers(out)
	if out[0].Line != 2 || out[1].Line != 10 {
		t.Fatalf("order %v", out)
	}
}

func TestFixtureDiffOrderKeyStrictLess(t *testing.T) {
	t.Parallel()
	a := FixtureDiff{Law: "G-12", File: "a.go", Line: 2}
	b := FixtureDiff{Law: "G-12", File: "a.go", Line: 1}
	if fixtureDiffOrderKey(a) <= fixtureDiffOrderKey(b) {
		t.Fatal("line order")
	}
	c := FixtureDiff{Law: "G-16", File: "a.go", Line: 1}
	if fixtureDiffOrderKey(c) <= fixtureDiffOrderKey(b) {
		t.Fatal("law order")
	}
	if fixtureDiffOrderKey(FixtureDiff{Law: "G-12", File: "a.go", Line: 10}) <= fixtureDiffOrderKey(FixtureDiff{Law: "G-12", File: "a.go", Line: 2}) {
		t.Fatal("numeric line order")
	}
}

func TestSortMarkersOrdersByFileThenLine(t *testing.T) {
	t.Parallel()
	out := []Marker{
		{File: "b.go", Line: 1, ID: "G-02"},
		{File: "a.go", Line: 5, ID: "G-07"},
		{File: "a.go", Line: 2, ID: "G-12"},
	}
	sortMarkers(out)
	if out[0].File != "a.go" || out[0].Line != 2 || out[1].Line != 5 || out[2].File != "b.go" {
		t.Fatalf("order %v", out)
	}
}

func TestSortFixtureDiffsOrders(t *testing.T) {
	t.Parallel()
	out := []FixtureDiff{
		{Law: "G-16", File: "b.go", Line: 1},
		{Law: "G-12", File: "a.go", Line: 2},
		{Law: "G-12", File: "a.go", Line: 1},
	}
	sortFixtureDiffs(out)
	if out[0].Law != "G-12" || out[0].Line != 1 || out[1].Line != 2 || out[2].Law != "G-16" {
		t.Fatalf("order %v", out)
	}
}

func TestDiffUnmarkedSkipsMarkedLine(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "a.go", "package p\nos.Remove(\"x\") // want G-12\n")
	issues := []LintIssue{{FromLinter: "errcheck", Text: "x", Pos: struct {
		Filename string
		Line     int
	}{Filename: "a.go", Line: 2}}}
	if d := diffUnmarkedFindings(dir, issues); len(d) != 0 {
		t.Fatalf("diff %v", d)
	}
}

func TestScanMarkersNestedFile(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "pkg")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, sub, "a.go", "package pkg\n// want G-18\n")
	m, err := ScanMarkers(dir)
	if err != nil || len(m) != 1 || m[0].File != "pkg/a.go" {
		t.Fatalf("markers %v err=%v", m, err)
	}
}

func TestDiffFixtureMarkersDiffsSorted(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "a.go", "package p\n\nfunc f() {}\n")
	issues := []LintIssue{
		{FromLinter: "errcheck", Text: "x", Pos: struct {
			Filename string
			Line     int
		}{Filename: "a.go", Line: 3}},
		{FromLinter: "govet", Text: "copylocks", Pos: struct {
			Filename string
			Line     int
		}{Filename: "a.go", Line: 3}},
	}
	d, err := DiffFixtureMarkers(dir, issues)
	if err != nil || len(d) < 2 {
		t.Fatalf("diff %v err=%v", d, err)
	}
	for i := 1; i < len(d); i++ {
		if fixtureDiffOrderKey(d[i-1]) >= fixtureDiffOrderKey(d[i]) {
			t.Fatalf("unsorted at %d: %v", i, d)
		}
	}
}

func TestGroupMarkersByLaw(t *testing.T) {
	t.Parallel()
	m := []Marker{{ID: "G-02", File: "a.go", Line: 1}, {ID: "G-02", File: "b.go", Line: 1}}
	got := groupMarkersByLaw(m)
	if len(got["G-02"]) != 2 {
		t.Fatalf("got %v", got)
	}
}
