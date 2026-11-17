package initcmd

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	beginMarker = "<!-- go-tops:begin -->"
	endMarker   = "<!-- go-tops:end -->"
)

// renderAgents returns the full new AGENTS.md. Only the block between the
// markers is the kit's: an existing block is replaced, the pair is appended
// when absent, and one marker without the other is a refusal (§12.1). Text
// outside the markers is never changed.
func renderAgents(o Options, in *inputs) ([]byte, error) {
	block, err := agentsBlock(o, in)
	if err != nil {
		return nil, err
	}
	existing, err := os.ReadFile(filepath.Join(o.Dir, agentsFile))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return []byte(block + "\n"), nil
	case err != nil:
		return nil, err
	}
	return mergeAgents(string(existing), block)
}

func agentsBlock(o Options, in *inputs) (string, error) {
	raw, err := fs.ReadFile(o.Assets, "config/AGENTS.template.md")
	if err != nil {
		return "", err
	}
	text, err := in.fill(string(raw))
	if err != nil {
		return "", err
	}
	b, e := strings.Index(text, beginMarker), strings.LastIndex(text, endMarker)
	if b < 0 || e < b {
		return "", errors.New("the embedded AGENTS template has no go-tops marker pair")
	}
	return text[b : e+len(endMarker)], nil
}

func mergeAgents(existing, block string) ([]byte, error) {
	nb, ne := strings.Count(existing, beginMarker), strings.Count(existing, endMarker)
	switch {
	case nb == 0 && ne == 0:
		return []byte(appendBlock(existing, block)), nil
	case nb != 1 || ne != 1:
		return nil, refuse("AGENTS.md has %d %s and %d %s: want one pair or none; nothing was written", nb, beginMarker, ne, endMarker)
	}
	b, e := strings.Index(existing, beginMarker), strings.Index(existing, endMarker)
	if e < b {
		return nil, refuse("AGENTS.md has %s before %s; nothing was written", endMarker, beginMarker)
	}
	return []byte(existing[:b] + block + existing[e+len(endMarker):]), nil
}

// appendBlock puts the pair after the existing text, on its own lines.
func appendBlock(existing, block string) string {
	switch {
	case existing == "":
		return block + "\n"
	case strings.HasSuffix(existing, "\n\n"):
		return existing + block + "\n"
	case strings.HasSuffix(existing, "\n"):
		return existing + "\n" + block + "\n"
	}
	return existing + "\n\n" + block + "\n"
}
