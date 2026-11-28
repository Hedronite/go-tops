package main

import (
	"strings"
	"testing"
)

// GO_TOPS.md §12.1: list, explain, and usage errors for the laws subcommands.

func TestLawsListExit0(t *testing.T) {
	code, stdout, stderr := gotopsCmd(t, "laws", "list")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "G-02") || !strings.Contains(stdout, "ID\tTITLE") {
		t.Fatalf("list output missing catalog header or G-02:\n%s", stdout)
	}
}

func TestLawsExplainKnownLaw(t *testing.T) {
	code, stdout, stderr := gotopsCmd(t, "laws", "explain", "G-02")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "id: G-02") {
		t.Fatalf("explain output %q", stdout)
	}
}

func TestLawsExplainAbsentLawExit1(t *testing.T) {
	code, _, stderr := gotopsCmd(t, "laws", "explain", "G-99")
	if code != 1 || !strings.Contains(stderr, "not in the catalog") {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
}

func TestLawsExplainBadIDExit2(t *testing.T) {
	code, _, stderr := gotopsCmd(t, "laws", "explain", "not-an-id")
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "not a law id") {
		t.Fatalf("stderr %q", stderr)
	}
}

func TestLawsMissingSubcommandExit2(t *testing.T) {
	code, _, stderr := gotopsCmd(t, "laws")
	if code != 2 || !strings.Contains(stderr, "missing subcommand") {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
}

func TestLawsUnknownSubcommandExit2(t *testing.T) {
	code, _, stderr := gotopsCmd(t, "laws", "nope")
	if code != 2 || !strings.Contains(stderr, "unknown subcommand") {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
}

func TestLawsListRejectsArgsExit2(t *testing.T) {
	code, _, stderr := gotopsCmd(t, "laws", "list", "extra")
	if code != 2 || !strings.Contains(stderr, "unexpected arguments") {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
}

func TestLawsExplainArityExit2(t *testing.T) {
	code, _, stderr := gotopsCmd(t, "laws", "explain")
	if code != 2 || !strings.Contains(stderr, "want exactly one law id") {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
}

func TestLawsRunDryRun(t *testing.T) {
	dir := gitModule(t, goodConfig(t))
	code, stdout, stderr := gotopsEnv(t, dir, []string{"CI="}, "laws", "run", "--dry-run")
	if code != 0 {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "dry-run") {
		t.Fatalf("stdout %q", stdout)
	}
}
