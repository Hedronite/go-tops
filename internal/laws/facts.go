package laws

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"go/version"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"

	"github.com/Hedronite/go-tops/internal/lock"
)

const protocolLangFloor = "go1.26"

// CheckFacts returns violation counts for fact laws G-26 and G-33.
func CheckFacts(dir, class string, goEnv func() (string, error)) (map[string]int, error) {
	out := map[string]int{"G-26": 0, "G-33": 0}
	if err := checkG26(dir, class, out); err != nil {
		return nil, err
	}
	if err := checkG33(dir, goEnv, out); err != nil {
		return nil, err
	}
	return out, nil
}

func checkG26(dir, class string, out map[string]int) error {
	var cgoInProd bool
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := d.Name()
			if base == "vendor" || base == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.Contains(path, string(filepath.Separator)+"testdata"+string(filepath.Separator)) {
			return nil
		}
		has, err := fileImportsC(path)
		if err != nil {
			return err
		}
		if has {
			cgoInProd = true
			if class != "cgo-bridge" {
				out["G-26"]++
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if cgoInProd || class == "cgo-bridge" {
		if err := checkGoModCGO(dir); err != nil {
			out["G-26"]++
		}
	}
	return nil
}

func fileImportsC(path string) (bool, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		return false, err
	}
	for _, imp := range f.Imports {
		if imp.Path.Value == `"C"` {
			return true, nil
		}
	}
	return false, nil
}

func checkGoModCGO(dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return err
	}
	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return err
	}
	// Minimal: module using cgo must not pin toolchain go without cgo when a toolchain line exists.
	if f.Toolchain != nil && f.Toolchain.Name != "" {
		if strings.Contains(f.Toolchain.Name, "nocgo") {
			return fmt.Errorf("toolchain %s disables cgo", f.Toolchain.Name)
		}
	}
	return nil
}

func checkG33(dir string, goEnv func() (string, error), out map[string]int) error {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return err
	}
	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return err
	}
	if f.Go == nil || f.Go.Version == "" {
		out["G-33"]++
		return nil
	}
	modLang := version.Lang("go" + strings.TrimPrefix(f.Go.Version, "go"))
	if version.Compare(modLang, protocolLangFloor) < 0 {
		out["G-33"]++
	}

	lk, err := lock.LoadDir(dir)
	if err != nil {
		out["G-33"]++
		return nil
	}
	if goEnv == nil {
		goEnv = defaultGoEnv
	}
	got, err := goEnv()
	if err != nil || got != lk.Toolchain {
		out["G-33"]++
	}
	return nil
}

func defaultGoEnv() (string, error) {
	out, err := exec.Command("go", "env", "GOVERSION").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ParseFileForTest exposes import C scan for tests.
func ParseFileForTest(path string) (bool, error) { return fileImportsC(path) }

// ModGoVersion reads the go directive from go.mod bytes.
func ModGoVersion(data []byte) (string, error) {
	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return "", err
	}
	if f.Go == nil {
		return "", nil
	}
	return f.Go.Version, nil
}

// CompareGoVersions returns Compare for two go directives.
func CompareGoVersions(a, b string) int {
	return version.Compare(version.Lang("go"+strings.TrimPrefix(a, "go")), version.Lang("go"+strings.TrimPrefix(b, "go")))
}

// ReadGoMod is a helper for tests.
func ReadGoMod(dir string) ([]byte, error) {
	return os.ReadFile(filepath.Join(dir, "go.mod"))
}

// LintGoMod validates go.mod syntax.
func LintGoMod(data []byte) error {
	_, err := modfile.Parse("go.mod", data, nil)
	return err
}

// FileHasCImport parses full file for import C (tests).
func FileHasCImport(src []byte) bool {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.ImportsOnly)
	if err != nil {
		return bytes.Contains(src, []byte(`"C"`))
	}
	for _, imp := range f.Imports {
		if imp.Path.Value == `"C"` {
			return true
		}
	}
	return false
}
