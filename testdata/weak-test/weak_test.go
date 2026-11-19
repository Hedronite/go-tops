package weak

import "testing"

// One call, one branch: the weak test of the fixture.
func TestGradeTopScore(t *testing.T) {
	if got := Grade(95, false, false); got != "A" {
		t.Errorf("Grade(95) = %q, want A", got)
	}
}

// Every branch of Shipping.
func TestShipping(t *testing.T) {
	for _, c := range []struct {
		kg            float64
		express, intl bool
		want          int
	}{
		{0, false, false, 0},
		{1, false, false, 500},
		{6, false, false, 800},
		{21, false, false, 1400},
		{1, true, false, 1000},
		{1, false, true, 1200},
		{1, true, true, 1500},
	} {
		if got := Shipping(c.kg, c.express, c.intl); got != c.want {
			t.Errorf("Shipping(%v, %v, %v) = %d, want %d", c.kg, c.express, c.intl, got, c.want)
		}
	}
}
