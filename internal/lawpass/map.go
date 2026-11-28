// Package lawpass runs the isolated delegated law lint pass.
package lawpass

import (
	"regexp"
	"strings"
)

var gosecRule = regexp.MustCompile(`\b(G401|G404|G501|G505)\b`)

// lawForIssue maps one lint finding to a law id, or "" when unmapped.
func lawForIssue(is LintIssue) string {
	switch is.FromLinter {
	case "errcheck":
		return "G-12"
	case "errorlint":
		return "G-11"
	case "noctx", "contextcheck":
		return "G-16"
	case "bodyclose", "sqlclosecheck", "rowserrcheck":
		return "G-18"
	case "gosec":
		if gosecRule.MatchString(is.Text) {
			return "G-37"
		}
		return ""
	case "govet":
		low := strings.ToLower(is.Text)
		if strings.Contains(low, "waitgroup") {
			return "G-02"
		}
		if strings.Contains(low, "copylocks") {
			return "G-07"
		}
		return ""
	default:
		return ""
	}
}

// CountByLaw counts issues per law id.
func CountByLaw(issues []LintIssue) map[string]int {
	out := map[string]int{}
	for _, is := range issues {
		if id := lawForIssue(is); id != "" {
			out[id]++
		}
	}
	return out
}

// CountsByLawFile counts mapped issues per law id and module-relative file path.
func CountsByLawFile(issues []LintIssue, rel func(filename string) string) map[string]map[string]int {
	out := map[string]map[string]int{}
	for _, is := range issues {
		id := lawForIssue(is)
		if id == "" {
			continue
		}
		bumpLawFile(out, id, rel(is.Pos.Filename))
	}
	return out
}

func bumpLawFile(out map[string]map[string]int, law, file string) {
	if out[law] == nil {
		out[law] = map[string]int{}
	}
	out[law][file]++
}

// toolLawIDs are tier-1 laws judged from lint-laws.json.
var toolLawIDs = []string{"G-02", "G-07", "G-11", "G-12", "G-16", "G-18", "G-37"}

// IsToolLaw reports whether id is judged from lint-laws.json.
func IsToolLaw(id string) bool {
	for _, x := range toolLawIDs {
		if x == id {
			return true
		}
	}
	return false
}
