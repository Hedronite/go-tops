package mutate

import "time"

// SurvivorKey uniquely identifies a mutated source location without a line number.
type SurvivorKey struct {
	File     string `json:"file"`
	Function string `json:"function"`
	Mutator  string `json:"mutator"`
	LineHash string `json:"line_hash"`
}

// String returns the canonical colon-delimited survivor key.
func (k SurvivorKey) String() string {
	return k.File + ":" + k.Function + ":" + k.Mutator + ":" + k.LineHash
}

// Survivor describes a surviving or uncovered mutant tracked by the gate.
type Survivor struct {
	Key       SurvivorKey
	Line      int
	Status    string
	Explained *string
}

// Ledger reports whether a surviving mutant is explained by a recorded entry.
type Ledger interface {
	Explains(key SurvivorKey) bool
}

// LedgerLoader loads a survivor ledger relative to module dir on today.
type LedgerLoader func(dir, ledgerPath string, today time.Time) (Ledger, error)

// DefaultLedger loads the module survivor ledger when configured by a downstream layer.
var DefaultLedger LedgerLoader
