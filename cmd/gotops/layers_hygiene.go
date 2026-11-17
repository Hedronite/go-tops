package main

import (
	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/lock"

	// Registering the hygiene layer with the runner.
	_ "github.com/Hedronite/go-tops/internal/layers/hygiene"
)

func init() {
	// A downloaded binary is checked against go-tops.lock before it runs (§10).
	gate.BinaryVerifier = lock.NewVerifier
}
