package lints

import "sync"

func g07() {
	var mu sync.Mutex
	mu2 := mu // want G-07
	_ = &mu2
}
