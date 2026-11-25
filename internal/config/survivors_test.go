package config_test

import (
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/config"
)

func survivorLedger(expires string) map[string]string {
	return map[string]string{".go-tops/survivors.yaml": "survivors:\n  - {file: a.go, function: F, mutator: ARITHMETIC_BASE, line_hash: " +
		strings.Repeat("a", 64) + ", reason: three words here, owner: x, expires: \"" + expires + "\"}\n"}
}

// §6.4: an entry whose expires is today (UTC) or earlier explains nothing and
// fails at tier 0, named, whether the mutation gate is on or off. fixedNow is
// 2026-11-17.
func TestAPassedSurvivorEntryFailsAtTierZero(t *testing.T) {
	on := set("gates.mutation.ledger", ".go-tops/survivors.yaml")
	off := set("gates.mutation", doc{"enabled": false, "reason": "not yet turned on", "expires": "2027-03-01"})
	for name, mut := range map[string]func(doc){"mutation on": on, "mutation off": off} {
		t.Run(name, func(t *testing.T) {
			d := baseDoc(t)
			mut(d)
			for _, day := range []string{"2026-11-17", "2026-11-16"} {
				res := check(t, writeModule(t, d, survivorLedger(day)))
				expect(t, res.Issues, want{file: ".go-tops/survivors.yaml", rule: "expiry", pointer: ptr("/survivors/0/expires"), contains: day})
			}
			requirePass(t, writeModule(t, d, survivorLedger("2026-11-18")))
		})
	}
}

// ParseSurvivors judges form and the calendar, not whether a day has passed: the
// ledger commands read a ledger that holds passed entries.
func TestParseSurvivorsDoesNotJudgeExpiry(t *testing.T) {
	for day, issues := range map[string]int{"2001-01-01": 0, "2027-02-30": 1} {
		data := []byte(survivorLedger(day)[".go-tops/survivors.yaml"])
		got, err := config.ParseSurvivors("s.yaml", data)
		if err != nil || len(got) != issues {
			t.Errorf("expires %s: issues %v, err %v, want %d issues", day, got, err, issues)
		}
	}
}
