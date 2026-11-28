// Package quote fetches exchange rates from an HTTP service.
//
// Go-POIRE conditions cured here: GO-03 severed context, GO-09 unbounded
// input, GO-11 leaked resource and missing timeout, GO-14 producer-side
// interface (New returns the concrete type; the interface lives with its
// consumer, package checkout).
package quote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// MaxBody is the largest response body a Client reads, in bytes.
const MaxBody = 64 << 10

// ErrTooLarge is returned when a response body exceeds [MaxBody].
var ErrTooLarge = errors.New("quote: response too large")

// ErrBadRate is returned when the service answers with a rate that is not
// for the pair asked about, or is not positive.
var ErrBadRate = errors.New("quote: bad rate")

// StatusError reports a response status other than 200.
type StatusError struct {
	Code int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("quote: status %d", e.Code)
}

// Rate is the price of one unit of From in To, in millionths.
type Rate struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Micro int64  `json:"micro"`
}

// Client fetches rates from one service.
type Client struct {
	base string
	http *http.Client
}

// New returns a Client for the service at base. Every request it makes is
// bounded by a five-second timeout as well as by the caller's context.
func New(base string) *Client {
	return &Client{base: base, http: &http.Client{Timeout: 5 * time.Second}}
}

// Rate fetches the rate from one currency to another.
func (c *Client) Rate(ctx context.Context, from, to string) (Rate, error) {
	target := c.base + "/rate?" + url.Values{"from": {from}, "to": {to}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return Rate{}, fmt.Errorf("quote %s/%s: %w", from, to, err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Rate{}, fmt.Errorf("quote %s/%s: %w", from, to, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Rate{}, &StatusError{Code: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
	if err != nil {
		return Rate{}, fmt.Errorf("quote %s/%s: read: %w", from, to, err)
	}
	if len(body) > MaxBody {
		return Rate{}, ErrTooLarge
	}
	var r Rate
	if err := json.Unmarshal(body, &r); err != nil {
		return Rate{}, fmt.Errorf("quote %s/%s: decode: %w", from, to, err)
	}
	if r.From != from || r.To != to || r.Micro <= 0 {
		return Rate{}, fmt.Errorf("quote %s/%s: %w", from, to, ErrBadRate)
	}
	return r, nil
}
