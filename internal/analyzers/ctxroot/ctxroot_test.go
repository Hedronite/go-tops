package ctxroot_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/Hedronite/go-tops/internal/analyzers/ctxroot"
)

func Test(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), ctxroot.Analyzer, "tp", "cmd/app")
}
