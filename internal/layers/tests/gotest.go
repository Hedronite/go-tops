package tests

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// event is one object of `go test -json` (test2json, plus the build events of
// the go command).
type event struct {
	Action      string
	Package     string
	Test        string
	Output      string
	ImportPath  string
	FailedBuild string
}

// Result is what the gate keeps of one `go test -json` run.
type Result struct {
	Packages    int      // packages with at least one test result
	Tests       int      // every test result, subtests and skipped ones included
	Skipped     int      // results that are skips
	FailedTests []string // "package.Test" of each failed test, in order
	// FailedPackages are packages that failed with no failing test of their
	// own: a build failure, a panic in TestMain, a race after the tests.
	FailedPackages []string
	Races          []string // packages whose output holds a data race report
	BuildErrors    []string // compiler output lines, without the "# package" headers
	Seed           int64    // the -test.shuffle seed (see Parse)
	HasSeed        bool
	Stray          string // the first line of the stream that was not JSON
}

// Ran is the number of tests that ran: results that are not skips.
func (r *Result) Ran() int { return r.Tests - r.Skipped }

var (
	seedRE = regexp.MustCompile(`^-test\.shuffle (\d+)$`)
	raceRE = "WARNING: DATA RACE"
)

// Parse reads a `go test -json` stream. Each test binary shuffles with a seed
// of its own, printed as "-test.shuffle <seed>"; the run records the seed of
// the first package that failed, and otherwise of the first package that
// printed one, so a failure can be replayed with `-test.shuffle`.
func Parse(stream []byte) (*Result, error) {
	if len(bytes.TrimSpace(stream)) == 0 {
		return nil, errors.New("no output: go test printed nothing")
	}
	res := &Result{}
	pkgSeed := map[string]int64{}
	var seedOrder, failedOrder []string
	hasTests := map[string]bool{}
	failedTest := map[string]bool{}
	raced := map[string]bool{}
	sawJSON := false

	sc := bufio.NewScanner(bytes.NewReader(stream))
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var e event
		if err := json.Unmarshal(line, &e); err != nil {
			if res.Stray == "" {
				res.Stray = strings.TrimSpace(string(line))
			}
			continue
		}
		sawJSON = true
		switch e.Action {
		case "build-output":
			if t := strings.TrimSpace(e.Output); t != "" && !strings.HasPrefix(t, "#") {
				res.BuildErrors = append(res.BuildErrors, t)
			}
		case "output":
			if m := seedRE.FindStringSubmatch(strings.TrimSpace(e.Output)); m != nil {
				if _, ok := pkgSeed[e.Package]; !ok {
					n, err := strconv.ParseInt(m[1], 10, 64)
					if err != nil {
						return nil, fmt.Errorf("shuffle seed %q: %w", m[1], err)
					}
					pkgSeed[e.Package] = n
					seedOrder = append(seedOrder, e.Package)
				}
			}
			if strings.Contains(e.Output, raceRE) && !raced[e.Package] {
				raced[e.Package] = true
				res.Races = append(res.Races, e.Package)
			}
		case "pass", "fail", "skip":
			if e.Test == "" {
				if e.Action == "fail" {
					failedOrder = append(failedOrder, e.Package)
				}
				continue
			}
			res.Tests++
			hasTests[e.Package] = true
			switch e.Action {
			case "skip":
				res.Skipped++
			case "fail":
				name := e.Package + "." + e.Test
				if !failedTest[name] {
					failedTest[name] = true
					res.FailedTests = append(res.FailedTests, name)
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if !sawJSON {
		return nil, fmt.Errorf("output is not go test JSON: %q", res.Stray)
	}
	res.Packages = len(hasTests)

	seen := map[string]bool{}
	for _, p := range failedOrder {
		if seen[p] {
			continue
		}
		seen[p] = true
		if !hasFailedTest(res.FailedTests, p) {
			res.FailedPackages = append(res.FailedPackages, p)
		}
	}
	sort.Strings(res.Races)

	// The seed: the first failed package that printed one, else the first to print one.
	for _, p := range failedOrder {
		if s, ok := pkgSeed[p]; ok {
			res.Seed, res.HasSeed = s, true
			break
		}
	}
	if !res.HasSeed && len(seedOrder) > 0 {
		res.Seed, res.HasSeed = pkgSeed[seedOrder[0]], true
	}
	return res, nil
}

// hasFailedTest reports whether one of the failed tests belongs to package pkg.
func hasFailedTest(failed []string, pkg string) bool {
	for _, f := range failed {
		if strings.HasPrefix(f, pkg+".") {
			return true
		}
	}
	return false
}
