package main

import (
	"fmt"
	"regexp"
	"strings"
)

// exampleSections are the sections of GO_TOPS.md in which every fenced YAML or
// JSON block must be a go-tops:example, so none can drift from its file or
// skip the schema unchecked. A section runs from its heading to the next
// heading of the same or a higher level, so its subsections are included.
var exampleSections = []string{"5.3", "9", "12.3"}

var headingNumRe = regexp.MustCompile(`^(\d+(?:\.\d+)*)\.?(?:\s|$)`)

// checkUnmarkedBlocks fails each yaml, yml or json block inside an example
// section that no go-tops:example marker precedes (marked holds, per file, the
// opening line of every marked block), and fails a missing section, so
// renumbering the protocol cannot turn the check off.
func (r *repo) checkUnmarkedBlocks(marked map[string]map[int]bool) []finding {
	f, ok := r.byRel[catalogFile]
	if !ok {
		return nil
	}
	var out []finding
	for _, num := range exampleSections {
		from, to, found := sectionLines(f, num)
		if !found {
			out = append(out, finding{f.Rel, 0, fmt.Sprintf("section %s not found: its YAML and JSON blocks cannot be checked", num)})
			continue
		}
		for _, fe := range f.Fences {
			if fe.Open > from && fe.Open < to && isDataFence(fe.Info) && !marked[f.Rel][fe.Open] {
				out = append(out, finding{f.Rel, fe.Open, fmt.Sprintf("fenced %s block in section %s has no go-tops:example marker", infoLang(fe.Info), num)})
			}
		}
	}
	return out
}

// sectionLines returns the heading line of numbered section num and the line
// of the heading that ends it (len(Lines)+1 at the end of the file).
func sectionLines(f *mdFile, num string) (from, to int, found bool) {
	for i, h := range f.Headings {
		if m := headingNumRe.FindStringSubmatch(h.Text); m == nil || m[1] != num {
			continue
		}
		to = len(f.Lines) + 1
		for _, next := range f.Headings[i+1:] {
			if next.Level <= h.Level {
				to = next.Line
				break
			}
		}
		return h.Line, to, true
	}
	return 0, 0, false
}

func infoLang(info string) string {
	if fields := strings.Fields(info); len(fields) > 0 {
		return strings.ToLower(fields[0])
	}
	return ""
}

func isDataFence(info string) bool {
	switch infoLang(info) {
	case "yaml", "yml", "json":
		return true
	}
	return false
}
