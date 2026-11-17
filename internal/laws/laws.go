// Package laws holds the law catalog of GO_TOPS.md §8.2 and the stub the laws
// layer runs until the analyzers land (M5).
//
// The table in table.go is the §8.2 table. laws_test.go parses GO_TOPS.md and
// fails on any difference, so the two cannot drift.
package laws

import "sort"

// Severity is the R or A column of §8.2.
type Severity string

// The two severities of §3.3: an instance of an R law is refused; an instance
// of an A law is acknowledged or refused.
const (
	SeverityR Severity = "R"
	SeverityA Severity = "A"
)

// Reader says how a law is judged (§8.1). The values are the report's
// lawResult.reader enum.
type Reader string

// The four readers.
const (
	ReaderAnalyzer Reader = "analyzer"
	ReaderTool     Reader = "tool"
	ReaderAttested Reader = "attested"
	ReaderTBD      Reader = "tbd"
)

// Verdict is what one law found (§3.3). The values are the core schema's
// verdict enum.
type Verdict string

// The four verdicts.
const (
	Allow        Verdict = "allow"
	Ack          Verdict = "ack"
	Refuse       Verdict = "refuse"
	NotEvaluated Verdict = "not-evaluated"
)

// Reason is why a law is not-evaluated (§3.3). The values are the core
// schema's reasonCode enum.
type Reason string

// The three reason codes.
const (
	NoReader     Reason = "no_reader"
	ConfigOff    Reason = "config_off"
	InputMissing Reason = "input_missing"
)

// Law is one row of §8.2.
type Law struct {
	ID       string
	Severity Severity
	Reader   Reader
	// Tier is where the law is judged (§3.2): 0 for an analyzer, 1 for a
	// tool, 3 where §8.2 says "tier 3". An attested or tbd law has no reader
	// to run, so it sits at 0 and is always in range.
	Tier    int
	V01     bool // the ✓ column: a live reader is targeted for the first mechanical release
	Retired bool // the Law cell begins "Retired"
}

// Active reports whether the law is neither retired nor planned.
func (l Law) Active() bool { return !l.Retired }

// Catalog returns every row of §8.2, retired ones included, in table order.
func Catalog() []Law {
	out := make([]Law, len(catalog))
	copy(out, catalog)
	return out
}

// Lookup finds a law by ID.
func Lookup(id string) (Law, bool) {
	for _, l := range catalog {
		if l.ID == id {
			return l, true
		}
	}
	return Law{}, false
}

// liveReaders holds the laws that have a reader in this release. The stub
// judges none, so none is listed; M5 adds each law as its reader lands.
var liveReaders = map[string]bool{}

// Counts are the release-level figures of §12.3: the same for every class.
type Counts struct {
	Catalog      int // every row, retired ones included
	Active       int // rows that are not retired
	V01          int // active rows marked ✓: the target of "16 of 43 laws judged"
	Judged       int // active laws with a live reader in this release
	NotEvaluated int // active laws without one
}

// Release returns the release-level counts.
func Release() Counts {
	var c Counts
	c.Catalog = len(catalog)
	for _, l := range catalog {
		if !l.Active() {
			continue
		}
		c.Active++
		if l.V01 {
			c.V01++
		}
		if liveReaders[l.ID] {
			c.Judged++
		} else {
			c.NotEvaluated++
		}
	}
	return c
}

// extra is the "Extra laws" row of §5.2: the laws a class carries beyond the
// ✓ set.
var extra = map[string][]string{
	"library-core":        {"G-21", "G-22", "G-23", "G-24"},
	"binary-cli":          {"G-41"},
	"tui-app":             {"G-10"},
	"binary-service":      {"G-19", "G-20"},
	"concurrency-runtime": {"G-01", "G-02", "G-03", "G-04", "G-05", "G-06", "G-07", "G-08", "G-09"},
}

// everyClass holds the laws §5.2 gives to every class: leak detection (G-42)
// and fuzz targets (G-43).
var everyClass = []string{"G-42", "G-43"}

// Applicable returns the active laws that apply to the class, sorted by ID.
//
// §8.3 says a law applies when the class is in its law file's classes list,
// and the law files arrive at M5. Until then the set is derived from the two
// places the protocol does state it: the ✓ laws every profile requires
// (§5.3), the "Extra laws" row of §5.2, and the two laws §5.2 gives to every
// class. For binary-cli that is the 19 laws of config/gate-report.example.json.
func Applicable(class string) []Law {
	want := map[string]bool{}
	for _, l := range catalog {
		if l.V01 {
			want[l.ID] = true
		}
	}
	for _, id := range extra[class] {
		want[id] = true
	}
	for _, id := range everyClass {
		want[id] = true
	}
	var out []Law
	for _, l := range catalog {
		if want[l.ID] && l.Active() {
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
