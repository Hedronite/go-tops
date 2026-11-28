package tp
import "testing"

import "strings"

func TestBad(t *testing.T) {
	var err error
	_ = strings.Contains(err.Error(), "x") // want "do not match on err.Error()"
}
