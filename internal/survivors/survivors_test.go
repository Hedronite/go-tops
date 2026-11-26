package survivors

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hedronite/go-tops/internal/mutate"
)

// The cases come from GO_TOPS.md §6.4: the entry rules, the full-key match, and
// an entry for an absent survivor being no error.

const rel = ".go-tops/survivors.yaml"

var today = time.Date(2027, 1, 10, 15, 30, 0, 0, time.UTC)

func hashOf(c string) string { return strings.Repeat(c, 64) }

// fields is one good entry, in file order, so a case can drop or change one.
func fields() [][2]string {
	return [][2]string{
		{"file", "internal/limits/parse.go"},
		{"function", "parseLimit"},
		{"mutator", "CONDITIONALS_BOUNDARY"},
		{"line_hash", `"` + hashOf("a") + `"`},
		{"reason", "equivalent at the boundary"},
		{"owner", "platform"},
		{"expires", `"2027-01-11"`},
	}
}

// ledgerText renders one entry with the changes: a value replaces the field, ""
// drops it, and a key the entry lacks is added.
func ledgerText(changes map[string]string) string {
	var b strings.Builder
	b.WriteString("survivors:\n")
	prefix := "  - "
	seen := map[string]bool{}
	line := func(k, v string) {
		b.WriteString(prefix + k + ": " + v + "\n")
		prefix = "    "
	}
	for _, f := range fields() {
		v, changed := changes[f[0]]
		seen[f[0]] = true
		switch {
		case !changed:
			line(f[0], f[1])
		case v != "":
			line(f[0], v)
		}
	}
	for k, v := range changes {
		if !seen[k] {
			line(k, v)
		}
	}
	return b.String()
}

func writeLedger(t *testing.T, text string) string {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func load(t *testing.T, text string) Ledger {
	t.Helper()
	l, err := Load(writeLedger(t, text), rel)
	if err != nil {
		t.Fatalf("Load: %v\n%s", err, text)
	}
	return l
}

var exact = mutate.SurvivorKey{File: "internal/limits/parse.go", Function: "parseLimit", Mutator: "CONDITIONALS_BOUNDARY", LineHash: strings.Repeat("a", 64)}

func TestAnExactEntryExplainsItsSurvivorAndNoOther(t *testing.T) {
	l := load(t, ledgerText(nil))
	if !l.Explains(exact, today) {
		t.Fatal("the exact key is not explained")
	}
	for name, change := range map[string]func(*mutate.SurvivorKey){
		"another file":      func(k *mutate.SurvivorKey) { k.File = "internal/limits/parse2.go" },
		"another function":  func(k *mutate.SurvivorKey) { k.Function = "parseLimits" },
		"another mutator":   func(k *mutate.SurvivorKey) { k.Mutator = "CONDITIONALS_NEGATION" },
		"another line hash": func(k *mutate.SurvivorKey) { k.LineHash = strings.Repeat("a", 63) + "b" },
	} {
		k := exact
		change(&k)
		if l.Explains(k, today) {
			t.Errorf("%s: a one-part change of the key is explained: %+v", name, k)
		}
	}
}

func TestAnEntryIsLiveOnlyBeforeItsDay(t *testing.T) {
	for day, want := range map[string]bool{
		"2027-01-11": true,  // tomorrow
		"2027-01-10": false, // today
		"2027-01-09": false,
	} {
		l := load(t, ledgerText(map[string]string{"expires": `"` + day + `"`}))
		if got := l.Explains(exact, today); got != want {
			t.Errorf("expires %s on %s: explains = %v, want %v", day, today.Format(time.DateOnly), got, want)
		}
	}
	// The day is the UTC day: 23:30 on the 9th two hours west is already the 10th.
	west := time.Date(2027, 1, 9, 23, 30, 0, 0, time.FixedZone("west", -2*3600))
	if load(t, ledgerText(map[string]string{"expires": `"2027-01-10"`})).Explains(exact, west) {
		t.Error("the entry's day is judged in local time, not UTC")
	}
}

func TestAnAbsentLedgerIsEmpty(t *testing.T) {
	l, err := Load(t.TempDir(), rel)
	if err != nil || len(l.Entries) != 0 {
		t.Fatalf("absent ledger: %v, %v", l.Entries, err)
	}
}

func TestABadEntryFailsTheLoadNamingIt(t *testing.T) {
	cases := map[string]struct{ text, names string }{
		"missing file":       {ledgerText(map[string]string{"file": ""}), "/survivors/0"},
		"missing owner":      {ledgerText(map[string]string{"owner": ""}), "/survivors/0"},
		"missing expires":    {ledgerText(map[string]string{"expires": ""}), "/survivors/0"},
		"unknown field":      {ledgerText(map[string]string{"line": "12"}), "/survivors/0/line"},
		"two-word reason":    {ledgerText(map[string]string{"reason": "equivalent mutant"}), "/survivors/0/reason"},
		"not a real day":     {ledgerText(map[string]string{"expires": `"2027-02-30"`}), "/survivors/0/expires"},
		"unquoted date":      {ledgerText(map[string]string{"expires": "2027-02-01"}), "/survivors/0/expires"},
		"short line hash":    {ledgerText(map[string]string{"line_hash": "abc"}), "/survivors/0/line_hash"},
		"anchor":             {ledgerText(map[string]string{"owner": "&o platform"}), "owner"},
		"empty file":         {"", "empty"},
		"second document":    {ledgerText(nil) + "---\nsurvivors: []\n", "document"},
		"second entry bad":   {ledgerText(nil) + "  - file: a.go\n", "/survivors/1"},
		"not a survivor map": {"entries: []\n", "entries"},
	}
	for name, c := range cases {
		l, err := Load(writeLedger(t, c.text), rel)
		var invalid *InvalidError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v, want an InvalidError", name, err)
			continue
		}
		if len(l.Entries) != 0 {
			t.Errorf("%s: a bad ledger still explains: %v", name, l.Entries)
		}
		if !strings.Contains(err.Error(), rel) || !strings.Contains(err.Error(), c.names) {
			t.Errorf("%s: the error does not name %s and %q: %v", name, rel, c.names, err)
		}
	}
}

func TestExpireRemovesOnlyThePassedEntries(t *testing.T) {
	passed := ledgerText(map[string]string{"expires": `"2027-01-10"`, "function": "old"})
	live := strings.TrimPrefix(ledgerText(nil), "survivors:\n")
	l := load(t, passed+live)
	kept, removed := l.Expire(today)
	if len(removed) != 1 || removed[0].Function != "old" {
		t.Errorf("removed %+v, want the entry that expires today", removed)
	}
	if len(kept.Entries) != 1 || kept.Entries[0].Key() != exact {
		t.Errorf("kept %+v, want the live entry", kept.Entries)
	}
}

func entry() Entry {
	return Entry{
		File: exact.File, Function: exact.Function, Mutator: exact.Mutator, LineHash: exact.LineHash,
		Reason: "equivalent at the boundary", Owner: "platform", Expires: "2027-01-11",
	}
}

func TestAddRefusesWhatTheGateWouldRefuse(t *testing.T) {
	for name, change := range map[string]func(*Entry){
		"two-word reason": func(e *Entry) { e.Reason = "equivalent mutant" },
		"no owner":        func(e *Entry) { e.Owner = "" },
		"today":           func(e *Entry) { e.Expires = "2027-01-10" },
		"not a day":       func(e *Entry) { e.Expires = "2027-02-30" },
		"short hash":      func(e *Entry) { e.LineHash = "abc" },
		"lower mutator":   func(e *Entry) { e.Mutator = "boundary" },
		"rooted file":     func(e *Entry) { e.File = "/etc/x.go" },
	} {
		e := entry()
		change(&e)
		if l, err := (Ledger{}).Add(e, today); err == nil {
			t.Errorf("%s: added %+v", name, l.Entries)
		}
	}
	l, err := (Ledger{}).Add(entry(), today)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Add(entry(), today); err == nil {
		t.Error("a second entry for the same key is added")
	}
}

func TestSaveWritesWhatLoadAndTheStrictReaderAccept(t *testing.T) {
	root := t.TempDir()
	second := entry()
	second.File = "a/first.go"
	// Values a YAML reader would take for another type, unless quoted.
	second.LineHash, second.Owner, second.Expires = strings.Repeat("1", 64), "true", "2027-03-01"
	second.Reason = "a: colon and # hash"
	l := Ledger{Entries: []Entry{entry(), second}}
	if err := Save(root, rel, l); err != nil {
		t.Fatal(err)
	}
	got, err := Load(root, rel)
	if err != nil {
		t.Fatal(err)
	}
	// Written in key order, so two branches add lines that merge.
	if len(got.Entries) != 2 || got.Entries[0] != second || got.Entries[1] != entry() {
		t.Errorf("round trip: %+v", got.Entries)
	}
	if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("the ledger is not readable by others: %v %v", info, err)
	}
	if err := Save(root, rel, Ledger{}); err != nil {
		t.Fatal(err)
	}
	if got, err = Load(root, rel); err != nil || len(got.Entries) != 0 {
		t.Errorf("an emptied ledger: %+v, %v", got.Entries, err)
	}
}

func TestSaveReportsAFailedWriteAndLeavesNoPartFile(t *testing.T) {
	root := t.TempDir()
	// A directory where the ledger goes, so the rename fails.
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(rel), "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Save(root, rel, Ledger{}); err == nil {
		t.Fatal("Save over a directory succeeded")
	}
	left, err := os.ReadDir(filepath.Join(root, ".go-tops"))
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 {
		t.Errorf("a temporary file is left behind: %v", left)
	}
}
