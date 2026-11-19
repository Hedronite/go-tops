package exempt

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hedronite/go-tops/internal/report"
)

// day is the run date every test uses (UTC).
var day = time.Date(2026, 11, 18, 15, 4, 5, 0, time.UTC)

// entry builds one §6.8 record; every test overrides what it cares about.
func entry(mod func(*report.Exemption)) report.Exemption {
	e := report.Exemption{
		Path: "internal/wire/dispatch.go", Layer: "crap",
		Reason: "opcode dispatch, every arm is one call", Owner: "platform", Expires: "2027-03-01",
	}
	if mod != nil {
		mod(&e)
	}
	return e
}

func fn(file, recv, name string) Func {
	key := file + "|" + name
	if recv != "" {
		key = file + "|" + recv + "." + name
	}
	return Func{Key: key, File: file, Recv: recv, Name: name}
}

func keys(fs []Func) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Key)
	}
	return out
}

// §6.8: `*` stays inside one path element, `**` crosses elements.
func TestGlob(t *testing.T) {
	for _, c := range []struct {
		pattern, name string
		want          bool
	}{
		{"internal/wire/dispatch.go", "internal/wire/dispatch.go", true},
		{"internal/wire/dispatch.go", "internal/wire/other.go", false},
		{"internal/*.go", "internal/a.go", true},
		{"internal/*.go", "internal/a/b.go", false},
		{"internal/*/x.go", "internal/a/x.go", true},
		{"internal/*/x.go", "internal/a/b/x.go", false},
		{"internal/legacy/**", "internal/legacy/x.go", true},
		{"internal/legacy/**", "internal/legacy/a/b/x.go", true},
		{"internal/legacy/**", "internal/legacyx/x.go", false},
		{"internal/legacy/**", "internal/legacy", false},
		{"**/x.go", "x.go", true},
		{"**/x.go", "a/b/x.go", true},
		{"**/x.go", "a/b/y.go", false},
		{"a/**/b.go", "a/b.go", true},
		{"a/**/b.go", "a/c/d/b.go", true},
		{"a/**/b.go", "x/a/b.go", false},
		{"**", "any/file.go", true},
		{"a/**/**/b.go", "a/b.go", true},
		{"internal/?.go", "internal/a.go", true},
		{"internal/?.go", "internal/ab.go", false},
		{"internal/[ab].go", "internal/b.go", true},
		{"internal/[ab].go", "internal/c.go", false},
		{"Internal/a.go", "internal/a.go", false},
	} {
		if got := Glob(c.pattern, c.name); got != c.want {
			t.Errorf("Glob(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}

// An entry is in force while today (UTC) is before `expires`: on that date and
// after, or when the date is not a real day, it exempts nothing.
func TestInForce(t *testing.T) {
	for _, c := range []struct {
		expires string
		want    bool
	}{
		{"2026-11-19", true},
		{"2026-11-18", false}, // the run date itself is expired
		{"2026-11-17", false},
		{"2027-02-29", false}, // not a real calendar day
		{"", false},
		{"soon", false},
	} {
		got := InForce([]report.Exemption{entry(func(e *report.Exemption) { e.Expires = c.expires })}, day)
		if (len(got) == 1) != c.want {
			t.Errorf("expires %q: in force %v, want %v", c.expires, len(got) == 1, c.want)
		}
	}
}

// The run date is UTC: a late evening west of Greenwich is already the next day.
func TestInForceUsesUTC(t *testing.T) {
	west := time.FixedZone("west", -8*3600)
	late := time.Date(2026, 11, 17, 18, 0, 0, 0, west) // 2026-11-18 02:00 UTC
	if got := InForce([]report.Exemption{entry(func(e *report.Exemption) { e.Expires = "2026-11-18" })}, late); len(got) != 0 {
		t.Errorf("an entry expiring on the UTC date stayed in force: %v", got)
	}
}

func TestMatchPathCoversEveryFunctionInTheFiles(t *testing.T) {
	funcs := []Func{
		fn("internal/wire/dispatch.go", "", "A"),
		fn("internal/wire/dispatch.go", "Server", "B"),
		fn("internal/wire/other.go", "", "C"),
	}
	r := Match([]report.Exemption{entry(nil)}, funcs, day, nil)
	if want := []string{"internal/wire/dispatch.go|A", "internal/wire/dispatch.go|Server.B"}; !slices.Equal(keys(r.Exempt), want) {
		t.Errorf("exempt = %v, want %v", keys(r.Exempt), want)
	}
	if len(r.Problems) != 0 {
		t.Errorf("problems = %v", r.Problems)
	}
}

func TestMatchSymbolForms(t *testing.T) {
	funcs := []Func{
		fn("internal/wire/dispatch.go", "", "handleOp"),
		fn("internal/wire/dispatch.go", "Server", "handleOp"),
		fn("internal/wire/dispatch.go", "Client", "handleOp"),
	}
	for _, c := range []struct {
		symbol string
		want   []string
	}{
		{"handleOp", []string{"internal/wire/dispatch.go|handleOp"}},
		{"Server.handleOp", []string{"internal/wire/dispatch.go|Server.handleOp"}},
		{"Client.handleOp", []string{"internal/wire/dispatch.go|Client.handleOp"}},
	} {
		r := Match([]report.Exemption{entry(func(e *report.Exemption) { e.Symbol = c.symbol })}, funcs, day, nil)
		if !slices.Equal(keys(r.Exempt), c.want) {
			t.Errorf("symbol %q: exempt = %v, want %v", c.symbol, keys(r.Exempt), c.want)
		}
	}
}

// A symbol reaches only the files `path` matches.
func TestMatchSymbolStaysInItsFiles(t *testing.T) {
	funcs := []Func{
		fn("internal/wire/dispatch.go", "", "F"),
		fn("internal/wire/other.go", "", "F"),
	}
	r := Match([]report.Exemption{entry(func(e *report.Exemption) { e.Symbol = "F" })}, funcs, day, nil)
	if want := []string{"internal/wire/dispatch.go|F"}; !slices.Equal(keys(r.Exempt), want) {
		t.Errorf("exempt = %v, want %v", keys(r.Exempt), want)
	}
}

// Several init functions, or twins, share one symbol: all are covered, and the
// identity keys stay distinct (G1 defines them).
func TestMatchRepeatedNames(t *testing.T) {
	a, b := fn("p/a.go", "", "init"), fn("p/a.go", "", "init")
	a.Key, b.Key = "p.init#1", "p.init#2"
	r := Match([]report.Exemption{entry(func(e *report.Exemption) { e.Path, e.Symbol = "p/*.go", "init" })}, []Func{a, b}, day, nil)
	if want := []string{"p.init#1", "p.init#2"}; !slices.Equal(keys(r.Exempt), want) {
		t.Errorf("exempt = %v, want %v", keys(r.Exempt), want)
	}
}

// An exempt function is reported once, in the order of funcs, however many
// entries cover it.
func TestMatchOverlapListsOnce(t *testing.T) {
	funcs := []Func{fn("a.go", "", "Z"), fn("a.go", "", "A")}
	es := []report.Exemption{
		entry(func(e *report.Exemption) { e.Path = "a.go" }),
		entry(func(e *report.Exemption) { e.Path, e.Symbol = "*.go", "A" }),
	}
	r := Match(es, funcs, day, nil)
	if want := []string{"a.go|Z", "a.go|A"}; !slices.Equal(keys(r.Exempt), want) {
		t.Errorf("exempt = %v, want %v", keys(r.Exempt), want)
	}
}

// §6.8: a path that matches no production file, or a symbol that names no
// function in the matching files, fails, and the entry is named.
func TestMatchUnmatchedEntriesFailNamingTheEntry(t *testing.T) {
	funcs := []Func{fn("internal/wire/dispatch.go", "Server", "handleOp")}
	for _, c := range []struct {
		name string
		mod  func(*report.Exemption)
		want []string // substrings the problem must carry
	}{
		{"path matches nothing", func(e *report.Exemption) { e.Path = "internal/gone/**" }, []string{"internal/gone/**", "no production file"}},
		{"symbol names nothing", func(e *report.Exemption) { e.Symbol = "Server.gone" }, []string{"internal/wire/dispatch.go", "Server.gone", "no function"}},
		{"plain name against a method", func(e *report.Exemption) { e.Symbol = "handleOp" }, []string{"handleOp", "no function"}},
		{"symbol in another file", func(e *report.Exemption) { e.Path, e.Symbol = "internal/wire/other.go", "Server.handleOp" }, []string{"internal/wire/other.go", "no production file"}},
	} {
		es := []report.Exemption{entry(c.mod)}
		r := Match(es, funcs, day, nil)
		if len(r.Problems) != 1 {
			t.Errorf("%s: problems = %v, want one", c.name, r.Problems)
			continue
		}
		p := r.Problems[0]
		if p.Entry != es[0] {
			t.Errorf("%s: problem names %+v, want the entry %+v", c.name, p.Entry, es[0])
		}
		for _, w := range c.want {
			if !strings.Contains(p.Message, w) {
				t.Errorf("%s: message %q lacks %q", c.name, p.Message, w)
			}
		}
		if len(r.Exempt) != 0 {
			t.Errorf("%s: an unmatched entry exempted %v", c.name, keys(r.Exempt))
		}
	}
}

// A path-only entry whose files hold no function covers nothing: the entry
// has outlived its code.
func TestMatchPathOnlyEntryWithNoFunctionsFails(t *testing.T) {
	r := Match([]report.Exemption{entry(nil)}, nil, day, nil)
	if len(r.Problems) != 1 {
		t.Errorf("problems = %v, want one", r.Problems)
	}
}

// An expired entry exempts nothing and is no problem here: tier 0 already
// failed it, by name (§6.8).
func TestMatchSkipsExpiredEntries(t *testing.T) {
	funcs := []Func{fn("internal/wire/dispatch.go", "", "A")}
	es := []report.Exemption{
		entry(func(e *report.Exemption) { e.Expires = "2026-11-18" }),
		entry(func(e *report.Exemption) { e.Path = "gone.go"; e.Expires = "2026-01-01" }),
	}
	r := Match(es, funcs, day, nil)
	if len(r.Exempt) != 0 || len(r.Problems) != 0 || r.Count != 0 {
		t.Errorf("exempt %v, problems %v, count %d: want none", keys(r.Exempt), r.Problems, r.Count)
	}
}

// Mutation-layer entries are M4's: they exempt no function from CRAP, are not
// matched here, and still count and print (§6.8).
func TestMatchLeavesMutationEntriesAlone(t *testing.T) {
	funcs := []Func{fn("internal/legacy/x.go", "", "A")}
	es := []report.Exemption{entry(func(e *report.Exemption) { e.Path, e.Layer = "internal/legacy/**", "mutation" })}
	r := Match(es, funcs, day, nil)
	if len(r.Exempt) != 0 || len(r.Problems) != 0 {
		t.Errorf("exempt %v, problems %v: a mutation entry acted on CRAP", keys(r.Exempt), r.Problems)
	}
	if r.Count != 1 {
		t.Errorf("count = %d, want 1: a mutation entry in force is counted", r.Count)
	}
}

// §6.8: with shape: table-switch the shape is checked on every run. A function
// that stopped being a table loses the exemption and answers to the cap.
func TestMatchTableSwitchChecksTheShape(t *testing.T) {
	f := fn("internal/wire/dispatch.go", "Server", "handleOp")
	es := []report.Exemption{entry(func(e *report.Exemption) { e.Symbol, e.Shape = "Server.handleOp", "table-switch" })}

	var asked []string
	table := func(f Func) bool { asked = append(asked, f.Key); return true }
	r := Match(es, []Func{f}, day, table)
	if !slices.Equal(keys(r.Exempt), []string{f.Key}) || len(r.Lost) != 0 || !slices.Equal(asked, []string{f.Key}) {
		t.Errorf("table-shaped: exempt %v, lost %v, asked %v", keys(r.Exempt), r.Lost, asked)
	}

	grown := func(Func) bool { return false }
	r = Match(es, []Func{f}, day, grown)
	if len(r.Exempt) != 0 {
		t.Errorf("a function that grew a branch kept the exemption: %v", keys(r.Exempt))
	}
	if len(r.Lost) != 1 || r.Lost[0].Func.Key != f.Key || r.Lost[0].Entry != es[0] {
		t.Errorf("lost = %+v, want the function and the entry named", r.Lost)
	}
	if len(r.Problems) != 0 {
		t.Errorf("a lost shape is not an unmatched entry: %v", r.Problems)
	}
	if r.Count != 1 {
		t.Errorf("count = %d: the record is still in force today", r.Count)
	}
}

// The shape check belongs to entries that ask for it, and to none else.
func TestMatchShapeCheckedOnlyForShapedEntries(t *testing.T) {
	f := fn("a.go", "", "F")
	called := false
	r := Match([]report.Exemption{entry(func(e *report.Exemption) { e.Path = "a.go" })}, []Func{f}, day, func(Func) bool { called = true; return false })
	if called || len(r.Exempt) != 1 {
		t.Errorf("called %v, exempt %v: an entry without a shape was shape-checked", called, keys(r.Exempt))
	}
}

// Another entry that exempts the function without a shape keeps it exempt.
func TestMatchLostShapeStaysExemptUnderAnotherEntry(t *testing.T) {
	f := fn("a.go", "", "F")
	es := []report.Exemption{
		entry(func(e *report.Exemption) { e.Path, e.Symbol, e.Shape = "a.go", "F", "table-switch" }),
		entry(func(e *report.Exemption) { e.Path = "a.go" }),
	}
	r := Match(es, []Func{f}, day, func(Func) bool { return false })
	if len(r.Exempt) != 1 || len(r.Lost) != 0 {
		t.Errorf("exempt %v, lost %v", keys(r.Exempt), r.Lost)
	}
}

// §6.8, "The exemption count": the records in force on the day, of either layer.
func TestCount(t *testing.T) {
	es := []report.Exemption{
		entry(nil),
		entry(func(e *report.Exemption) { e.Layer = "mutation"; e.Path = "x/**" }),
		entry(func(e *report.Exemption) { e.Expires = "2026-11-18" }),
	}
	if got := Count(es, day); got != 2 {
		t.Errorf("Count = %d, want 2", got)
	}
	if got := Count(nil, day); got != 0 {
		t.Errorf("Count(nil) = %d, want 0", got)
	}
}

// §6.8: the gate prints every exemption in force, with its expiry, on every run.
func TestLines(t *testing.T) {
	es := []report.Exemption{
		entry(func(e *report.Exemption) { e.Symbol, e.Shape = "Server.handleOp", "table-switch" }),
		entry(func(e *report.Exemption) { e.Layer, e.Path, e.Expires = "mutation", "internal/legacy/**", "2027-01-31" }),
		entry(func(e *report.Exemption) { e.Expires = "2026-01-01" }),
	}
	got := Lines(es, day)
	if len(got) != 2 {
		t.Fatalf("lines = %q, want two (the expired one is not in force)", got)
	}
	for i, want := range [][]string{
		{"crap", "internal/wire/dispatch.go", "Server.handleOp", "table-switch", "2027-03-01", "platform", "opcode dispatch"},
		{"mutation", "internal/legacy/**", "2027-01-31"},
	} {
		for _, w := range want {
			if !strings.Contains(got[i], w) {
				t.Errorf("line %d %q lacks %q", i, got[i], w)
			}
		}
		if strings.Contains(got[i], "\n") {
			t.Errorf("line %d spans several lines: %q", i, got[i])
		}
	}
	if len(Lines(nil, day)) != 0 {
		t.Error("lines for no exemptions")
	}
}
