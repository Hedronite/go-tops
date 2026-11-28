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
	var out []Marker
	err := filepath.WalkDir(moduleRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(moduleRoot, path)
		if err != nil {
			return err
		}
		marks, err := markersInFile(path)
		if err != nil {
			return err
		}
		for _, m := range marks {
			out = append(out, Marker{File: filepath.ToSlash(rel), Line: m.line, ID: m.id})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
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
	defer func() { _ = f.Close() }()
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
	wantByLaw := map[string][]Marker{}
	for _, m := range markers {
		wantByLaw[m.ID] = append(wantByLaw[m.ID], m)
	}
	gotByPos := map[string]string{}
	for _, is := range issues {
		id := lawForIssue(is)
		if id == "" {
			continue
		}
		key := is.Pos.Filename + ":" + strconv.Itoa(is.Pos.Line)
		gotByPos[key] = id
	}
	var diffs []FixtureDiff
	for law, wants := range wantByLaw {
		for _, m := range wants {
			key := m.File + ":" + strconv.Itoa(m.Line)
			got, ok := gotByPos[key]
			if !ok {
				diffs = append(diffs, FixtureDiff{Law: law, File: m.File, Line: m.Line, Reason: "marked line has no finding for " + law})
				continue
			}
			if got != law {
				diffs = append(diffs, FixtureDiff{Law: law, File: m.File, Line: m.Line, Reason: fmt.Sprintf("finding is %s, want %s", got, law)})
			}
		}
	}
	for _, is := range issues {
		id := lawForIssue(is)
		if id == "" {
			continue
		}
		line := is.Pos.Line
		file := filepath.ToSlash(is.Pos.Filename)
		text := readLine(moduleRoot, file, line)
		m := wantLawRe.FindStringSubmatch(text)
		if len(m) == 2 && m[1] == id {
			continue
		}
		diffs = append(diffs, FixtureDiff{Law: id, File: file, Line: line, Reason: "unmarked finding for " + id})
	}
	sort.Slice(diffs, func(i, j int) bool {
		if diffs[i].Law != diffs[j].Law {
			return diffs[i].Law < diffs[j].Law
		}
		if diffs[i].File != diffs[j].File {
			return diffs[i].File < diffs[j].File
		}
		return diffs[i].Line < diffs[j].Line
	})
	return diffs, nil
}

func readLine(root, rel string, line int) string {
	f, err := os.Open(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
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
