package lints

import "io"

func g11() {
	var err error = io.EOF
	if err == io.EOF { // want G-11
		return
	}
}
