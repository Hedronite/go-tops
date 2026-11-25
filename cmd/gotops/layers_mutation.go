package main

import (
	"time"

	_ "github.com/Hedronite/go-tops/internal/layers/mutation"
	"github.com/Hedronite/go-tops/internal/mutate"
	"github.com/Hedronite/go-tops/internal/survivors"
)

// The mutation layer judges survivors against the ledger of §6.4.
func init() { mutate.DefaultLedger = loadLedger }

// ledgerOn is a ledger as the gate judges it on one day.
type ledgerOn struct {
	ledger survivors.Ledger
	today  time.Time
}

func (l ledgerOn) Explains(k mutate.SurvivorKey) bool { return l.ledger.Explains(k, l.today) }

func loadLedger(dir, rel string, today time.Time) (mutate.Ledger, error) {
	l, err := survivors.Load(dir, rel)
	if err != nil {
		return nil, err
	}
	return ledgerOn{ledger: l, today: today}, nil
}
