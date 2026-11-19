package initcmd

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	gitignoreFile  = ".gitignore"
	ignoreBegin    = "# go-tops:begin"
	ignoreEnd      = "# go-tops:end"
	ignoreRulesTxt = `.go-tops/*
!.go-tops/baseline.json
!.go-tops/acks.yaml
!.go-tops/survivors.yaml
!.go-tops/golangci.yml`
)

// ignoreBlock is the kit's part of .gitignore. Everything a gate run writes under
// .go-tops/ is untracked (the report, the coverage profile, the lint output, the
// downloaded binaries), except the files the adopter commits: the baseline, the
// acknowledgement and survivor ledgers and the kit's lint config (GO_TOPS.md §6.7).
func ignoreBlock() string {
	return ignoreBegin + " (written by gotops init; edit outside these lines)\n" + ignoreRulesTxt + "\n" + ignoreEnd
}

// renderGitignore returns the full new .gitignore. Only the block between the
// markers is the kit's: an existing block is replaced, the pair is appended when
// absent, and one marker without the other is a refusal. Text outside the markers
// is never changed.
func renderGitignore(dir string) ([]byte, error) {
	existing, err := os.ReadFile(filepath.Join(dir, gitignoreFile))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return []byte(ignoreBlock() + "\n"), nil
	case err != nil:
		return nil, err
	}
	return mergeIgnore(string(existing))
}

func mergeIgnore(existing string) ([]byte, error) {
	nb, ne := strings.Count(existing, ignoreBegin), strings.Count(existing, ignoreEnd)
	switch {
	case nb == 0 && ne == 0:
		return []byte(appendBlock(existing, ignoreBlock())), nil
	case nb != 1 || ne != 1:
		return nil, refuse(".gitignore has %d %s and %d %s: want one pair or none; nothing was written", nb, ignoreBegin, ne, ignoreEnd)
	}
	b, e := strings.Index(existing, ignoreBegin), strings.Index(existing, ignoreEnd)
	if e < b {
		return nil, refuse(".gitignore has %s before %s; nothing was written", ignoreEnd, ignoreBegin)
	}
	return []byte(existing[:b] + ignoreBlock() + existing[e+len(ignoreEnd):]), nil
}
