package initcmd

import (
	"reflect"
	"strings"
	"testing"
)

// §12.1: init writes AGENTS.md only between its markers. It replaces the block
// on a re-run, appends the pair when absent, creates the file with the block
// when the file is absent, and treats one marker without the other as a
// configuration error. Text outside the markers is never changed.

const (
	agentsBefore = "# My repo agent notes\n\nUse tabs.\n\n"
	agentsAfter  = "\n## Local rules\n\nNever touch `legacy/`.\n"
)

func TestAgentsCreatedWhenAbsent(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	if code, _, errs := run(opts(dir, "binary-service")); code != ExitOK {
		t.Fatal(errs)
	}
	got := read(t, dir, "AGENTS.md")
	if !strings.HasPrefix(got, beginMarker+"\n") || !strings.HasSuffix(got, endMarker+"\n") {
		t.Fatalf("a new AGENTS.md is the block alone:\n%s", got)
	}
	for _, want := range []string{
		"**Class:** `binary-service`", "**Protocol:** Go-TOPS " + mustProtocol(t), "**Module:** `github.com/acme/demo`",
		"**Enforce:** `ratchet`", "Statement coverage ≥ `75%`", "CRAP cap ≤ `10`",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("AGENTS.md lacks %q", want)
		}
	}
}

func TestAgentsBlockReplacedAndOutsideTextKept(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	write(t, dir, "AGENTS.md", agentsBefore+beginMarker+"\nOLD KIT TEXT\n"+endMarker+"\n"+agentsAfter)
	if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
		t.Fatal(errs)
	}
	got := read(t, dir, "AGENTS.md")
	if strings.Contains(got, "OLD KIT TEXT") || !strings.HasPrefix(got, agentsBefore+beginMarker) || !strings.HasSuffix(got, endMarker+"\n"+agentsAfter) {
		t.Fatalf("the block was not replaced in place:\n%s", got)
	}
	if strings.Count(got, beginMarker) != 1 || strings.Count(got, endMarker) != 1 {
		t.Fatalf("want one marker pair:\n%s", got)
	}
}

func TestAgentsPairAppendedWhenAbsent(t *testing.T) {
	for name, existing := range map[string]string{
		"newline-end": "# Notes\n\nKeep it short.\n", "no-newline": "# Notes", "blank-line-end": "# Notes\n\n", "empty": "",
	} {
		t.Run(name, func(t *testing.T) {
			dir := newModule(t, "github.com/acme/demo", "1.26")
			write(t, dir, "AGENTS.md", existing)
			if code, _, errs := run(opts(dir, "binary-cli")); code != ExitOK {
				t.Fatal(errs)
			}
			got := read(t, dir, "AGENTS.md")
			if !strings.HasPrefix(got, existing) || strings.Count(got, beginMarker) != 1 || !strings.HasSuffix(got, endMarker+"\n") {
				t.Fatalf("the pair must follow the untouched text:\n%s", got)
			}
			if tail := got[len(existing):]; existing != "" && !strings.HasSuffix(existing, "\n") && !strings.HasPrefix(tail, "\n") {
				t.Errorf("the pair must start on its own line:\n%q", tail[:20])
			}
		})
	}
}

func TestAgentsOneMarkerExitsTwoAndWritesNothing(t *testing.T) {
	cases := map[string]string{
		"begin-only": agentsBefore + beginMarker + "\nhalf\n",
		"end-only":   "half\n" + endMarker + "\n" + agentsAfter,
		"reversed":   endMarker + "\nx\n" + beginMarker + "\n",
		"two-pairs":  beginMarker + "\na\n" + endMarker + "\n" + beginMarker + "\nb\n" + endMarker + "\n",
		"two-begins": beginMarker + "\n" + beginMarker + "\n" + endMarker + "\n",
		"two-ends":   beginMarker + "\n" + endMarker + "\n" + endMarker + "\n",
	}
	for name, existing := range cases {
		t.Run(name, func(t *testing.T) {
			dir := newModule(t, "github.com/acme/demo", "1.26")
			write(t, dir, "AGENTS.md", existing)
			code, _, errs := run(opts(dir, "binary-cli"))
			if code != ExitRefused || !strings.Contains(errs, "AGENTS.md") {
				t.Fatalf("exit %d, want %d naming AGENTS.md:\n%s", code, ExitRefused, errs)
			}
			if got := listing(t, dir); !reflect.DeepEqual(got, []string{"AGENTS.md", "go.mod"}) || read(t, dir, "AGENTS.md") != existing {
				t.Fatalf("a refusal changed the tree: %v", got)
			}
		})
	}
}

// Replacing twice gives the same file: the block is the only thing init owns.
func TestAgentsMergeIsIdempotent(t *testing.T) {
	block := beginMarker + "\nkit text\n" + endMarker
	once, err := mergeAgents(agentsBefore+beginMarker+"\nold\n"+endMarker+"\n"+agentsAfter, block)
	if err != nil {
		t.Fatal(err)
	}
	twice, err := mergeAgents(string(once), block)
	if err != nil || string(once) != string(twice) {
		t.Fatalf("the second merge changed the file: %v\n%s\n---\n%s", err, once, twice)
	}
}
