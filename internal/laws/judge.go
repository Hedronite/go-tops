package laws

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

// Judge is the stub of the laws layer. No reader exists yet (M5), so it
// judges nothing: an applicable law is never allow, ack or refuse.
//
//   - a law above --max-tier, or any law when --only left the layer out, is
//     out of range: not-evaluated, input_missing, listed as out of range;
//   - when --fail-fast stopped the run before the layer, the output no law
//     could read was never produced: not-evaluated, input_missing;
//   - otherwise not-evaluated, no_reader.
//
// It returns one result per applicable law, sorted by ID.
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
			r.Reason = NoReader
		}
		out = append(out, r)
	}
	return out
}
