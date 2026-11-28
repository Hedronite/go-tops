package lawpass

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHeadErrAndModuleRel(t *testing.T) {
	t.Parallel()
	if got := headErr(Output{Stderr: []byte("one\ntwo\nthree")}); got != "one\ntwo" {
		t.Fatalf("headErr multiline = %q", got)
	}
	if got := headErr(Output{}); got != "(no stderr)" {
		t.Fatalf("empty stderr = %q", got)
	}
	dir := t.TempDir()
	abs := filepath.Join(dir, "sub", "a.go")
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if rel := moduleRel(dir, abs); rel != "sub/a.go" {
		t.Fatalf("abs rel = %q", rel)
	}
	if moduleRel(dir, "") != "" {
		t.Fatal("empty name")
	}
	if moduleRel(dir, "./x.go") != "x.go" {
		t.Fatalf("./ prefix = %q", moduleRel(dir, "./x.go"))
	}
}

func TestPrepareLintJSONCreatesPath(t *testing.T) {
	dir := t.TempDir()
	if err := WriteLawsConfig(dir); err != nil {
		t.Fatal(err)
	}
	path, err := prepareLintJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, ".go-tops/lint-laws.json") {
		t.Fatalf("path %q", path)
	}
}

func TestParseLintOutputEmptyFailsClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".go-tops/lint-laws.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("  "), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := parseLintOutput(dir, path, Output{ExitCode: 1, Stderr: []byte("boom")})
	var le ErrLintPass
	if !errors.As(err, &le) {
		t.Fatalf("want ErrLintPass, got %v", err)
	}
	if !strings.Contains(le.Msg, LintLawsJSON) {
		t.Fatalf("msg %q", le.Msg)
	}
}

func TestHeadErrTwoLinesReturnedWhole(t *testing.T) {
	t.Parallel()
	got := headErr(Output{Stderr: []byte("line1\nline2")})
	if got != "line1\nline2" {
		t.Fatalf("two stderr lines = %q", got)
	}
}

func TestParseLintOutputMissingFileFailsClosed(t *testing.T) {
	dir := t.TempDir()
	_, err := parseLintOutput(dir, filepath.Join(dir, "missing.json"), Output{ExitCode: 1})
	var le ErrLintPass
	if !errors.As(err, &le) {
		t.Fatalf("want ErrLintPass, got %v", err)
	}
}

func TestParseLintOutputValidIssues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lint.json")
	if err := os.WriteFile(path, []byte(`{"Issues":[{"FromLinter":"errcheck","Text":"x","Pos":{"Filename":"a.go","Line":1}}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := parseLintOutput(dir, path, Output{ExitCode: 0})
	if err != nil {
		t.Fatal(err)
	}
	if res.ByLaw["G-12"] != 1 {
		t.Fatalf("ByLaw=%v", res.ByLaw)
	}
}

func TestHeadErrSingleLine(t *testing.T) {
	t.Parallel()
	if got := headErr(Output{Stderr: []byte("solo")}); got != "solo" {
		t.Fatalf("got %q", got)
	}
}

func TestParseLintOutputErrorIncludesTruncatedStderr(t *testing.T) {
	dir := t.TempDir()
	_, err := parseLintOutput(dir, filepath.Join(dir, "nope.json"), Output{
		ExitCode: 2,
		Stderr:   []byte("one\ntwo\nthree\n"),
	})
	var le ErrLintPass
	if !errors.As(err, &le) {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(le.Msg, "one\ntwo") || strings.Contains(le.Msg, "three") {
		t.Fatalf("stderr in msg %q", le.Msg)
	}
}
