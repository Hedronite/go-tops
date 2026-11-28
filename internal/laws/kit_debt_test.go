package laws_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Hedronite/go-tops/internal/laws"
)

func TestKitG17AnalyzerDebt(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	rr, err := laws.Run(context.Background(), root, "binary-cli", nil)
	if err != nil {
		t.Fatal(err)
	}
	if n := rr.ByLaw["G-17"]; n != 0 {
		t.Fatalf("G-17=%d byFile=%v", n, rr.ByLawFile["G-17"])
	}
}
