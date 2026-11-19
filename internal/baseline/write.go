package baseline

import (
	"slices"
	"sort"
	"strings"
)

// Measure is what a run measured over the line (§6.7): the ledger the repo
// would have if it were written today.
type Measure struct {
	Functions  map[string]float64 // CRAP score of each function over the cap
	Packages   map[string]Package // each package under the coverage floor
	Findings   Counts             // lint findings no law maps, per linter and file
	Nolint     Counts             // //nolint comments, per linter and file
	Ack        Counts             // //tops:ack comments and acks.yaml entries, per law and file
	Exemptions int                // live exemption records
	JudgedLaws int                // laws the run judged
	Build      Build              // the build the run measured; Init records it
}

// The kinds of one entry of the ledger, as a Regression names them. Every
// kind holds one number per key, so a record says what moved and by how much.
// A package has two: its percentage and its uncovered-statement count.
const (
	kindFunctions     = "functions"
	kindPkgPct        = "packages.statements_pct"
	kindPkgUncovered  = "packages.uncovered"
	kindFindings      = "findings"
	kindLaws          = "laws"
	kindNolint        = "suppressions.nolint"
	kindAck           = "suppressions.ack"
	kindExemptions    = "exemptions"
	kindSurvivors     = "survivors"
	kindJudgedLaws    = "judged_laws"
	keyExemptions     = "exemptions"
	keyJudgedLaws     = "judged_laws"
	nameFileSeparator = ":"
)

// lowIsWorse is true for the numbers that protect the repo when they are high:
// the percentage of a package and the count of judged laws. For every other
// number, higher is worse.
func lowIsWorse(kind string) bool { return kind == kindPkgPct || kind == kindJudgedLaws }

// ekey names one number of the ledger.
type ekey struct{ kind, key string }

// flat is every number of a file, by kind and key. Keys of the two-level kinds
// are "name:file"; a linter or law name has no colon.
func flat(f File) map[ekey]float64 {
	out := map[ekey]float64{{kindJudgedLaws, keyJudgedLaws}: float64(f.JudgedLaws)}
	l := f.Ledger
	for k, v := range l.Functions {
		out[ekey{kindFunctions, k}] = v
	}
	for k, p := range l.Packages {
		out[ekey{kindPkgPct, k}] = p.StatementsPct
		out[ekey{kindPkgUncovered, k}] = float64(p.Uncovered)
	}
	two := func(kind string, c Counts) {
		for name, files := range c {
			for file, n := range files {
				if n > 0 {
					out[ekey{kind, name + nameFileSeparator + file}] = float64(n)
				}
			}
		}
	}
	two(kindFindings, l.Findings)
	two(kindLaws, l.Laws)
	two(kindNolint, l.Nolint)
	two(kindAck, l.Ack)
	for k, n := range l.Survivors {
		if n > 0 {
			out[ekey{kindSurvivors, k}] = float64(n)
		}
	}
	if l.Exemptions > 0 {
		out[ekey{kindExemptions, keyExemptions}] = float64(l.Exemptions)
	}
	return out
}

// unflat is the inverse of flat. A package with only one of its two numbers is
// left out: the schema requires both.
func unflat(m map[ekey]float64, recs []Regression) File {
	f := File{JudgedLaws: int(m[ekey{kindJudgedLaws, keyJudgedLaws}]), Regressions: recs}
	for k, v := range m {
		if put := putters[k.kind]; put != nil {
			put(&f.Ledger, m, k, v)
		}
	}
	if len(recs) == 0 {
		f.Regressions = nil
	}
	return f
}

// putters add one number of the flat form to a ledger, one function per kind.
// The uncovered count and judged_laws have none: a package takes its count when
// its percentage is put, and judged_laws is the file's.
var putters = map[string]func(l *Ledger, m map[ekey]float64, k ekey, v float64){
	kindFunctions:  putFunction,
	kindPkgPct:     putPackage,
	kindFindings:   func(l *Ledger, _ map[ekey]float64, k ekey, v float64) { putCount(&l.Findings, k, v) },
	kindLaws:       func(l *Ledger, _ map[ekey]float64, k ekey, v float64) { putCount(&l.Laws, k, v) },
	kindNolint:     func(l *Ledger, _ map[ekey]float64, k ekey, v float64) { putCount(&l.Nolint, k, v) },
	kindAck:        func(l *Ledger, _ map[ekey]float64, k ekey, v float64) { putCount(&l.Ack, k, v) },
	kindSurvivors:  putSurvivor,
	kindExemptions: func(l *Ledger, _ map[ekey]float64, _ ekey, v float64) { l.Exemptions = int(v) },
}

func putFunction(l *Ledger, _ map[ekey]float64, k ekey, v float64) {
	if l.Functions == nil {
		l.Functions = map[string]float64{}
	}
	l.Functions[k.key] = v
}

// putPackage needs both numbers of the package: the schema requires them
// together, so a package with only one is left out.
func putPackage(l *Ledger, m map[ekey]float64, k ekey, v float64) {
	u, ok := m[ekey{kindPkgUncovered, k.key}]
	if !ok {
		return
	}
	if l.Packages == nil {
		l.Packages = map[string]Package{}
	}
	l.Packages[k.key] = Package{StatementsPct: v, Uncovered: int(u)}
}

// putCount adds one "name:file" count.
func putCount(c *Counts, k ekey, v float64) {
	name, file, _ := strings.Cut(k.key, nameFileSeparator)
	if *c == nil {
		*c = Counts{}
	}
	if (*c)[name] == nil {
		(*c)[name] = map[string]int{}
	}
	(*c)[name][file] = int(v)
}

func putSurvivor(l *Ledger, _ map[ekey]float64, k ekey, v float64) {
	if l.Survivors == nil {
		l.Survivors = map[string]int{}
	}
	l.Survivors[k.key] = int(v)
}

// asFile is the ledger a measure describes, with nothing recorded. The round
// trip through the flat form copies every map and drops counts of zero.
func (m Measure) asFile() File {
	return unflat(flat(File{JudgedLaws: m.JudgedLaws, Ledger: Ledger{
		Functions: m.Functions, Packages: m.Packages, Findings: m.Findings, Nolint: m.Nolint, Ack: m.Ack, Exemptions: m.Exemptions,
	}}), nil)
}

// Init is the first ledger: what was measured, and nothing recorded (§6.7).
func Init(m Measure) File {
	f := m.asFile()
	f.Build = m.Build
	return f
}

// Lower is a plain write: it lowers a value, removes an entry that is no longer
// over the line, and raises judged_laws. It never adds an entry and never raises
// one. A package entry tightens toward coverage, its percentage rising and its
// uncovered count falling, each on its own. The ledger holds only what its own
// build measured (ruling C3), so an entry the run does not list is removed: a
// function paid back, exempt, or gone from the build. The ledger keeps its build. The laws and survivors kinds are
// fed by other commands and pass through untouched. A regressions record is
// removed when its entry is back at the value it had before, and follows the
// entry down while it is on the way.
func Lower(old File, m Measure) File {
	fo, fm := flat(old), flat(m.asFile())
	out := map[ekey]float64{}
	for k, ov := range fo {
		if k.kind == kindLaws || k.kind == kindSurvivors {
			out[k] = ov
			continue
		}
		mv, measured := fm[k]
		if !measured {
			continue // paid back, exempt, or no longer compiled on the ledger leg
		}
		out[k] = stricter(k.kind, ov, mv)
	}
	f := unflat(out, prune(old.Regressions, out))
	f.Build = old.Build
	return f
}

func stricter(kind string, a, b float64) float64 {
	if lowIsWorse(kind) {
		return max(a, b)
	}
	return min(a, b)
}

func worse(kind string, a, than float64) bool {
	if lowIsWorse(kind) {
		return a < than
	}
	return a > than
}

// prune drops each record whose entry is back at, or better than, the value
// before it, and lets the others follow their entry down.
func prune(recs []Regression, cur map[ekey]float64) []Regression {
	var out []Regression
	for _, r := range recs {
		v, ok := cur[ekey{r.Kind, r.Key}]
		if !ok || (r.From != nil && !worse(r.Kind, v, *r.From)) {
			continue
		}
		if worse(r.Kind, r.To, v) {
			r.To = v
		}
		out = append(out, r)
	}
	return out
}

// ErrNothingToAccept is returned by Accept when the measure raises nothing, adds
// nothing and lowers no judged_laws, so there is no regression to record.
var ErrNothingToAccept = &RefusalError{Msg: "nothing to accept: the measure raises no entry, adds none and does not lower judged_laws; run a plain `gotops baseline`"}

// CheckAcceptArgs says whether a reason and an owner are what an accepted
// regression needs: a reason of at least three words, and one handle. The
// command checks them before it measures anything, and Accept checks them again.
func CheckAcceptArgs(reason, owner string) error {
	if len(strings.Fields(reason)) < 3 {
		return refuse("--reason needs at least three words saying why the debt grows")
	}
	if len(strings.Fields(owner)) != 1 {
		return refuse("--owner needs one handle, a person or a team")
	}
	return nil
}

// Accept is a write that accepts regressions: every entry the measure raises or
// adds, and a lowering of judged_laws, is written and recorded with its reason
// and owner. It also lowers and removes like Lower.
func Accept(old File, m Measure, reason, owner string) (File, error) {
	if err := CheckAcceptArgs(reason, owner); err != nil {
		return File{}, err
	}
	owner = strings.TrimSpace(owner)
	lowered := Lower(old, m)
	fl, fm, fo := flat(lowered), flat(m.asFile()), flat(old)
	recs := append([]Regression(nil), lowered.Regressions...)
	accepted := 0
	for k, mv := range fm {
		if !raises(fl, k, mv) {
			continue
		}
		fl[k] = mv
		var rec Regression
		rec, recs = recordFor(recs, k, mv, fo, reason, owner)
		recs = append(recs, rec)
		accepted++
	}
	if accepted == 0 {
		return File{}, ErrNothingToAccept
	}
	out := unflat(fl, sortedRecords(recs))
	out.Build = old.Build
	return out, nil
}

// raises reports whether writing mv at k raises the ledger entry there or adds
// one. judged_laws always has an entry, so it is only ever raised or not.
func raises(ledger map[ekey]float64, k ekey, mv float64) bool {
	cur, had := ledger[k]
	return !had || worse(k.kind, mv, cur)
}

// recordFor is the regression record for the entry k written at mv. Its from is
// the entry's value before the write, or nil for an added entry. An earlier
// record of the same entry is replaced and keeps its own from, which is the
// value the gate's diff base holds. The records left over are returned.
func recordFor(recs []Regression, k ekey, mv float64, before map[ekey]float64, reason, owner string) (Regression, []Regression) {
	rec := Regression{Kind: k.kind, Key: k.key, To: mv, Reason: reason, Owner: owner}
	if ov, was := before[k]; was {
		rec.From = &ov
	}
	for i := range recs {
		if recs[i].Kind == k.kind && recs[i].Key == k.key {
			rec.From = recs[i].From
			return rec, slices.Delete(slices.Clone(recs), i, i+1)
		}
	}
	return rec, recs
}

// Change is one entry that moved the wrong way: a raised or added entry, or a
// judged_laws that fell. Kind and Key are the ones a Regression carries; From
// is nil for an added entry.
type Change struct {
	Kind, Key string
	From      *float64
	To        float64
}

// Raised lists the entries of cur that are raised, or added, against old, and
// the judged_laws if it fell, in order of kind and key. These are the changes
// that need a regressions record (§6.7).
func Raised(old, cur File) []Change {
	fo, fc := flat(old), flat(cur)
	var out []Change
	for k, cv := range fc {
		ov, had := fo[k]
		switch {
		case !had:
			out = append(out, Change{Kind: k.kind, Key: k.key, To: cv})
		case worse(k.kind, cv, ov):
			out = append(out, Change{Kind: k.kind, Key: k.key, From: &ov, To: cv})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// NeedsAccept keeps, of the changes Raised lists, the ones the ratchet would fail
// without a record. A package fails only when its percentage falls and its
// uncovered count rises (§6.7), so a change to one of the two alone is not one:
// deleting tested code moves the percentage and not the count. The rest are
// whole.
func NeedsAccept(changes []Change) []Change {
	pct, unc := map[string]bool{}, map[string]bool{}
	for _, c := range changes {
		switch c.Kind {
		case kindPkgPct:
			pct[c.Key] = true
		case kindPkgUncovered:
			unc[c.Key] = true
		}
	}
	var out []Change
	for _, c := range changes {
		isPkg := c.Kind == kindPkgPct || c.Kind == kindPkgUncovered
		both := pct[c.Key] && unc[c.Key]
		if !isPkg || both {
			out = append(out, c)
		}
	}
	return out
}

// Kind says what a ledger key names.
type Kind int

// The two kinds of key --rename moves.
const (
	KindFunction Kind = iota
	KindPackage
)

// Inventory tells whether a key exists in the code today. Function identity is
// defined once, in internal/crap (§6.7); the package import path is the
// directory's.
type Inventory interface {
	Has(k Kind, key string) bool
}

// KeySets is an Inventory over the keys found in the source: every function
// identity and every package import path, whatever any build compiles.
type KeySets struct{ Functions, Packages []string }

// Has implements Inventory.
func (s KeySets) Has(k Kind, key string) bool {
	if k == KindPackage {
		return slices.Contains(s.Packages, key)
	}
	return slices.Contains(s.Functions, key)
}

// Rename rewrites one key of the ledger, a function or a package, and never a
// value. It refuses unless the old key has an entry, has vanished from the
// code, and the new key exists in the code and has no entry yet (§6.7). A
// regressions record of the old key follows it.
func Rename(old File, from, to string, inv Inventory) (File, error) {
	kind, err := keyKind(old, from)
	if err != nil {
		return File{}, err
	}
	if err := checkRename(old, from, to, kind, inv); err != nil {
		return File{}, err
	}
	out := copyFile(old)
	moveKey(&out, kind, from, to)
	return out, nil
}

// keyKind says whether the ledger holds from as a function or as a package.
func keyKind(old File, from string) (Kind, error) {
	_, isFn := old.Ledger.Functions[from]
	_, isPkg := old.Ledger.Packages[from]
	switch {
	case isFn && isPkg:
		return 0, refuse("%q is both a function key and a package key in the ledger; rename cannot tell which", from)
	case isFn:
		return KindFunction, nil
	case isPkg:
		return KindPackage, nil
	}
	return 0, refuse("the ledger has no entry for %q", from)
}

// checkRename applies the rules of §6.7: the old key has vanished from the code,
// the new key exists in it, and the ledger has no entry for the new key yet.
func checkRename(old File, from, to string, kind Kind, inv Inventory) error {
	_, toFn := old.Ledger.Functions[to]
	_, toPkg := old.Ledger.Packages[to]
	switch {
	case from == to:
		return refuse("the old and the new key are the same")
	case inv.Has(kind, from):
		return refuse("%q still exists in the code: a key is renamed only after it has vanished", from)
	case !inv.Has(kind, to):
		return refuse("%q does not exist in the code: a key is renamed only to one that exists", to)
	case toFn || toPkg:
		return refuse("the ledger already has an entry for %q", to)
	}
	return nil
}

// moveKey moves the entry of kind from one key to another, with its records.
func moveKey(f *File, kind Kind, from, to string) {
	if kind == KindFunction {
		f.Ledger.Functions[to] = f.Ledger.Functions[from]
		delete(f.Ledger.Functions, from)
	} else {
		f.Ledger.Packages[to] = f.Ledger.Packages[from]
		delete(f.Ledger.Packages, from)
	}
	for i, r := range f.Regressions {
		if r.Key == from && recordIsOf(kind, r.Kind) {
			f.Regressions[i].Key = to
		}
	}
}

// recordIsOf says whether a record kind names an entry of the kind of key.
func recordIsOf(kind Kind, recordKind string) bool {
	if kind == KindFunction {
		return recordKind == kindFunctions
	}
	return strings.HasPrefix(recordKind, "packages.")
}

// copyFile is a deep copy.
func copyFile(f File) File {
	out := unflat(flat(f), append([]Regression(nil), f.Regressions...))
	out.Build = f.Build
	return out
}
