package tp

import "errors"

var errSentinel = errors.New("sentinel")

func Bad(err error) bool {
	return err.Error() == errSentinel.Error() // want "do not match on err.Error()"
}
