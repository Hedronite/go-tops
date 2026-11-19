package initcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gotops has no laws command until the laws layer lands (M5): it exits 2 on an
// unknown command. The embedded AGENTS template is what every generated block
// comes from, so a return of the line is red here, at the source.
func TestAgentsTemplateDoesNotNameALawsCommand(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot, "config", "AGENTS.template.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "gotops laws") {
		t.Error("config/AGENTS.template.md documents gotops laws")
	}
}
