package lints

import "net/http"

func g16() {
	resp, err := http.Get("http://example.com") // want G-16
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}