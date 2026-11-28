package badctx

import "net/http"

func four() {
	_, _ = http.Get("http://example.com")
	_, _ = http.Get("http://example.com")
	_, _ = http.Get("http://example.com")
	_, _ = http.Get("http://example.com")
}
