package ratchet

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// LedgerPath is where the baseline lives unless go-tops.yaml names another path,
// relative to the module root.
const LedgerPath = ".go-tops/baseline.json"

// ReadBase reads the baseline at rel at the merge base of HEAD and base through git, not
// the file in the working tree (§6.7). A base with no copy is a first ledger
// (Ledger nil). No merge base, a shallow clone, an unknown ref and a copy that
// does not parse are errors: the judgment fails and makes no guess.
func ReadBase(ctx context.Context, dir, base, rel string) Base {
	if err := notShallow(ctx, dir); err != nil {
		return Base{Err: err}
	}
	mb, err := mergeBase(ctx, dir, base)
	if err != nil {
		return Base{Err: err}
	}
	baseRel, err := baselinePathAt(ctx, dir, mb)
	if err != nil {
		return Base{Err: err}
	}
	b := ledgerAt(ctx, dir, mb, baseRel)
	if path.Clean(baseRel) != path.Clean(rel) {
		b.Moved = &Moved{From: baseRel, To: rel}
	}
	return b
}

// atRev names a file of a commit by a path relative to the working directory. A bare
// `rev:path` is read from the root of the repository, not from where git runs, so for
// a module in a subdirectory it names another file or none: the base copy would be
// "not found" and the ledger would be judged as a first ledger. `rev:./path` is
// resolved against the working directory (git -C dir), which is the module root.
func atRev(rev, rel string) string {
	return rev + ":./" + path.Clean(rel)
}

// baselinePathAt is the path the go-tops.yaml of commit rev names for the baseline,
// or LedgerPath when the commit has no go-tops.yaml or names none. The copy at the
// diff base is read where the base itself kept it, never where this run's
// configuration says: a branch that moves the baseline must not turn the base's
// ledger into a first ledger.
func baselinePathAt(ctx context.Context, dir, rev string) (string, error) {
	out, code, err := git(ctx, dir, "show", atRev(rev, "go-tops.yaml"))
	if err != nil {
		return "", err
	}
	if code != 0 {
		return LedgerPath, nil
	}
	var doc struct {
		Baseline string `yaml:"baseline"`
	}
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		return "", fmt.Errorf("go-tops.yaml at the diff base: %w", err)
	}
	if doc.Baseline == "" {
		return LedgerPath, nil
	}
	return doc.Baseline, nil
}

// notShallow fails for a shallow clone: its history may hide the merge base.
func notShallow(ctx context.Context, dir string) error {
	out, code, err := git(ctx, dir, "rev-parse", "--is-shallow-repository")
	if err != nil || code != 0 {
		return fmt.Errorf("the repository cannot be read: %s", failure(err, out))
	}
	if strings.TrimSpace(out) == "true" {
		return errors.New("the repository is a shallow clone, so there is no merge base to read the baseline at: fetch full history (fetch-depth: 0)")
	}
	return nil
}

// mergeBase is the commit HEAD and base last had in common. Exit 1 with no output
// is git's answer for no common ancestor; any other failure is a bad ref.
func mergeBase(ctx context.Context, dir, base string) (string, error) {
	out, code, err := git(ctx, dir, "merge-base", "HEAD", base)
	switch {
	case err != nil:
		return "", err
	case code == 1 && strings.TrimSpace(out) == "":
		return "", fmt.Errorf("HEAD and %s have no merge base, so there is no baseline to compare with", base)
	case code != 0:
		return "", fmt.Errorf("git merge-base HEAD %s: %s", base, strings.TrimSpace(out))
	}
	return strings.TrimSpace(out), nil
}

// ledgerAt reads the baseline in commit rev: no ledger when the commit has none.
func ledgerAt(ctx context.Context, dir, rev, rel string) Base {
	spec := atRev(rev, rel)
	if _, code, err := git(ctx, dir, "cat-file", "-e", spec); err != nil {
		return Base{Err: err}
	} else if code != 0 {
		return Base{}
	}
	raw, code, err := git(ctx, dir, "show", spec)
	if err != nil || code != 0 {
		return Base{Err: fmt.Errorf("read the baseline at the merge base: %s", failure(err, raw))}
	}
	l, err := Parse([]byte(raw))
	if err != nil {
		return Base{Err: fmt.Errorf("the baseline at the merge base: %w", err)}
	}
	return Base{Ledger: l}
}

// git runs git in dir and returns its output and exit code. The error is non-nil
// only when git did not run.
func git(ctx context.Context, dir string, args ...string) (string, int, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return out.String(), 0, nil
	case errors.As(err, &exit):
		return out.String(), exit.ExitCode(), nil
	}
	return "", -1, fmt.Errorf("run git: %w", err)
}

func failure(err error, out string) string {
	if err != nil {
		return err.Error()
	}
	return strings.TrimSpace(out)
}

// readLedgerFile reads the baseline file in the working tree at rel, the path the
// configuration names: nil when there is none, an error when it exists and does not
// parse.
func readLedgerFile(dir, rel string) (*Ledger, error) {
	raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}
