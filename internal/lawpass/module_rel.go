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
	if filepath.IsAbs(name) {
		return slashRel(dir, name)
	}
	name = resolveLintPath(dir, lintConfig, name)
	if filepath.IsAbs(name) {
		return slashRel(dir, name)
	}
	return path.Clean(filepath.ToSlash(name))
}

func slashRel(dir, abs string) string {
	rel, err := filepath.Rel(dir, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path.Clean(filepath.ToSlash(abs))
	}
	return path.Clean(filepath.ToSlash(rel))
}

func resolveLintPath(dir, lintConfig, name string) string {
	cfg := lintConfig
	if cfg == "" {
		cfg = defaultLintConfig
	}
	cfgDir := filepath.Dir(filepath.Join(dir, filepath.FromSlash(cfg)))
	if fileExists(filepath.Join(dir, filepath.FromSlash(name))) {
		return name
	}
	candidate := filepath.Join(cfgDir, filepath.FromSlash(name))
	if !fileExists(candidate) {
		return name
	}
	if rel, err := filepath.Rel(dir, candidate); err == nil {
		return rel
	}
	return name
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
