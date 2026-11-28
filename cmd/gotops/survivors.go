package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Hedronite/go-tops/internal/cli"
	"github.com/Hedronite/go-tops/internal/config"
	"github.com/Hedronite/go-tops/internal/mutate"
	"github.com/Hedronite/go-tops/internal/survivors"
)

func init() {
	cli.Register(cli.Command{
		Name:    "survivors",
		Summary: "manage the mutation survivor ledger: list, explain or expire",
		Run:     runSurvivors,
	})
}

const survivorsUsage = `Usage: gotops survivors list
       gotops survivors explain <key> --reason <text> --owner <handle> --expires <YYYY-MM-DD>
       gotops survivors expire

list prints each entry of the ledger and writes nothing. explain adds one entry
for a key the last gate report lists as an unexplained survivor; the reason has
at least three words and the day is after today (UTC). expire removes the entries
whose day is today or earlier and prints them. A write puts the whole ledger in
one form, so comments in it are not kept. A key is
<file>:<function>:<mutator>:<line_hash>, as the report prints it.

An expired entry fails the gate before it runs, so the order is: expire, gate,
explain.
`

// refusedBy is exit 1: the command understood the request and will not do it.
const refusedBy = 1

// survivorsError is why a ledger command stopped, with its exit code.
type survivorsError struct {
	code int
	err  error
}

func (e *survivorsError) Error() string { return e.err.Error() }

func stop(code int, format string, a ...any) error {
	return &survivorsError{code: code, err: fmt.Errorf(format, a...)}
}

// ledgerFiles is what the commands work on: the module directory and the ledger
// and report paths go-tops.yaml names.
type ledgerFiles struct{ dir, ledger, report string }

func runSurvivors(env cli.Env, args []string) int {
	err := survivorsCommand(env, ".", args, time.Now())
	if err == nil {
		return 0
	}
	var failure *survivorsError
	if errors.As(err, &failure) {
		env.Stderr.Printf("gotops survivors: %v\n", err)
		return failure.code
	}
	return usageError(env, "survivors", survivorsUsage, err)
}

// survivorsCommand runs one subcommand. A plain error is a usage error.
func survivorsCommand(env cli.Env, dir string, args []string, today time.Time) error {
	if len(args) == 0 {
		return errors.New("needs list, explain or expire")
	}
	run, ok := map[string]func(cli.Env, ledgerFiles, []string, time.Time) error{
		"list": survivorsList, "explain": survivorsExplain, "expire": survivorsExpire,
	}[args[0]]
	switch {
	case slices.Contains([]string{"-h", "--help"}, args[0]):
		return flag.ErrHelp
	case !ok:
		return fmt.Errorf("unknown subcommand %s", args[0])
	}
	files, err := survivorFiles(dir)
	if err != nil {
		return err
	}
	return run(env, files, args[1:], today)
}

// survivorFiles reads go-tops.yaml for the two paths. It does not run check: a
// ledger with a passed entry fails check, and expire is how it is mended.
func survivorFiles(dir string) (ledgerFiles, error) {
	data, err := os.ReadFile(filepath.Join(dir, "go-tops.yaml"))
	if err != nil {
		return ledgerFiles{}, stop(cli.ExitUsage, "read go-tops.yaml: %w", err)
	}
	cfg, issues, err := config.Load(data)
	if err != nil {
		return ledgerFiles{}, stop(cli.ExitEnvironment, "%w", err)
	}
	if len(issues) > 0 {
		return ledgerFiles{}, stop(cli.ExitUsage, "go-tops.yaml is invalid: %s", &survivors.InvalidError{Issues: issues})
	}
	eff, err := config.Effective(cfg)
	if err != nil {
		return ledgerFiles{}, stop(cli.ExitEnvironment, "%w", err)
	}
	files := ledgerFiles{dir: dir, ledger: eff.Gates.Mutation.Ledger, report: eff.Report.Path}
	for _, p := range []string{files.ledger, files.report} {
		if !filepath.IsLocal(filepath.FromSlash(p)) {
			return ledgerFiles{}, stop(cli.ExitUsage, "go-tops.yaml names %q, which is outside the module", p)
		}
	}
	return files, nil
}

func (f ledgerFiles) load() (survivors.Ledger, error) {
	l, err := survivors.Load(f.dir, f.ledger)
	if err == nil {
		return l, nil
	}
	var invalid *survivors.InvalidError
	if errors.As(err, &invalid) {
		return l, stop(cli.ExitUsage, "%w", err)
	}
	return l, stop(cli.ExitEnvironment, "%w", err)
}

func (f ledgerFiles) save(l survivors.Ledger) error {
	if err := survivors.Save(f.dir, f.ledger, l); err != nil {
		return stop(cli.ExitEnvironment, "write %s: %w", f.ledger, err)
	}
	return nil
}

func noArgs(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("takes no arguments, got %s", args[0])
	}
	return nil
}

func survivorsList(env cli.Env, f ledgerFiles, args []string, today time.Time) error {
	if err := noArgs(args); err != nil {
		return err
	}
	l, err := f.load()
	if err != nil {
		return err
	}
	if len(l.Entries) == 0 {
		env.Stdout.Printf("no survivor entries in %s\n", f.ledger)
		return nil
	}
	for _, e := range l.Entries {
		state := "expired"
		if e.Live(today) {
			state = "live   "
		}
		env.Stdout.Printf("%s %s  expires %s  owner %s  %s\n", state, e.Key(), e.Expires, e.Owner, e.Reason)
	}
	return nil
}

func survivorsExpire(env cli.Env, f ledgerFiles, args []string, today time.Time) error {
	if err := noArgs(args); err != nil {
		return err
	}
	l, err := f.load()
	if err != nil {
		return err
	}
	kept, removed := l.Expire(today)
	if len(removed) == 0 {
		env.Stdout.Printf("no passed entries in %s\n", f.ledger)
		return nil
	}
	if err := f.save(kept); err != nil {
		return err
	}
	for _, e := range removed {
		env.Stdout.Printf("removed %s (expired %s)\n", e.Key(), e.Expires)
	}
	return nil
}

// explainRequest reads the key and the three flags of explain.
func explainRequest(args []string) (mutate.SurvivorKey, survivors.Entry, error) {
	var e survivors.Entry
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return mutate.SurvivorKey{}, e, errors.New("explain needs a key before its flags")
	}
	flags := flag.NewFlagSet("explain", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&e.Reason, "reason", "", "")
	flags.StringVar(&e.Owner, "owner", "", "")
	flags.StringVar(&e.Expires, "expires", "", "")
	if err := flags.Parse(args[1:]); err != nil {
		return mutate.SurvivorKey{}, e, err
	}
	if err := noArgs(flags.Args()); err != nil {
		return mutate.SurvivorKey{}, e, err
	}
	for name, v := range map[string]string{"--reason": e.Reason, "--owner": e.Owner, "--expires": e.Expires} {
		if v == "" {
			return mutate.SurvivorKey{}, e, fmt.Errorf("explain needs %s", name)
		}
	}
	k, err := survivors.ParseKey(args[0])
	if err != nil {
		return k, e, err
	}
	e.File, e.Function, e.Mutator, e.LineHash = k.File, k.Function, k.Mutator, k.LineHash
	return k, e, nil
}

func survivorsExplain(env cli.Env, f ledgerFiles, args []string, today time.Time) error {
	k, entry, err := explainRequest(args)
	if err != nil {
		return err
	}
	l, err := f.load()
	if err != nil {
		return err
	}
	if err := f.listedUnexplained(k); err != nil {
		return err
	}
	if l.Holds(k) {
		return stop(refusedBy, "%s already holds this key", f.ledger)
	}
	next, err := l.Add(entry, today)
	if err != nil {
		return stop(cli.ExitUsage, "%w", err)
	}
	if err := f.save(next); err != nil {
		return err
	}
	env.Stdout.Printf("added %s to %s, expires %s\n", k, f.ledger, entry.Expires)
	return nil
}

// mutationRows is what explain reads of the mutation layer's details (§12.3).
type mutationRows struct {
	Survivors []struct {
		mutate.SurvivorKey
		Explained *string `json:"explained"`
	} `json:"survivors"`
	Exempt []string `json:"exempt"`
}

// listedUnexplained refuses a key the last report does not list as an
// unexplained survivor, and a report that is not of this HEAD.
func (f ledgerFiles) listedUnexplained(k mutate.SurvivorKey) error {
	head, rows, err := f.lastReport()
	if err != nil {
		return err
	}
	now, err := currentHead(f.dir)
	if err != nil {
		return stop(cli.ExitEnvironment, "%w", err)
	}
	if head != now {
		return stop(refusedBy, "%s was made at %s and HEAD is %s: run gotops gate again", f.report, head, now)
	}
	if slices.Contains(rows.Exempt, k.String()) {
		return stop(refusedBy, "a mutation exemption already covers this survivor")
	}
	for _, r := range rows.Survivors {
		if r.Explained == nil && k == r.SurvivorKey {
			return nil
		}
	}
	return stop(refusedBy, "%s does not list this key as an unexplained survivor", f.report)
}

// lastReport reads the HEAD of the last gate report and its survivor rows.
func (f ledgerFiles) lastReport() (string, mutationRows, error) {
	var rows mutationRows
	var doc struct {
		Git struct {
			Head string `json:"head"`
		} `json:"git"`
		Layers []struct {
			ID      string          `json:"id"`
			Details json.RawMessage `json:"details"`
		} `json:"layers"`
	}
	data, err := os.ReadFile(filepath.Join(f.dir, filepath.FromSlash(f.report)))
	if errors.Is(err, fs.ErrNotExist) {
		return "", rows, stop(refusedBy, "no report at %s: run gotops gate first", f.report)
	}
	if err != nil {
		return "", rows, stop(cli.ExitEnvironment, "%w", err)
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", rows, stop(refusedBy, "%s is not a gate report: %w", f.report, err)
	}
	for _, layer := range doc.Layers {
		if layer.ID == "mutation" && json.Unmarshal(layer.Details, &rows) == nil && rows.Survivors != nil {
			return doc.Git.Head, rows, nil
		}
	}
	return "", rows, stop(refusedBy, "%s lists no mutation survivors", f.report)
}

func currentHead(dir string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
