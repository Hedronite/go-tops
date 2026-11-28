package tn

import "testing"

func TestNoSleep(t *testing.T) {
	ch := make(chan struct{})
	close(ch)
	<-ch
}
