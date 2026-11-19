package ratchet

import (
	"maps"
	"slices"
)

func (j *Judgment) firstFunctions(in Input) {
	measured := map[string]float64{}
	for _, f := range in.M.Functions {
		measured[f.Key] = f.CRAP
	}
	for _, key := range slices.Sorted(maps.Keys(in.Ledger.Functions)) {
		entry := in.Ledger.Functions[key]
		score, ok := measured[key]
		switch {
		case !ok:
			j.fail("first ledger", key, "has an entry, and no function with this key is measured on the ledger leg")
		case in.M.Exempt[key]:
			j.fail("first ledger", key, "has an entry, and an exemption covers the function")
		case score <= in.M.Cap:
			j.fail("first ledger", key, "has an entry, and the function scores %.2f, within the cap %g", score, in.M.Cap)
		case !near(entry, score, Noise):
			j.fail("first ledger", key, "records %.2f, this run measures %.2f", entry, score)
		}
	}
}

func (j *Judgment) firstPackages(in Input) {
	for _, path := range slices.Sorted(maps.Keys(in.Ledger.Packages)) {
		entry := in.Ledger.Packages[path]
		p, ok := in.M.Packages[path]
		switch {
		case !ok:
			j.fail("first ledger", path, "has an entry, and the ledger leg measures this package at or over the floor, or not at all")
		case !near(entry.StatementsPct, p.StatementsPct, 0) || entry.Uncovered != p.Uncovered:
			j.fail("first ledger", path, "records %.1f%% and %d uncovered, this run measures %.1f%% and %d",
				entry.StatementsPct, entry.Uncovered, p.StatementsPct, p.Uncovered)
		}
	}
}

// firstCounts holds each count of a first ledger to exactly what the run counts.
func (j *Judgment) firstCounts(kind string, run, ledger Counts) {
	for _, name := range slices.Sorted(maps.Keys(ledger)) {
		for _, file := range slices.Sorted(maps.Keys(ledger[name])) {
			if n, entry := run[name][file], ledger[name][file]; n != entry {
				j.fail("first ledger", name+" "+file, "%s records %d, this run counts %d", kind, entry, n)
			}
		}
	}
}
