package gate

import "context"

// stub is a registered layer with no command yet. Enabled and in range it is
// not-run, which fails the gate (§3.3, §7); disabled it is a skip with its
// reason. Real layers register under the same ID and replace it.
type stub struct {
	id    string
	tier  int
	needs []string
}

func (s stub) ID() string      { return s.id }
func (s stub) Tier() int       { return s.tier }
func (s stub) Needs() []string { return s.needs }

// HasCommand is false: a stub is a layer with no command mapped (§3.3).
func (stub) HasCommand() bool { return false }

func (s stub) Run(context.Context, *Env) Result {
	return NoCommand(s.id)
}

func init() {
	for _, s := range []stub{
		{id: "hygiene", tier: 1},
		{id: "tests", tier: 1},
		{id: "coverage", tier: 1, needs: []string{"tests"}},
		{id: "crap", tier: 1, needs: []string{"coverage"}},
		{id: "mutation", tier: 1, needs: []string{"tests"}},
		{id: "allocs", tier: 1}, // allocation budgets (§7); switched by layers.bench
		{id: "fuzz", tier: 2},
		{id: "stress", tier: 2},
		{id: "bench", tier: 2},
	} {
		Register(s)
	}
}
