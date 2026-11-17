package main

import (
	"fmt"
	"regexp"
)

// tops-core markers fence the text the TOPS shared core will take over. Each
// is a comment alone on its line:
//
//	<!-- tops-core:begin NAME -->  ...  <!-- tops-core:end NAME -->
//
// Pairs must balance, must not nest, and NAME comes from this list.
var markerNames = map[string]bool{
	"verdicts": true, "report": true, "enforcement": true, "exemptions": true, "exit-codes": true,
}

const markerNameList = "verdicts, report, enforcement, exemptions, exit-codes"

var (
	markerRe    = regexp.MustCompile(`^\s*<!--\s*tops-core:(begin|end)\s+(\S+?)\s*-->\s*$`)
	markerAnyRe = regexp.MustCompile(`<!--\s*tops-core:`)
)

func checkMarkers(r *repo) checkResult {
	res := checkResult{}
	pairs := 0
	for _, f := range r.Files {
		openName, openLine := "", 0
		f.prose(func(n int, line string) {
			m := markerRe.FindStringSubmatch(line)
			if m == nil {
				if markerAnyRe.MatchString(line) {
					res.Findings = append(res.Findings, finding{f.Rel, n, "malformed tops-core marker: want <!-- tops-core:begin|end NAME --> alone on its line"})
				}
				return
			}
			kind, name := m[1], m[2]
			add := func(format string, a ...any) {
				res.Findings = append(res.Findings, finding{f.Rel, n, fmt.Sprintf(format, a...)})
			}
			if !markerNames[name] {
				add("tops-core %s names %q; allowed: %s", kind, name, markerNameList)
				return
			}
			switch {
			case kind == "begin" && openName != "":
				add("tops-core:begin %s inside %s, which began at line %d", name, openName, openLine)
			case kind == "begin":
				openName, openLine = name, n
			case openName == "":
				add("tops-core:end %s has no begin", name)
			case openName != name:
				add("tops-core:end %s closes %s, which began at line %d", name, openName, openLine)
				openName = ""
			default:
				pairs++
				openName = ""
			}
		})
		if openName != "" {
			res.Findings = append(res.Findings, finding{f.Rel, openLine, fmt.Sprintf("tops-core:begin %s is never closed", openName)})
		}
	}
	res.Summary = fmt.Sprintf("%d balanced pairs", pairs)
	return res
}
