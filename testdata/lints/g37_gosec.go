package lints

import "crypto/md5" //nolint:gosec // fixture: G-37 is exercised below

func g37() {
	sum := md5.Sum([]byte("x")) // want G-37
	_ = sum
}
