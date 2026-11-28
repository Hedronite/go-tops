package quote

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func serve(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(srv.URL)
}

func TestRateReadsAGoodAnswer(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("from") != "EUR" || r.URL.Query().Get("to") != "USD" {
			http.Error(w, "bad pair", http.StatusBadRequest)
			return
		}
		if _, err := fmt.Fprint(w, `{"from":"EUR","to":"USD","micro":1080000}`); err != nil {
			t.Error(err)
		}
	})
	got, err := c.Rate(t.Context(), "EUR", "USD")
	if err != nil || got != (Rate{From: "EUR", To: "USD", Micro: 1_080_000}) {
		t.Fatalf("Rate = %+v, %v", got, err)
	}
}

func TestRateRefusesWhatItShouldNotTrust(t *testing.T) {
	cases := []struct {
		name string
		body string
		code int
		want func(error) bool
	}{
		{"status", "", http.StatusTeapot, func(err error) bool {
			var se *StatusError
			return errors.As(err, &se) && se.Code == http.StatusTeapot && se.Error() == "quote: status 418"
		}},
		{"too large", strings.Repeat(" ", MaxBody+1), http.StatusOK, func(err error) bool { return errors.Is(err, ErrTooLarge) }},
		{"exactly the limit is read", strings.Repeat(" ", MaxBody-2) + "{}", http.StatusOK, func(err error) bool { return errors.Is(err, ErrBadRate) }},
		{"not json", "nope", http.StatusOK, func(err error) bool { return err != nil && strings.Contains(err.Error(), "decode") }},
		{"wrong pair", `{"from":"GBP","to":"USD","micro":1}`, http.StatusOK, func(err error) bool { return errors.Is(err, ErrBadRate) }},
		{"wrong target", `{"from":"EUR","to":"JPY","micro":1}`, http.StatusOK, func(err error) bool { return errors.Is(err, ErrBadRate) }},
		{"zero rate", `{"from":"EUR","to":"USD","micro":0}`, http.StatusOK, func(err error) bool { return errors.Is(err, ErrBadRate) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := serve(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.code)
				if _, err := fmt.Fprint(w, tc.body); err != nil {
					t.Error(err)
				}
			})
			if _, err := c.Rate(t.Context(), "EUR", "USD"); !tc.want(err) {
				t.Fatalf("Rate error = %v", err)
			}
		})
	}
}

func TestNewBoundsEveryRequest(t *testing.T) {
	if got := New("http://example.test").http.Timeout; got != 5*time.Second {
		t.Fatalf("client timeout = %v, want 5s", got)
	}
}

func TestRateStopsWhenTheCallerDoes(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Rate(ctx, "EUR", "USD"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Rate error = %v, want context.Canceled", err)
	}
	if _, err := New("http://[::1]:namedport").Rate(t.Context(), "EUR", "USD"); err == nil {
		t.Fatal("a malformed base URL was accepted")
	}
}
