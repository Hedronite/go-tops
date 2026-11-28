package lints

import "os"

func g12() {
	os.Remove("testdata-lints-errcheck-miss") // want G-12
}