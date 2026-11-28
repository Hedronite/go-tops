package tn

import (
	"net/http"
	"time"
)

func Good() {
	_ = http.Client{Timeout: time.Second}
	_ = http.Server{ReadTimeout: time.Second}
}
