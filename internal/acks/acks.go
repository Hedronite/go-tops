// Package acks loads and validates //tops:ack comments and .go-tops/acks.yaml
// entries (GO_TOPS.md §3.3, §13 rule 6).
package acks

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"

)

// reasonWords is the core.schema.json reasonText pattern: at least three words.
var reasonWords = regexp.MustCompile(`^\S+(\s+\S+){2,}$`)

// LawID is the G-NN form of a law id in a suppression comment or ack ledger entry.
var lawID = regexp.MustCompile(`^G-[0-9]{2}$`)

// Entry is one row of .go-tops/acks.yaml.
type Entry struct {
	Law    string `yaml:"law"`
	File   string `yaml:"file"`
	Reason string `yaml:"reason"`
	Owner  string `yaml:"owner"`
}

// File is the shape of .go-tops/acks.yaml.
type File struct {
	Acks []Entry `yaml:"acks"`
}

// ReasonOK reports whether text satisfies reasonText (≥3 words).
func ReasonOK(text string) bool {
	return reasonWords.MatchString(strings.TrimSpace(text))
}

// Comment is a parsed //tops:ack G-NN <reason> line comment (reason only; no owner).
func Comment(text string) (law, reason string, ok bool) {
	text, ok = strings.CutPrefix(text, "//")
	if !ok {
		return "", "", false
	}
	text = strings.TrimLeft(text, " \t")
	if !strings.HasPrefix(text, "tops:ack ") {
		return "", "", false
	}
	rest := strings.TrimPrefix(text, "tops:ack ")
	law, rest, ok = strings.Cut(rest, " ")
	if !ok || !lawID.MatchString(law) {
		return "", "", false
	}
	reason = strings.TrimSpace(rest)
	if !ReasonOK(reason) {
		return "", "", false
	}
	return law, reason, true
}

// ParseYAML reads and strictly validates acks.yaml at path.
func ParseYAML(path string, data []byte) (File, error) {
	var doc File
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return File{}, fmt.Errorf("%s: %w", path, err)
	}
	for i, e := range doc.Acks {
		if !lawID.MatchString(e.Law) {
			return File{}, fmt.Errorf("%s: acks[%d].law %q is not G-NN", path, i, e.Law)
		}
		if e.File == "" {
			return File{}, fmt.Errorf("%s: acks[%d].file is empty", path, i)
		}
		if !ReasonOK(e.Reason) {
			return File{}, fmt.Errorf("%s: acks[%d].reason needs at least three words", path, i)
		}
		if strings.TrimSpace(e.Owner) == "" {
			return File{}, fmt.Errorf("%s: acks[%d].owner is empty", path, i)
		}
	}
	return doc, nil
}

// LoadFile reads path relative to dir and validates it.
func LoadFile(dir, rel string) (File, error) {
	if rel == "" {
		return File{}, nil
	}
	raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		return File{}, err
	}
	return ParseYAML(rel, raw)
}