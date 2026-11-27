//go:build !unix

package gate

import "os/exec"

func configureExecCmd(*exec.Cmd) {}
