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
