package laws

import "testing"

func TestAckCountsSatisfy(t *testing.T) {
	need := map[string]int{"a.go": 2}
	have := map[string]int{"a.go": 2}
	if !ackCountsSatisfy(need, have) {
		t.Fatal("want true")
	}
	if ackCountsSatisfy(need, map[string]int{"a.go": 1}) {
		t.Fatal("want false when short")
	}
	if ackCountsSatisfy(nil, have) {
		t.Fatal("want false for empty need")
	}
}
