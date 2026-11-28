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

var analyzerLaws = map[string]bool{
	"G-08": true, "G-17": true, "G-19": true, "G-25": true,
	"G-26": true, "G-33": true, "G-38": true,
}

// Judge applies all v0.1 readers: analyzers and facts, delegated lint, hygiene.
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
		if analyzerLaws[l.ID] {
			judgeAnalyzer(r, l.ID, in.Evidence)
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
	count := ev.Lint.ByLaw[id]
	if id == "G-11" && ev.AnalyzersOK {
		count += ev.Analyzers["G-11"]
	}
	if count > 0 {
		r.Verdict = Refuse
		return
	}
	r.Verdict = Allow
}

func judgeAnalyzer(r *Result, id string, ev Evidence) {
	if !ev.AnalyzersOK {
		r.Reason = InputMissing
		return
	}
	if ev.Analyzers[id] > 0 {
		r.Verdict = Refuse
		return
	}
	r.Verdict = Allow
}
