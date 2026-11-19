package ratchet

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// LedgerPath is where the baseline lives, relative to the module root.
const LedgerPath = ".go-tops/baseline.json"

// ReadBase reads the baseline at the merge base of HEAD and base through git, not
// the file in the working tree (§6.7). A base with no copy is a first ledger
// (Ledger nil). No merge base, a shallow clone, an unknown ref and a copy that
// does not parse are errors: the judgment fails and makes no guess.
func ReadBase(dir, base string) Base {
	if err := notShallow(dir); err != nil {
		return Base{Err: err}
	}
	mb, err := mergeBase(dir, base)
	if err != nil {
		return Base{Err: err}
	}
	return ledgerAt(dir, mb)
}

// notShallow fails for a shallow clone: its history may hide the merge base.
func notShallow(dir string) error {
	out, code, err := git(dir, "rev-parse", "--is-shallow-repository")
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
func mergeBase(dir, base string) (string, error) {
	out, code, err := git(dir, "merge-base", "HEAD", base)
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
func ledgerAt(dir, rev string) Base {
	spec := rev + ":" + LedgerPath
	if _, code, err := git(dir, "cat-file", "-e", spec); err != nil {
		return Base{Err: err}
	} else if code != 0 {
		return Base{}
	}
	raw, code, err := git(dir, "show", spec)
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
func git(dir string, args ...string) (string, int, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
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

// readLedgerFile reads the baseline file in the working tree: nil when there is
// none, an error when it exists and does not parse.
func readLedgerFile(dir string) (*Ledger, error) {
	raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(LedgerPath)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}
