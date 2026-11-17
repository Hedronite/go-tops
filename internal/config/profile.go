package config

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"

	gotops "github.com/Hedronite/go-tops"
	"github.com/Hedronite/go-tops/internal/laws"
)

// ErrNoProfile is returned by ProfileFor for a class that has no embedded
// profile: only the three launch classes have one (GO_TOPS.md §5.1).
var ErrNoProfile = errors.New("no profile for this class")

// ProfileFor returns the embedded profile of a class: the defaults a config
// starts from. A config may tighten them and never loosen them (§12.1).
func ProfileFor(class string) (*Config, error) {
	data, err := fs.ReadFile(gotops.Files(), "profiles/"+class+".yaml")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoProfile
	}
	if err != nil {
		return nil, fmt.Errorf("read profile %s: %w", class, err)
	}
	cfg, issues, err := load(data, "profiles/"+class+".yaml", schemaConfig)
	if err != nil {
		return nil, err
	}
	if len(issues) > 0 {
		return nil, fmt.Errorf("embedded profile %s is invalid: %s", class, issues[0])
	}
	return cfg, nil
}

// Limits are the class column of GO_TOPS.md §5.2: the highest CRAP cap and the
// lowest statement-coverage floor a config of the class may state.
type Limits struct {
	Cap   float64
	Floor float64
}

var classLimits = map[string]Limits{
	"library-core":        {Cap: 8, Floor: 85},
	"binary-cli":          {Cap: 10, Floor: 70},
	"tui-app":             {Cap: 8, Floor: 65},
	"binary-service":      {Cap: 10, Floor: 75},
	"concurrency-runtime": {Cap: 8, Floor: 90},
}

// noColumn is what a class with no column of §5.2 takes: the cap of the
// strictest launch class and the strictest floor in the table.
var noColumn = Limits{Cap: 8, Floor: 90}

// LimitsFor returns the limits of a class.
func LimitsFor(class string) Limits {
	if l, ok := classLimits[class]; ok {
		return l
	}
	return noColumn
}

// LawInfo is what check needs to know about one law of the catalog (§8.2, §8.3).
type LawInfo struct {
	// Applies is true when the law's classes list includes the class.
	Applies bool
	// HasReader is true when the law is judged by an analyzer or a tool, not
	// attested or unimplemented.
	HasReader bool
	// Tier is the tier at which the reader runs.
	Tier int
}

// Laws answers questions about the law catalog. DefaultLaws is the real one:
// the table of GO_TOPS.md §8.2 in internal/laws.
type Laws interface {
	// Lookup returns the law's facts for a class, and false when the ID is not
	// in the catalog.
	Lookup(id, class string) (LawInfo, bool)
}

// DefaultLaws is the catalog of §8.2, held by internal/laws.
func DefaultLaws() Laws { return catalogLaws{} }

type catalogLaws struct{}

// Lookup reads the §8.2 table. A law applies to a class when it is active and
// the class is in its classes list (§8.3); it has a reader when an analyzer
// or a tool judges it, not when it is attested or not yet implemented; its
// tier is the tier of that reader (§3.2).
func (catalogLaws) Lookup(id, class string) (LawInfo, bool) {
	l, ok := laws.Lookup(id)
	if !ok {
		return LawInfo{}, false
	}
	return LawInfo{
		Applies:   l.Active() && slices.Contains(l.Classes, class),
		HasReader: l.Reader == laws.ReaderAnalyzer || l.Reader == laws.ReaderTool,
		Tier:      l.Tier,
	}, true
}
