package config

import (
	"errors"
	"fmt"
	"go/version"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	configFile = "go-tops.yaml"
	lockFile   = "go-tops.lock"

	defaultSurvivors = ".go-tops/survivors.yaml"
	defaultAcks      = ".go-tops/acks.yaml"
	defaultBaseline  = ".go-tops/baseline.json"
)

// Options are the two things check reads from outside the repository: the clock
// and the law catalog. The zero value uses the real clock and DefaultLaws.
type Options struct {
	Now  func() time.Time
	Laws Laws
}

// Result is the outcome of Check. Config is set when go-tops.yaml passed the
// schema, even if a semantic rule then failed.
type Result struct {
	Config *Config
	Issues []Issue
}

// Check validates the config, the lock and the ledger files under the module
// root (GO_TOPS.md §12.1 `check`): the schema first, then each semantic rule.
// Issues are configuration failures (exit 2). The error is an environment
// failure (exit 3): an unreadable file, or a repository git cannot read.
func Check(root string, opts Options) (Result, error) {
	c := &checker{root: root, laws: opts.Laws}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	y, m, d := now().UTC().Date()
	c.today = time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	if c.laws == nil {
		c.laws = DefaultLaws()
	}
	if err := c.run(); err != nil {
		return Result{}, err
	}
	return Result{Config: c.cfg, Issues: sortIssues(c.issues)}, nil
}

type checker struct {
	root   string
	laws   Laws
	today  time.Time
	cfg    *Config
	issues []Issue
}

func (c *checker) add(rule, ptr, format string, a ...any) {
	c.addTo(configFile, rule, ptr, format, a...)
}

func (c *checker) addTo(file, rule, ptr, format string, a ...any) {
	c.issues = append(c.issues, Issue{File: file, Rule: rule, Pointer: ptr, Msg: fmt.Sprintf(format, a...)})
}

func (c *checker) readRel(rel string) ([]byte, error) {
	return os.ReadFile(filepath.Join(c.root, filepath.FromSlash(rel)))
}

func (c *checker) run() error {
	data, err := c.readRel(configFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		c.add("config", "", "not found: check reads go-tops.yaml at the module root")
	case err != nil:
		return err
	default:
		if err := c.loadAndCheck(data); err != nil {
			return err
		}
	}
	return c.lock()
}

func (c *checker) loadAndCheck(data []byte) error {
	cfg, issues, err := Load(data)
	if err != nil {
		return err
	}
	if len(issues) > 0 {
		c.issues = append(c.issues, issues...)
		return nil
	}
	c.cfg = cfg
	for _, rule := range []func() error{
		c.identity, c.toolchain, c.limits, c.switches, c.lawsRequired,
		c.tighten, c.exemptions, c.ledgers, c.baseline,
	} {
		if err := rule(); err != nil {
			return err
		}
	}
	return nil
}

// date checks a YYYY-MM-DD value: a real calendar day and, when inForce is set,
// one that has not passed. A date is in force before it, so it expires on the
// day it names (§6.8).
func (c *checker) date(file, ptr, s string, inForce bool) {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		c.addTo(file, "expiry", ptr, "%q is not a calendar day, YYYY-MM-DD", s)
		return
	}
	if inForce && !d.After(c.today) {
		c.addTo(file, "expiry", ptr, "expired on %s; today is %s (UTC), and an entry is in force before its date (§6.8)", s, c.today.Format(time.DateOnly))
	}
}

// identity: a non-empty version, and a module equal to go.mod's.
func (c *checker) identity() error {
	cfg := c.cfg
	if strings.TrimSpace(cfg.Version) == "" {
		c.add("version", "/version", "go-tops.yaml needs a non-empty version")
	}
	if cfg.Module == "" {
		c.add("module", "/module", "go-tops.yaml needs the module path")
		return nil
	}
	data, err := c.readRel("go.mod")
	if errors.Is(err, fs.ErrNotExist) {
		c.add("module", "/module", "go.mod not found next to go-tops.yaml")
		return nil
	}
	if err != nil {
		return err
	}
	got, ok := modulePath(data)
	switch {
	case !ok:
		c.add("module", "/module", "go.mod has no module line")
	case got != cfg.Module:
		c.add("module", "/module", "%q differs from the module in go.mod, %q", cfg.Module, got)
	}
	return nil
}

func modulePath(gomod []byte) (string, bool) {
	for line := range strings.SplitSeq(string(gomod), "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(line)
		if len(f) < 2 || f[0] != "module" {
			continue
		}
		if unq, err := strconv.Unquote(f[1]); err == nil {
			return unq, true
		}
		return f[1], true
	}
	return "", false
}

// toolchain: the language floor is not above the current release, in Go's own
// version order.
func (c *checker) toolchain() error {
	tc := c.cfg.Toolchain
	if tc.GoFloor != "" && tc.GoCurrent != "" && version.Compare("go"+tc.GoFloor, "go"+tc.GoCurrent) > 0 {
		c.add("toolchain", "/toolchain/go_floor", "go_floor %s is above go_current %s", tc.GoFloor, tc.GoCurrent)
	}
	return nil
}

// limits: the cap is not above the class column of §5.2 and the floor is not
// below it. A class with no column takes the cap 8 and the floor 90.
func (c *checker) limits() error {
	cfg := c.cfg
	lim := LimitsFor(cfg.Class)
	where := cfg.Class
	if _, ok := classLimits[cfg.Class]; !ok {
		where += ", a class with no column, which takes the cap 8 and the floor 90%"
	}
	if cp := cfg.Gates.Crap.Cap; cp != nil && *cp > lim.Cap {
		c.add("cap-class", "/gates/crap/cap", "cap %v is above the class cap %v (%s, §5.2)", *cp, lim.Cap, where)
	}
	if fl := cfg.Gates.Coverage.StatementsFloor; fl != nil && *fl < lim.Floor {
		c.add("floor-class", "/gates/coverage/statements_floor", "statements_floor %v is below the class floor %v (%s, §5.2)", *fl, lim.Floor, where)
	}
	return nil
}

type offSwitch struct {
	ptr     string
	enabled *bool
	reason  string
	expires string
	dated   bool // a disabled switch needs an expires that has not passed
}

// switches: a reason is more than whitespace, and a disabled crap or mutation
// gate has an expires that has not passed. The advisory key's expires must not
// have passed either; its reason is held by the schema's three-word rule.
func (c *checker) switches() error {
	g, l := c.cfg.Gates, c.cfg.Layers
	for _, s := range []offSwitch{
		{"/gates/crap", g.Crap.Enabled, g.Crap.Reason, g.Crap.Expires, true},
		{"/gates/mutation", g.Mutation.Enabled, g.Mutation.Reason, g.Mutation.Expires, true},
		{"/layers/fuzz", l.Fuzz.Enabled, l.Fuzz.Reason, "", false},
		{"/layers/stress", l.Stress.Enabled, l.Stress.Reason, "", false},
		{"/layers/bench", l.Bench.Enabled, l.Bench.Reason, "", false},
	} {
		off := s.enabled != nil && !*s.enabled
		c.reasonOK(s.ptr+"/reason", s.reason)
		if s.expires != "" {
			c.date(configFile, s.ptr+"/expires", s.expires, off && s.dated)
		}
	}
	if a := c.cfg.Advisory; a != nil {
		c.date(configFile, "/advisory/expires", a.Expires, true)
	}
	return nil
}

func (c *checker) reasonOK(ptr, reason string) {
	if reason != "" && strings.TrimSpace(reason) == "" {
		c.add("reason", ptr, "the reason is blank")
	}
}

// lawsRequired: each required law is in the catalog, applies to the class, has
// a reader and is read at tier 1 or below.
func (c *checker) lawsRequired() error {
	for i, id := range c.cfg.Laws.Required {
		ptr := "/laws/required/" + strconv.Itoa(i)
		info, ok := c.laws.Lookup(id, c.cfg.Class)
		switch {
		case !ok:
			c.add("laws-required", ptr, "%s is not in the catalog (§8.2)", id)
		case !info.Applies:
			c.add("laws-required", ptr, "%s does not apply to class %s (§8.3)", id, c.cfg.Class)
		case !info.HasReader:
			c.add("laws-required", ptr, "%s has no reader yet, so it cannot be required (§3.3)", id)
		case info.Tier > 1:
			c.add("laws-required", ptr, "%s is read at tier %d; a required law is read at tier 1 or below (§12.1)", id, info.Tier)
		}
	}
	return nil
}

// tighten: the class profile supplies the defaults, and a config may tighten
// them and never loosen them. The cap and the floor are held by limits; this
// holds the laws the profile requires and the benchmark threshold.
func (c *checker) tighten() error {
	cfg := c.cfg
	p, err := ProfileFor(cfg.Class)
	if errors.Is(err, ErrNoProfile) {
		return nil
	}
	if err != nil {
		return err
	}
	if cfg.Laws.Required != nil {
		var missing []string
		for _, id := range p.Laws.Required {
			if !slices.Contains(cfg.Laws.Required, id) {
				missing = append(missing, id)
			}
		}
		if len(missing) > 0 {
			c.add("loosens-profile", "/laws/required", "removes %s, which the %s profile requires; go-tops.yaml may add laws and never remove one (§3.3)", strings.Join(missing, ", "), cfg.Class)
		}
	}
	if cur, base := cfg.Layers.Bench.RegressionPct, p.Layers.Bench.RegressionPct; cur != nil && base != nil && *cur > *base {
		c.add("loosens-profile", "/layers/bench/regression_pct", "regression_pct %v is looser than the %v of the %s profile", *cur, *base, cfg.Class)
	}
	return nil
}

// exemptions: every record has a valid path glob and a date that has not
// passed. The schema holds the rest of the record (§6.8).
func (c *checker) exemptions() error {
	for i, e := range c.cfg.Exemptions {
		base := "/exemptions/" + strconv.Itoa(i)
		if why := globProblem(e.Path); why != "" {
			c.add("exemption", base+"/path", "path %q %s", e.Path, why)
		}
		c.date(configFile, base+"/expires", e.Expires, true)
	}
	return nil
}

// relProblem says why a path is not a module-relative, slash-separated path, or
// returns "".
func relProblem(p string) string {
	switch {
	case strings.HasPrefix(p, "/"):
		return "must be relative to the module root"
	case strings.Contains(p, `\`):
		return "must be slash-separated, with no backslash"
	}
	if slices.Contains(strings.Split(p, "/"), "..") {
		return "must not contain a .. element"
	}
	return ""
}

func globProblem(p string) string {
	if why := relProblem(p); why != "" {
		return why
	}
	for el := range strings.SplitSeq(p, "/") {
		if _, err := path.Match(el, ""); err != nil {
			return "is not a valid glob: " + err.Error()
		}
	}
	return ""
}

// ledgers: survivors.yaml and acks.yaml, when present, validate against the
// definitions of core.schema.json (§6.4, §3.3). An absent file is no ledger.
func (c *checker) ledgers() error {
	for _, l := range []struct {
		key, def, schema, ptr string
		survivors             bool
	}{
		{c.cfg.Gates.Mutation.Ledger, defaultSurvivors, schemaSurvivors, "/gates/mutation/ledger", true},
		{c.cfg.Laws.AckLedger, defaultAcks, schemaAcks, "/laws/ack_ledger", false},
	} {
		rel := l.def
		if l.key != "" {
			if why := relProblem(l.key); why != "" {
				c.add("ledger-file", l.ptr, "%q %s", l.key, why)
				continue
			}
			rel = l.key
		}
		if err := c.ledger(rel, l.schema, l.survivors); err != nil {
			return err
		}
	}
	return nil
}

func (c *checker) ledger(rel, schema string, survivors bool) error {
	data, err := c.readRel(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	v, issues := parseYAML(data)
	if len(issues) > 0 {
		c.issues = append(c.issues, tag(issues, rel)...)
		return nil
	}
	found, err := validate(schema, rel, v)
	if err != nil {
		return err
	}
	c.issues = append(c.issues, found...)
	if len(found) == 0 && survivors {
		c.survivorDays(rel, v)
	}
	return nil
}

// survivorDays: the pattern of `expires` checks its shape; this checks that it
// is a real day. Whether it has passed is the mutation layer's judgement.
func (c *checker) survivorDays(file string, v any) {
	for i, e := range asList(asMap(v)["survivors"]) {
		if s, ok := asMap(e)["expires"].(string); ok {
			c.date(file, "/survivors/"+strconv.Itoa(i)+"/expires", s, false)
		}
	}
}

// baseline: under ratchet (the default for adopters) the baseline file exists
// and is clean against HEAD (§6.7).
func (c *checker) baseline() error {
	if c.cfg.Enforce != "" && c.cfg.Enforce != "ratchet" {
		return nil
	}
	ptr, rel := "/enforce", defaultBaseline
	if c.cfg.Baseline != "" {
		ptr, rel = "/baseline", c.cfg.Baseline
		if why := relProblem(rel); why != "" {
			c.add("baseline", ptr, "%q %s", rel, why)
			return nil
		}
	}
	problem, err := baselineProblem(c.root, rel)
	if err != nil {
		return err
	}
	if problem != "" {
		c.add("baseline", ptr, "under ratchet the baseline %s %s (§6.7)", rel, problem)
	}
	return nil
}
