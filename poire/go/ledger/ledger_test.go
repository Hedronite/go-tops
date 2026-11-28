package ledger

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestTheZeroValueIsReady(t *testing.T) {
	var l Ledger
	if _, err := l.Balance("a"); !errors.Is(err, ErrUnknownAccount) {
		t.Fatalf("Balance on an empty ledger = %v, want ErrUnknownAccount", err)
	}
	if s := l.Snapshot(); len(s) != 0 {
		t.Fatalf("Snapshot of an empty ledger = %v, want empty", s)
	}
	l.Credit("a", 5)
	if got, err := l.Balance("a"); err != nil || got != 5 {
		t.Fatalf("Balance = %d, %v; want 5, nil", got, err)
	}
}

func TestDebitErrorsKeepTheirCause(t *testing.T) {
	var l Ledger
	l.Credit("a", 10)
	if err := l.Debit("a", 4); err != nil {
		t.Fatalf("Debit = %v, want nil", err)
	}
	if got, err := l.Balance("a"); err != nil || got != 6 {
		t.Fatalf("balance after debit = %d, %v; want 6, nil", got, err)
	}
	if err := l.Debit("b", 1); !errors.Is(err, ErrUnknownAccount) || err.Error() != "debit b: ledger: unknown account" {
		t.Fatalf("Debit unknown = %v, want a wrapped ErrUnknownAccount", err)
	}
	err := l.Debit("a", 7)
	var over *OverdraftError
	if !errors.As(err, &over) || over.Balance != 6 || over.Debit != 7 {
		t.Fatalf("Debit too much = %v, want an OverdraftError for 7 against 6", err)
	}
	if err.Error() != "ledger: debit 7 exceeds balance 6 of a" {
		t.Fatalf("message = %q", err.Error())
	}
	if err := l.Debit("a", 6); err != nil {
		t.Fatalf("debiting the whole balance = %v, want nil", err)
	}
	if got, err := l.Balance("a"); err != nil || got != 0 {
		t.Fatalf("balance after emptying = %d, %v; want 0, nil", got, err)
	}
}

// GO-07's harm: flattening the cause to text, as %v does, loses it.
func TestWrappingWithVLosesTheCause(t *testing.T) {
	var l Ledger
	err := l.Debit("x", 1)
	erased := errors.New("transfer: " + err.Error()) // what %v leaves: the words, no chain
	if errors.Is(erased, ErrUnknownAccount) {
		t.Fatal("errors.Is found the cause in a flattened message")
	}
	if !errors.Is(fmt.Errorf("transfer: %w", err), ErrUnknownAccount) {
		t.Fatal("errors.Is lost the cause through %w")
	}
}

// GO-06's harm: a nil pointer in an error interface is not a nil error.
func TestATypedNilIsNotNil(t *testing.T) {
	typed := func() error {
		var over *OverdraftError // stays nil
		return over
	}
	if typed() == nil {
		t.Fatal("a typed nil compared equal to nil")
	}
}

func TestConcurrentCreditsAllLand(t *testing.T) {
	var l Ledger
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() { l.Credit("a", 1) })
	}
	wg.Wait()
	if got, err := l.Balance("a"); err != nil || got != 100 {
		t.Fatalf("balance = %d, %v; want 100, nil", got, err)
	}
	snap := l.Snapshot()
	l.Credit("a", 1)
	if snap["a"] != 100 {
		t.Fatalf("snapshot changed after a later credit: %d", snap["a"])
	}
}
