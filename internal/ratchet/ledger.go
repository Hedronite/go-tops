// Package ratchet judges a run against the debt ledger, .go-tops/baseline.json,
// under enforce: ratchet (GO_TOPS.md §6.7).
//
// The ledger lists only what is over the line today. The judge holds everything
// else to its hard rule, and holds each entry to its own limit: a function to
// its score plus 0.01, a package under its coverage floor to a percentage that
// may not fall while its uncovered statements rise, a count to its entry. It
// reads the ledger at the merge base of the diff base through git: a raised or
// added entry, or a lowered judged_laws, needs a regressions record, and a repo
// whose base has no ledger is a first ledger, which must equal what the run
// measured.
//
// Only the ledger leg judges. A run whose build is not the ledger's skips the
// ratchet, naming the ledger build, and holds to the cap the functions the
// ledger leg does not compile (ruling C3).
//
// The file's reading, its kinds and keys, what a raised entry is and what a run
// measures for the ledger belong to internal/baseline; this package reads them
// from there and defines none of them again.
package ratchet

import (
	"github.com/Hedronite/go-tops/internal/baseline"
	"github.com/Hedronite/go-tops/internal/gobuild"
)

// The types of the baseline file, as the baseline package defines them.
type (
	// Package is a ledger entry of kind packages: a package under the coverage
	// floor, with the percentage and the uncovered statements when it was written.
	Package = baseline.Package
	// Counts is a count per name (a linter or a law ID), then per file.
	Counts = baseline.Counts
	// Regression is one accepted regression, written by --accept-regression.
	Regression = baseline.Regression
)

// Ledger is the baseline file as the judge reads it: the file's build, its
// entries by kind, judged_laws and the regressions records.
type Ledger struct {
	Build       gobuild.Build
	Functions   map[string]float64
	Packages    map[string]Package
	Findings    Counts
	Laws        Counts
	Nolint      Counts
	Ack         Counts
	Exemptions  int
	Survivors   map[string]int
	JudgedLaws  int
	Regressions []Regression
}

// FromFile is the judge's view of a parsed baseline file.
func FromFile(f baseline.File) *Ledger {
	l := f.Ledger
	return &Ledger{
		Build: f.Build, Functions: l.Functions, Packages: l.Packages,
		Findings: l.Findings, Laws: l.Laws, Nolint: l.Nolint, Ack: l.Ack,
		Exemptions: l.Exemptions, Survivors: l.Survivors,
		JudgedLaws: f.JudgedLaws, Regressions: f.Regressions,
	}
}

// File is the baseline file a Ledger describes.
func (l *Ledger) File() baseline.File {
	return baseline.File{
		Build: l.Build,
		Ledger: baseline.Ledger{
			Functions: l.Functions, Packages: l.Packages, Findings: l.Findings, Laws: l.Laws,
			Nolint: l.Nolint, Ack: l.Ack, Exemptions: l.Exemptions, Survivors: l.Survivors,
		},
		JudgedLaws: l.JudgedLaws, Regressions: l.Regressions,
	}
}

// Parse reads a baseline file with the baseline package's reader.
func Parse(raw []byte) (*Ledger, error) {
	f, err := baseline.Parse(raw)
	if err != nil {
		return nil, err
	}
	return FromFile(f), nil
}
