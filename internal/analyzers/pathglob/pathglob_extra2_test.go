package pathglob

import "testing"

func TestMatchDoubleStarSuffixPlain(t *testing.T) {
	if !matchDoubleStar("root/**/leaf", "root/a/leaf") {
		t.Fatal("plain suffix")
	}
	if matchDoubleStar("root/**/leaf", "root/a/wrong") {
		t.Fatal("wrong suffix")
	}
}

func TestWildcardPathPatternWithoutSlash(t *testing.T) {
	if !wildcardPath("*.go", "main.go") {
		t.Fatal("want match")
	}
	if wildcardPath("*.go", "dir/main.go") {
		t.Fatal("want base fallback attempt")
	}
}
