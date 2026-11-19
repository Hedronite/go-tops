package ratchet

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/Hedronite/go-tops/internal/crap"
	"github.com/Hedronite/go-tops/internal/gobuild"
	"github.com/Hedronite/go-tops/internal/report"
)

// Outcome is the ratchet judgment of a run (the outcome of the report's ratchet
// object, §12.3).
type Outcome string

// The outcomes. Skip is for a leg that is not the ledger leg, and is never a pass.
const (
	Pass Outcome = "pass"
	Fail Outcome = "fail"
	Skip Outcome = "skip"
)

// Noise is the rise in a function's CRAP score the ratchet does not count: a
// function over the cap may not exceed its entry plus 0.01 (§6.7). tolerance
// absorbs the float64 representation of a decimal such as 14.2 + 0.01, so that
// 14.21 is not over 14.21.
const (
	Noise     = 0.01
	tolerance = 1e-9
)

// Measure is what a run measured, on the build it ran under.
type Measure struct {
	Cap   float64 // the CRAP cap
	Floor float64 // the coverage floor, in percent

	// Functions are the measured functions over the cap, and NotMeasured the
	// functions the run does not measure. A function a crap exemption covers is in
	// neither: the CRAP layer leaves it out of the violations it lists, so it has no
	// ledger entry and is not judged (§6.8).
	Functions   []crap.Function
	NotMeasured []crap.NotMeasured
	// Problems are the in-force crap exemptions that match no production file or
	// no function (§6.8): an exemption leaves the file with the code it covered,
	// so one that no longer matches fails the run, whatever the ledger holds.
	Problems []Failure

	// Packages holds each package under the coverage floor, with its percentage and
	// uncovered statements, as the baseline measures them. A package at or over the
	// floor is absent.
	Packages map[string]Package

	Findings   Counts // findings that no law maps, per linter and file
	Laws       Counts // findings that make a law refuse, per law ID and file
	Nolint     Counts // //nolint, per linter and file
	Ack        Counts // //tops:ack comments and acks.yaml entries, per law ID and file
	Exemptions int    // live exemption records
	Survivors  map[string]int
	JudgedLaws int
}

// Base is the diff base's copy of the ledger.
type Base struct {
	// Err is set when the base cannot be read: no merge base, a shallow clone, an
	// unknown ref, a ledger that does not parse. The judgment fails.
	Err error
	// Ledger is the copy at the merge base; nil means there is none, so the file in
	// the tree is a first ledger.
	Ledger *Ledger
	// Moved is set when the baseline path of this run's configuration is not the
	// one the base commit's go-tops.yaml names; Ledger is then the copy at the
	// base's own path.
	Moved *Moved
}

// Moved is a baseline that this run's configuration keeps at another path than the
// diff base did.
type Moved struct{ From, To string }

// Input is everything a judgment reads.
type Input struct {
	// Ledger is the baseline file in the working tree; nil when it is missing.
	Ledger *Ledger
	Base   Base
	// Run is the build the run's go commands used.
	Run gobuild.Build
	M   Measure
	// LedgerCompiled holds the key of every function the ledger build compiles,
	// found by loading with the recorded build (crap.Compiled). A leg that is not
	// the ledger leg needs it; the ledger leg does not.
	LedgerCompiled map[string]bool
	// LedgerFiles and LedgerPackages are what the ledger build selects from the
	// source tree (crap.Select): the files, and the packages with a compiled
	// non-test file. A leg that is not the ledger leg needs them (ruling C3d).
	LedgerFiles    map[string]bool
	LedgerPackages map[string]bool
}

// Failure is one thing that fails the ratchet.
type Failure struct {
	Kind string // functions, packages, findings, regressions, first ledger, ...
	Key  string
	Why  string
}

// Judgment is the result of the ratchet.
type Judgment struct {
	Outcome     Outcome
	LedgerBuild gobuild.Build
	RunBuild    gobuild.Build
	// Failures are what failed the ledger leg's ratchet.
	Failures []Failure
	// Advice are lines for the text output that change no verdict.
	Advice []string
	// HeldToCap is what a leg that is not the ledger leg judges at hard: the
	// functions the ledger build does not compile (ruling C3), and the findings,
	// suppressions, laws and packages in files or packages it does not compile
	// (ruling C3d). They fail the run while the ratchet judgment itself stays skip.
	HeldToCap []Failure
}

// Failed reports whether the run fails because of the ratchet: the judgment
// failed, or a skipping leg found a function over the cap that only it compiles.
func (j Judgment) Failed() bool { return j.Outcome == Fail || len(j.HeldToCap) > 0 }

// Report is the report's ratchet object (§12.3): the outcome and both builds.
func (j Judgment) Report() report.Ratchet {
	return report.Ratchet{Outcome: string(j.Outcome), LedgerBuild: reportBuild(j.LedgerBuild), RunBuild: reportBuild(j.RunBuild)}
}

func reportBuild(b gobuild.Build) report.Build {
	tags := slices.Clone(b.Tags)
	slices.Sort(tags)
	return report.Build{GOOS: b.GOOS, GOARCH: b.GOARCH, Tags: tags, CgoEnabled: b.CGOEnabled}
}

func (j *Judgment) fail(kind, key, format string, args ...any) {
	j.Failures = append(j.Failures, Failure{Kind: kind, Key: key, Why: fmt.Sprintf(format, args...)})
}

// Judge judges a run under enforce: ratchet. Only the ledger leg runs the
// comparison with the ledger; any other leg skips it, naming the ledger build.
func Judge(in Input) Judgment {
	j := Judgment{RunBuild: in.Run, LedgerBuild: in.Run}
	j.advise(in)
	switch {
	case in.Ledger == nil:
		j.fail("baseline", "", "no baseline file: under ratchet a missing baseline fails, and deleting the file is not a reset")
	case !in.Run.Equal(in.Ledger.Build):
		j.LedgerBuild = in.Ledger.Build
		j.otherLeg(in)
		return j
	default:
		j.LedgerBuild = in.Ledger.Build
		j.ledgerLeg(in)
	}
	j.Outcome = Pass
	if len(j.Failures) > 0 {
		j.Outcome = Fail
	}
	return j
}

// advise adds the lines that change no verdict.
func (j *Judgment) advise(in Input) {
	j.noteBuildChange(in)
	if m := in.Base.Moved; m != nil {
		j.Advice = append(j.Advice, fmt.Sprintf("ratchet: the baseline moved from %s at the diff base to %s here; judged against the base copy, never as a first ledger\n", m.From, m.To))
	}
}

// noteBuildChange says, without judging it, that the ledger's build is not the
// build of its copy at the diff base. Moving the ledger leg is not a rule of this
// release: the one guard is the CI assertion that the ledger leg never reports a
// skipped ratchet (§6.7). The line puts the move in front of the reviewer.
func (j *Judgment) noteBuildChange(in Input) {
	if in.Ledger == nil || in.Base.Ledger == nil || in.Ledger.Build.Equal(in.Base.Ledger.Build) {
		return
	}
	j.Advice = append(j.Advice, fmt.Sprintf("ratchet: the ledger's build (%s) differs from its copy at the diff base (%s)\n",
		describe(in.Ledger.Build), describe(in.Base.Ledger.Build)))
}

func (j *Judgment) ledgerLeg(in Input) {
	j.Failures = append(j.Failures, in.M.Problems...)
	j.diffBase(in)
	j.functions(in)
	j.notMeasured(in)
	j.packages(in)
	j.counts("findings", in.M.Findings, in.Ledger.Findings)
	j.counts("laws", in.M.Laws, in.Ledger.Laws)
	j.counts("suppressions.nolint", in.M.Nolint, in.Ledger.Nolint)
	j.counts("suppressions.ack", in.M.Ack, in.Ledger.Ack)
	j.scalars(in)
}

// otherLeg is the judgment of a run whose build is not the ledger's: the
// ratchet is skipped, and the functions the ledger leg does not compile are held
// to the cap.
func (j *Judgment) otherLeg(in Input) {
	j.Outcome = Skip
	if in.LedgerCompiled == nil || in.LedgerFiles == nil || in.LedgerPackages == nil {
		j.Outcome = Fail
		j.fail("compiled set", "", "this run is not the ledger leg (ledger build %s), and it was not given the functions, files and packages that build compiles", describe(in.Ledger.Build))
		return
	}
	j.heldFunctions(in)
	j.heldSources(in)
	// An exemption that matches nothing fails on every leg: it names files, which
	// exist whatever the build.
	j.HeldToCap = append(j.HeldToCap, in.M.Problems...)
}

// heldFunctions holds to the cap what the ledger build does not compile: a function
// has no entry there, and a leg that compiles it judges it as a new function. A
// function the ledger build compiles is the ledger leg's, entry or not.
func (j *Judgment) heldFunctions(in Input) {
	for _, f := range in.M.Functions {
		if !in.LedgerCompiled[f.Key] && f.CRAP > in.M.Cap {
			j.HeldToCap = append(j.HeldToCap, Failure{
				Kind: "functions", Key: f.Key,
				Why: fmt.Sprintf("scores %.2f, over the cap %g; the ledger build does not compile it, so it has no entry", f.CRAP, in.M.Cap),
			})
		}
	}
	for _, f := range in.M.NotMeasured {
		if !in.LedgerCompiled[f.Key] && float64(f.Comp) > in.M.Cap {
			j.HeldToCap = append(j.HeldToCap, notMeasuredFailure(f, in.M.Cap))
		}
	}
}

// heldSources judges at hard what sits in a file or a package the ledger build does
// not compile (ruling C3d): the ledger leg never sees it, so no ledger entry can
// cover it and nothing else would ever judge it. A finding, a suppression or a law
// count there fails past its ledger entry, absent meaning 0, and a package there is
// held to the coverage floor. What sits in the ledger build's files stays the
// ledger leg's, reported here and never failing this leg (ruling C3c).
func (j *Judgment) heldSources(in Input) {
	l, files := in.Ledger, in.LedgerFiles
	j.heldCounts("findings", in.M.Findings, l.Findings, files)
	j.heldCounts("laws", in.M.Laws, l.Laws, files)
	j.heldCounts("suppressions.nolint", in.M.Nolint, l.Nolint, files)
	j.heldCounts("suppressions.ack", in.M.Ack, l.Ack, files)
	for _, path := range slices.Sorted(maps.Keys(in.M.Packages)) {
		if in.LedgerPackages[path] {
			continue
		}
		p := in.M.Packages[path]
		j.HeldToCap = append(j.HeldToCap, Failure{
			Kind: "packages", Key: path,
			Why: fmt.Sprintf("covers %.1f%%, under the floor %g%%, and the ledger build does not compile this package, so it has no entry", p.StatementsPct, in.M.Floor),
		})
	}
}

// heldCounts fails each count in a file the ledger build does not compile that is
// over the ledger's entry for it, absent meaning 0.
func (j *Judgment) heldCounts(kind string, run, ledger Counts, ledgerFiles map[string]bool) {
	for _, name := range slices.Sorted(maps.Keys(run)) {
		for _, file := range slices.Sorted(maps.Keys(run[name])) {
			if n, entry := run[name][file], ledger[name][file]; !ledgerFiles[file] && n > entry {
				j.HeldToCap = append(j.HeldToCap, Failure{
					Kind: kind, Key: name + " " + file,
					Why: fmt.Sprintf("counts %d in a file the ledger build does not compile, over its ledger entry %d", n, entry),
				})
			}
		}
	}
}

func notMeasuredFailure(f crap.NotMeasured, limit float64) Failure {
	return Failure{
		Kind: "not measured", Key: f.Key,
		Why: fmt.Sprintf("not measured on this run, and its complexity %d is over the cap %g", f.Comp, limit),
	}
}

func describe(b gobuild.Build) string {
	cgo := "cgo off"
	if b.CGOEnabled {
		cgo = "cgo on"
	}
	text := fmt.Sprintf("%s/%s %s", b.GOOS, b.GOARCH, cgo)
	if len(b.Tags) > 0 {
		text += " tags=" + strings.Join(b.Tags, ",")
	}
	return text
}

// functions holds each measured function to its entry plus the noise, or to the
// cap when it has none (§6.7). An exempt function is not judged.
func (j *Judgment) functions(in Input) {
	for _, f := range in.M.Functions {
		if entry, ok := in.Ledger.Functions[f.Key]; ok {
			if f.CRAP > entry+Noise+tolerance {
				j.fail("functions", f.Key, "scores %.2f, over its ledger entry %.2f plus %g", f.CRAP, entry, Noise)
			}
			continue
		}
		if f.CRAP > in.M.Cap {
			j.fail("functions", f.Key, "scores %.2f, over the cap %g, and has no ledger entry", f.CRAP, in.M.Cap)
		}
	}
}

// notMeasured fails a function that this run does not measure and whose
// complexity is above the cap: CRAP is at least comp at every coverage (§6.2).
func (j *Judgment) notMeasured(in Input) {
	for _, f := range in.M.NotMeasured {
		if float64(f.Comp) > in.M.Cap {
			ff := notMeasuredFailure(f, in.M.Cap)
			j.fail(ff.Kind, ff.Key, "%s", ff.Why)
		}
	}
}

// packages applies the two-condition rule: a package under the floor fails only
// when its percentage falls and its uncovered statements rise; one at or over
// the floor is held to the floor, which it meets (§6.7).
func (j *Judgment) packages(in Input) {
	for _, path := range slices.Sorted(maps.Keys(in.M.Packages)) {
		p := in.M.Packages[path]
		entry, ok := in.Ledger.Packages[path]
		switch {
		case !ok:
			j.fail("packages", path, "covers %.1f%%, under the floor %g%%, and has no ledger entry", p.StatementsPct, in.M.Floor)
		case p.StatementsPct < entry.StatementsPct-tolerance && p.Uncovered > entry.Uncovered:
			j.fail("packages", path, "covers %.1f%% (was %.1f%%) and has %d uncovered statements (was %d)",
				p.StatementsPct, entry.StatementsPct, p.Uncovered, entry.Uncovered)
		}
	}
}

// counts holds each count to its entry, per name and file; absent means 0.
func (j *Judgment) counts(kind string, run, ledger Counts) {
	for _, name := range slices.Sorted(maps.Keys(run)) {
		for _, file := range slices.Sorted(maps.Keys(run[name])) {
			if n, entry := run[name][file], ledger[name][file]; n > entry {
				j.fail(kind, name+" "+file, "counts %d, over its ledger entry %d", n, entry)
			}
		}
	}
}

// scalars judges the exemption count, which may only fall, the survivors, and
// judged_laws, which may not fall.
func (j *Judgment) scalars(in Input) {
	if in.M.Exemptions > in.Ledger.Exemptions {
		j.fail("exemptions", "", "%d live exemptions, over the ledger's %d", in.M.Exemptions, in.Ledger.Exemptions)
	}
	for _, file := range slices.Sorted(maps.Keys(in.M.Survivors)) {
		if n, entry := in.M.Survivors[file], in.Ledger.Survivors[file]; n > entry {
			j.fail("survivors", file, "%d unexplained survivors, over its ledger entry %d", n, entry)
		}
	}
	if in.M.JudgedLaws < in.Ledger.JudgedLaws {
		j.fail("judged_laws", "", "%d laws judged, under the ledger's %d", in.M.JudgedLaws, in.Ledger.JudgedLaws)
	}
}

func near(a, b, within float64) bool { return math.Abs(a-b) <= within+tolerance }
