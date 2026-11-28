// Package pathglob matches repository-relative paths against §8.3 allow_paths globs.
package pathglob

import (
	"path"
	"strings"
)

// Match reports whether rel matches any of patterns. Paths use forward slashes.
func Match(patterns []string, rel string) bool {
	rel = strings.ReplaceAll(rel, "\\", "/")
	for _, p := range patterns {
		if matchOne(p, rel) {
			return true
		}
	}
	return false
}

func matchOne(pattern, rel string) bool {
	pattern = strings.ReplaceAll(pattern, "\\", "/")
	if strings.Contains(pattern, "**") {
		return matchDoubleStar(pattern, rel)
	}
	ok, err := path.Match(pattern, rel)
	if err != nil {
		return false
	}
	return ok
}

func matchDoubleStar(pattern, rel string) bool {
	parts := strings.Split(pattern, "**")
	if len(parts) != 2 {
		return strings.Contains(pattern, "*") && wildcardPath(pattern, rel)
	}
	pre, suf := trimDoubleStarAnchors(parts[0], parts[1])
	if pre != "" && !strings.HasPrefix(rel, pre) {
		return false
	}
	rest := restAfterDoubleStarPrefix(rel, pre)
	if suf == "" {
		return true
	}
	return matchDoubleStarSuffix(suf, rest)
}

func trimDoubleStarAnchors(pre, suf string) (string, string) {
	pre = strings.TrimSuffix(pre, "/")
	suf = strings.TrimPrefix(suf, "/")
	return pre, suf
}

func restAfterDoubleStarPrefix(rel, pre string) string {
	rest := strings.TrimPrefix(rel, pre)
	return strings.TrimPrefix(rest, "/")
}

func matchDoubleStarSuffix(suf, rest string) bool {
	if strings.Contains(suf, "*") {
		return wildcardPath(suf, rest) || wildcardPath(suf, path.Base(rest))
	}
	return plainSuffixMatch(suf, rest)
}

func plainSuffixMatch(suf, rest string) bool {
	if rest == suf {
		return true
	}
	if strings.HasSuffix(rest, suf) {
		return true
	}
	return strings.Contains(rest, "/"+suf)
}

func wildcardPath(pattern, name string) bool {
	ok, err := path.Match(pattern, name)
	if err != nil {
		return false
	}
	if ok {
		return true
	}
	segs := strings.Split(name, "/")
	base := pattern
	if i := strings.LastIndex(pattern, "/"); i >= 0 {
		base = pattern[i+1:]
		name = segs[len(segs)-1]
	}
	ok, err = path.Match(base, name)
	if err != nil {
		return false
	}
	return ok
}
