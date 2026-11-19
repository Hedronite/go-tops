package crap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestModuleOf(t *testing.T) {
	tests := []struct {
		name, gomod, want string
		fail              bool
	}{
		{"plain", "module example.com/m\n\ngo 1.26\n", "example.com/m", false},
		{"quoted", "module \"example.com/m\"\n", "example.com/m", false},
		{"raw quoted", "module `example.com/m`\n", "example.com/m", false},
		{"comment", "// the module\nmodule example.com/m // the path\n", "example.com/m", false},
		{"tab", "module\texample.com/m\n", "example.com/m", false},
		{"after other lines", "go 1.26\n\nrequire x.y/z v1\n\nmodule example.com/m\n", "example.com/m", false},
		{"modules is not module", "modules example.com/m\n", "", true},
		{"none", "go 1.26\n", "", true},
		{"empty path", "module \n", "", true},
	}
	for _, tc := range tests {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(tc.gomod), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := moduleOf(dir)
		if (err != nil) != tc.fail || got != tc.want {
			t.Errorf("%s: (%q, %v), want %q (fail %v)", tc.name, got, err, tc.want, tc.fail)
		}
	}
}
