package checkout

import (
	"context"
	"errors"
	"testing"

	"github.com/Hedronite/go-tops/poire/go/quote"
)

// The producer satisfies the consumer's interface without naming it.
var _ Rates = (*quote.Client)(nil)

// rateFunc is the whole fake: no mock framework, no generated code.
type rateFunc func(ctx context.Context, from, to string) (quote.Rate, error)

func (f rateFunc) Rate(ctx context.Context, from, to string) (quote.Rate, error) {
	return f(ctx, from, to)
}

func TestTotalConvertsAndRoundsTowardZero(t *testing.T) {
	rates := rateFunc(func(_ context.Context, from, to string) (quote.Rate, error) {
		return quote.Rate{From: from, To: to, Micro: 1_500_000}, nil
	})
	got, err := Total(t.Context(), rates, "EUR", "USD", []int64{100, 233})
	if err != nil || got != 499 {
		t.Fatalf("Total = %d, %v; want 499, nil", got, err)
	}
}

func TestTotalSkipsTheLookupForOneCurrency(t *testing.T) {
	rates := rateFunc(func(context.Context, string, string) (quote.Rate, error) {
		t.Fatal("a same-currency total asked for a rate")
		return quote.Rate{}, nil
	})
	if got, err := Total(t.Context(), rates, "EUR", "EUR", []int64{1, 2}); err != nil || got != 3 {
		t.Fatalf("Total = %d, %v; want 3, nil", got, err)
	}
}

func TestTotalKeepsTheRateError(t *testing.T) {
	rates := rateFunc(func(context.Context, string, string) (quote.Rate, error) {
		return quote.Rate{}, quote.ErrBadRate
	})
	if _, err := Total(t.Context(), rates, "EUR", "USD", []int64{1}); !errors.Is(err, quote.ErrBadRate) {
		t.Fatalf("Total error = %v, want ErrBadRate in the chain", err)
	}
}
