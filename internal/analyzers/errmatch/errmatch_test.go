package errmatch_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/Hedronite/go-tops/internal/analyzers/errmatch"
)

func Test(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), errmatch.Analyzer, "tp", "tn")
}
