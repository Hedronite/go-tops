package baseline

import (
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
	// Scored is every production function the run measured, over the cap or not.
	// A function in a file outside the run's build is not in it (ruling C2).
	Scored map[string]bool
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
	l := &f.Ledger
	two := func(c *Counts, k ekey, v float64) {
		name, file, _ := strings.Cut(k.key, nameFileSeparator)
		if *c == nil {
			*c = Counts{}
		}
		if (*c)[name] == nil {
			(*c)[name] = map[string]int{}
		}
		(*c)[name][file] = int(v)
	}
	for k, v := range m {
		switch k.kind {
		case kindFunctions:
			if l.Functions == nil {
				l.Functions = map[string]float64{}
			}
			l.Functions[k.key] = v
		case kindPkgPct:
			u, ok := m[ekey{kindPkgUncovered, k.key}]
			if !ok {
				continue
			}
			if l.Packages == nil {
				l.Packages = map[string]Package{}
			}
			l.Packages[k.key] = Package{StatementsPct: v, Uncovered: int(u)}
		case kindFindings:
			two(&l.Findings, k, v)
		case kindLaws:
			two(&l.Laws, k, v)
		case kindNolint:
			two(&l.Nolint, k, v)
		case kindAck:
			two(&l.Ack, k, v)
		case kindSurvivors:
			if l.Survivors == nil {
				l.Survivors = map[string]int{}
			}
			l.Survivors[k.key] = int(v)
		case kindExemptions:
			l.Exemptions = int(v)
		}
	}
	if len(recs) == 0 {
		f.Regressions = nil
	}
	return f
}

// asFile is the ledger a measure describes, with nothing recorded. The round
// trip through the flat form copies every map and drops counts of zero.
func (m Measure) asFile() File {
	return unflat(flat(File{JudgedLaws: m.JudgedLaws, Ledger: Ledger{
		Functions: m.Functions, Packages: m.Packages, Findings: m.Findings, Nolint: m.Nolint, Ack: m.Ack, Exemptions: m.Exemptions,
	}}), nil)
}

// Init is the first ledger: what was measured, and nothing recorded (§6.7).
func Init(m Measure) File { return m.asFile() }

// Lower is a plain write: it lowers a value, removes an entry that is no longer
// over the line, and raises judged_laws. It never adds an entry and never raises
// one. A package entry tightens toward coverage, its percentage rising and its
// uncovered count falling, each on its own. A function entry is removed only
// when the run scored its function at or under the cap, or when it exists in no
// file; an entry the run did not measure is carried unchanged (ruling C2). The laws and survivors kinds are
// fed by other commands and pass through untouched. A regressions record is
// removed when its entry is back at the value it had before, and follows the
// entry down while it is on the way.
func Lower(old File, m Measure, inv Inventory) File {
	fo, fm := flat(old), flat(m.asFile())
	out := map[ekey]float64{}
	for k, ov := range fo {
		if k.kind == kindLaws || k.kind == kindSurvivors {
			out[k] = ov
			continue
		}
		mv, measured := fm[k]
		if !measured {
			// Paid back or gone, unless the run did not measure it: a function in a
			// file outside the run's build is carried (ruling C2).
			if k.kind == kindFunctions && !m.Scored[k.key] && inv.Has(KindFunction, k.key) {
				out[k] = ov
			}
			continue
		}
		out[k] = stricter(k.kind, ov, mv)
	}
	return unflat(out, prune(old.Regressions, out))
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

// Accept is a write that accepts regressions: every entry the measure raises or
// adds, and a lowering of judged_laws, is written and recorded with its reason
// and owner. It also lowers and removes like Lower.
func Accept(old File, m Measure, reason, owner string, inv Inventory) (File, error) {
	if len(strings.Fields(reason)) < 3 {
		return File{}, refuse("--reason needs at least three words saying why the debt grows")
	}
	if owner = strings.TrimSpace(owner); owner == "" || len(strings.Fields(owner)) != 1 {
		return File{}, refuse("--owner needs one handle, a person or a team")
	}
	lowered := Lower(old, m, inv)
	fl, fm, fo := flat(lowered), flat(m.asFile()), flat(old)
	recs := append([]Regression(nil), lowered.Regressions...)
	accepted := 0
	for k, mv := range fm {
		cur, had := fl[k]
		switch {
		case !had:
			// A new entry. judged_laws is always had.
		case worse(k.kind, mv, cur):
			// An entry raised.
		default:
			continue
		}
		fl[k] = mv
		rec := Regression{Kind: k.kind, Key: k.key, To: mv, Reason: reason, Owner: owner}
		if ov, was := fo[k]; was {
			rec.From = &ov
		}
		for i := range recs {
			if recs[i].Kind == k.kind && recs[i].Key == k.key {
				rec.From = recs[i].From // the value the diff base holds
				recs = append(recs[:i], recs[i+1:]...)
				break
			}
		}
		recs = append(recs, rec)
		accepted++
	}
	if accepted == 0 {
		return File{}, ErrNothingToAccept
	}
	return unflat(fl, sortedRecords(recs)), nil
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

// Rename rewrites one key of the ledger, a function or a package, and never a
// value. It refuses unless the old key has an entry, has vanished from the
// code, and the new key exists in the code and has no entry yet (§6.7). A
// regressions record of the old key follows it.
func Rename(old File, from, to string, inv Inventory) (File, error) {
	_, isFn := old.Ledger.Functions[from]
	_, isPkg := old.Ledger.Packages[from]
	var kind Kind
	switch {
	case isFn && isPkg:
		return File{}, refuse("%q is both a function key and a package key in the ledger; rename cannot tell which", from)
	case isFn:
		kind = KindFunction
	case isPkg:
		kind = KindPackage
	default:
		return File{}, refuse("the ledger has no entry for %q", from)
	}
	_, toFn := old.Ledger.Functions[to]
	_, toPkg := old.Ledger.Packages[to]
	switch {
	case from == to:
		return File{}, refuse("the old and the new key are the same")
	case inv.Has(kind, from):
		return File{}, refuse("%q still exists in the code: a key is renamed only after it has vanished", from)
	case !inv.Has(kind, to):
		return File{}, refuse("%q does not exist in the code: a key is renamed only to one that exists", to)
	case toFn || toPkg:
		return File{}, refuse("the ledger already has an entry for %q", to)
	}
	out := copyFile(old)
	if kind == KindFunction {
		out.Ledger.Functions[to] = out.Ledger.Functions[from]
		delete(out.Ledger.Functions, from)
	} else {
		out.Ledger.Packages[to] = out.Ledger.Packages[from]
		delete(out.Ledger.Packages, from)
	}
	for i, r := range out.Regressions {
		onKind := (kind == KindFunction && r.Kind == kindFunctions) || (kind == KindPackage && strings.HasPrefix(r.Kind, "packages."))
		if r.Key == from && onKind {
			out.Regressions[i].Key = to
		}
	}
	return out, nil
}

// copyFile is a deep copy.
func copyFile(f File) File {
	return unflat(flat(f), append([]Regression(nil), f.Regressions...))
}
