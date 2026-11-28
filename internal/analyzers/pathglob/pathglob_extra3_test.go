package pathglob

import "testing"

func TestPlainSuffixMatchBranches(t *testing.T) {
	if !plainSuffixMatch("x", "x") {
		t.Fatal("equal")
	}
	if !plainSuffixMatch("go", "main.go") {
		t.Fatal("suffix")
	}
	if !plainSuffixMatch("mid", "pre/mid/suf") {
		t.Fatal("slash segment")
	}
	if plainSuffixMatch("nope", "other") {
		t.Fatal("no match")
	}
}

func TestWildcardPathLeadingSlashPattern(t *testing.T) {
	if !wildcardPath("/x.go", "dir/x.go") {
		t.Fatal("leading slash in pattern")
	}
}
