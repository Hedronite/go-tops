package unsafeuse_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/Hedronite/go-tops/internal/analyzers/unsafeuse"
)

func Test(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), unsafeuse.Analyzer, "tp", "tn")
}
