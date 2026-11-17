package gate

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	gotops "github.com/Hedronite/go-tops"
	"github.com/Hedronite/go-tops/internal/laws"
	"github.com/Hedronite/go-tops/internal/report"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// ---- fake layers ----

type fakeLayer struct {
	id      string
	tier    int
	needs   []string
	res     Result
	ran     *[]string
	preErr  error
	preSeen bool
}

func (f *fakeLayer) ID() string      { return f.id }
func (f *fakeLayer) Tier() int       { return f.tier }
func (f *fakeLayer) Needs() []string { return f.needs }
func (f *fakeLayer) Run(context.Context, *Env) Result {
	if f.ran != nil {
		*f.ran = append(*f.ran, f.id)
	}
	return f.res
}
func (f *fakeLayer) Preflight(context.Context, *Env) error {
	f.preSeen = true
	return f.preErr
}

// details that satisfy schema/gate-report.schema.json for a layer that ran.
func details(id string) map[string]any {
	switch id {
	case "tests":
		return map[string]any{"packages": 3, "tests": 12, "race": true, "skipped": 0, "shuffle_seed": 7}
	case "coverage":
		return map[string]any{"statements_pct": 80.5, "floor": 70.0, "profile_bytes": 1200}
	case "crap":
		return map[string]any{"cap": 10.0, "violations": []any{}}
	case "mutation":
		return map[string]any{"mutants": 0, "killed": 0, "survived_explained": 0, "survived_unexplained": 0, "max": 0,
			"timed_out": 0, "timed_out_keys": []any{}, "dropped": 0, "exempt": []any{}}
	}
	return nil
}

func res(id string, o Outcome) Result {
	r := Result{Outcome: o}
	if o == Pass || o == Fail || o == Vacuous {
		r.Details = details(id)
	}
	return r
}

// gateLayers is the tier 1 set of §3.1 with the dependencies of §3.5, every
// layer returning pass unless the map says otherwise.
func gateLayers(ran *[]string, outcomes map[string]Outcome) []Layer {
	mk := func(id string, needs ...string) *fakeLayer {
		o := Pass
		if x, ok := outcomes[id]; ok {
			o = x
		}
		return &fakeLayer{id: id, tier: 1, needs: needs, res: res(id, o), ran: ran}
	}
	return []Layer{mk("hygiene"), mk("tests"), mk("coverage", "tests"), mk("crap", "coverage"), mk("mutation", "tests")}
}

// ---- fake executor ----

type fakeExec struct {
	calls   [][]string
	head    string
	dirty   bool
	gitFail bool
	toolOK  map[string]bool // `go tool -n <name>` succeeds for these
}

func newExec() *fakeExec { return &fakeExec{head: "abc1234"} }

func (f *fakeExec) Run(_ context.Context, c Cmd) (Output, error) {
	f.calls = append(f.calls, c.Argv)
	a := strings.Join(c.Argv, " ")
	switch {
	case a == "git rev-parse HEAD":
		if f.gitFail {
			return Output{ExitCode: 128, Stderr: []byte("fatal: not a git repository")}, nil
		}
		return Output{Stdout: []byte(f.head + "\n")}, nil
	case a == "git status --porcelain":
		if f.dirty {
			return Output{Stdout: []byte(" M go.mod\n")}, nil
		}
		return Output{}, nil
	case strings.HasPrefix(a, "go tool -modfile=tools/go.mod -n "):
		name := c.Argv[len(c.Argv)-1]
		if f.toolOK[name] {
			return Output{Stdout: []byte("/cache/" + name + "\n")}, nil
		}
		return Output{ExitCode: 2, Stderr: []byte("go: no such tool " + name)}, nil
	}
	return Output{}, errors.New("fakeExec: unexpected " + a)
}

// ---- config and run helpers ----

func v01() []string {
	var ids []string
	for _, l := range laws.Catalog() {
		if l.V01 {
			ids = append(ids, l.ID)
		}
	}
	return ids
}

func cfg() Config {
	return Config{Module: "example.com/m", Class: "binary-cli", Enforce: "hard", RequiredLaws: v01()}
}

var epoch = time.Date(2026, 11, 17, 18, 0, 0, 0, time.UTC)

func deps(ls []Layer) Deps {
	return Deps{Dir: ".", Layers: ls, Exec: newExec(), Tools: &ToolResolver{Dir: ".", Exec: newExec()}, ToolVersion: "0.0.0-dev",
		Now: func() time.Time { return epoch }}
}

func opts() Options { return Options{MaxTier: 1} }

// mustRun executes the gate, requires a run, and checks its report against the
// embedded schema: a schema-invalid report is a test failure.
func mustRun(t *testing.T, c Config, o Options, d Deps) *Run {
	t.Helper()
	r, err := Execute(context.Background(), c, o, d)
	if err != nil {
		t.Fatalf("Execute returned %v, want a run", err)
	}
	if r.DryRun {
		return r
	}
	checkSchema(t, r.Report)
	return r
}

func mustRefuse(t *testing.T, c Config, o Options, d Deps, code int) {
	t.Helper()
	if refusal(t, c, o, d, code) == nil {
		t.Fatal("refusal returned no error")
	}
}

// refusal is mustRefuse that also hands back the error.
func refusal(t *testing.T, c Config, o Options, d Deps, code int) error {
	t.Helper()
	r, err := Execute(context.Background(), c, o, d)
	if err == nil {
		t.Fatalf("Execute started a run (exit %d), want exit %d before any layer", r.ExitCode, code)
	}
	if got := exitCodeFor(err); got != code {
		t.Fatalf("exit code %d (%v), want %d", got, err, code)
	}
	return err
}

func layerOutcomes(r *Run) map[string]string {
	m := map[string]string{}
	for _, l := range r.Report.Layers {
		m[l.ID] = l.Outcome
	}
	return m
}

func ids(r *Run) []string {
	var out []string
	for _, l := range r.Report.Layers {
		out = append(out, l.ID)
	}
	return out
}

func has(xs []string, x string) bool { return slices.Contains(xs, x) }

// ---- schema ----

func checkSchema(t *testing.T, rep report.Report) {
	t.Helper()
	doc, err := report.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateDoc(doc); err != nil {
		t.Fatalf("the report is invalid against schema/gate-report.schema.json: %v\n%s", err, doc)
	}
}

func validateDoc(doc []byte) error {
	c := jsonschema.NewCompiler()
	for _, name := range []string{"core.schema.json", "go-tops.schema.json", "gate-report.schema.json"} {
		b, err := gotops.FS.ReadFile("schema/" + name)
		if err != nil {
			return err
		}
		v, err := jsonschema.UnmarshalJSON(strings.NewReader(string(b)))
		if err != nil {
			return err
		}
		if err := c.AddResource("mem:///schema/"+name, v); err != nil {
			return err
		}
	}
	sch, err := c.Compile("mem:///schema/gate-report.schema.json")
	if err != nil {
		return err
	}
	inst, err := jsonschema.UnmarshalJSON(strings.NewReader(string(doc)))
	if err != nil {
		return err
	}
	return sch.Validate(inst)
}

// analyzersAllow stands in for a laws layer whose analyzers have readers: every
// law in range is allow, every law out of range stays not-evaluated. The stub
// judges nothing, so a partial run (nothing in range failed) needs this.
func analyzersAllow(in laws.Input) []laws.Result {
	rs := laws.Judge(in)
	for i := range rs {
		if !rs[i].OutOfRange && in.Reached {
			rs[i].Verdict, rs[i].Reason = laws.Allow, ""
		}
	}
	return rs
}

func regexpShell() *regexp.Regexp {
	return regexp.MustCompile(`"(/bin/|/usr/bin/)?(sh|bash|zsh|dash|fish)"|\bsh -c\b|"-c",\s*"`)
}
