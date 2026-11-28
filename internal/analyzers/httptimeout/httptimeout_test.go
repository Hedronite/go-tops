package httptimeout_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/Hedronite/go-tops/internal/analyzers/httptimeout"
)

func Test(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), httptimeout.Analyzer, "tp", "tn")
}
