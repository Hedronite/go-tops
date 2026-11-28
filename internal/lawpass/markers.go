package lawpass

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var wantLawRe = regexp.MustCompile(`//\s*want\s+(G-\d{2})\b`)

// ToolLawIDs are tier-1 laws proved by testdata/lints/ markers.
var ToolLawIDs = []string{"G-02", "G-07", "G-11", "G-12", "G-16", "G-18", "G-37"}

// LawForIssue maps one lint finding to a catalog law id.
func LawForIssue(is LintIssue) string { return lawForIssue(is) }

// Marker is a // want G-NN line in a fixture module.
type Marker struct {
	File string
	Line int
	ID   string
}

// ScanMarkers walks moduleRoot for // want G-NN markers in .go files.
func ScanMarkers(moduleRoot string) ([]Marker, error) {
	files, err := listFixtureGoFiles(moduleRoot)
	if err != nil {
		return nil, err
	}
	var out []Marker
	for _, rel := range files {
		marks, err := markersInFile(filepath.Join(moduleRoot, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		for _, m := range marks {
			out = append(out, Marker{File: rel, Line: m.line, ID: m.id})
		}
	}
	sortMarkers(out)
	return out, nil
}

func listFixtureGoFiles(moduleRoot string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(moduleRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(moduleRoot, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	return files, err
}

func markerOrderKey(m Marker) string {
	return fmt.Sprintf("%s\x00%010d\x00%s", m.File, m.Line, m.ID)
}

func sortMarkers(out []Marker) {
	sort.Slice(out, func(i, j int) bool {
		return markerOrderKey(out[i]) < markerOrderKey(out[j])
	})
}

type fileMarker struct {
	line int
	id   string
}

func markersInFile(path string) ([]fileMarker, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck // read-only scan
	var out []fileMarker
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		if m := wantLawRe.FindStringSubmatch(sc.Text()); len(m) == 2 {
			out = append(out, fileMarker{line: line, id: m[1]})
		}
	}
	return out, sc.Err()
}

// FixtureDiff is a marker or finding that did not match.
type FixtureDiff struct {
	Law    string
	File   string
	Line   int
	Reason string
}

// DiffFixtureMarkers compares lint issues to // want markers in moduleRoot.
func DiffFixtureMarkers(moduleRoot string, issues []LintIssue) ([]FixtureDiff, error) {
	markers, err := ScanMarkers(moduleRoot)
	if err != nil {
		return nil, err
	}
	wantByLaw := groupMarkersByLaw(markers)
	gotByPos := indexIssuesByPos(issues)
	out := diffMarkedFindings(wantByLaw, gotByPos)
	out = append(out, diffUnmarkedFindings(moduleRoot, issues)...)
	sortFixtureDiffs(out)
	return out, nil
}

func groupMarkersByLaw(markers []Marker) map[string][]Marker {
	out := map[string][]Marker{}
	for _, m := range markers {
		out[m.ID] = append(out[m.ID], m)
	}
	return out
}

func indexIssuesByPos(issues []LintIssue) map[string]string {
	out := map[string]string{}
	for _, is := range issues {
		id := lawForIssue(is)
		if id == "" {
			continue
		}
		key := is.Pos.Filename + ":" + strconv.Itoa(is.Pos.Line)
		out[key] = id
	}
	return out
}

func diffMarkedFindings(wantByLaw map[string][]Marker, gotByPos map[string]string) []FixtureDiff {
	var out []FixtureDiff
	for law, wants := range wantByLaw {
		for _, m := range wants {
			key := m.File + ":" + strconv.Itoa(m.Line)
			got, ok := gotByPos[key]
			if !ok {
				out = append(out, FixtureDiff{Law: law, File: m.File, Line: m.Line, Reason: "marked line has no finding for " + law})
				continue
			}
			if got != law {
				out = append(out, FixtureDiff{Law: law, File: m.File, Line: m.Line, Reason: fmt.Sprintf("finding is %s, want %s", got, law)})
			}
		}
	}
	return out
}

func diffUnmarkedFindings(moduleRoot string, issues []LintIssue) []FixtureDiff {
	var out []FixtureDiff
	for _, is := range issues {
		id := lawForIssue(is)
		if id == "" {
			continue
		}
		line := is.Pos.Line
		file := filepath.ToSlash(is.Pos.Filename)
		if lineHasWantLaw(moduleRoot, file, line, id) {
			continue
		}
		out = append(out, FixtureDiff{Law: id, File: file, Line: line, Reason: "unmarked finding for " + id})
	}
	return out
}

func lineHasWantLaw(moduleRoot, file string, line int, id string) bool {
	text := readLine(moduleRoot, file, line)
	m := wantLawRe.FindStringSubmatch(text)
	return len(m) == 2 && m[1] == id
}

func fixtureDiffOrderKey(d FixtureDiff) string {
	return fmt.Sprintf("%s\x00%s\x00%010d", d.Law, d.File, d.Line)
}

func sortFixtureDiffs(out []FixtureDiff) {
	sort.Slice(out, func(i, j int) bool {
		return fixtureDiffOrderKey(out[i]) < fixtureDiffOrderKey(out[j])
	})
}

func readLine(root, rel string, line int) string {
	f, err := os.Open(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return ""
	}
	defer f.Close() //nolint:errcheck // read-only scan
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		if n == line {
			return sc.Text()
		}
	}
	return ""
}

// RequiredToolLawMarkers checks every delegated law has exactly one marker.
func RequiredToolLawMarkers(markers []Marker) []string {
	counts := map[string]int{}
	for _, m := range markers {
		counts[m.ID]++
	}
	var missing []string
	for _, id := range ToolLawIDs {
		if counts[id] != 1 {
			missing = append(missing, fmt.Sprintf("%s: want 1 marker, have %d", id, counts[id]))
		}
	}
	return missing
}
