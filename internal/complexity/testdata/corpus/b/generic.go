// Package b holds generic functions and methods on generic types.
package b

// Map: 1 + range + if = 3.
func Map[T, U any](xs []T, f func(T) (U, bool)) []U {
	var out []U
	for _, x := range xs {
		if u, ok := f(x); ok {
			out = append(out, u)
		}
	}
	return out
}

// Box is a generic container.
type Box[T any] struct{ v *T }

// Get: 1 + if = 2.
func (b Box[T]) Get() (T, bool) {
	var zero T
	if b.v == nil {
		return zero, false
	}
	return *b.v, true
}

// Pair has two type parameters.
type Pair[K comparable, V any] struct {
	k K
	v V
}

// Set: 1 + if + && = 3.
func (p *Pair[K, V]) Set(k K, v V, ok bool) {
	if ok && k != p.k {
		p.k, p.v = k, v
	}
}

// Sum: 1 + range = 2.
func Sum[N ~int | ~float64](xs ...N) N {
	var s N
	for _, x := range xs {
		s += x
	}
	return s
}
