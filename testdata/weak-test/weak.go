// Package weak is a fixture for Go-TOPS: two functions of nearly the same
// complexity, one tested well and one tested weakly. Cap 10 passes the first
// and fails the second, because CRAP charges for what the tests do not reach.
package weak

// Grade turns a score into a letter. Its complexity is 9 and its test calls it
// once, so most of its statements run in no test.
func Grade(score int, late, repeat bool) string {
	if score < 0 || score > 100 {
		return "invalid"
	}
	if late && repeat {
		score -= 20
	}
	switch {
	case score >= 90:
		return "A"
	case score >= 80:
		return "B"
	case score >= 70:
		return "C"
	case score >= 60:
		return "D"
	}
	return "F"
}

// Shipping prices a parcel in cents. Its complexity is 8 and its test reaches
// every branch.
func Shipping(kg float64, express, intl bool) int {
	cost := 500
	if kg > 20 {
		cost += 900
	} else if kg > 5 {
		cost += 300
	}
	if express && intl {
		cost *= 3
	} else if express {
		cost *= 2
	} else if intl {
		cost += 700
	}
	if kg <= 0 {
		return 0
	}
	return cost
}
