package tp

import (
	"testing"
	"time"
)

func TestSleep(t *testing.T) {
	time.Sleep(time.Millisecond) // want "time.Sleep in tests"
}
