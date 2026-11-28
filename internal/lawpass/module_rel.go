package lawpass

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

const defaultLintConfig = ".golangci.yml"

// ModuleRel makes a linter file name a module-relative slash path.
func ModuleRel(dir, lintConfig, name string) string {
	if !filepath.IsAbs(name) {
		name = fromConfigDir(dir, lintConfig, name)
	}
	if filepath.IsAbs(name) {
		if rel, err := filepath.Rel(dir, name); err == nil && !strings.HasPrefix(rel, "..") {
			name = rel
		}
	}
	return path.Clean(filepath.ToSlash(name))
}

func fromConfigDir(dir, lintConfig, name string) string {
	cfg := lintConfig
	if cfg == "" {
		cfg = defaultLintConfig
	}
	cfgDir := filepath.Dir(filepath.Join(dir, filepath.FromSlash(cfg)))
	if st, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); err == nil && !st.IsDir() {
		return name
	}
	candidate := filepath.Join(cfgDir, filepath.FromSlash(name))
	if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
		if rel, err := filepath.Rel(dir, candidate); err == nil {
			return rel
		}
	}
	return name
}
