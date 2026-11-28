// Command docs is the Go-TOPS docs checker. It reads the Markdown, the lint
// config and the docs workflow under a repository root and fails when they
// disagree with each other: broken relative links and anchors, counts that do
// not match the law catalog, example blocks that drifted from their files or
// do not validate, unbalanced tops-core markers, a lint config that disables
// a rule without a reason, and a workflow that is not pinned and read-only.
//
// Usage: go run . -root ../.. [-checks links,counts,...] [--allow-vacuous]
//
// Exit codes: 0 every selected check passed, 1 a check failed (or was vacuous
// without --allow-vacuous), 2 bad usage or an unreadable root.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// finding is one defect, located by file and (when known) 1-based line.
type finding struct {
	File string
	Line int
	Msg  string
}

func (f finding) String() string {
	switch {
	case f.File == "":
		return f.Msg
	case f.Line == 0:
		return f.File + ": " + f.Msg
	}
	return fmt.Sprintf("%s:%d: %s", f.File, f.Line, f.Msg)
}

// checkResult is what one check reports. A check with no findings passes,
// unless Vacuous is set: it found nothing to check.
type checkResult struct {
	Name     string
	Summary  string
	Findings []finding
	Vacuous  bool
}

type checkFunc func(r *repo) checkResult

// checks lists every check in run order. "parse" always runs.
var checks = []struct {
	name string
	run  checkFunc
}{
	{"links", checkLinks},
	{"counts", checkCounts},
	{"examples", checkExamples},
	{"markers", checkMarkers},
	{"lintconfig", checkLintConfig},
	{"workflow", checkWorkflow},
	{"version", checkVersion},
	{"laws", checkLaws},
}

func checkNames() []string {
	names := make([]string, len(checks))
	for i, c := range checks {
		names[i] = c.name
	}
	return names
}

func selectChecks(list string) (map[string]bool, error) {
	known := map[string]bool{}
	for _, n := range checkNames() {
		known[n] = true
	}
	sel := map[string]bool{}
	for _, n := range strings.Split(list, ",") {
		n = strings.TrimSpace(n)
		if n == "parse" {
			continue // parse always runs; naming it is allowed
		}
		if !known[n] {
			return nil, fmt.Errorf("unknown check %q (known: %s)", n, strings.Join(checkNames(), ", "))
		}
		sel[n] = true
	}
	return sel, nil
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("docs", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository root")
	list := fs.String("checks", strings.Join(checkNames(), ","), "comma-separated checks to run")
	allowVacuous := fs.Bool("allow-vacuous", false, "a check that found nothing to check passes instead of failing")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	sel, err := selectChecks(*list)
	if err != nil {
		fmt.Fprintln(stderr, "docs:", err)
		return 2
	}
	r, parse, err := loadRepo(*root)
	if err != nil {
		fmt.Fprintln(stderr, "docs:", err)
		return 2
	}
	results := []checkResult{parse}
	for _, c := range checks {
		if sel[c.name] {
			res := c.run(r)
			res.Name = c.name
			results = append(results, res)
		}
	}
	return report(stdout, results, *allowVacuous)
}

// report prints each result and the verdict, and returns the exit code.
func report(w io.Writer, results []checkResult, allowVacuous bool) int {
	failed, vacuous := 0, 0
	for _, res := range results {
		switch {
		case len(res.Findings) > 0:
			failed++
			fmt.Fprintf(w, "FAIL     %-10s %d finding(s)\n", res.Name, len(res.Findings))
			sort.SliceStable(res.Findings, func(i, j int) bool { return res.Findings[i].File < res.Findings[j].File })
			for _, f := range res.Findings {
				fmt.Fprintf(w, "           %s\n", strings.ReplaceAll(f.String(), "\n", "\n             "))
			}
		case res.Vacuous && !allowVacuous:
			failed++
			vacuous++
			fmt.Fprintf(w, "VACUOUS  %-10s %s (fails without --allow-vacuous)\n", res.Name, res.Summary)
		case res.Vacuous:
			vacuous++
			fmt.Fprintf(w, "VACUOUS  %-10s %s (allowed)\n", res.Name, res.Summary)
		default:
			fmt.Fprintf(w, "PASS     %-10s %s\n", res.Name, res.Summary)
		}
	}
	switch {
	case failed > 0:
		fmt.Fprintf(w, "docs: FAIL (%d of %d checks)\n", failed, len(results))
		return 1
	case vacuous > 0:
		fmt.Fprintf(w, "docs: PASS with %d vacuous check(s) allowed\n", vacuous)
	default:
		fmt.Fprintf(w, "docs: PASS (%d checks)\n", len(results))
	}
	return 0
}
