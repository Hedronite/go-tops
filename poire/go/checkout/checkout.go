// Package checkout prices a cart in the buyer's currency.
//
// Go-POIRE condition cured here: GO-14 producer-side interface. The interface
// checkout needs is declared here, where it is used, with the one method it
// calls. The producer, package quote, returns a concrete type.
package checkout

import (
	"context"
	"fmt"

	"github.com/Hedronite/go-tops/poire/go/quote"
)

// Rates is what checkout needs from an exchange-rate source. [*quote.Client]
// satisfies it; a test passes a function.
type Rates interface {
	Rate(ctx context.Context, from, to string) (quote.Rate, error)
}

// Total sums amounts, given in minor units of from, and converts the sum into
// to, rounding toward zero.
func Total(ctx context.Context, rates Rates, from, to string, amounts []int64) (int64, error) {
	var sum int64
	for _, a := range amounts {
		sum += a
	}
	if from == to {
		return sum, nil
	}
	r, err := rates.Rate(ctx, from, to)
	if err != nil {
		return 0, fmt.Errorf("checkout total: %w", err)
	}
	return sum * r.Micro / 1_000_000, nil
}
