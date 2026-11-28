package sqlparam_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/Hedronite/go-tops/internal/analyzers/sqlparam"
)

func Test(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), sqlparam.Analyzer, "tp", "tn")
}
