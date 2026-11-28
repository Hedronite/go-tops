package pathglob

import "testing"

func TestMatchDoubleStarSuffixExact(t *testing.T) {
	if !matchDoubleStar("pre/**/suf", "pre/mid/suf") {
		t.Fatal("suffix path")
	}
	if !matchDoubleStarSuffix("tail", "tail") {
		t.Fatal("rest equals suf")
	}
	if !matchDoubleStarSuffix("main.go", "c/main.go") {
		t.Fatal("has suffix")
	}
	if !matchDoubleStarSuffix("x", "a/x") {
		t.Fatal("slash+suf containment")
	}
}

func TestWildcardPathNoMatch(t *testing.T) {
	if wildcardPath("[", "file.go") {
		t.Fatal("invalid pattern")
	}
	if wildcardPath("*.go", "file.txt") {
		t.Fatal("wrong ext")
	}
}

func TestMatchOnePlainGlob(t *testing.T) {
	if !matchOne("cmd/*.go", "cmd/main.go") {
		t.Fatal("plain glob")
	}
	if matchOne("[", "x") {
		t.Fatal("bad pattern")
	}
}
