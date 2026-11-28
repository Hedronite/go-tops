package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/lawpass"
)

func TestDelegatedLintFixtureMarkersMatchFindings(t *testing.T) {
	gcl, err := exec.LookPath("golangci-lint")
	if err != nil {
		t.Skip("golangci-lint not on PATH")
	}
	src, err := filepath.Abs(filepath.Join("..", "..", "testdata", "lints"))
	if err != nil {
		t.Fatal(err)
	}
	root := copyLintFixtureModule(t, src)
	res, err := lawpass.Run(context.Background(), root, execRunner{bin: gcl}, toolBin{bin: gcl})
	if err != nil {
		t.Fatal(err)
	}
	markers, err := lawpass.ScanMarkers(root)
	if err != nil {
		t.Fatal(err)
	}
	if miss := lawpass.RequiredToolLawMarkers(markers); len(miss) > 0 {
		t.Fatal(strings.Join(miss, "\n"))
	}
	diffs, err := lawpass.DiffFixtureMarkers(root, res.Issues)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) > 0 {
		var b strings.Builder
		for _, d := range diffs {
			b.WriteString(d.Law)
			b.WriteByte('\t')
			b.WriteString(d.File)
			b.WriteByte(':')
			b.WriteString(strconv.Itoa(d.Line))
			b.WriteByte('\t')
			b.WriteString(d.Reason)
			b.WriteByte('\n')
		}
		t.Fatalf("marker diff:\n%s", b.String())
	}
}

// copyLintFixtureModule runs the law pass in an isolated copy so parallel gate
// tests do not race on testdata/lints/.go-tops/.
func copyLintFixtureModule(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".go-tops" {
				return filepath.SkipDir
			}
			rel, err := filepath.Rel(src, path)
			if err != nil {
				return err
			}
			if rel == "." {
				return nil
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if !strings.HasSuffix(path, ".go") && filepath.Base(path) != "go.mod" {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		return copyFile(path, filepath.Join(dst, rel))
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close() //nolint:errcheck // test helper
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

type toolBin struct{ bin string }

func (t toolBin) Resolve(context.Context, string) ([]string, error) { return []string{t.bin}, nil }

type execRunner struct{ bin string }

func (e execRunner) Run(ctx context.Context, dir string, argv []string) (lawpass.Output, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exit = ee.ExitCode()
		} else {
			return lawpass.Output{}, err
		}
	}
	return lawpass.Output{ExitCode: exit, Stderr: []byte(stderr.String())}, nil
}
