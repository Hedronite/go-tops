package mutation

import "testing"

func TestBelowWeak(t *testing.T) {
	if !Below(1, 5) {
		t.Fatal("expected Below(1, 5) to be true")
	}
	if Below(6, 5) {
		t.Fatal("expected Below(6, 5) to be false")
	}
}
