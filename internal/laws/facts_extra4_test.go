package laws

import "testing"

func TestFileHasCImportUnparseableWithoutC(t *testing.T) {
	if FileHasCImport([]byte("not go source at all")) {
		t.Fatal("unparseable without C should be false")
	}
}

func TestFileHasCImportUnparseableWithC(t *testing.T) {
	if !FileHasCImport([]byte("syntax {{{\n\"C\"\n")) {
		t.Fatal("unparseable with C substring should be true")
	}
}
