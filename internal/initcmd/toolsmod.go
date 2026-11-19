package initcmd

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// An adopter that follows GO_TOPS.md §4.2 already has tools/go.mod, with gotops
// pinned by `go get -tool`. init adds the kit's pins to it and never removes a
// line: every tool, require, replace and comment it has survives, and a tool the
// kit pins that is missing is added after them.

var (
	toolLineRe    = regexp.MustCompile(`^tool\s+(\S+)\s*(//.*)?$`)
	requireLineRe = regexp.MustCompile(`^(?:require\s+)?(\S+)\s+(v\S+)\s*(//.*)?$`)
)

// toolsModState is what an existing tools/go.mod pins.
type toolsModState struct {
	tools    map[string]bool   // tool packages
	requires map[string]string // module -> version
}

var blockEntryRe = regexp.MustCompile(`^(\S+)\s*(//.*)?$`)

// readToolsMod reads the pins of an existing tools/go.mod. A line it does not
// understand is left alone: only tool and require lines are read.
func readToolsMod(text string) toolsModState {
	st := toolsModState{tools: map[string]bool{}, requires: map[string]string{}}
	block := ""
	for _, raw := range strings.Split(text, "\n") {
		block = st.read(strings.TrimSpace(raw), block)
	}
	return st
}

// read takes one trimmed line and returns the block it leaves open: "tool",
// "require" or "".
func (st toolsModState) read(line, block string) string {
	switch {
	case line == "" || strings.HasPrefix(line, "//"):
		return block
	case line == ")":
		return ""
	case line == "tool (" || line == "require (":
		return strings.TrimSuffix(line, " (")
	case block == "tool":
		st.addTool(blockEntryRe.FindStringSubmatch(line))
	case block == "require":
		st.addRequire(requireLineRe.FindStringSubmatch(line))
	case strings.HasPrefix(line, "tool "):
		st.addTool(toolLineRe.FindStringSubmatch(line))
	case strings.HasPrefix(line, "require "):
		st.addRequire(requireLineRe.FindStringSubmatch(line))
	}
	return block
}

func (st toolsModState) addTool(m []string) {
	if m != nil {
		st.tools[m[1]] = true
	}
}

func (st toolsModState) addRequire(m []string) {
	if m != nil {
		st.requires[m[1]] = m[2]
	}
}

// existingToolsMod is the text of tools/go.mod in dir, or "" and false when there
// is none.
func existingToolsMod(dir string) (string, bool, error) {
	b, err := os.ReadFile(filepath.Join(dir, "tools", "go.mod"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(b), true, nil
}

// wantedTools are the tools the kit pins: the module tools, and gotops itself
// when this build is a release.
func (in *inputs) wantedTools() []moduleTool {
	tools := append([]moduleTool(nil), moduleTools...)
	if in.kitTool {
		tools = append(tools, moduleTool{gotopsPackage, gotopsModule, in.o.KitVersion})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Package < tools[j].Package })
	return tools
}

// missingPins are the tool lines and require lines the kit pins that the file
// lacks. A kit tool pinned at another version is a refusal: the record and the lock
// name the kit's version. gotops itself is never a conflict: the adopter's pin of
// it is the one the record reads.
func (in *inputs) missingPins(st toolsModState) (tools, requires []string, err error) {
	for _, t := range in.wantedTools() {
		have, pinned := st.requires[t.Module]
		if pinned && have != t.Version && t.Module != gotopsModule {
			return nil, nil, refuse("tools/go.mod requires %s %s, and the kit pins %s: change the line, or leave the tool out of tools/go.mod and run init again; nothing was written", t.Module, have, t.Version)
		}
		if !pinned {
			requires = append(requires, "\t"+t.Module+" "+t.Version)
		}
		if !st.tools[t.Package] {
			tools = append(tools, "\t"+t.Package)
		}
	}
	return tools, requires, nil
}

// mergeToolsMod returns the existing tools/go.mod with the kit's missing pins
// added after its last line. A pin the file already has is left as it is.
func (in *inputs) mergeToolsMod(existing string) (string, error) {
	tools, requires, err := in.missingPins(readToolsMod(existing))
	if err != nil {
		return "", err
	}
	if len(tools) == 0 && len(requires) == 0 {
		return existing, nil
	}
	return strings.TrimRight(existing, "\n") + "\n\n// Added by gotops init: the kit's tool pins (GO_TOPS.md §10).\n" + block("tool", tools) + block("require", requires), nil
}

// block is a grouped go.mod directive, or nothing when it has no entries.
func block(directive string, entries []string) string {
	if len(entries) == 0 {
		return ""
	}
	return directive + " (\n" + strings.Join(entries, "\n") + "\n)\n"
}

// pinnedKit is the version of gotops that tools/go.mod pins, "" when it does not.
func pinnedKit(toolsMod string) string {
	return readToolsMod(toolsMod).requires[gotopsModule]
}
