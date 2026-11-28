package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// baselineProblem says why the baseline file is not clean against HEAD, or
// returns "" when it is tracked and unchanged. The error is an environment
// failure: git missing, or a repository git cannot read.
func baselineProblem(ctx context.Context, root, rel string) (string, error) {
	_, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
	if errors.Is(err, fs.ErrNotExist) {
		return "does not exist", nil
	}
	if err != nil {
		return "", err
	}
	if code, out, err := gitRun(ctx, root, "rev-parse", "--is-inside-work-tree"); err != nil || code != 0 {
		return "", fmt.Errorf("the repository cannot be read: %w", gitFailure(err, out))
	}
	steps := []struct {
		problem string
		args    []string
	}{
		{"cannot be clean: the repository has no commit", []string{"rev-parse", "--verify", "--quiet", "HEAD"}},
		{"is not tracked by git", []string{"ls-files", "--error-unmatch", "--", rel}},
		{"differs from HEAD", []string{"diff", "--quiet", "HEAD", "--", rel}},
	}
	for _, s := range steps {
		code, out, err := gitRun(ctx, root, s.args...)
		switch {
		case err != nil:
			return "", err
		case code == 1:
			return s.problem, nil
		case code != 0:
			return "", fmt.Errorf("git %s: %w", s.args[0], gitFailure(nil, out))
		}
	}
	return "", nil
}

// gitRun runs git in root and returns its exit code and combined output. The
// error is non-nil when git did not run at all.
func gitRun(ctx context.Context, root string, args ...string) (int, string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, out.String(), nil
	case errors.As(err, &exit):
		return exit.ExitCode(), out.String(), nil
	default:
		return -1, "", fmt.Errorf("run git: %w", err)
	}
}

func gitFailure(err error, out string) error {
	if err != nil {
		return err
	}
	return errors.New(strings.TrimSpace(out))
}
