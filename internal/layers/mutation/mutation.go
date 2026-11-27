// Package mutation is gate 5 of GO_TOPS.md §3.1: the mutation layer. It runs
// the configured mutation engine over the branch diff, maps statuses and survivor
// keys per §6.4, and accounts exemptions and explained survivors.
package mutation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Hedronite/go-tops/internal/exempt"
	"github.com/Hedronite/go-tops/internal/gate"
	"github.com/Hedronite/go-tops/internal/gobuild"
	"github.com/Hedronite/go-tops/internal/layers/tests"
	"github.com/Hedronite/go-tops/internal/mutate"
)

// ID is the layer's ID.
const ID = "mutation"

func init() { gate.Register(Layer{}) }

// Layer is gate 5.
type Layer struct{}

// ID implements gate.Layer.
func (Layer) ID() string { return ID }

// Tier implements gate.Layer.
func (Layer) Tier() int { return 1 }

// Needs implements gate.Layer.
func (Layer) Needs() []string { return []string{tests.TestsID} }

// Preflight implements gate.Preflighter.
func (Layer) Preflight(ctx context.Context, env *gate.Env) error {
	if env == nil {
		return errors.New("mutation preflight: nil env")
	}
	if env.Config.Mutation.Engine == "overlay" {
		return gate.PreflightTools(ctx, env, "go")
	}
	return gate.PreflightTools(ctx, env, "gremlins")
}

// Run implements gate.Layer.
func (Layer) Run(ctx context.Context, env *gate.Env) gate.Result {
	build, err := gobuild.Resolve(env.Dir, tests.Build)
	if err != nil {
		return failure(err)
	}
	diff, err := mutate.ComputeDiff(ctx, env.Exec, env.Dir, env.Config.BaseRef(env.Base), build)
	if err != nil {
		return failure(err)
	}
	return runWithDiff(ctx, env, diff, build)
}

func runWithDiff(ctx context.Context, env *gate.Env, diff mutate.DiffResult, build gobuild.Build) gate.Result {
	if !diff.HasChangedProdLines() {
		return evaluateAndJudge(env, diff, nil, true)
	}
	if len(diff.InBuildFiles) == 0 {
		return evaluateAndJudge(env, diff, nil, false)
	}
	return executeAndJudge(ctx, env, diff, build)
}

func executeAndJudge(ctx context.Context, env *gate.Env, diff mutate.DiffResult, build gobuild.Build) gate.Result {
	if env.Config.Mutation.Engine == "overlay" {
		return executeOverlay(ctx, env, diff, build)
	}
	reports, err := mutate.RunGremlinsBatches(ctx, *env, diff, build)
	if err != nil {
		if errors.Is(err, gate.ErrToolMissing) {
			return gate.MissingTool("gremlins", err)
		}
		return failure(err)
	}
	return evaluateAndJudge(env, diff, reports, false)
}

func executeOverlay(ctx context.Context, env *gate.Env, diff mutate.DiffResult, build gobuild.Build) gate.Result {
	ev, err := mutate.RunOverlay(ctx, *env, diff, build, mutate.OverlayOptions{Today: time.Now()})
	if err != nil {
		if errors.Is(err, gate.ErrToolMissing) || errors.Is(err, gate.ErrNotFound) {
			return gate.MissingTool("go", err)
		}
		return failure(err)
	}
	return judgeResult(env.Config.Mutation.UnexplainedMax, ev)
}

func evaluateAndJudge(env *gate.Env, diff mutate.DiffResult, reports [][]byte, vacuous bool) gate.Result {
	var (
		ev  mutate.Evaluation
		err error
	)
	if env.Config.Mutation.Engine == "overlay" {
		ev, err = mutate.EvaluateOverlayEmpty(env.Config.Mutation, env.Config.Exemptions, diff, env.Dir, time.Now())
	} else {
		ev, err = mutate.EvaluateBatches(env.Config.Mutation, env.Config.Exemptions, reports, diff, env.Dir, time.Now())
	}
	if err != nil {
		return failure(err)
	}
	if vacuous {
		return vacuousResult(ev)
	}
	return judgeResult(env.Config.Mutation.UnexplainedMax, ev)
}

func vacuousResult(ev mutate.Evaluation) gate.Result {
	if len(ev.ExemptionProblems) > 0 {
		return gate.Result{
			Outcome: gate.Fail,
			Reason:  problemSummary(ev.ExemptionProblems),
			Details: ev.Details(),
		}
	}
	return gate.Result{
		Outcome: gate.Vacuous,
		Reason:  "no changed production Go lines in diff",
		Details: ev.Details(),
	}
}

func judgeResult(maxUnexplained int, ev mutate.Evaluation) gate.Result {
	outcome := gate.Pass
	if ev.SurvivedUnexplained > maxUnexplained || len(ev.ExemptionProblems) > 0 {
		outcome = gate.Fail
	}
	return gate.Result{
		Outcome: outcome,
		Reason:  resultReason(ev, outcome),
		Details: ev.Details(),
	}
}

func resultReason(ev mutate.Evaluation, outcome gate.Outcome) string {
	var parts []string
	parts = append(parts, countNote(ev))
	if ev.NotMeasured > 0 {
		parts = append(parts, fmt.Sprintf("%d not measured under build tags", ev.NotMeasured))
	}
	if len(ev.TimedOutKeys) > 0 {
		parts = append(parts, fmt.Sprintf("%d timed out", ev.TimedOut))
	}
	if len(ev.ExemptionProblems) > 0 {
		parts = append(parts, problemSummary(ev.ExemptionProblems))
	}
	if outcome == gate.Fail && ev.SurvivedUnexplained > 0 {
		parts = append(parts, fmt.Sprintf("first survivor: %s", firstUnexplainedKey(ev)))
	}
	return strings.Join(parts, "; ")
}

func firstUnexplainedKey(ev mutate.Evaluation) string {
	exemptSet := make(map[string]bool, len(ev.Exempt))
	for _, k := range ev.Exempt {
		exemptSet[k] = true
	}
	for _, s := range ev.Survivors {
		if s.Explained == nil && !exemptSet[s.Key.String()] {
			return s.Key.String()
		}
	}
	return ""
}

func countNote(ev mutate.Evaluation) string {
	if ev.Mutants == 0 {
		return "0 mutants"
	}
	return fmt.Sprintf("%d mutants (%d killed, %d explained, %d unexplained)",
		ev.Mutants, ev.Killed, ev.SurvivedExplained, ev.SurvivedUnexplained)
}

func problemSummary(probs []exempt.Problem) string {
	msgs := make([]string, 0, len(probs))
	for _, p := range probs {
		msgs = append(msgs, p.Message)
	}
	return strings.Join(msgs, "; ")
}

func failure(err error) gate.Result {
	return gate.Result{
		Outcome: gate.Fail,
		Reason:  err.Error(),
		Details: map[string]any{"error": err.Error()},
	}
}
