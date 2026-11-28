package laws

import "testing"

func TestAcksCoverRespectsAckable(t *testing.T) {
	ev := Evidence{
		Refusals: map[string]map[string]int{"G-11": {"a.go": 1}},
		Acks:     map[string]map[string]int{"G-11": {"a.go": 1}},
	}
	if acksCover("G-11", ev) {
		t.Fatal("G-11 refuse law must not ack")
	}
}

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