// Package survivors is the mutation survivor ledger of GO_TOPS.md §6.4: the file
// .go-tops/survivors.yaml, the entries that explain a survivor, and the writes
// `gotops survivors` makes. The strict reader and the schema are check's, so the
// ledger has one definition.
package survivors

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/Hedronite/go-tops/internal/config"
	"github.com/Hedronite/go-tops/internal/mutate"
)

// ParseKey reads the text form of the adapter's key, which String writes. Only
// the file part may hold a colon, so the last three parts are the others.
func ParseKey(s string) (mutate.SurvivorKey, error) {
	bad := fmt.Errorf("%q is not a survivor key, <file>:<function>:<mutator>:<line_hash>", s)
	parts := strings.Split(s, ":")
	if len(parts) < 4 {
		return mutate.SurvivorKey{}, bad
	}
	file, tail := parts[:len(parts)-3], parts[len(parts)-3:]
	k := mutate.SurvivorKey{File: strings.Join(file, ":"), Function: tail[0], Mutator: tail[1], LineHash: tail[2]}
	if slices.Contains([]string{k.File, k.Function, k.Mutator, k.LineHash}, "") {
		return mutate.SurvivorKey{}, bad
	}
	return k, nil
}

// Entry is one accepted survivor, with every field §6.4 requires.
type Entry struct {
	File     string `yaml:"file"`
	Function string `yaml:"function"`
	Mutator  string `yaml:"mutator"`
	LineHash string `yaml:"line_hash"`
	Reason   string `yaml:"reason"`
	Owner    string `yaml:"owner"`
	Expires  string `yaml:"expires"`
}

// Key is the survivor the entry explains.
func (e Entry) Key() mutate.SurvivorKey {
	return mutate.SurvivorKey{File: e.File, Function: e.Function, Mutator: e.Mutator, LineHash: e.LineHash}
}

// Live reports whether the entry's day is after today (UTC). On its day and
// after, an entry explains nothing.
func (e Entry) Live(today time.Time) bool {
	return today.UTC().Format(time.DateOnly) < e.Expires
}

// Ledger is the entries of one survivors file.
type Ledger struct{ Entries []Entry }

// InvalidError is a ledger that is not strict YAML or does not validate. Such a
// ledger explains nothing and fails the gate, naming each fault.
type InvalidError struct{ Issues []config.Issue }

func (e *InvalidError) Error() string {
	lines := make([]string, len(e.Issues))
	for i, issue := range e.Issues {
		lines[i] = issue.String()
	}
	return strings.Join(lines, "; ")
}

// Load reads the ledger at rel, slash-separated, under root. An absent file is
// an empty ledger. An entry past its day loads: Live judges the day, so that
// list can show it and expire can remove it.
func Load(root, rel string) (Ledger, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if errors.Is(err, fs.ErrNotExist) {
		return Ledger{}, nil
	}
	if err != nil {
		return Ledger{}, err
	}
	return parse(rel, data)
}

func parse(rel string, data []byte) (Ledger, error) {
	issues, err := config.ParseSurvivors(rel, data)
	if err != nil {
		return Ledger{}, err
	}
	if len(issues) > 0 {
		return Ledger{}, &InvalidError{Issues: issues}
	}
	var file struct {
		Survivors []Entry `yaml:"survivors"`
	}
	if err := yaml.Unmarshal(data, &file); err != nil {
		return Ledger{}, fmt.Errorf("read %s: %w", rel, err)
	}
	return Ledger{Entries: file.Survivors}, nil
}

// Explains reports whether a live entry has exactly the key.
func (l Ledger) Explains(k mutate.SurvivorKey, today time.Time) bool {
	return slices.ContainsFunc(l.Entries, func(e Entry) bool { return e.Key() == k && e.Live(today) })
}

// Holds reports whether an entry, live or not, has exactly the key.
func (l Ledger) Holds(k mutate.SurvivorKey) bool {
	return slices.ContainsFunc(l.Entries, func(e Entry) bool { return e.Key() == k })
}

// Add returns the ledger with the entry. It refuses an entry the gate would
// refuse and a key the ledger already holds.
func (l Ledger) Add(e Entry, today time.Time) (Ledger, error) {
	if l.Holds(e.Key()) {
		return l, fmt.Errorf("the ledger already holds this key: %s %s %s", e.File, e.Function, e.Mutator)
	}
	data, err := marshal(Ledger{Entries: []Entry{e}})
	if err != nil {
		return l, err
	}
	if _, err := parse("the new entry", data); err != nil {
		return l, err
	}
	if !e.Live(today) {
		return l, fmt.Errorf("expires %s is not after today (UTC)", e.Expires)
	}
	return Ledger{Entries: append(slices.Clone(l.Entries), e)}, nil
}

// Expire splits the ledger into the live entries and the passed ones.
func (l Ledger) Expire(today time.Time) (kept Ledger, removed []Entry) {
	for _, e := range l.Entries {
		if e.Live(today) {
			kept.Entries = append(kept.Entries, e)
			continue
		}
		removed = append(removed, e)
	}
	return kept, removed
}

// quoted is a string written in double quotes, so no YAML reader takes a date or
// a hash of digits for another type.
type quoted string

func (q quoted) MarshalYAML() (any, error) {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Style: yaml.DoubleQuotedStyle, Value: string(q)}, nil
}

// record is an entry as the file spells it, in the order §6.4 shows.
type record struct {
	File     quoted `yaml:"file"`
	Function quoted `yaml:"function"`
	Mutator  string `yaml:"mutator"`
	LineHash quoted `yaml:"line_hash"`
	Reason   quoted `yaml:"reason"`
	Owner    quoted `yaml:"owner"`
	Expires  quoted `yaml:"expires"`
}

func order(a, b Entry) int {
	return cmp.Or(
		cmp.Compare(a.File, b.File), cmp.Compare(a.Function, b.Function),
		cmp.Compare(a.Mutator, b.Mutator), cmp.Compare(a.LineHash, b.LineHash),
	)
}

// marshal writes the entries in key order, so two branches that each add one
// merge.
func marshal(l Ledger) ([]byte, error) {
	records := make([]record, 0, len(l.Entries))
	for _, e := range slices.SortedFunc(slices.Values(l.Entries), order) {
		records = append(records, record{
			File: quoted(e.File), Function: quoted(e.Function), Mutator: e.Mutator, LineHash: quoted(e.LineHash),
			Reason: quoted(e.Reason), Owner: quoted(e.Owner), Expires: quoted(e.Expires),
		})
	}
	return yaml.Marshal(struct {
		Survivors []record `yaml:"survivors"`
	}{records})
}

// Save replaces the ledger at rel under root, in one rename.
func Save(root, rel string, l Ledger) (err error) {
	data, err := marshal(l)
	if err != nil {
		return err
	}
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".survivors-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(tmp.Name()))
		}
	}()
	if err := fill(tmp, data); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// fill writes the data, opens the file to other readers and closes it.
func fill(f *os.File, data []byte) (err error) {
	defer func() { err = errors.Join(err, f.Close()) }()
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Chmod(0o644)
}
