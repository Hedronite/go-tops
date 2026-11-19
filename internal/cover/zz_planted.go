package cover

// PlantedComplex is a planted, complex and untested function. It exists only for
// the red run of the M2 exit demonstration (GO_TOPS.md section 6.2): its CRAP score
// is far over the cap of 10, so the CRAP layer fails and the CI assertion step goes
// red. The next commit reverts it.
func PlantedComplex(a, b, c, d int) int {
	n := 0
	if a > 0 {
		n++
	}
	if b > 0 {
		n += 2
	}
	if c > 0 {
		n += 3
	}
	if d > 0 {
		n += 4
	}
	if a > b {
		n++
	}
	if b > c {
		n++
	}
	if c > d {
		n++
	}
	if d > a {
		n++
	}
	if a+b > c {
		n++
	}
	if b+c > d {
		n++
	}
	if c+d > a {
		n++
	}
	if d+a > b {
		n++
	}
	return n
}
