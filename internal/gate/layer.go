package gate

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/Hedronite/go-tops/internal/report"
)

// Layer is one gate of §3.1 or one class layer of §7.
//
// Run does the layer's work through env.Exec and env.Tools and returns its
// outcome. Whether the layer is enabled, in range, reached or skipped is the
// runner's decision, not the layer's.
type Layer interface {
	ID() string
	// Tier is the tier the layer runs at (§3.2).
	Tier() int
	// Needs lists the layers that must be pass for this one to run (§3.5).
	Needs() []string
	Run(ctx context.Context, env *Env) Result
}

// Commanded is implemented by a layer that can say it has no command mapped
// yet. A layer that does not implement it has one. The runner asks before it
// applies the needs rule: an enabled layer in range with no command is
// not-run whatever its needs did, because not-reached is for a layer that has a
// command (§3.3).
type Commanded interface {
	HasCommand() bool
}

func hasCommand(l Layer) bool {
	c, ok := l.(Commanded)
	return !ok || c.HasCommand()
}

// Preflighter is implemented by a layer that can find, before any layer runs,
// that the run cannot start: a lock mismatch (*LockError, exit 2) or an
// environment fault such as a missing C compiler (*EnvError, exit 3).
type Preflighter interface {
	Preflight(ctx context.Context, env *Env) error
}

// Env is what a layer sees.
type Env struct {
	Dir    string
	Config Config
	Base   string
	Exec   Executor
	Tools  Resolver
	// RatchetLaws holds ApplicableJudged for baseline/ratchet parity (§6.7, M5 G2).
	RatchetLaws report.Laws
	// RatchetLawRefusals holds per-file law refusal counts for the ledger (M5 G5).
	RatchetLawRefusals map[string]map[string]int
}

// order is the §3.1 gate order. A registered layer takes its place here; one
// not listed runs after them, in registration order.
var order = []string{"hygiene", "tests", "coverage", "crap", "mutation", "allocs", "fuzz", "stress", "bench"}

var (
	mu       sync.Mutex
	registry []Layer
)

// Register adds a layer to the default set. A layer with the ID of one
// already registered replaces it: the real layers of hygiene, tests and
// coverage replace the not-run stubs this way.
func Register(l Layer) {
	mu.Lock()
	defer mu.Unlock()
	for i, old := range registry {
		if old.ID() == l.ID() {
			registry[i] = l
			return
		}
	}
	registry = append(registry, l)
}

// Layers returns the registered layers in gate order.
func Layers() []Layer {
	mu.Lock()
	out := make([]Layer, len(registry))
	copy(out, registry)
	mu.Unlock()
	return sorted(out)
}

func rank(id string) int {
	for i, o := range order {
		if o == id {
			return i
		}
	}
	return len(order)
}

func sorted(ls []Layer) []Layer {
	sort.SliceStable(ls, func(i, j int) bool { return rank(ls[i].ID()) < rank(ls[j].ID()) })
	return ls
}

// check rejects a layer set whose IDs repeat or whose Needs name a layer that
// does not come first.
func check(ls []Layer) error {
	seen := map[string]bool{}
	for _, l := range ls {
		for _, n := range l.Needs() {
			if !seen[n] {
				return fmt.Errorf("gate: layer %s needs %s, which is not registered before it", l.ID(), n)
			}
		}
		if seen[l.ID()] {
			return fmt.Errorf("gate: layer %s is registered twice", l.ID())
		}
		seen[l.ID()] = true
	}
	return nil
}
