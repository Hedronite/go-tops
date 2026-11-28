package tp

import "errors"
import "strings"

var errSentinel = errors.New("sentinel")

func Eq(err error) bool {
	return err.Error() == errSentinel.Error() // want "do not match on err.Error()"
}

func Neq(err error) bool {
	return err.Error() != "x" // want "do not match on err.Error()"
}

func Sw(err error) bool {
	switch err.Error() { // want "do not match on err.Error()"
	case "x":
		return true
	default:
		return false
	}
}

func HasPrefix(err error) bool {
	return strings.HasPrefix(err.Error(), "p") // want "do not match on err.Error()"
}

func HasSuffix(err error) bool {
	return strings.HasSuffix(err.Error(), "s") // want "do not match on err.Error()"
}

func EqualFold(err error) bool {
	return strings.EqualFold(err.Error(), "x") // want "do not match on err.Error()"
}

func SavedCompare(err error) bool {
	msg := err.Error()
	return msg == "x" // want "do not match on err.Error()"
}
