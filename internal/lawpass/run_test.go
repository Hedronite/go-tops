package lawpass_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/lawpass"
)

func TestEmbeddedYAMLUncapsIssues(t *testing.T) {
	t.Parallel()
	if err := lawpass.WriteLawsConfig(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	// Embedded bytes ship with the binary; spot-check via written file.
	b, err := os.ReadFile(filepath.Join(t.TempDir(), ".go-tops", "laws.golangci.yml"))
	if err != nil {
		// WriteLawsConfig wrote to temp above — re-write
		dir := t.TempDir()
		if err := lawpass.WriteLawsConfig(dir); err != nil {
			t.Fatal(err)
		}
		b, err = os.ReadFile(filepath.Join(dir, lawpass.LawsConfigRel))
		if err != nil {
			t.Fatal(err)
		}
	}
	s := string(b)
	for _, want := range []string{
		"max-issues-per-linter: 0",
		"max-same-issues: 0",
		"uniq-by-line: false",
		"relative-path-mode: gomod",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("embedded config missing %q", want)
		}
	}
}

func TestParseLintLawsJSONRejectsGarbage(t *testing.T) {
	t.Parallel()
	if _, err := lawpass.ParseLintLawsJSON([]byte("{")); err == nil {
		t.Fatal("want error on bad JSON")
	}
	if _, err := lawpass.ParseLintLawsJSON([]byte(`{"Report":{}}`)); err == nil {
		t.Fatal("want error when Issues missing")
	}
	if _, err := lawpass.ParseLintLawsJSON([]byte(`{"Issues":null,"Report":{"Linters":[]}}`)); err == nil {
		t.Fatal("want error when Issues is null")
	}
	if _, err := lawpass.ParseLintLawsJSON([]byte(`{"Issues":[]}`)); err == nil {
		t.Fatal("want error when Report missing")
	}
	if _, err := lawpass.ParseLintLawsJSON([]byte(`{"Issues":[],"Report":{"Linters":{}}}`)); err == nil {
		t.Fatal("want error when Report.Linters is not an array")
	}
	if _, err := lawpass.ParseLintLawsJSON([]byte(`{"Issues":[],"Report":{"Linters":[]},"Version":1}`)); err != nil {
		t.Fatalf("extra top-level keys are allowed: %v", err)
	}
	if _, err := lawpass.ParseLintLawsJSON([]byte(`{"Issues":[],"Report":[]}`)); err == nil {
		t.Fatal("want error when Report is not an object")
	}
	if _, err := lawpass.ParseLintLawsJSON([]byte(`{"Issues":[],"Report":{}}`)); err == nil {
		t.Fatal("want error when Report.Linters missing")
	}
}

func TestParseLintLawsJSONAcceptsGolangciV215EmptyRun(t *testing.T) {
	t.Parallel()
	doc, err := os.ReadFile(filepath.Join("testdata", "golangci-v2.15.0-clean.json"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := lawpass.ParseLintLawsJSON(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Issues) != 0 {
		t.Fatalf("Issues len = %d", len(res.Issues))
	}
}

func TestMissingAndTamperedLintLawsJSONFailClosed(t *testing.T) {
	dir := t.TempDir()
	if err := lawpass.WriteLawsConfig(dir); err != nil {
		t.Fatal(err)
	}
	_, err := lawpass.Run(context.Background(), dir, nopExec{}, fakeTools{})
	if err == nil {
		t.Fatal("want fail-closed when JSON missing")
	}
	var le lawpass.ErrLintPass
	if !errors.As(err, &le) {
		t.Fatalf("want ErrLintPass, got %T: %v", err, err)
	}
	path := filepath.Join(dir, lawpass.LintLawsJSON)
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = lawpass.Run(context.Background(), dir, touchExec{path: path}, fakeTools{})
	if err == nil {
		t.Fatal("want fail-closed on tampered JSON")
	}
	if !errors.As(err, &le) {
		t.Fatalf("want ErrLintPass, got %T", err)
	}
}

func TestGosecRuleMapsFromText(t *testing.T) {
	res, err := lawpass.ParseLintLawsJSON([]byte(`{"Issues":[{"FromLinter":"gosec","Text":"G401: Use of weak cryptographic primitive","Pos":{"Filename":"a.go","Line":1}}],"Report":{"Linters":[{"Name":"gosec","Enabled":true}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	res.ByLaw = lawpass.CountByLaw(res.Issues)
	if res.ByLaw["G-37"] != 1 {
		t.Fatalf("G-37 = %d", res.ByLaw["G-37"])
	}
}

func TestNoctxFourIdenticalFireL14(t *testing.T) {
	gcl, err := exec.LookPath("golangci-lint")
	if err != nil {
		t.Skip("golangci-lint not on PATH")
	}
	dir, err := filepath.Abs("testdata/noctxfour")
	if err != nil {
		t.Fatal(err)
	}
	res, err := lawpass.Run(context.Background(), dir, execRunner{bin: gcl}, toolBin{gcl})
	if err != nil {
		t.Fatal(err)
	}
	if n := res.ByLaw["G-16"]; n != 4 {
		t.Fatalf("G-16 findings %d, want 4 (L14 uncapped)", n)
	}
}

func TestOracleKitDelegatedLintCounts(t *testing.T) {
	gcl, err := exec.LookPath("golangci-lint")
	if err != nil {
		t.Skip("golangci-lint not on PATH")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	res, err := lawpass.Run(context.Background(), root, execRunner{bin: gcl}, toolBin{gcl})
	if err != nil {
		t.Fatal(err)
	}
	// GO_TOPS.md G0 oracle @ kit 44c1586 / G0-run.md (not read from code constants elsewhere).
	if res.ByLaw["G-16"] != 30 {
		t.Fatalf("kit G-16 = %d, want 30", res.ByLaw["G-16"])
	}
}

type fakeTools struct{}

func (fakeTools) Resolve(context.Context, string) ([]string, error) {
	return []string{"golangci-lint"}, nil
}

type nopExec struct{}

func (nopExec) Run(context.Context, string, []string) (lawpass.Output, error) {
	return lawpass.Output{ExitCode: 0}, nil
}

type touchExec struct{ path string }

func (touchExec) Run(context.Context, string, []string) (lawpass.Output, error) {
	return lawpass.Output{ExitCode: 0}, nil
}

type execRunner struct{ bin string }

func (e execRunner) Resolve(context.Context, string) ([]string, error) {
	return []string{e.bin}, nil
}

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

type toolBin struct{ bin string }

func (t toolBin) Resolve(context.Context, string) ([]string, error) { return []string{t.bin}, nil }
