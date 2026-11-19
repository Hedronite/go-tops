package main

import gotops "github.com/Hedronite/go-tops"

// protocolID is the protocol version the binary implements, read from its one
// source, so a fixture never names it and a protocol bump changes no test.
var protocolID = func() string {
	p, err := gotops.Protocol()
	if err != nil {
		panic(err)
	}
	return p
}()
