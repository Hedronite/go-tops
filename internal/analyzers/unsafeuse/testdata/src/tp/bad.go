package tp

import "unsafe"

func Bad() uintptr {
	return uintptr(unsafe.Pointer(&x)) // want "unsafe use outside"
}

var x int
