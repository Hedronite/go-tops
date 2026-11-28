// Package embedded holds the kit law-pass golangci-lint config.
package embedded

import _ "embed"

// LawsGolangciYAML is the G0 oracle law-pass configuration (GO_TOPS.md §8.1).
//
//go:embed laws.golangci.yml
var LawsGolangciYAML []byte
