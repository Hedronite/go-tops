package tn

import "errors"

func Good(err error) bool {
	return errors.Is(err, ErrX)
}

var ErrX = errors.New("x")
