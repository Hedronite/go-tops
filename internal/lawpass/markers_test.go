package lawpass

import (
	"path/filepath"
	"testing"
)

func TestRequiredToolLawMarkersCountsEachDelegatedLawOnce(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "lints"))
	if err != nil {
		t.Fatal(err)
	}
	markers, err := ScanMarkers(root)
	if err != nil {
		t.Fatal(err)
	}
	if miss := RequiredToolLawMarkers(markers); len(miss) > 0 {
		t.Fatal(miss)
	}
}
