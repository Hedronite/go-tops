package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hedronite/go-tops/internal/cli"
	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/mutate"
)

// The cases come from GO_TOPS.md §12.1 and §6.4: list reads only; explain writes
// one entry and refuses a key the last report does not list as unexplained;
// expire removes the passed entries and prints them.

const (
	survivorsRel = ".go-tops/survivors.yaml"
	farDay       = "2999-01-01"
)

var survivorHash = strings.Repeat("a", 64)

var survivorKey = "internal/x/x.go:Limit:CONDITIONALS_BOUNDARY:" + survivorHash

func ledgerOf(entries ...string) string {
	return "survivors:\n" + strings.Join(entries, "")
}

func ledgerEntry(function, expires string) string {
	return "  - {file: internal/x/x.go, function: " + function + ", mutator: CONDITIONALS_BOUNDARY, line_hash: " + survivorHash +
		", reason: three words here, owner: platform, expires: \"" + expires + "\"}\n"
}

func put(t *testing.T, dir, rel, text string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readText(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func headOf(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// survivorRow is one row of the mutation layer's survivors (§12.3).
func survivorRow(explained any) map[string]any {
	return map[string]any{
		"key": survivorKey, "file": "internal/x/x.go", "function": "Limit", "mutator": "CONDITIONALS_BOUNDARY",
		"line_hash": survivorHash, "line": 7, "status": "LIVED", "explained": explained,
	}
}

func lastReportOf(t *testing.T, head string, details map[string]any) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"git":    map[string]any{"head": head},
		"layers": []any{map[string]any{"id": "tests", "details": map[string]any{"packages": 1}}, map[string]any{"id": "mutation", "details": details}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// survivorModule is a committed module whose last report lists the survivor.
func survivorModule(t *testing.T) string {
	t.Helper()
	dir := gitModule(t, goodConfig(t))
	put(t, dir, ".go-tops/report.json", lastReportOf(t, headOf(t, dir), map[string]any{"survivors": []any{survivorRow(nil)}, "exempt": []any{}}))
	return dir
}

func explainArgs(key string, extra ...string) []string {
	return append([]string{"survivors", "explain", key, "--reason", "equivalent at the boundary", "--owner", "platform", "--expires", farDay}, extra...)
}

func TestSurvivorsListPrintsEachEntryAndWritesNothing(t *testing.T) {
	dir := gitModule(t, goodConfig(t))
	code, out, errs := gotopsEnv(t, dir, nil, "survivors", "list")
	if code != 0 || !strings.Contains(out, "no survivor entries") || readText(t, dir, survivorsRel) != "" {
		t.Fatalf("no ledger: exit %d %q %q", code, out, errs)
	}
	text := ledgerOf(ledgerEntry("Old", "2001-01-01"), ledgerEntry("Limit", farDay))
	put(t, dir, survivorsRel, text)
	code, out, errs = gotopsEnv(t, dir, nil, "survivors", "list")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "expired ") || !strings.HasPrefix(lines[1], "live ") || !strings.Contains(lines[1], survivorKey) {
		t.Errorf("want an expired line and a live line with its key:\n%s", out)
	}
	if got := readText(t, dir, survivorsRel); got != text {
		t.Errorf("list changed the ledger:\n%s", got)
	}
}

func TestSurvivorsRefuseAMalformedLedgerAndConfig(t *testing.T) {
	for _, args := range [][]string{{"survivors", "list"}, {"survivors", "expire"}, explainArgs(survivorKey)} {
		dir := survivorModule(t)
		bad := ledgerOf("  - {file: a.go}\n")
		put(t, dir, survivorsRel, bad)
		code, _, errs := gotopsEnv(t, dir, nil, args...)
		if code != 2 || !strings.Contains(errs, "/survivors/0") || readText(t, dir, survivorsRel) != bad {
			t.Errorf("%v on a malformed ledger: exit %d %q", args[1], code, errs)
		}
		if code, _, errs := gotopsEnv(t, module(t, ""), nil, args...); code != 2 || !strings.Contains(errs, "go-tops.yaml") {
			t.Errorf("%v with no config: exit %d %q", args[1], code, errs)
		}
	}
}

func TestSurvivorsExplainWritesOneCompleteEntry(t *testing.T) {
	dir := survivorModule(t)
	code, out, errs := gotopsEnv(t, dir, nil, explainArgs(survivorKey)...)
	if code != 0 || !strings.Contains(out, survivorKey) {
		t.Fatalf("exit %d %q %q", code, out, errs)
	}
	got := readText(t, dir, survivorsRel)
	for _, want := range []string{
		`file: "internal/x/x.go"`, `function: "Limit"`, "mutator: CONDITIONALS_BOUNDARY", `line_hash: "` + survivorHash + `"`,
		`reason: "equivalent at the boundary"`, `owner: "platform"`, `expires: "` + farDay + `"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the entry lacks %q:\n%s", want, got)
		}
	}
	if code, out, errs := gotopsEnv(t, dir, nil, "check"); code != 0 {
		t.Errorf("check refuses the ledger explain wrote: exit %d %s %s", code, out, errs)
	}
	code, _, errs = gotopsEnv(t, dir, nil, explainArgs(survivorKey)...)
	if code != 1 || !strings.Contains(errs, "already") || readText(t, dir, survivorsRel) != got {
		t.Errorf("a second explain of the key: exit %d %q", code, errs)
	}
}

// Each case leaves the ledger unwritten.
func TestSurvivorsExplainRefusals(t *testing.T) {
	other := "internal/x/x.go:Other:CONDITIONALS_BOUNDARY:" + survivorHash
	for _, c := range []struct {
		name    string
		report  func(t *testing.T, head string) string // nil keeps the good report, "" removes it
		args    []string
		code    int
		message string
	}{
		{"no report", func(*testing.T, string) string { return "" }, explainArgs(survivorKey), 1, "gotops gate"},
		{"a report that is not JSON", func(*testing.T, string) string { return "{" }, explainArgs(survivorKey), 1, "report"},
		{"a report of another HEAD", func(t *testing.T, _ string) string {
			return lastReportOf(t, "0123456", map[string]any{"survivors": []any{survivorRow(nil)}})
		}, explainArgs(survivorKey), 1, "HEAD"},
		{"a report with no survivor rows", func(t *testing.T, head string) string {
			return lastReportOf(t, head, map[string]any{"error": "the engine wrote nothing"})
		}, explainArgs(survivorKey), 1, "no mutation survivors"},
		{"an unlisted key", nil, explainArgs(other), 1, "not list"},
		{"a survivor the report has as explained", func(t *testing.T, head string) string {
			return lastReportOf(t, head, map[string]any{"survivors": []any{survivorRow("ledger")}})
		}, explainArgs(survivorKey), 1, "not list"},
		{"an exempt survivor", func(t *testing.T, head string) string {
			return lastReportOf(t, head, map[string]any{"survivors": []any{survivorRow(nil)}, "exempt": []any{survivorKey}})
		}, explainArgs(survivorKey), 1, "exemption"},
		{"a two-word reason", nil, explainArgs(survivorKey, "--reason", "equivalent mutant"), 2, "reason"},
		{"a passed day", nil, explainArgs(survivorKey, "--expires", "2001-01-01"), 2, "2001-01-01"},
		{"an empty owner", nil, explainArgs(survivorKey, "--owner", ""), 2, "--owner"},
		{"no reason", nil, []string{"survivors", "explain", survivorKey, "--owner", "x", "--expires", farDay}, 2, "--reason"},
		{"no key", nil, []string{"survivors", "explain", "--owner", "x"}, 2, "needs a key before its flags"},
		{"explain alone", nil, []string{"survivors", "explain"}, 2, "needs a key before its flags"},
		{"a key with a part missing", nil, explainArgs("a.go:F:X"), 2, "not a survivor key"},
		{"a second key", nil, explainArgs(survivorKey, "more"), 2, "more"},
		{"an unknown subcommand", nil, []string{"survivors", "accept"}, 2, "accept"},
		{"no subcommand", nil, []string{"survivors"}, 2, "list"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := survivorModule(t)
			if c.report != nil {
				if text := c.report(t, headOf(t, dir)); text != "" {
					put(t, dir, ".go-tops/report.json", text)
				} else if err := os.Remove(filepath.Join(dir, ".go-tops", "report.json")); err != nil {
					t.Fatal(err)
				}
			}
			code, out, errs := gotopsEnv(t, dir, nil, c.args...)
			if code != c.code || !strings.Contains(errs, c.message) {
				t.Errorf("exit %d, want %d with %q:\n%s%s", code, c.code, c.message, out, errs)
			}
			if got := readText(t, dir, survivorsRel); got != "" {
				t.Errorf("a refused explain wrote:\n%s", got)
			}
		})
	}
}

func TestSurvivorsExpireRemovesOnlyThePassedEntries(t *testing.T) {
	dir := gitModule(t, goodConfig(t))
	code, out, errs := gotopsEnv(t, dir, nil, "survivors", "expire")
	if code != 0 || !strings.Contains(out, "no passed entries") || readText(t, dir, survivorsRel) != "" {
		t.Fatalf("no ledger: exit %d %q %q", code, out, errs)
	}
	live := ledgerOf(ledgerEntry("Limit", farDay))
	put(t, dir, survivorsRel, live)
	if code, out, _ = gotopsEnv(t, dir, nil, "survivors", "expire"); code != 0 || readText(t, dir, survivorsRel) != live {
		t.Errorf("nothing passed, so nothing is rewritten: exit %d %q\n%s", code, out, readText(t, dir, survivorsRel))
	}
	put(t, dir, survivorsRel, ledgerOf(ledgerEntry("Old", "2001-01-01"), ledgerEntry("Limit", farDay)))
	code, out, errs = gotopsEnv(t, dir, nil, "survivors", "expire")
	got := readText(t, dir, survivorsRel)
	if code != 0 || !strings.Contains(out, "removed internal/x/x.go:Old:") || strings.Contains(out, ":Limit:") {
		t.Errorf("exit %d, want the passed entry printed alone:\n%s%s", code, out, errs)
	}
	if strings.Contains(got, `"Old"`) || !strings.Contains(got, `"Limit"`) {
		t.Errorf("the ledger keeps the live entry alone:\n%s", got)
	}
}

// judged runs the adapter's accounting over one changed line whose two mutants
// both lived, with the ledger this command wires in.
func judged(t *testing.T, ledger string, today time.Time) (mutate.Evaluation, error) {
	t.Helper()
	dir := t.TempDir()
	put(t, dir, "go.mod", "module example.com/m\n\ngo 1.26\n")
	put(t, dir, "x.go", "package m\n\nfunc F(a int) bool { return a > 0 }\n")
	if ledger != "" {
		put(t, dir, survivorsRel, ledger)
	}
	out := []byte(`{"go_module":"example.com/m","files":[{"file_name":"x.go","mutations":[` +
		`{"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":3,"column":31},` +
		`{"type":"CONDITIONALS_NEGATION","status":"LIVED","line":3,"column":31}]}]}`)
	diff := mutate.DiffResult{InBuildFiles: map[string]map[int]bool{"x.go": {3: true}}}
	return mutate.Evaluate(gate.MutationConfig{Ledger: survivorsRel}, nil, out, false, diff, dir, today)
}

func TestTheGateExplainsASurvivorOnlyByALiveEntryOfItsExactKey(t *testing.T) {
	today := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	bare, err := judged(t, "", today)
	if err != nil || bare.SurvivedExplained != 0 || bare.SurvivedUnexplained != 2 {
		t.Fatalf("no ledger: %+v, %v", bare, err)
	}
	var key mutate.SurvivorKey
	for _, s := range bare.Survivors {
		if s.Key.Mutator == "CONDITIONALS_BOUNDARY" {
			key = s.Key
		}
	}
	entry := func(hash, expires string) string {
		return ledgerOf("  - {file: x.go, function: F, mutator: CONDITIONALS_BOUNDARY, line_hash: \"" + hash +
			"\", reason: three words here, owner: platform, expires: \"" + expires + "\"}\n")
	}
	other := "0" + key.LineHash[1:]
	if other == key.LineHash {
		other = "1" + key.LineHash[1:]
	}
	for name, tc := range map[string]struct {
		ledger    string
		explained int
	}{
		"the exact key":          {entry(key.LineHash, farDay), 1},
		"one character changed":  {entry(other, farDay), 0},
		"an entry on its day":    {entry(key.LineHash, "2026-11-25"), 0},
		"an absent survivor too": {entry(key.LineHash, farDay) + strings.TrimPrefix(entry(other, farDay), "survivors:\n"), 1},
	} {
		ev, err := judged(t, tc.ledger, today)
		if err != nil || ev.SurvivedExplained != tc.explained || ev.SurvivedUnexplained != 2-tc.explained {
			t.Errorf("%s: %d explained, %d unexplained, %v", name, ev.SurvivedExplained, ev.SurvivedUnexplained, err)
		}
	}
}

func TestTheGateFailsOnALedgerItCannotRead(t *testing.T) {
	_, err := judged(t, "survivors:\n  - {file: x.go}\n", time.Now())
	if err == nil || !strings.Contains(err.Error(), survivorsRel) {
		t.Fatalf("err = %v, want one naming %s", err, survivorsRel)
	}
}

// The command as the binary runs it, in the module directory: its exit code and
// what it prints for each way it ends (§12.1).
func TestTheSurvivorsCommandExitsByHowItEnded(t *testing.T) {
	dir := gitModule(t, goodConfig(t))
	t.Chdir(dir)
	run := func(args ...string) (int, string) {
		var out, errs strings.Builder
		code := runSurvivors(cli.NewEnv(&out, &errs), args)
		return code, errs.String()
	}
	if code, errs := run("list"); code != 0 || errs != "" {
		t.Errorf("list: exit %d %q", code, errs)
	}
	if code, errs := run("explain", survivorKey, "--reason", "three words here", "--owner", "x", "--expires", farDay); code != 1 || !strings.HasPrefix(errs, "gotops survivors: no report") {
		t.Errorf("explain with no report: exit %d %q", code, errs)
	}
	if code, errs := run("accept"); code != 2 || !strings.Contains(errs, "unknown subcommand accept") {
		t.Errorf("an unknown subcommand: exit %d %q", code, errs)
	}
	// A ledger that cannot be read is the environment's fault, not the ledger's.
	if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(survivorsRel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if code, errs := run("list"); code != cli.ExitEnvironment {
		t.Errorf("an unreadable ledger: exit %d %q", code, errs)
	}
}
