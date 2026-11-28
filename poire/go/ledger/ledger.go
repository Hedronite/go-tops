// Package ledger keeps account balances in memory.
//
// Go-POIRE conditions cured here: GO-04 unsynchronized shared state, GO-06
// typed-nil error, GO-07 erased error chain, GO-15 constructor ceremony and a
// useless zero value, GO-31 lock held across I/O.
package ledger

import (
	"errors"
	"fmt"
	"maps"
	"sync"
)

// ErrUnknownAccount is returned for an account the ledger has never credited.
var ErrUnknownAccount = errors.New("ledger: unknown account")

// OverdraftError reports a debit larger than the balance. Find it with [errors.As].
type OverdraftError struct {
	Account string
	Balance int64
	Debit   int64
}

func (e *OverdraftError) Error() string {
	return fmt.Sprintf("ledger: debit %d exceeds balance %d of %s", e.Debit, e.Balance, e.Account)
}

// Ledger holds balances. Its zero value is an empty ledger, ready to use, and
// safe for concurrent use. A Ledger must not be copied after first use.
type Ledger struct {
	mu       sync.Mutex // guards balances
	balances map[string]int64
}

// Credit adds amount to account, opening the account if it is new.
func (l *Ledger) Credit(account string, amount int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.balances == nil {
		l.balances = make(map[string]int64)
	}
	l.balances[account] += amount
}

// Debit subtracts amount from account.
//
// The error wraps [ErrUnknownAccount] for an unknown account, and is an
// [*OverdraftError] when the balance is too small.
func (l *Ledger) Debit(account string, amount int64) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	balance, ok := l.balances[account]
	if !ok {
		return fmt.Errorf("debit %s: %w", account, ErrUnknownAccount)
	}
	if amount > balance {
		return &OverdraftError{Account: account, Balance: balance, Debit: amount}
	}
	l.balances[account] = balance - amount
	return nil
}

// Balance reports the balance of account. The error wraps [ErrUnknownAccount].
func (l *Ledger) Balance(account string) (int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	balance, ok := l.balances[account]
	if !ok {
		return 0, fmt.Errorf("balance %s: %w", account, ErrUnknownAccount)
	}
	return balance, nil
}

// Snapshot returns a copy of every balance. A caller writes the copy to disk or
// to the network after the lock is released, so slow I/O never blocks a credit.
func (l *Ledger) Snapshot() map[string]int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return maps.Clone(l.balances)
}
