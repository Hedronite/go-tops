//go:build skills

package gotops_test

import (
	"os"
	"path/filepath"
	"testing"

	gotops "github.com/Hedronite/go-tops"
	"github.com/Hedronite/go-tops/internal/report"
)

// The skills read a report without validating it, so their tests could pass on a
// shape the gate never writes. The fixture and each mutation state the tests put
// into it must be reports the schema of this protocol accepts.
func TestTheSkillTestsRunOnReportsTheSchemaAccepts(t *testing.T) {
	for name, data := range gotops.SkillReports(t) {
		path := filepath.Join(t.TempDir(), "report.json")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := report.Load(path); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}
