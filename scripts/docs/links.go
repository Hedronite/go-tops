package main

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	inlineLinkRe = regexp.MustCompile(`!?\[[^\]]*\]\(\s*(<[^>]*>|[^)\s]*)[^)]*\)`)
	refDefRe     = regexp.MustCompile(`^ {0,3}\[[^\]]+\]:\s*(<[^>]*>|\S+)`)
	schemeRe     = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)
)

// checkLinks resolves every relative link and anchor in every Markdown file
// without the network. External links (any scheme) are not fetched.
func checkLinks(r *repo) checkResult {
	res := checkResult{}
	links := 0
	for _, f := range r.Files {
		f.prose(func(n int, line string) {
			var targets []string
			for _, m := range inlineLinkRe.FindAllStringSubmatch(line, -1) {
				targets = append(targets, m[1])
			}
			if m := refDefRe.FindStringSubmatch(line); m != nil {
				targets = append(targets, m[1])
			}
			for _, t := range targets {
				t = strings.TrimSuffix(strings.TrimPrefix(t, "<"), ">")
				if t == "" || schemeRe.MatchString(t) {
					continue
				}
				links++
				if msg := r.resolveLink(f, t); msg != "" {
					res.Findings = append(res.Findings, finding{f.Rel, n, msg})
				}
			}
		})
	}
	res.Summary = fmt.Sprintf("%d relative links in %d files", links, len(r.Files))
	return res
}

// resolveLink returns "" when target (as written in from) resolves, else why not.
func (r *repo) resolveLink(from *mdFile, target string) string {
	p, frag, _ := strings.Cut(target, "#")
	p, err1 := url.PathUnescape(p)
	frag, err2 := url.PathUnescape(frag)
	if err1 != nil || err2 != nil {
		return fmt.Sprintf("link %q is not valid percent-encoding", target)
	}
	rel := from.Rel
	if p != "" {
		if strings.HasPrefix(p, "/") {
			rel = path.Clean(strings.TrimPrefix(p, "/"))
		} else {
			rel = path.Join(path.Dir(from.Rel), p)
		}
		if rel == ".." || strings.HasPrefix(rel, "../") {
			return fmt.Sprintf("link %q leaves the repository", target)
		}
		isDir, ok := r.existsExact(rel)
		if !ok {
			return fmt.Sprintf("link %q: %s does not exist (case-sensitive)", target, rel)
		}
		if isDir {
			return ""
		}
	}
	if frag == "" || !strings.HasSuffix(rel, ".md") {
		return ""
	}
	to, fnd := r.load(rel)
	if to == nil || len(fnd) > 0 {
		return fmt.Sprintf("link %q: cannot read %s", target, rel)
	}
	if !to.Anchors[frag] {
		return fmt.Sprintf("link %q: no heading or id %q in %s", target, frag, rel)
	}
	return ""
}

// existsExact reports whether rel exists under the root with every path
// component spelled exactly (GitHub is case-sensitive, macOS is not).
func (r *repo) existsExact(rel string) (isDir, ok bool) {
	dir := r.Root
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false, false
		}
		found := false
		for _, e := range entries {
			if e.Name() == part {
				found = true
				if i == len(parts)-1 {
					return e.IsDir(), true
				}
				break
			}
		}
		if !found {
			return false, false
		}
		dir = filepath.Join(dir, part)
	}
	return true, true
}
