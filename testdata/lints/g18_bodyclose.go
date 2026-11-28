package lints

import (
	"context"
	"net/http"
	"net/http/httptest"
)

func g18() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("ok")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodGet, srv.URL, nil)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req) // want G-18
	if err != nil {
		return
	}
	_ = resp.StatusCode
}