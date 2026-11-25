package survivors

import (
	"strings"
	"testing"
)

// The key's one string form is <file>:<function>:<mutator>:<line_hash> (§6.4).

func TestAKeyHasOneStringFormThatReadsBack(t *testing.T) {
	for _, k := range []Key{
		exact,
		{File: "a:b/c.go", Function: "Server.handle", Mutator: "INVERT_LOGICAL", LineHash: strings.Repeat("0", 64)},
	} {
		want := k.File + ":" + k.Function + ":" + k.Mutator + ":" + k.LineHash
		if got := k.String(); got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
		got, err := ParseKey(k.String())
		if err != nil || got != k {
			t.Errorf("ParseKey(%q) = %+v, %v", k.String(), got, err)
		}
	}
}

func TestParseKeyRefusesAStringThatLacksAPart(t *testing.T) {
	for _, s := range []string{
		"", "a.go", "a.go:F:ARITHMETIC_BASE", ":F:ARITHMETIC_BASE:abc", "a.go::ARITHMETIC_BASE:abc",
		"a.go:F::abc", "a.go:F:ARITHMETIC_BASE:",
	} {
		if k, err := ParseKey(s); err == nil {
			t.Errorf("ParseKey(%q) = %+v, want an error", s, k)
		}
	}
}
