package tests

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
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
	Packages    int      // packages with at least one leaf test
	Tests       int      // leaf tests: a test with no subtest, skipped ones included
	Skipped     int      // leaf tests that are skips
	FailedTests []string // "package.Test" of each failed test, in order
	// FailedPackages are packages that failed with no failing test of their
	// own: a build failure, a panic in TestMain, a race after the tests.
	FailedPackages []string
	BuildErrors    []string // compiler output lines, without the "# package" headers
	Seed           int64    // the -test.shuffle seed (see Parse)
	HasSeed        bool
	Stray          string // the first line of the stream that was not JSON
}

// Ran is the number of tests that ran: results that are not skips.
func (r *Result) Ran() int { return r.Tests - r.Skipped }

var seedRE = regexp.MustCompile(`^-test\.shuffle (\d+)$`)

// Parse reads a `go test -json` stream. Each test binary shuffles with a seed
// of its own, printed as "-test.shuffle <seed>"; the run records the seed of
// the first package that failed, and otherwise of the first package that
// printed one, so a failure can be replayed with `-test.shuffle`.
func Parse(stream []byte) (*Result, error) {
	if len(bytes.TrimSpace(stream)) == 0 {
		return nil, errors.New("no output: go test printed nothing")
	}
	p := &parser{
		res:        &Result{},
		pkgSeed:    map[string]int64{},
		results:    map[string]map[string]string{},
		failedTest: map[string]bool{},
	}
	sawJSON, err := p.scan(stream)
	if err != nil {
		return nil, err
	}
	if !sawJSON {
		return nil, fmt.Errorf("output is not go test JSON: %q", p.res.Stray)
	}
	p.countPackages()
	p.collectFailedPackages()
	p.chooseSeed()
	return p.res, nil
}

// parser holds what the events of one stream add up to. Each action of an event
// has a method of its own, and each figure the Result derives from them has one.
type parser struct {
	res         *Result
	pkgSeed     map[string]int64
	seedOrder   []string
	failedOrder []string
	results     map[string]map[string]string // package, test: pass, fail or skip
	failedTest  map[string]bool
}

// scan applies every line of the stream and reports whether any was JSON.
func (p *parser) scan(stream []byte) (bool, error) {
	sc := bufio.NewScanner(bytes.NewReader(stream))
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	sawJSON := false
	for sc.Scan() {
		isJSON, err := p.line(sc.Bytes())
		if err != nil {
			return false, err
		}
		sawJSON = sawJSON || isJSON
	}
	return sawJSON, sc.Err()
}

// line applies one line. A blank line is nothing; a line that is not JSON is
// kept, the first of them, as the stray text of a stream that is not go test's.
func (p *parser) line(raw []byte) (isJSON bool, err error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return false, nil
	}
	var e event
	if err := json.Unmarshal(raw, &e); err != nil {
		if p.res.Stray == "" {
			p.res.Stray = strings.TrimSpace(string(raw))
		}
		return false, nil
	}
	return true, p.apply(e)
}

func (p *parser) apply(e event) error {
	switch e.Action {
	case "build-output":
		p.buildOutput(e)
	case "output":
		return p.shuffleLine(e)
	case "pass", "fail", "skip":
		p.result(e)
	}
	return nil
}

// buildOutput keeps a compiler line, without the "# package" header lines.
func (p *parser) buildOutput(e event) {
	if t := strings.TrimSpace(e.Output); t != "" && !strings.HasPrefix(t, "#") {
		p.res.BuildErrors = append(p.res.BuildErrors, t)
	}
}

// shuffleLine records the first seed a package prints.
func (p *parser) shuffleLine(e event) error {
	m := seedRE.FindStringSubmatch(strings.TrimSpace(e.Output))
	if m == nil {
		return nil
	}
	if _, ok := p.pkgSeed[e.Package]; ok {
		return nil
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return fmt.Errorf("shuffle seed %q: %w", m[1], err)
	}
	p.pkgSeed[e.Package] = n
	p.seedOrder = append(p.seedOrder, e.Package)
	return nil
}

// result records the outcome of a test, or of a whole package when the event has
// no test name.
func (p *parser) result(e event) {
	if e.Test == "" {
		if e.Action == "fail" {
			p.failedOrder = append(p.failedOrder, e.Package)
		}
		return
	}
	if p.results[e.Package] == nil {
		p.results[e.Package] = map[string]string{}
	}
	p.results[e.Package][e.Test] = e.Action
	if name := e.Package + "." + e.Test; e.Action == "fail" && !p.failedTest[name] {
		p.failedTest[name] = true
		p.res.FailedTests = append(p.res.FailedTests, name)
	}
}

// countPackages counts the packages that had at least one leaf test.
func (p *parser) countPackages() {
	for _, tests := range p.results {
		if countLeaves(tests, p.res) > 0 {
			p.res.Packages++
		}
	}
}

// collectFailedPackages lists the packages that failed with no failing test of
// their own, once each, in the order they failed.
func (p *parser) collectFailedPackages() {
	seen := map[string]bool{}
	for _, pkg := range p.failedOrder {
		if seen[pkg] {
			continue
		}
		seen[pkg] = true
		if !hasFailedTest(p.res.FailedTests, pkg) {
			p.res.FailedPackages = append(p.res.FailedPackages, pkg)
		}
	}
}

// chooseSeed takes the first failed package that printed a seed, else the first
// package to print one.
func (p *parser) chooseSeed() {
	for _, pkg := range p.failedOrder {
		if s, ok := p.pkgSeed[pkg]; ok {
			p.res.Seed, p.res.HasSeed = s, true
			return
		}
	}
	if len(p.seedOrder) > 0 {
		p.res.Seed, p.res.HasSeed = p.pkgSeed[p.seedOrder[0]], true
	}
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

// countLeaves adds the leaf tests of one package to res and returns how many
// there are. A leaf is a test that no other test of the package has as its
// parent: a name is a parent when another name starts with it and a "/". A
// parent that only holds skipped subtests is not itself a test that ran, and
// a subtest is counted once, not with its parent.
func countLeaves(tests map[string]string, res *Result) int {
	parents := map[string]bool{}
	for name := range tests {
		for i := 0; i < len(name); i++ {
			if name[i] == '/' {
				parents[name[:i]] = true
			}
		}
	}
	leaves := 0
	for name, action := range tests {
		if parents[name] {
			continue
		}
		leaves++
		res.Tests++
		if action == "skip" {
			res.Skipped++
		}
	}
	return leaves
}
