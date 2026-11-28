//tops:unsafe
package tn

import "unsafe"

func Good() uintptr {
	// SAFETY: x is stack-local for the call duration.
	return uintptr(unsafe.Pointer(&x))
}

var x int
