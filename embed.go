// Package gotops carries the protocol files the gotops binary embeds: the
// schemas, the class profiles and the AGENTS.md template. The binary validates
// against these copies, so a build can never disagree with the files it ships
// with. The protocol text itself is GO_TOPS.md at the repository root.
package gotops

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
)

//go:embed schema/*.json profiles/*.yaml config/AGENTS.template.md laws/*.yaml
var files embed.FS

// Files returns the embedded schemas, profiles and template as a read-only
// file system, rooted at the repository root: schema/*.json, profiles/*.yaml
// and config/AGENTS.template.md.
func Files() fs.FS { return files }

// Protocol returns the protocol version the binary implements: the constant of
// the embedded go-tops.schema.json, which the schema owns (GO_TOPS.md §2, §18).
func Protocol() (string, error) { return protocolOf(files) }

func protocolOf(fsys fs.FS) (string, error) {
	data, err := fs.ReadFile(fsys, "schema/go-tops.schema.json")
	if err != nil {
		return "", fmt.Errorf("read embedded config schema: %w", err)
	}
	var schema struct {
		Properties struct {
			Protocol struct {
				Const string `json:"const"`
			} `json:"protocol"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		return "", fmt.Errorf("parse embedded config schema: %w", err)
	}
	if schema.Properties.Protocol.Const == "" {
		return "", errors.New("embedded config schema has no protocol const")
	}
	return schema.Properties.Protocol.Const, nil
}
