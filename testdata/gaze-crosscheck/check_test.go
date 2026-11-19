package gazecheck

import "testing"

func TestParse(t *testing.T) {
	if n, err := Parse("5"); err != nil || n != 55 {
		t.Errorf("Parse(5) = %d, %v", n, err)
	}
}

func TestWrapv(t *testing.T) {
	if err := Wrapv("1"); err != nil {
		t.Error(err)
	}
}

func TestClassify(t *testing.T) {
	if got := Classify(ErrMissing); got != "missing" {
		t.Errorf("Classify = %q", got)
	}
}

func TestDispatch(t *testing.T) {
	if Dispatch("add") != 1 || Dispatch("sub") != 2 {
		t.Error("Dispatch")
	}
}

func TestSum(t *testing.T) {
	if got := Sum([]int{1, 2, 3}, nil); got != 6 {
		t.Errorf("Sum = %d", got)
	}
}

func TestDouble(t *testing.T) {
	if Double(3) != 6 {
		t.Error("Double")
	}
}

func TestMaxAndStack(t *testing.T) {
	if Max(1, 2) != 2 {
		t.Error("Max")
	}
	var s Stack[int]
	s.Push(4)
	if v, ok := s.Pop(); !ok || v != 4 {
		t.Error("Pop")
	}
}

func TestNoop(t *testing.T) { Noop() }
