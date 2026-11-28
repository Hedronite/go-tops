package laws

import (
	"context"
	"testing"
)

func TestDefaultGoEnvMissingTool(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := defaultGoEnv(context.Background()); err == nil {
		t.Fatal("want error when go is not on PATH")
	}
}

func TestFileHasCImportParsedWithoutC(t *testing.T) {
	if FileHasCImport([]byte("package p\nimport \"fmt\"\n")) {
		t.Fatal("no C import")
	}
}
