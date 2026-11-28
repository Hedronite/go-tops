package lawpass

import "testing"

func TestLawForIssueBranches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		linter string
		text   string
		want   string
	}{
		{"errcheck", "x", "G-12"},
		{"errorlint", "x", "G-11"},
		{"noctx", "x", "G-16"},
		{"contextcheck", "x", "G-16"},
		{"bodyclose", "x", "G-18"},
		{"sqlclosecheck", "x", "G-18"},
		{"rowserrcheck", "x", "G-18"},
		{"gosec", "G401 weak", "G-37"},
		{"gosec", "unrelated", ""},
		{"govet", "waitgroup misuse", "G-02"},
		{"govet", "copylocks", "G-07"},
		{"govet", "other", ""},
		{"unknown", "", ""},
	}
	for _, c := range cases {
		got := lawForIssue(LintIssue{FromLinter: c.linter, Text: c.text})
		if got != c.want {
			t.Errorf("%s %q => %q, want %q", c.linter, c.text, got, c.want)
		}
	}
}

func TestIsToolLawMembership(t *testing.T) {
	t.Parallel()
	for _, id := range toolLawIDs {
		if !IsToolLaw(id) {
			t.Fatalf("%s should be a tool law", id)
		}
	}
	if IsToolLaw("G-34") || IsToolLaw("G-99") {
		t.Fatal("unexpected tool law")
	}
}
