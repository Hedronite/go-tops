package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	lintConfigFile = ".markdownlint-cli2.jsonc"
	workflowFile   = ".github/workflows/docs.yml"
)

var disabledRuleRe = regexp.MustCompile(`^\s*"([^"]+)"\s*:\s*false\b`)

// checkLintConfig requires a reason, as a // comment on the same line or on
// the comment lines directly above, for every rule the lint config disables.
func checkLintConfig(r *repo) checkResult {
	res := checkResult{}
	data, err := os.ReadFile(filepath.Join(r.Root, lintConfigFile))
	if err != nil {
		res.Findings = append(res.Findings, finding{File: lintConfigFile, Msg: "not found"})
		return res
	}
	lines := strings.Split(string(data), "\n")
	disabled := 0
	for i, line := range lines {
		m := disabledRuleRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		disabled++
		if !hasReason(lines, i) {
			res.Findings = append(res.Findings, finding{lintConfigFile, i + 1, fmt.Sprintf("rule %s is disabled with no reason: add a // comment on this line or directly above", m[1])})
		}
	}
	res.Summary = fmt.Sprintf("%d disabled rules, each with a reason", disabled)
	return res
}

// A reason is a comment of at least a dozen characters: a word is not one.
var reasonRe = regexp.MustCompile(`//\s*\S.{10,}`)

func hasReason(lines []string, i int) bool {
	if reasonRe.MatchString(lines[i]) {
		return true
	}
	for j := i - 1; j >= 0; j-- {
		t := strings.TrimSpace(lines[j])
		if !strings.HasPrefix(t, "//") {
			return false
		}
		if reasonRe.MatchString(t) {
			return true
		}
	}
	return false
}

var shaPinRe = regexp.MustCompile(`^[^@\s]+@[0-9a-f]{40}$`)

// checkWorkflow reads docs.yml and holds it to the floor G1 sets: read-only,
// runs superseded cancel, every action pinned by commit SHA, ubuntu-latest,
// and one aggregate job "docs" that needs every other job and runs always.
func checkWorkflow(r *repo) checkResult {
	res := checkResult{}
	add := func(format string, a ...any) {
		res.Findings = append(res.Findings, finding{File: workflowFile, Msg: fmt.Sprintf(format, a...)})
	}
	data, err := os.ReadFile(filepath.Join(r.Root, workflowFile))
	if err != nil {
		add("not found")
		return res
	}
	var wf struct {
		On          any            `yaml:"on"`
		Permissions map[string]any `yaml:"permissions"`
		Concurrency struct {
			Group  string `yaml:"group"`
			Cancel bool   `yaml:"cancel-in-progress"`
		} `yaml:"concurrency"`
		Jobs map[string]struct {
			RunsOn      string         `yaml:"runs-on"`
			Needs       any            `yaml:"needs"`
			If          string         `yaml:"if"`
			Permissions map[string]any `yaml:"permissions"`
			Steps       []struct {
				Uses string `yaml:"uses"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.NewDecoder(bytes.NewReader(data)).Decode(&wf); err != nil {
		add("invalid YAML: %v", err)
		return res
	}
	if len(wf.Permissions) != 1 || wf.Permissions["contents"] != "read" {
		add("top-level permissions must be exactly {contents: read}")
	}
	if wf.Concurrency.Group == "" || !wf.Concurrency.Cancel {
		add("concurrency needs a group and cancel-in-progress: true")
	}
	steps := 0
	for name, j := range wf.Jobs {
		if j.RunsOn != "ubuntu-latest" {
			add("job %s runs on %q; want ubuntu-latest", name, j.RunsOn)
		}
		for perm, v := range j.Permissions {
			if v != "read" && v != "none" {
				add("job %s grants %s: %v; jobs may only read", name, perm, v)
			}
		}
		for _, s := range j.Steps {
			if s.Uses == "" || strings.HasPrefix(s.Uses, "./") {
				continue
			}
			steps++
			if !shaPinRe.MatchString(s.Uses) {
				add("job %s: action %q is not pinned by a 40-hex commit SHA", name, s.Uses)
			}
		}
	}
	docs, ok := wf.Jobs["docs"]
	if !ok {
		add("no aggregate job named docs")
	} else {
		need := map[string]bool{}
		switch v := docs.Needs.(type) {
		case string:
			need[v] = true
		case []any:
			for _, x := range v {
				if s, ok := x.(string); ok {
					need[s] = true
				}
			}
		}
		for name := range wf.Jobs {
			if name != "docs" && !need[name] {
				add("job docs does not need %s", name)
			}
		}
		if !strings.Contains(docs.If, "always()") {
			add("job docs needs `if: always()` so a skipped or failed dependency fails it instead of skipping it")
		}
	}
	res.Summary = fmt.Sprintf("%d jobs, %d pinned actions, aggregate docs job", len(wf.Jobs), steps)
	return res
}
