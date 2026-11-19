package crap_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/Hedronite/go-tops/internal/crap"
)

// The oracle for the formula, from the worked examples and the minimum-coverage
// table of GO_TOPS.md §6.2. Nothing here reads the implementation.

type example struct {
	row            int
	cap            float64
	comp           int
	covered, total int
	want           float64 // to one decimal
	meets          bool
}

var workedExamples = []example{
	{1, 10, 14, 52, 100, 35.7, false},
	{2, 10, 8, 70, 100, 9.7, true},
	{3, 10, 8, 68, 100, 10.1, false},
	{4, 10, 10, 40, 40, 10.0, true},
	{5, 10, 11, 44, 44, 11.0, false},
	{6, 8, 8, 40, 40, 8.0, true},
	{7, 8, 8, 99, 100, 8.0, false}, // 8.000064: compared unrounded
	{8, 8, 6, 14, 17, 6.2, true},
	{9, 8, 6, 14, 20, 7.0, true},
	{10, 8, 9, 14, 20, 11.2, false},
	{11, 10, 30, 31, 31, 30.0, false}, // over the cap; exempt, still printed
}

func TestWorkedExamplesOfTheFormula(t *testing.T) {
	for _, ex := range workedExamples {
		t.Run(fmt.Sprint("row ", ex.row), func(t *testing.T) {
			got := crap.Compute(ex.comp, float64(ex.covered)/float64(ex.total))
			if math.Abs(got-ex.want) >= 0.05 {
				t.Errorf("CRAP = %v, want %.1f to one decimal", got, ex.want)
			}
			if meets := got <= ex.cap; meets != ex.meets {
				t.Errorf("meets cap %g = %v at %v, want %v", ex.cap, meets, got, ex.meets)
			}
		})
	}
}

// Row 7: one statement short of full coverage is 8.000064, which is over a cap
// of 8. Rounding before the comparison would pass it.
func TestRow7IsComparedUnrounded(t *testing.T) {
	got := crap.Compute(8, 0.99)
	if !(got > 8) || math.Abs(got-8.000064) > 1e-9 {
		t.Errorf("CRAP(8, 0.99) = %v, want 8.000064 and above 8", got)
	}
}

// At full coverage the score is comp, so the cap is a ceiling on complexity.
func TestFullCoverageScoresComp(t *testing.T) {
	for _, comp := range []int{1, 8, 10, 11, 30} {
		if got := crap.Compute(comp, 1); got != float64(comp) {
			t.Errorf("CRAP(%d, 1) = %v, want %d exactly", comp, got, comp)
		}
	}
}

func TestMinimumCoverageTable(t *testing.T) {
	table := []struct {
		cap     float64
		comp    int
		minimum float64 // percent, rounded up to 0.1
	}{
		{10, 5, 41.6},
		{10, 8, 68.6},
		{10, 9, 76.9},
		{10, 10, 100.0},
		{8, 5, 50.7},
		{8, 6, 61.9},
		{8, 7, 72.7},
		{8, 8, 100.0},
	}
	for _, row := range table {
		at := crap.Compute(row.comp, row.minimum/100)
		below := crap.Compute(row.comp, (row.minimum-0.1)/100)
		if at > row.cap {
			t.Errorf("cap %g comp %d: %.1f%% scores %v, over the cap", row.cap, row.comp, row.minimum, at)
		}
		if below <= row.cap && row.minimum < 100 {
			t.Errorf("cap %g comp %d: %.1f%% already meets the cap, so %.1f%% is not the minimum", row.cap, row.comp, row.minimum-0.1, row.minimum)
		}
	}
}
