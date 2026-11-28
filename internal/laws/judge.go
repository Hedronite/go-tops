package laws

import "github.com/Hedronite/go-tops/internal/lawpass"

// Input is what the laws layer needs to know about the run.
type Input struct {
	Class   string
	MaxTier int
	// InRange is false when --only left the laws layer out of the run.
	InRange bool
	// Reached is false when --fail-fast stopped the run before the laws layer.
	Reached bool
	// Required is the profile's laws.required plus any IDs go-tops.yaml adds.
	Required []string
	Evidence Evidence
}

// Result is one law's outcome in one run.
type Result struct {
	ID       string
	Verdict  Verdict
	Reason   Reason // set exactly when Verdict is NotEvaluated
	Reader   Reader
	Required bool
	// OutOfRange: the run's range leaves the law out, by --max-tier or by
	// --only (§3.2, §3.5). The verdict is then not-evaluated, input_missing.
	OutOfRange bool
}

// Judge applies the v0.1 readers wired in G2: delegated lint laws from
// lint-laws.json, G-34 and G-40 from hygiene; other readers stay stubbed
// until G3.
func Judge(in Input) []Result {
	req := map[string]bool{}
	for _, id := range in.Required {
		req[id] = true
	}
	applicable := Applicable(in.Class)
	out := make([]Result, 0, len(applicable))
	for _, l := range applicable {
		r := Result{ID: l.ID, Verdict: NotEvaluated, Reader: l.Reader, Required: req[l.ID]}
		switch {
		case !in.InRange || l.Tier > in.MaxTier:
			r.Reason, r.OutOfRange = InputMissing, true
		case !in.Reached:
			r.Reason = InputMissing
		default:
			judgeOne(&r, l, in)
		}
		out = append(out, r)
	}
	return out
}

func judgeOne(r *Result, l Law, in Input) {
	switch l.ID {
	case "G-34":
		judgeHygienePair(r, in.Evidence.Hygiene, in.Evidence.Hygiene.TidyPass && in.Evidence.Hygiene.VerifyPass)
	case "G-40":
		judgeHygienePair(r, in.Evidence.Hygiene, in.Evidence.Hygiene.VulnsPass)
	default:
		if lawpass.IsToolLaw(l.ID) {
			judgeTool(r, l.ID, in.Evidence)
			return
		}
		r.Reason = NoReader
	}
}

func judgeHygienePair(r *Result, h HygieneEvidence, ok bool) {
	if !h.Ran {
		r.Reason = InputMissing
		return
	}
	if ok {
		r.Verdict = Allow
		r.Reason = ""
		return
	}
	r.Verdict = Refuse
	r.Reason = ""
}

func judgeTool(r *Result, id string, ev Evidence) {
	if !ev.LintPassOK {
		r.Reason = InputMissing
		return
	}
	if ev.Lint.ByLaw[id] > 0 {
		r.Verdict = Refuse
		return
	}
	r.Verdict = Allow
}
