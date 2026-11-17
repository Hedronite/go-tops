package gate

import (
	"errors"
	"fmt"
)

// Exit codes (§12.2). A run that starts returns 0 or 1; the others are decided
// before any layer runs.
const (
	ExitOK      = 0
	ExitFail    = 1
	ExitConfig  = 2
	ExitEnv     = 3
	ExitRefused = 4
)

// ExitError ends a run before any layer runs, with the exit code §12.2 gives.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

func exitf(code int, format string, a ...any) *ExitError {
	return &ExitError{Code: code, Err: fmt.Errorf(format, a...)}
}

// ConfigError marks an invalid configuration (exit 2): a schema or semantic
// failure of check, or a missing or dirty baseline under ratchet.
type ConfigError struct{ Err error }

func (e *ConfigError) Error() string { return "configuration invalid: " + e.Err.Error() }
func (e *ConfigError) Unwrap() error { return e.Err }

// LockError marks a tool lock mismatch (exit 2): a pinned tool present at
// another version or hash. An absent tool is tool-missing, exit 1.
type LockError struct{ Err error }

func (e *LockError) Error() string { return "tool lock mismatch: " + e.Err.Error() }
func (e *LockError) Unwrap() error { return e.Err }

// EnvError marks an environment error that is not a missing tool (exit 3): an
// unreadable repository, no C compiler where -race needs one.
type EnvError struct{ Err error }

func (e *EnvError) Error() string { return "environment error: " + e.Err.Error() }
func (e *EnvError) Unwrap() error { return e.Err }

// exitCodeFor maps an error found before any layer ran to its exit code.
func exitCodeFor(err error) int {
	var ee *ExitError
	var ce *ConfigError
	var le *LockError
	switch {
	case errors.As(err, &ee):
		return ee.Code
	case errors.As(err, &ce), errors.As(err, &le):
		return ExitConfig
	}
	return ExitEnv
}
