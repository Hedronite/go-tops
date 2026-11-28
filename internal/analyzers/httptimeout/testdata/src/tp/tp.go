package tp

import (
	"net/http"
	"time"
)

func BadClient() {
	_ = http.DefaultClient // want "http.DefaultClient has no timeout"
}

func BadLiteral() {
	_ = http.Client{} // want "http.Client without Timeout"
}

func BadServer() {
	_ = http.Server{} // want "http.Server without timeout fields"
}

func GoodClient() {
	_ = http.Client{Timeout: time.Second}
}
