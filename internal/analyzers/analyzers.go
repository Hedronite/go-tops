// Package analyzers registers the custom go/analysis passes for Go-TOPS laws.
package analyzers

import (
	"golang.org/x/tools/go/analysis"

	"github.com/Hedronite/go-tops/internal/analyzers/ctxroot"
	"github.com/Hedronite/go-tops/internal/analyzers/errmatch"
	"github.com/Hedronite/go-tops/internal/analyzers/httptimeout"
	"github.com/Hedronite/go-tops/internal/analyzers/sqlparam"
	"github.com/Hedronite/go-tops/internal/analyzers/synctest"
	"github.com/Hedronite/go-tops/internal/analyzers/unsafeuse"
)

// LawID maps an analyzer name to a law id.
var LawID = map[string]string{
	"synctest":    "G-08",
	"errmatch":    "G-11",
	"ctxroot":     "G-17",
	"httptimeout": "G-19",
	"unsafeuse":   "G-25",
	"sqlparam":    "G-38",
}

// All is every custom analyzer, in stable order.
func All() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		synctest.Analyzer,
		errmatch.Analyzer,
		ctxroot.Analyzer,
		httptimeout.Analyzer,
		unsafeuse.Analyzer,
		sqlparam.Analyzer,
	}
}

// ResetConfig restores package-level analyzer settings after a run.
func ResetConfig() {
	ctxroot.ModuleDir = ""
	ctxroot.AllowPaths = ctxroot.DefaultAllowPaths
}
