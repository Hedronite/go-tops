package a

// A second init in another file: the key repeats within the package.
func init() {
	for range 3 {
		_ = errX
	}
}

// Shadow is a function with a function literal assigned inside: the literal
// belongs to the function, 1 + if = 2.
func Shadow() int {
	f := func() int {
		if errX != nil {
			return 1
		}
		return 0
	}
	return f()
}
