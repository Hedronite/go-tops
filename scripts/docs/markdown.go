package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// skipDirs are never walked: VCS data, dependencies, the checker's own
// deliberately broken fixtures, and generated gate output.
var skipDirs = map[string]bool{".git": true, "node_modules": true, "testdata": true, ".go-tops": true}

type fence struct {
	Open, Close int // 1-based line numbers of the opening and closing fence
	Info        string
	Body        []string // lines between the fences, without line endings' "\n"
}

type heading struct {
	Line  int
	Level int // 1 for "#" .. 6 for "######"
	Text  string
	Slug  string
}

// mdFile is one parsed Markdown file. Line numbers are 1-based; InFence[i]
// says whether line i+1 is a fence line or inside a fenced block.
type mdFile struct {
	Rel      string // slash-separated, relative to the root
	Lines    []string
	InFence  []bool
	Fences   []fence
	Headings []heading
	Anchors  map[string]bool
}

type repo struct {
	Root  string
	Files []*mdFile
	byRel map[string]*mdFile
}

func loadRepo(root string) (*repo, checkResult, error) {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil, checkResult{}, fmt.Errorf("root %q is not a directory", root)
	}
	r := &repo{Root: root, byRel: map[string]*mdFile{}}
	parse := checkResult{Name: "parse"}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		f, fnd := r.parseFile(filepath.ToSlash(rel))
		parse.Findings = append(parse.Findings, fnd...)
		if f != nil {
			r.Files = append(r.Files, f)
		}
		return nil
	})
	if err != nil {
		return nil, parse, err
	}
	sort.Slice(r.Files, func(i, j int) bool { return r.Files[i].Rel < r.Files[j].Rel })
	parse.Summary = fmt.Sprintf("%d Markdown files", len(r.Files))
	return r, parse, nil
}

// load returns a parsed Markdown file by root-relative path, reading it on
// demand when the walk skipped it.
func (r *repo) load(rel string) (*mdFile, []finding) {
	if f, ok := r.byRel[rel]; ok {
		return f, nil
	}
	return r.parseFile(rel)
}

func (r *repo) parseFile(rel string) (*mdFile, []finding) {
	data, err := os.ReadFile(filepath.Join(r.Root, filepath.FromSlash(rel)))
	if err != nil {
		return nil, []finding{{File: rel, Msg: err.Error()}}
	}
	f, fnd := parseMarkdown(rel, string(data))
	r.byRel[rel] = f
	return f, fnd
}

var (
	fenceOpenRe = regexp.MustCompile("^( {0,3})(`{3,}|~{3,})(.*)$")
	headingRe   = regexp.MustCompile(`^ {0,3}(#{1,6})[ \t]+(.*?)[ \t]*$`)
	closingHash = regexp.MustCompile(`[ \t]+#+$`)
	htmlIDRe    = regexp.MustCompile(`(?:id|name)="([^"]+)"`)
)

func parseMarkdown(rel, text string) (*mdFile, []finding) {
	f := &mdFile{Rel: rel, Anchors: map[string]bool{}}
	f.Lines = strings.Split(text, "\n")
	if n := len(f.Lines); n > 0 && f.Lines[n-1] == "" {
		f.Lines = f.Lines[:n-1]
	}
	f.InFence = make([]bool, len(f.Lines))
	var out []finding
	var open *fence
	var mark string
	seen := map[string]int{}
	for i, line := range f.Lines {
		n := i + 1
		if open != nil {
			f.InFence[i] = true
			if isFenceClose(line, mark) {
				open.Close = n
				f.Fences = append(f.Fences, *open)
				open = nil
			} else {
				open.Body = append(open.Body, line)
			}
			continue
		}
		if m := fenceOpenRe.FindStringSubmatch(line); m != nil && !(m[2][0] == '`' && strings.Contains(m[3], "`")) {
			f.InFence[i] = true
			open, mark = &fence{Open: n, Info: strings.TrimSpace(m[3])}, m[2]
			continue
		}
		if m := headingRe.FindStringSubmatch(line); m != nil && m[2] != "" {
			text := closingHash.ReplaceAllString(m[2], "")
			slug := slugify(text)
			if c := seen[slug]; c > 0 {
				f.Anchors[fmt.Sprintf("%s-%d", slug, c)] = true
			} else {
				f.Anchors[slug] = true
			}
			seen[slug]++
			f.Headings = append(f.Headings, heading{Line: n, Level: len(m[1]), Text: text, Slug: slug})
		}
		for _, m := range htmlIDRe.FindAllStringSubmatch(line, -1) {
			f.Anchors[m[1]] = true
		}
	}
	if open != nil {
		out = append(out, finding{rel, open.Open, "fenced block opened here is never closed"})
	}
	return f, out
}

func isFenceClose(line, mark string) bool {
	t := strings.TrimLeft(line, " ")
	if len(line)-len(t) > 3 || !strings.HasPrefix(t, mark[:1]) {
		return false
	}
	n := len(t) - len(strings.TrimLeft(t, mark[:1]))
	return n >= len(mark) && strings.TrimSpace(t[n:]) == ""
}

var (
	mdLinkRe  = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	htmlTagRe = regexp.MustCompile(`<[^>]+>`)
)

// slugify applies GitHub's heading-anchor rule: render the inline text, lower
// it, drop everything but letters, digits, "_" and "-", and turn spaces into "-".
func slugify(heading string) string {
	s := mdLinkRe.ReplaceAllString(heading, "$1")
	s = htmlTagRe.ReplaceAllString(s, "")
	s = strings.NewReplacer("`", "", "*", "", "~~", "").Replace(s)
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r == ' ':
			b.WriteByte('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// maskCode blanks inline code spans (backticks included) so patterns do not
// match text that is only being quoted.
func maskCode(s string) string {
	b := []byte(s)
	for i := 0; i < len(b); {
		if b[i] != '`' {
			i++
			continue
		}
		j := i
		for j < len(b) && b[j] == '`' {
			j++
		}
		run := j - i
		end := -1
		for k := j; k < len(b); {
			if b[k] != '`' {
				k++
				continue
			}
			m := k
			for m < len(b) && b[m] == '`' {
				m++
			}
			if m-k == run {
				end = m
				break
			}
			k = m
		}
		if end < 0 {
			i = j
			continue
		}
		for k := i; k < end; k++ {
			b[k] = ' '
		}
		i = end
	}
	return string(b)
}

// prose yields each line outside a fenced block, with its 1-based number and
// inline code masked.
func (f *mdFile) prose(visit func(n int, line string)) {
	for i, line := range f.Lines {
		if !f.InFence[i] {
			visit(i+1, maskCode(line))
		}
	}
}

// splitRow splits a Markdown table row into trimmed cells, honouring "\|".
func splitRow(line string) []string {
	t := strings.TrimSpace(line)
	t = strings.TrimPrefix(t, "|")
	t = strings.TrimSuffix(t, "|")
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(t); i++ {
		switch {
		case t[i] == '\\' && i+1 < len(t) && t[i+1] == '|':
			cur.WriteString(`\|`)
			i++
		case t[i] == '|':
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(t[i])
		}
	}
	return append(cells, strings.TrimSpace(cur.String()))
}
