package synctest_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/Hedronite/go-tops/internal/analyzers/synctest"
)

func Test(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, synctest.Analyzer, "tp", "tn")
}
