package config_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	gotops "github.com/Hedronite/go-tops"
	"github.com/Hedronite/go-tops/internal/config"
)

// fixedNow is "today" for every test: 2026-11-17, UTC. Dates in the planted
// configs are relative to it, so no test depends on the real clock.
func fixedNow() time.Time { return time.Date(2026, 11, 17, 15, 4, 5, 0, time.UTC) }

var opts = config.Options{Now: fixedNow}

type doc = map[string]any

// deleted marks a key to remove in setPath.
type deletedKey struct{}

var deleted = deletedKey{}

func protocolID(t *testing.T) string {
	t.Helper()
	p, err := gotops.Protocol()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// baseDoc is the smallest go-tops.yaml that check accepts: no ratchet, no dates.
func baseDoc(t *testing.T) doc {
	t.Helper()
	return doc{
		"protocol": protocolID(t),
		"version":  "0.1.0",
		"module":   "example.com/app",
		"class":    "binary-cli",
		"enforce":  "hard",
	}
}

// setPath sets a dotted path inside d, creating the maps on the way.
func setPath(d doc, path string, v any) {
	keys := strings.Split(path, ".")
	cur := d
	for _, k := range keys[:len(keys)-1] {
		next, ok := cur[k].(doc)
		if !ok {
			next = doc{}
			cur[k] = next
		}
		cur = next
	}
	last := keys[len(keys)-1]
	if _, isDel := v.(deletedKey); isDel {
		delete(cur, last)
		return
	}
	cur[last] = v
}

func mustYAML(t *testing.T, v any) []byte {
	t.Helper()
	data, err := yaml.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeFile(t *testing.T, dir, rel string, data []byte) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeModule writes a module directory: go.mod, go-tops.yaml from d, a valid
// go-tops.lock unless files carries its own, and any extra files (path to
// content).
func writeModule(t *testing.T, d doc, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", []byte("module example.com/app\n\ngo 1.26\n"))
	if d != nil {
		writeFile(t, dir, "go-tops.yaml", mustYAML(t, d))
	}
	if _, own := files[lockName]; !own {
		writeFile(t, dir, lockName, mustYAML(t, lockDoc(t)))
	}
	for rel, content := range files {
		writeFile(t, dir, rel, []byte(content))
	}
	return dir
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)
	cmd := exec.CommandContext(t.Context(), "git", full...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func commitAll(t *testing.T, dir string) {
	t.Helper()
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "c")
}

// want names one expected issue. Empty file means go-tops.yaml. A nil pointer
// is not checked; the empty string is the whole document.
type want struct {
	file     string
	rule     string
	pointer  *string
	contains string
}

func ptr(s string) *string { return &s }

func findIssue(issues []config.Issue, w want) bool {
	file := w.file
	if file == "" {
		file = "go-tops.yaml"
	}
	for _, is := range issues {
		if is.File != file || is.Rule != w.rule {
			continue
		}
		if w.pointer != nil && is.Pointer != *w.pointer {
			continue
		}
		if !strings.Contains(is.Msg, w.contains) {
			continue
		}
		return true
	}
	return false
}

func expect(t *testing.T, issues []config.Issue, w want) {
	t.Helper()
	if findIssue(issues, w) {
		return
	}
	var b strings.Builder
	for _, is := range issues {
		b.WriteString("\n  " + is.String())
	}
	got := "<nil>"
	if w.pointer != nil {
		got = *w.pointer
	}
	t.Errorf("no issue with rule %q, pointer %s, text %q in:%s", w.rule, got, w.contains, b.String())
}

func check(t *testing.T, dir string) config.Result {
	t.Helper()
	res, err := config.Check(context.Background(), dir, opts)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	return res
}

func requirePass(t *testing.T, dir string) {
	t.Helper()
	res := check(t, dir)
	if len(res.Issues) != 0 {
		var b strings.Builder
		for _, is := range res.Issues {
			b.WriteString("\n  " + is.String())
		}
		t.Fatalf("want no issues, got:%s", b.String())
	}
}
