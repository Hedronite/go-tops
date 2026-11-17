package initcmd

import (
	"embed"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// templates hold the drop-in files with {PLACEHOLDER} markers, the notation
// of GO_TOPS.md §4.1.
//
//go:embed templates
var templates embed.FS

// templateFiles maps each written path to its template.
var templateFiles = []struct{ path, template string }{
	{"go-tops.yaml", "go-tops.yaml"},
	{".golangci.yml", "golangci.yml"},
	{".github/workflows/go-tops.yml", "workflow.yml"},
	{".github/CODEOWNERS", "codeowners"},
	{"justfile", "justfile"},
	{"GO-TOPS.md", "go-tops-record.md"},
}

var leftoverRe = regexp.MustCompile(`\{[A-Z][A-Z0-9_]*\}`)

func (in *inputs) values() map[string]string {
	return map[string]string{
		"{PROTOCOL}":       in.protocol,
		"{MODULE}":         in.module,
		"{CLASS}":          in.o.Class,
		"{ENFORCE}":        "ratchet",
		"{COVERAGE_FLOOR}": number(in.profile.Gates.Coverage.Floor),
		"{CRAP_CAP}":       number(in.profile.Gates.Crap.Cap),
		"{GO_FLOOR}":       in.goFloor,
		"{GO_CURRENT}":     goCurrent(),
		"{TOOLCHAIN}":      toolchain,
		"{TOOLCHAIN_NUM}":  strings.TrimPrefix(toolchain, "go"),
		"{OWNER}":          in.owner,
		"{EXPIRES_M2}":     expiresM2,
		"{EXPIRES_M4}":     expiresM4,
		"{LAYERS}":         in.layersYAML(),
		"{KIT_VERSION}":    in.kitVersionText(),
		"{TOOL_VERSIONS}":  toolVersionsText(),
	}
}

func number(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func (in *inputs) fill(text string) (string, error) {
	vals := in.values()
	pairs := make([]string, 0, 2*len(vals))
	for k, v := range vals {
		pairs = append(pairs, k, v)
	}
	out := strings.NewReplacer(pairs...).Replace(text)
	if m := leftoverRe.FindString(out); m != "" {
		return "", fmt.Errorf("template placeholder %s has no value", m)
	}
	return out, nil
}

func renderFiles(in *inputs) ([]file, error) {
	var out []file
	for _, tf := range templateFiles {
		raw, err := templates.ReadFile("templates/" + tf.template)
		if err != nil {
			return nil, err
		}
		text, err := in.fill(string(raw))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", tf.path, err)
		}
		out = append(out, file{tf.path, []byte(text)})
	}
	return append(out, file{"go-tops.lock", []byte(lockText(in.protocol))}, file{"tools/go.mod", []byte(in.toolsMod())}), nil
}

// layersYAML writes the class layers init leaves off: they run at tier 2, which
// lands at M6, so each is off with that reason (§3.3).
func (in *inputs) layersYAML() string {
	const reason = "tier 2 layers land at M6"
	var b strings.Builder
	b.WriteString("  fuzz:   { enabled: false, reason: \"" + reason + "\", targets: [] }\n")
	b.WriteString("  stress: { enabled: false, reason: \"" + reason + "\" }\n")
	bench := in.profile.Layers["bench"]
	why := reason
	if bench.Reason != "" {
		why = bench.Reason
	}
	b.WriteString("  bench:  { enabled: false, reason: \"" + why + "\"")
	if bench.RegressionPct != 0 {
		b.WriteString(", regression_pct: " + number(bench.RegressionPct))
	}
	b.WriteString(" }\n")
	return strings.TrimSuffix(b.String(), "\n")
}

func (in *inputs) kitVersionText() string {
	if in.kitTool {
		return in.o.KitVersion
	}
	return "not pinned (this init ran from a development build)"
}

func toolVersionsText() string {
	parts := []string{"toolchain " + toolchain + " and " + golangciLint.Name + " " + golangciLint.Version + " (go-tops.lock)"}
	var mods []string
	for _, t := range moduleTools {
		mods = append(mods, path0(t.Package)+" "+t.Version)
	}
	sort.Strings(mods)
	return strings.Join(append(parts, strings.Join(mods, ", ")+" (tools/go.mod)"), "; ")
}

// path0 is the last path element that names the tool: .../v2 names its parent.
func path0(pkg string) string {
	parts := strings.Split(pkg, "/")
	last := parts[len(parts)-1]
	if regexp.MustCompile(`^v\d+$`).MatchString(last) && len(parts) > 1 {
		return parts[len(parts)-2]
	}
	return last
}

// lockText is go-tops.lock written from the embedded pins (§10).
func lockText(protocol string) string {
	var b strings.Builder
	b.WriteString("# Pins for the Go toolchain and the downloaded binaries (GO_TOPS.md §10).\n")
	b.WriteString("# Written by gotops init from the kit's pins; change it with a reviewed gotops lock --update.\n")
	fmt.Fprintf(&b, "protocol: %q\ntoolchain: %s\n\n", protocol, toolchain)
	b.WriteString("trust:\n  mode: checksum\n  note: archive SHA-256 values copied from the upstream release checksum file; executable SHA-256 values computed from those verified archives\n\nbinaries:\n")
	p := golangciLint
	fmt.Fprintf(&b, "  - name: %s\n    version: %q\n    url: %s\n    extract: %s\n    sha256:\n", p.Name, p.Version, p.URL, p.Extract)
	for _, pl := range platforms {
		fmt.Fprintf(&b, "      %s: %s\n", pl, p.SHA256[pl])
	}
	b.WriteString("    executable_sha256:\n")
	for _, pl := range platforms {
		fmt.Fprintf(&b, "      %s: %s\n", pl, p.Executable[pl])
	}
	return b.String()
}

// toolsMod is tools/go.mod: the tool lines and their pinned versions (§10).
// go mod tidy -modfile fills the indirect requirements and tools/go.sum.
func (in *inputs) toolsMod() string {
	tools := append([]moduleTool(nil), moduleTools...)
	if in.kitTool {
		tools = append(tools, moduleTool{gotopsPackage, gotopsModule, in.o.KitVersion})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Package < tools[j].Package })
	var b strings.Builder
	fmt.Fprintf(&b, "module %s/tools\n\ngo %s\n\ntool (\n", in.module, in.goFloor)
	for _, t := range tools {
		fmt.Fprintf(&b, "\t%s\n", t.Package)
	}
	b.WriteString(")\n\nrequire (\n")
	for _, t := range tools {
		fmt.Fprintf(&b, "\t%s %s\n", t.Module, t.Version)
	}
	b.WriteString(")\n")
	return b.String()
}
