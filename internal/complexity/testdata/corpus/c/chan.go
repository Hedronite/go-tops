// Package c holds select and channel code.
package c

// Pump: 1 + for + two comm clauses (the default is not counted) + if = 5.
func Pump(in <-chan int, out chan<- int, done <-chan struct{}) {
	for {
		select {
		case v := <-in:
			if v > 0 {
				out <- v
			}
		case <-done:
			return
		default:
		}
	}
}

// Fan: 1 + range + comm + comm = 4.
func Fan(cs []chan int, quit chan struct{}) int {
	n := 0
	for _, c := range cs {
		select {
		case <-c:
			n++
		case quit <- struct{}{}:
			n--
		}
	}
	return n
}
