package mutation

import "testing"

func TestBelowStrong(t *testing.T) {
	if !Below(1, 5) {
		t.Fatal("expected Below(1, 5) to be true")
	}
	if Below(5, 5) {
		t.Fatal("expected Below(5, 5) to be false")
	}
	if Below(6, 5) {
		t.Fatal("expected Below(6, 5) to be false")
	}
}
