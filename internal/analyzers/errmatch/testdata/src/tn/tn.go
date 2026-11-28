package tn

import (
	"errors"
	"strings"
)

func Good(err error) bool {
	return errors.Is(err, ErrX)
}

func HasSuffixOnLiteral() bool {
	return strings.HasSuffix("errors", "rs")
}

func EqualFoldOnLiteral() bool {
	return strings.EqualFold("abc", "ABC")
}

var ErrX = errors.New("x")
