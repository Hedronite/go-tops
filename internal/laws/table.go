// Code generated from GO_TOPS.md §8.2 by hand-run script; the drift test in laws_test.go is the guard.
// Classes is provisional (see the Law type): the drift test does not cover it.

package laws

// allClasses are the twelve classes of the schema.
var allClasses = []string{"library-core", "binary-cli", "tui-app", "binary-service", "concurrency-runtime", "library-util", "parser-codec", "unsafe-kernel", "cgo-bridge", "codegen", "embedded-tinygo", "workspace-root"}

// catalog is the §8.2 table: one row per law, retired rows included.
var catalog = []Law{
	{ID: "G-01", Severity: SeverityA, Reader: ReaderAnalyzer, Tier: 0, V01: false, Retired: false, Classes: []string{"concurrency-runtime"}},
	{ID: "G-02", Severity: SeverityR, Reader: ReaderTool, Tier: 1, V01: true, Retired: false, Classes: allClasses},
	{ID: "G-03", Severity: SeverityR, Reader: ReaderAnalyzer, Tier: 0, V01: false, Retired: true, Classes: nil},
	{ID: "G-04", Severity: SeverityA, Reader: ReaderAnalyzer, Tier: 0, V01: false, Retired: false, Classes: []string{"concurrency-runtime"}},
	{ID: "G-05", Severity: SeverityA, Reader: ReaderAnalyzer, Tier: 0, V01: false, Retired: false, Classes: []string{"concurrency-runtime"}},
	{ID: "G-06", Severity: SeverityA, Reader: ReaderAnalyzer, Tier: 0, V01: false, Retired: false, Classes: []string{"concurrency-runtime"}},
	{ID: "G-07", Severity: SeverityR, Reader: ReaderTool, Tier: 1, V01: true, Retired: false, Classes: allClasses},
	{ID: "G-08", Severity: SeverityR, Reader: ReaderAnalyzer, Tier: 0, V01: true, Retired: false, Classes: allClasses},
	{ID: "G-09", Severity: SeverityA, Reader: ReaderAnalyzer, Tier: 0, V01: false, Retired: false, Classes: []string{"concurrency-runtime"}},
	{ID: "G-10", Severity: SeverityR, Reader: ReaderAnalyzer, Tier: 0, V01: false, Retired: false, Classes: []string{"tui-app"}},
	{ID: "G-42", Severity: SeverityR, Reader: ReaderAnalyzer, Tier: 0, V01: false, Retired: false, Classes: allClasses},
	{ID: "G-11", Severity: SeverityR, Reader: ReaderTool, Tier: 1, V01: true, Retired: false, Classes: allClasses},
	{ID: "G-12", Severity: SeverityR, Reader: ReaderTool, Tier: 1, V01: true, Retired: false, Classes: allClasses},
	{ID: "G-13", Severity: SeverityA, Reader: ReaderAnalyzer, Tier: 0, V01: false, Retired: false, Classes: nil},
	{ID: "G-14", Severity: SeverityA, Reader: ReaderAttested, Tier: 0, V01: false, Retired: false, Classes: nil},
	{ID: "G-15", Severity: SeverityA, Reader: ReaderAnalyzer, Tier: 0, V01: false, Retired: false, Classes: nil},
	{ID: "G-16", Severity: SeverityR, Reader: ReaderTool, Tier: 1, V01: true, Retired: false, Classes: allClasses},
	{ID: "G-17", Severity: SeverityR, Reader: ReaderAnalyzer, Tier: 0, V01: true, Retired: false, Classes: allClasses},
	{ID: "G-18", Severity: SeverityR, Reader: ReaderTool, Tier: 1, V01: true, Retired: false, Classes: allClasses},
	{ID: "G-19", Severity: SeverityR, Reader: ReaderAnalyzer, Tier: 0, V01: true, Retired: false, Classes: allClasses},
	{ID: "G-20", Severity: SeverityA, Reader: ReaderAnalyzer, Tier: 0, V01: false, Retired: false, Classes: []string{"binary-service"}},
	{ID: "G-21", Severity: SeverityA, Reader: ReaderTool, Tier: 1, V01: false, Retired: false, Classes: []string{"library-core"}},
	{ID: "G-22", Severity: SeverityR, Reader: ReaderTool, Tier: 3, V01: false, Retired: false, Classes: []string{"library-core"}},
	{ID: "G-23", Severity: SeverityA, Reader: ReaderAnalyzer, Tier: 0, V01: false, Retired: false, Classes: []string{"library-core"}},
	{ID: "G-24", Severity: SeverityA, Reader: ReaderAnalyzer, Tier: 0, V01: false, Retired: false, Classes: []string{"library-core"}},
	{ID: "G-25", Severity: SeverityR, Reader: ReaderAnalyzer, Tier: 0, V01: true, Retired: false, Classes: allClasses},
	{ID: "G-26", Severity: SeverityR, Reader: ReaderAnalyzer, Tier: 0, V01: true, Retired: false, Classes: allClasses},
	{ID: "G-27", Severity: SeverityA, Reader: ReaderAnalyzer, Tier: 0, V01: false, Retired: false, Classes: nil},
	{ID: "G-28", Severity: SeverityA, Reader: ReaderAttested, Tier: 0, V01: false, Retired: false, Classes: nil},
	{ID: "G-29", Severity: SeverityR, Reader: ReaderAnalyzer, Tier: 0, V01: false, Retired: false, Classes: nil},
	{ID: "G-30", Severity: SeverityA, Reader: ReaderAttested, Tier: 0, V01: false, Retired: false, Classes: nil},
	{ID: "G-31", Severity: SeverityR, Reader: ReaderAnalyzer, Tier: 0, V01: false, Retired: false, Classes: nil},
	{ID: "G-32", Severity: SeverityA, Reader: ReaderAnalyzer, Tier: 0, V01: false, Retired: false, Classes: nil},
	{ID: "G-43", Severity: SeverityR, Reader: ReaderAnalyzer, Tier: 0, V01: false, Retired: false, Classes: allClasses},
	{ID: "G-33", Severity: SeverityR, Reader: ReaderAnalyzer, Tier: 0, V01: true, Retired: false, Classes: allClasses},
	{ID: "G-34", Severity: SeverityR, Reader: ReaderTool, Tier: 1, V01: true, Retired: false, Classes: allClasses},
	{ID: "G-35", Severity: SeverityA, Reader: ReaderTool, Tier: 3, V01: false, Retired: false, Classes: nil},
	{ID: "G-36", Severity: SeverityR, Reader: ReaderTool, Tier: 1, V01: false, Retired: false, Classes: nil},
	{ID: "G-37", Severity: SeverityR, Reader: ReaderTool, Tier: 1, V01: true, Retired: false, Classes: allClasses},
	{ID: "G-38", Severity: SeverityR, Reader: ReaderAnalyzer, Tier: 0, V01: true, Retired: false, Classes: allClasses},
	{ID: "G-39", Severity: SeverityA, Reader: ReaderAnalyzer, Tier: 0, V01: false, Retired: false, Classes: nil},
	{ID: "G-40", Severity: SeverityR, Reader: ReaderTool, Tier: 1, V01: true, Retired: false, Classes: allClasses},
	{ID: "G-41", Severity: SeverityA, Reader: ReaderAttested, Tier: 0, V01: false, Retired: false, Classes: []string{"binary-cli"}},
}
