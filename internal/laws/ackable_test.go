package laws

import "testing"

func TestAckableOnlySeverityA(t *testing.T) {
	if Ackable("G-11") {
		t.Fatal("refuse law must not be ackable")
	}
	if !Ackable("G-30") {
		t.Fatal("G-30 should be ackable (severity ack in catalog)")
	}
}
