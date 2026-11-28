package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const lawsTestGoTops = "# Doc\n\n### 8.2 Law catalog\n\n| ID | Law | Severity | Reader | v0.1 |\n| --- | --- | --- | --- | --- |\n| G-02 | wait group misuse | R | tl (govet) | ✓ |\n| G-10 | (`tui-app`) Bubble Tea model | R | an | |\n| G-16 | context on outbound calls | R | tl (noctx) | ✓ |\n\n### 8.3 Law metadata\n\nEach law lives in laws/G-NN.yaml.\n"

func lawCheckRepo(t *testing.T, names map[string]string) *repo {
	t.Helper()
	root := t.TempDir()
	if err := copyLawSchema(t, root); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, lawsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range names {
		p := filepath.Join(root, lawsDir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "GO_TOPS.md"), []byte(lawsTestGoTops), 0o644); err != nil {
		t.Fatal(err)
	}
	r, parse, err := loadRepo(root)
	if err != nil {
		t.Fatalf("loadRepo: %v", err)
	}
	if len(parse.Findings) > 0 {
		t.Fatalf("parse: %v", parse.Findings)
	}
	return r
}

func copyLawSchema(t *testing.T, root string) error {
	t.Helper()
	srcDir := filepath.Join("..", "..", "schema")
	dstDir := filepath.Join(root, "schema")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"law.schema.json", "core.schema.json", "go-tops.schema.json"} {
		data, err := os.ReadFile(filepath.Join(srcDir, name))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dstDir, name), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func lawYAML(id string, classes []string, v01 bool) string {
	classesBlock := classesYAML(classes)
	return fmt.Sprintf(`id: %s
title: test law %s
severity: refuse
status: active
classes:
%s
reader: tool
tool: govet
fixture: testdata/laws/pending/%s
rationale: rationale
fix: fix
v01: %t
`, id, id, classesBlock, id, v01)
}

func classesYAML(classes []string) string {
	var b strings.Builder
	for _, c := range classes {
		b.WriteString("- ")
		b.WriteString(c)
		b.WriteString("\n")
	}
	return b.String()
}

func findingsText(res checkResult) string {
	var parts []string
	for _, f := range res.Findings {
		parts = append(parts, f.Msg)
	}
	return strings.Join(parts, "\n")
}

func TestLawClassesPlantG02MissingLaunchClass(t *testing.T) {
	r := lawCheckRepo(t, map[string]string{
		"G-02.yaml": lawYAML("G-02", []string{"library-core", "binary-service"}, true),
		"G-10.yaml": lawYAML("G-10", []string{"tui-app"}, false),
		"G-16.yaml": lawYAML("G-16", []string{"library-core", "binary-service", "binary-cli"}, true),
	})
	res := checkLaws(r)
	msg := findingsText(res)
	if !strings.Contains(msg, `v0.1 law missing launch class "binary-cli"`) {
		t.Fatalf("want launch-class failure, got:\n%s", msg)
	}
}

func TestLawClassesPlantG10ExtraClass(t *testing.T) {
	r := lawCheckRepo(t, map[string]string{
		"G-02.yaml": lawYAML("G-02", []string{"library-core", "binary-service", "binary-cli"}, true),
		"G-10.yaml": lawYAML("G-10", []string{"tui-app", "binary-cli"}, false),
		"G-16.yaml": lawYAML("G-16", []string{"library-core", "binary-service", "binary-cli"}, true),
	})
	res := checkLaws(r)
	msg := findingsText(res)
	if !strings.Contains(msg, "classes differ from §8.2") || !strings.Contains(msg, "tui-app") {
		t.Fatalf("want class-tagged failure for G-10, got:\n%s", msg)
	}
}

func TestLawV01PendingFixtureFailsL15(t *testing.T) {
	r := lawCheckRepo(t, map[string]string{
		"G-02.yaml": lawYAML("G-02", []string{"library-core", "binary-service", "binary-cli"}, true),
		"G-10.yaml": lawYAML("G-10", []string{"tui-app"}, false),
		"G-16.yaml": lawYAML("G-16", []string{"library-core", "binary-service", "binary-cli"}, true),
	})
	res := checkLaws(r)
	msg := findingsText(res)
	if !strings.Contains(msg, "placeholder fixture") || !strings.Contains(msg, "L15") {
		t.Fatalf("want L15 pending failure, got:\n%s", msg)
	}
}

func TestLawIDFilenamePlantG16G17(t *testing.T) {
	body := lawYAML("G-17", []string{"library-core", "binary-service", "binary-cli"}, true)
	r := lawCheckRepo(t, map[string]string{
		"G-02.yaml": lawYAML("G-02", []string{"library-core", "binary-service", "binary-cli"}, true),
		"G-10.yaml": lawYAML("G-10", []string{"tui-app"}, false),
		"G-16.yaml": body,
	})
	res := checkLaws(r)
	msg := findingsText(res)
	if !strings.Contains(msg, `laws/G-16.yaml: id "G-17" does not match file name`) {
		t.Fatalf("want id/filename failure, got:\n%s", msg)
	}
}
