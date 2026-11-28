package pathglob

import "testing"

func TestMatch(t *testing.T) {
	allow := []string{"cmd/**/main.go", "internal/app/wire*.go", "**/*_test.go"}
	cases := []struct {
		path string
		want bool
	}{
		{"cmd/gotops/main.go", true},
		{"internal/app/wire.go", true},
		{"internal/app/wire_extra.go", true},
		{"pkg/foo/foo_test.go", true},
		{"internal/work/context.go", false},
		{"cmd/app/main.go", true},
	}
	for _, c := range cases {
		if got := Match(allow, c.path); got != c.want {
			t.Errorf("Match(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestMatchDoubleStarEdges(t *testing.T) {
	if !matchDoubleStar("cmd/**", "cmd/foo/bar.go") {
		t.Fatal("cmd/**")
	}
	if matchDoubleStar("cmd/**", "other/foo.go") {
		t.Fatal("wrong prefix")
	}
	if !matchDoubleStar("**/main.go", "deep/nested/main.go") {
		t.Fatal("**/main.go suffix")
	}
	if !matchDoubleStar("a/**/b", "a/x/y/b") {
		t.Fatal("middle **")
	}
	if matchDoubleStar("bad*", "nomatch.go") {
		t.Fatal("single-star pattern len!=2")
	}
}

func TestWildcardPathSlashes(t *testing.T) {
	if !wildcardPath("*.go", "file.go") {
		t.Fatal("base match")
	}
	if !wildcardPath("subdir/*.go", "pkg/subdir/file.go") {
		t.Fatal("slash pattern")
	}
}

func TestMatchBackslash(t *testing.T) {
	if !Match([]string{"cmd/foo.go"}, "cmd\\foo.go") {
		t.Fatal("backslash normalize")
	}
}
