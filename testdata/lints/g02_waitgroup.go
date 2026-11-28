package lints

import "sync"

func g02() {
	var wg sync.WaitGroup
	go func() {
		wg.Add(1) // want G-02
		defer wg.Done()
	}()
	wg.Wait()
}