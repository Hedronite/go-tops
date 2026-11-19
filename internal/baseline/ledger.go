// Package baseline reads, writes and measures the debt ledger
// .go-tops/baseline.json (GO_TOPS.md §6.7).
//
// The file is a value: File holds what core.schema.json#/$defs/baseline
// allows, Marshal writes it with sorted keys and one entry per line so that
// parallel branches merge without conflict, and Parse reads it back. Init,
// Lower, Accept and Rename are the four ways a ledger changes; none of them
// touches the disk. Collect turns what the feeding layers left behind into the
// Measure they take. The ratchet judge (G4) reads the same types.
package baseline

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Counts is a count per name (a linter or a law ID), then per module-relative
// slash-separated file.
type Counts map[string]map[string]int

// Package is the ledger entry of a package under its coverage floor.
type Package struct {
	StatementsPct float64
	Uncovered     int
}

// Ledger is the debt ledger: only what is over the line. A field the schema
// has a kind for is a map or a count, and a kind with no entry is nil or zero,
// never empty.
type Ledger struct {
	Functions  map[string]float64
	Packages   map[string]Package
	Findings   Counts
	Laws       Counts
	Nolint     Counts // suppressions.nolint
	Ack        Counts // suppressions.ack
	Exemptions int
	Survivors  map[string]int
}

// Regression is one accepted regression, written by --accept-regression. Kind
// and Key name one scalar of the ledger (see Change); From is nil when the
// entry did not exist.
type Regression struct {
	Kind, Key string
	From      *float64
	To        float64
	Reason    string
	Owner     string
}

// Build is the build a ledger was measured on (ruling C3): the operating
// system, the architecture, the build tags and whether cgo is on. The run whose
// build equals it is the ledger leg, the only run that writes the file.
type Build struct {
	GOOS, GOARCH string
	Tags         []string
	CGOEnabled   bool
}

func (b Build) sortedTags() []string {
	tags := append([]string{}, b.Tags...)
	sort.Strings(tags)
	return slices.Compact(tags)
}

// Equal reports whether two builds are the same: tags are a set.
func (b Build) Equal(o Build) bool {
	return b.GOOS == o.GOOS && b.GOARCH == o.GOARCH && b.CGOEnabled == o.CGOEnabled && slices.Equal(b.sortedTags(), o.sortedTags())
}

// String is the build as a message names it.
func (b Build) String() string {
	cgo := "cgo=0"
	if b.CGOEnabled {
		cgo = "cgo=1"
	}
	return fmt.Sprintf("%s/%s %s tags=%s", b.GOOS, b.GOARCH, cgo, strings.Join(b.sortedTags(), ","))
}

// File is .go-tops/baseline.json.
type File struct {
	Build       Build
	Ledger      Ledger
	JudgedLaws  int
	Regressions []Regression
}

// RefusalError is a request the ledger rules refuse: nothing was written. The
// message says why.
type RefusalError struct{ Msg string }

func (e *RefusalError) Error() string { return e.Msg }

func refuse(format string, a ...any) *RefusalError {
	return &RefusalError{Msg: fmt.Sprintf(format, a...)}
}

// wire is the file as JSON, the way Parse reads it.
type wire struct {
	Build       *wireBuild   `json:"build"`
	JudgedLaws  *int         `json:"judged_laws"`
	Ledger      *wireLedger  `json:"ledger"`
	Regressions []wireRecord `json:"regressions"`
}

type wireBuild struct {
	GOOS       *string   `json:"goos"`
	GOARCH     *string   `json:"goarch"`
	Tags       *[]string `json:"tags"`
	CGOEnabled *bool     `json:"cgo_enabled"`
}

type wireLedger struct {
	Functions    map[string]float64 `json:"functions"`
	Packages     map[string]wirePkg `json:"packages"`
	Findings     Counts             `json:"findings"`
	Laws         Counts             `json:"laws"`
	Suppressions *struct {
		Nolint Counts `json:"nolint"`
		Ack    Counts `json:"ack"`
	} `json:"suppressions"`
	Exemptions int            `json:"exemptions"`
	Survivors  map[string]int `json:"survivors"`
}

type wirePkg struct {
	StatementsPct float64 `json:"statements_pct"`
	Uncovered     int     `json:"uncovered"`
}

type wireRecord struct {
	From   *float64 `json:"from"`
	Key    string   `json:"key"`
	Kind   string   `json:"kind"`
	Owner  string   `json:"owner"`
	Reason string   `json:"reason"`
	To     float64  `json:"to"`
}

// Parse reads a baseline file. It is strict about shape (one JSON value, no
// unknown key, the two required keys present) and leaves the schema's finer
// rules to `check`, which validates the same bytes against core.schema.json.
func Parse(data []byte) (File, error) {
	w, err := decodeWire(data)
	if err != nil {
		return File{}, err
	}
	if err := w.check(); err != nil {
		return File{}, err
	}
	return w.file(), nil
}

// decodeWire reads exactly one JSON value with no key the file does not have.
func decodeWire(data []byte) (wire, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var w wire
	if err := dec.Decode(&w); err != nil {
		if errors.Is(err, io.EOF) {
			return wire{}, errors.New("the file is empty")
		}
		return wire{}, fmt.Errorf("not a baseline: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return wire{}, errors.New("more than one JSON value, or text after the first")
	}
	return w, nil
}

// check refuses a file that lacks a required key or a field of its build.
func (w wire) check() error {
	if w.Ledger == nil || w.JudgedLaws == nil || w.Build == nil {
		return errors.New(`not a baseline: "build", "ledger" and "judged_laws" are required`)
	}
	if !w.Build.complete() {
		return errors.New(`not a baseline: "build" needs goos, goarch, tags and cgo_enabled`)
	}
	return nil
}

func (b *wireBuild) complete() bool {
	return b.GOOS != nil && b.GOARCH != nil && b.Tags != nil && b.CGOEnabled != nil && *b.GOOS != "" && *b.GOARCH != ""
}

// file is the checked wire form as a File.
func (w wire) file() File {
	b := w.Build
	return File{
		JudgedLaws:  *w.JudgedLaws,
		Build:       Build{GOOS: *b.GOOS, GOARCH: *b.GOARCH, Tags: *b.Tags, CGOEnabled: *b.CGOEnabled},
		Ledger:      w.Ledger.ledger(),
		Regressions: recordsOf(w.Regressions),
	}
}

func (l *wireLedger) ledger() Ledger {
	out := Ledger{Functions: nonEmpty(l.Functions), Packages: packagesOf(l.Packages), Findings: nonEmptyCounts(l.Findings), Laws: nonEmptyCounts(l.Laws), Exemptions: l.Exemptions, Survivors: nonEmpty(l.Survivors)}
	if s := l.Suppressions; s != nil {
		out.Nolint, out.Ack = nonEmptyCounts(s.Nolint), nonEmptyCounts(s.Ack)
	}
	return out
}

func packagesOf(in map[string]wirePkg) map[string]Package {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]Package, len(in))
	for k, p := range in {
		out[k] = Package(p)
	}
	return out
}

func recordsOf(in []wireRecord) []Regression {
	var out []Regression
	for _, r := range in {
		out = append(out, Regression{Kind: r.Kind, Key: r.Key, From: r.From, To: r.To, Reason: r.Reason, Owner: r.Owner})
	}
	return out
}

func nonEmpty[V any](m map[string]V) map[string]V {
	if len(m) == 0 {
		return nil
	}
	return m
}

func nonEmptyCounts(c Counts) Counts {
	if len(c) == 0 {
		return nil
	}
	return c
}

// node is a piece of the output: a literal that goes on one line, or an object
// whose entries each get a line of their own.
type node struct {
	lit  string
	kids []kid
}

type kid struct {
	key string
	val node
}

func lit(s string) node { return node{lit: s} }

// obj is an object node. A key that holds nothing is left out.
func obj(kids ...kid) node {
	var out []kid
	for _, k := range kids {
		if k.val.lit != "" || len(k.val.kids) > 0 {
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return node{kids: out}
}

func quote(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil { // a string always encodes
		panic(err)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func num(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		panic("baseline: a ledger value is not finite") // Collect refuses these
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func emit(b *bytes.Buffer, indent string, n node) {
	if n.lit != "" {
		b.WriteString(n.lit)
		return
	}
	if len(n.kids) == 0 {
		b.WriteString("{}")
		return
	}
	b.WriteString("{\n")
	for i, k := range n.kids {
		b.WriteString(indent + "  " + quote(k.key) + ": ")
		emit(b, indent+"  ", k.val)
		if i < len(n.kids)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString(indent + "}")
}

func countsNode(c Counts) node {
	var names []kid
	for name, files := range c {
		var fk []kid
		for file, n := range files {
			if n > 0 {
				fk = append(fk, kid{file, lit(strconv.Itoa(n))})
			}
		}
		names = append(names, kid{name, obj(fk...)})
	}
	return obj(names...)
}

func ledgerNode(l Ledger) node {
	var fns, pkgs, surv []kid
	for k, v := range l.Functions {
		fns = append(fns, kid{k, lit(num(v))})
	}
	for k, p := range l.Packages {
		pkgs = append(pkgs, kid{k, lit(`{"statements_pct": ` + num(p.StatementsPct) + `, "uncovered": ` + strconv.Itoa(p.Uncovered) + `}`)})
	}
	for k, n := range l.Survivors {
		if n > 0 {
			surv = append(surv, kid{k, lit(strconv.Itoa(n))})
		}
	}
	kids := []kid{
		{"findings", countsNode(l.Findings)},
		{"functions", obj(fns...)},
		{"laws", countsNode(l.Laws)},
		{"packages", obj(pkgs...)},
		{"survivors", obj(surv...)},
		{"suppressions", obj(kid{"ack", countsNode(l.Ack)}, kid{"nolint", countsNode(l.Nolint)})},
	}
	if l.Exemptions > 0 {
		kids = append(kids, kid{"exemptions", lit(strconv.Itoa(l.Exemptions))})
	}
	return obj(kids...)
}

// Marshal writes the file as it is committed: keys sorted at every level, one
// entry per line, and a final newline. Equal files give equal bytes. Counts of
// zero and an exemption count of zero are left out, as the schema requires.
// Regressions are written in order of kind and key.
func Marshal(f File) []byte {
	var b bytes.Buffer
	b.WriteString("{\n  \"build\": {\n")
	b.WriteString("    \"cgo_enabled\": " + strconv.FormatBool(f.Build.CGOEnabled) + ",\n")
	b.WriteString("    \"goarch\": " + quote(f.Build.GOARCH) + ",\n    \"goos\": " + quote(f.Build.GOOS) + ",\n")
	tags := make([]string, 0, len(f.Build.Tags))
	for _, tag := range f.Build.sortedTags() {
		tags = append(tags, quote(tag))
	}
	b.WriteString("    \"tags\": [" + strings.Join(tags, ", ") + "]\n  },\n")
	b.WriteString("  \"judged_laws\": " + strconv.Itoa(f.JudgedLaws) + ",\n  \"ledger\": ")
	emit(&b, "  ", ledgerNode(f.Ledger))
	if recs := sortedRecords(f.Regressions); len(recs) > 0 {
		b.WriteString(",\n  \"regressions\": [\n")
		for i, r := range recs {
			from := "null"
			if r.From != nil {
				from = num(*r.From)
			}
			b.WriteString(`    {"from": ` + from + `, "key": ` + quote(r.Key) + `, "kind": ` + quote(r.Kind) +
				`, "owner": ` + quote(r.Owner) + `, "reason": ` + quote(r.Reason) + `, "to": ` + num(r.To) + `}`)
			if i < len(recs)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString("  ]")
	}
	b.WriteString("\n}\n")
	return b.Bytes()
}

func sortedRecords(in []Regression) []Regression {
	out := append([]Regression(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Key < out[j].Key
	})
	return out
}
