package cli

import "os"

// plantedLintError discards an error with a blank assignment, which the kit's
// errcheck (check-blank) reports. It exists only for the red run of M1-G8.
func plantedLintError() { _ = os.Remove("planted") }
