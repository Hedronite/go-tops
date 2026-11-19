// Package exempt applies the exemption records of GO_TOPS.md §6.8 to the
// production functions of a module: which functions a `crap` entry covers,
// which entries no longer match anything, which table-shaped exemptions have
// lost their shape, how many records are in force today, and the line that
// prints each one.
//
// The package knows functions only as the Func record the CRAP layer hands it.
// Function identity is defined once, in internal/crap (§6.7); Key is that
// identity, and this package never derives it.
package exempt

import (
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/Hedronite/go-tops/internal/report"
)

const (
	layerCrap   = "crap"
	shapeTable  = "table-switch"
	dateLayout  = time.DateOnly
	globAnyDirs = "**"
)

// Func is one production function, as the CRAP layer scores it.
type Func struct {
	// Key is the function identity of §6.7, defined by internal/crap.
	Key string
	// File is the module-relative, slash-separated path of the file.
	File string
	// Recv is the receiver type name without * or type parameters, "" for a
	// package-level function. The symbol of an entry is Name or Recv.Name.
	Recv string
	// Name is the function name, or the variable a package-level literal
	// initializes.
	Name string
	// Table reports whether the function has the table shape of §6.8. The CRAP
	// scorer computes it from the source on every run.
	Table bool
}

func (f Func) symbol() string {
	if f.Recv == "" {
		return f.Name
	}
	return f.Recv + "." + f.Name
}

// Problem is an in-force `crap` entry that no longer matches the code, which
// fails the gate with the entry named (§6.8).
type Problem struct {
	Entry   report.Exemption
	Message string
}

// Lost is a function whose `shape: table-switch` entry no longer holds: the
// function is not table-shaped now, so it answers to the cap (§6.8).
type Lost struct {
	Entry report.Exemption
	Func  Func
}

// Result is what the in-force entries do to one set of functions.
type Result struct {
	// Exempt are the functions a `crap` entry covers, in the order of the
	// functions given, once each. They are scored and printed, not judged
	// against the cap.
	Exempt []Func
	// Lost are the functions that kept no exemption because they stopped
	// being table-shaped. They are judged against the cap.
	Lost []Lost
	// Problems are the entries that matched no production file or no function.
	Problems []Problem
	// Count is the number of records in force today, of either layer: the
	// `exemptions` count of the ledger (§6.7).
	Count int
}

// Glob reports whether name matches the exemption glob pattern. Both are
// module-relative and slash-separated. `*`, `?` and `[...]` follow path.Match
// and stay inside one path element; an element that is exactly `**` stands for
// one or more elements, or for zero when other elements follow it.
func Glob(pattern, name string) bool {
	return globElems(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

// globElems matches the elements of a pattern against the elements of a name.
func globElems(pat, name []string) bool {
	switch {
	case len(pat) == 0:
		return len(name) == 0
	case pat[0] == globAnyDirs:
		return globAny(pat[1:], name)
	case len(name) == 0:
		return false
	}
	ok, err := path.Match(pat[0], name[0])
	return err == nil && ok && globElems(pat[1:], name[1:])
}

// globAny matches what follows a `**`. As the last element it takes one or more
// elements; before others it takes zero or more, so repeated `**` need no
// special case.
func globAny(rest, name []string) bool {
	if len(rest) == 0 {
		return len(name) > 0
	}
	for i := 0; i <= len(name); i++ {
		if globElems(rest, name[i:]) {
			return true
		}
	}
	return false
}

// InForce returns the entries in force on the day of today (UTC): those whose
// `expires` is a real calendar date after it. An entry on or past its date, or
// with no valid date, exempts nothing; tier 0 fails it by name (§6.8).
func InForce(entries []report.Exemption, today time.Time) []report.Exemption {
	day := today.UTC().Format(dateLayout)
	var live []report.Exemption
	for _, e := range entries {
		if _, err := time.Parse(dateLayout, e.Expires); err == nil && day < e.Expires {
			live = append(live, e)
		}
	}
	return live
}

// Count is the number of records in force today, of either layer (§6.8).
func Count(entries []report.Exemption, today time.Time) int {
	return len(InForce(entries, today))
}

// Lines returns one printable line for every exemption in force, with its
// expiry, in the order given (§6.8: the gate prints them on every run).
func Lines(entries []report.Exemption, today time.Time) []string {
	var out []string
	for _, e := range InForce(entries, today) {
		out = append(out, e.Line())
	}
	return out
}

// Match applies the in-force `crap` entries to funcs, which are every
// production function of the module (tests, generated files and testdata
// already left out, §6.2).
//
// An entry whose path matches no file that holds a function, or whose symbol
// names no function in the files the path matches, is a Problem. An entry with
// `shape: table-switch` covers a function only while the function is
// table-shaped (its Table flag, which the scorer sets on every run). Entries of
// any other layer exempt nothing from CRAP (the mutation layer matches its own,
// from M4) but are counted. funcs holds every production function, whether or
// not the run's build compiled it: a path matches production files whatever
// their build tags (§6.8).
func Match(entries []report.Exemption, funcs []Func, today time.Time) Result {
	live := InForce(entries, today)
	r := Result{Count: len(live)}
	exempt := make([]bool, len(funcs))
	lostBy := map[int][]report.Exemption{}
	for _, e := range live {
		if e.Layer != layerCrap {
			continue
		}
		covered, why := covers(e, funcs)
		if why != "" {
			r.Problems = append(r.Problems, Problem{Entry: e, Message: why})
			continue
		}
		for _, i := range covered {
			if e.Shape == shapeTable && !funcs[i].Table {
				lostBy[i] = append(lostBy[i], e)
				continue
			}
			exempt[i] = true
		}
	}
	for i, f := range funcs {
		if exempt[i] {
			r.Exempt = append(r.Exempt, f)
			continue
		}
		for _, e := range lostBy[i] {
			r.Lost = append(r.Lost, Lost{Entry: e, Func: f})
		}
	}
	return r
}

// covers returns the indexes of the functions entry e names, or the reason it
// names none.
func covers(e report.Exemption, funcs []Func) ([]int, string) {
	var idx []int
	matched := false
	for i, f := range funcs {
		if !Glob(e.Path, f.File) {
			continue
		}
		matched = true
		if e.Symbol == "" || e.Symbol == f.symbol() {
			idx = append(idx, i)
		}
	}
	switch {
	case !matched:
		return nil, fmt.Sprintf("exemption path %q (layer crap) matches no production file", e.Path)
	case len(idx) == 0:
		return nil, fmt.Sprintf("exemption symbol %q (layer crap) names no function in the files %q matches", e.Symbol, e.Path)
	}
	return idx, ""
}
