package main

import (
	"context"
	"errors"
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
	root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "lints"))
	if err != nil {
		t.Fatal(err)
	}
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