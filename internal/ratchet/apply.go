package ratchet

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/Hedronite/go-tops/internal/layers/hygiene"
	"github.com/Hedronite/go-tops/internal/report"
)

// Apply gives the layers the thresholds the ledger replaces under ratchet
// (§6.7): the CRAP layer and the coverage layer judge by the hard rules, and a
// function over the cap or a package under the floor that the ledger covers is
// not a failure. On the ledger leg their outcome is the ratchet's; on any other
// leg it is the ratchet's skip, and only a function the ledger leg does not
// compile can fail the CRAP layer. A layer that failed for another reason (no
// profile, a profile with no block) stays failed.
//
// It returns a copy of the layers as they ran, with only the verdict fields it
// changes: the outcome and the reason of the three layers it judges, and the details
// key it judges (the CRAP rows, the lint check). It reads a normalized copy, so no
// other value in a report is rewritten, and a number keeps the digits it had.
func Apply(layers []report.Layer, j Judgment, l *Ledger) []report.Layer {
	out := slices.Clone(layers)
	for i, read := range plain(layers) {
		switch read.ID {
		case "crap":
			out[i] = merge(layers[i], read, applyCrap(read, j, l), "violations")
		case "coverage":
			out[i] = merge(layers[i], read, applyCoverage(read, j))
		case "hygiene":
			out[i] = merge(layers[i], read, applyHygiene(read, j), "lint")
		}
	}
	return out
}

// merge is the layer as it ran with the outcome and reason of the judged layer and,
// for each named key whose value the judgment changed, that value. A key it left as
// it found keeps the layer's own value, in the type the layer gave it.
func merge(ran, read, judged report.Layer, keys ...string) report.Layer {
	ran.Outcome, ran.Reason = judged.Outcome, judged.Reason
	var changed []string
	for _, k := range keys {
		if !reflect.DeepEqual(read.Details[k], judged.Details[k]) {
			changed = append(changed, k)
		}
	}
	if len(changed) == 0 {
		return ran
	}
	details := make(map[string]any, len(ran.Details))
	for k, v := range ran.Details {
		details[k] = v
	}
	for _, k := range changed {
		details[k] = judged.Details[k]
	}
	ran.Details = details
	return ran
}

// applyHygiene replaces only the lint-finding verdict (§6.7, findings kind): the
// one adopter-style lint run fails the hard rule on any finding, and under
// ratchet a finding fails only past the ledger's count for its linter and file.
// Every other check (tidy, verify, toolchain, format, vulnerabilities, licenses,
// depguard) keeps its own verdict, and a lint run that failed for another reason
// (no result, a non-zero exit with no finding) stays failed.
func applyHygiene(l report.Layer, j Judgment) report.Layer {
	return withHeldCounts(judgeLint(l, j), j)
}

// withHeldCounts books to the hygiene layer what a leg that is not the ledger leg
// finds in files the ledger build does not compile: the suppressions and law counts
// past the ledger's entry (ruling C3d). Hygiene is where suppressions and findings
// belong; the CRAP layer fails for functions only.
func withHeldCounts(l report.Layer, j Judgment) report.Layer {
	held := filterKinds(j.HeldToCap, "suppressions.nolint", "suppressions.ack", "laws")
	if ledgerLeg(j) || len(held) == 0 || (l.Outcome != "pass" && l.Outcome != "fail") {
		return l
	}
	l.Outcome = "fail"
	l.Reason = joinNote(l.Reason, fmt.Sprintf("%d suppression or law count(s) over the ledger in files the ledger build does not compile: %s", len(held), summary(held)))
	return l
}

// judgeLint replaces the lint-finding verdict of a layer that failed on findings.
func judgeLint(l report.Layer, j Judgment) report.Layer {
	if l.Outcome != "fail" {
		return l
	}
	lint, ok := l.Details["lint"].(map[string]any)
	if !ok || lint["outcome"] != "fail" {
		return l
	}
	if number(lint["findings"]) == 0 {
		return l
	}
	verdict := lintVerdict(j)
	details := make(map[string]any, len(l.Details))
	for k, v := range l.Details {
		details[k] = v
	}
	entry := make(map[string]any, len(lint))
	for k, v := range lint {
		entry[k] = v
	}
	entry["outcome"] = "pass"
	delete(entry, "message")
	if verdict != "" {
		entry["outcome"], entry["message"] = "fail", verdict
	}
	details["lint"] = entry
	l.Details = details
	outcome, reason := hygiene.Fold(hygiene.ChecksOf(details))
	l.Outcome, l.Reason = string(outcome), reason
	return l
}

// lintVerdict is the message of the ratchet's findings failures, or "" when the
// lint findings are within the ledger. A leg that is not the ledger leg does not
// judge them.
func lintVerdict(j Judgment) string {
	failures := filterKinds(j.Failures, "findings")
	if !ledgerLeg(j) {
		failures = filterKinds(j.HeldToCap, "findings")
	}
	if len(failures) == 0 {
		return ""
	}
	return fmt.Sprintf("%d finding count(s) over the ledger: %s", len(failures), summary(failures))
}

// ProblemKind is the kind of a failure for a crap exemption that matches nothing.
const ProblemKind = "exemption entry"

func ledgerLeg(j Judgment) bool { return j.Outcome != Skip }

func hasKey(m map[string]any, key string) bool { _, ok := m[key]; return ok }

func applyCrap(l report.Layer, j Judgment, ledger *Ledger) report.Layer {
	if l.Outcome != "fail" || hasKey(l.Details, "error") || !hasKey(l.Details, "violations") {
		return l
	}
	details := make(map[string]any, len(l.Details))
	for k, v := range l.Details {
		details[k] = v
	}
	details["violations"] = withEntries(l.Details["violations"], j, ledger)
	l.Details = details
	kinds := []string{"functions", "not measured", ProblemKind}
	failures := filterKinds(j.HeldToCap, kinds...)
	if ledgerLeg(j) {
		failures = filterKinds(j.Failures, kinds...)
	}
	note := notMeasuredNote(l.Details)
	if len(failures) > 0 {
		l.Outcome, l.Reason = "fail", joinNote(fmt.Sprintf("%d function(s) fail the ratchet: %s", len(failures), summary(failures)), note)
		return l
	}
	l.Outcome, l.Reason = "pass", joinNote("every function over the cap is within its ledger entry, or this leg leaves it to the ledger leg", note)
	return l
}

// applyCoverage gives the coverage layer the ratchet's verdict on the packages.
// Under ratchet the layer is wrapped (see WrapCoverage) so that a floor that is
// red does not stop the layers that need it; the packages judgment then decides
// here: on the ledger leg a package fails only past its ledger entry, and on any
// other leg the packages are the ledger leg's to judge.
func applyCoverage(l report.Layer, j Judgment) report.Layer {
	if (l.Outcome != "fail" && l.Outcome != "pass") || hasKey(l.Details, "error") || !hasKey(l.Details, "statements_pct") {
		return l
	}
	failures := filterKinds(j.Failures, "packages")
	if !ledgerLeg(j) {
		failures = filterKinds(j.HeldToCap, "packages")
		if len(failures) == 0 {
			l.Outcome, l.Reason = "pass", "the ratchet is skipped on this leg: the ledger leg judges the packages it compiles"
			return l
		}
	}
	if len(failures) > 0 {
		l.Outcome, l.Reason = "fail", fmt.Sprintf("%d package(s) fail the ratchet: %s", len(failures), summary(failures))
		return l
	}
	l.Outcome = "pass"
	return l
}

// withEntries fills each violation row's baseline with its ledger entry; a row
// with none, or on a leg that is not the ledger leg, keeps null.
func withEntries(rows any, j Judgment, ledger *Ledger) any {
	list, ok := rows.([]any)
	if !ok {
		return rows
	}
	out := make([]any, len(list))
	for i, r := range list {
		row, ok := r.(map[string]any)
		if !ok || !ledgerLeg(j) || ledger == nil {
			out[i] = r
			continue
		}
		cp := make(map[string]any, len(row))
		for k, v := range row {
			cp[k] = v
		}
		if key, ok := row["func"].(string); ok {
			if entry, has := ledger.Functions[key]; has {
				cp["baseline"] = entry
			}
		}
		out[i] = cp
	}
	return out
}

func notMeasuredNote(details map[string]any) string {
	if n := number(details["not_measured"]); n > 0 {
		return fmt.Sprintf("%d production function(s) not measured on this build", n)
	}
	return ""
}

func joinNote(reason, note string) string {
	switch {
	case note == "":
		return reason
	case reason == "":
		return note
	}
	return reason + "; " + note
}

func filterKinds(fs []Failure, kinds ...string) []Failure {
	var out []Failure
	for _, f := range fs {
		for _, k := range kinds {
			if f.Kind == k {
				out = append(out, f)
			}
		}
	}
	return out
}

// summary lists the first few failures as `key: why`.
func summary(fs []Failure) string {
	var parts []string
	for i, f := range fs {
		if i == 5 {
			parts = append(parts, fmt.Sprintf("and %d more", len(fs)-5))
			break
		}
		parts = append(parts, f.Key+" "+f.Why)
	}
	return strings.Join(parts, "; ")
}

// Notes renders what failed, one line each, for the text output: every failure
// of the judgment and every function an other leg holds to the cap.
func Notes(j Judgment) []string {
	out := append([]string(nil), j.Advice...)
	for _, f := range append(append([]Failure(nil), j.Failures...), j.HeldToCap...) {
		key := ""
		if f.Key != "" {
			key = " " + f.Key
		}
		out = append(out, fmt.Sprintf("ratchet: %s%s: %s\n", f.Kind, key, f.Why))
	}
	return out
}

// notJudged is the reason of a layer whose ledger-replaced threshold a partial run
// did not judge.
const notJudged = "not judged: partial run under ratchet"

// Partial is what a partial run under ratchet is judged from (ruling C3e).
type Partial struct {
	// Problems are the exemptions that match nothing.
	Problems []Failure
	// Ledger is the baseline in the working tree; nil when there is none, so that
	// nothing has an entry.
	Ledger *Ledger
	// Cap is the CRAP cap.
	Cap float64
	// Packages are the packages under the floor, and Findings the lint findings per
	// linter and file, as the run measured them; nil when the layer that measures them
	// is not in the range or its output cannot be read.
	Packages map[string]Package
	Findings Counts
}

// ApplyPartial gives the layers of a partial run under ratchet (ruling C3e). The
// range holds fewer layers than the ledger is measured from, and no diff base is
// judged, so an item that has an entry in the working-tree ledger is not judged: the
// layers report it as not judged. An item with no entry is judged at hard, absent
// meaning held to the cap, the floor or 0, so a partial run never hides new debt. What
// the ledger does not replace stands: a function that is not measured and over the cap,
// an exemption that matches nothing, and every other hygiene check. It returns a copy as
// Apply does, with only the verdict fields changed.
func ApplyPartial(layers []report.Layer, p Partial) []report.Layer {
	out := slices.Clone(layers)
	for i, read := range plain(layers) {
		switch read.ID {
		case "crap":
			out[i] = merge(layers[i], read, partialCrap(read, p))
		case "coverage":
			out[i] = merge(layers[i], read, partialCoverage(read, p))
		case "hygiene":
			out[i] = merge(layers[i], read, partialHygiene(read, p), "lint")
		}
	}
	return out
}

func partialCrap(l report.Layer, p Partial) report.Layer {
	if l.Outcome != "fail" || hasKey(l.Details, "error") || !hasKey(l.Details, "violations") {
		return l
	}
	standing := slices.Clone(p.Problems)
	for _, row := range listOf(l.Details["violations"]) {
		if f, ok := standingRow(row, p); ok {
			standing = append(standing, f)
		}
	}
	note := notMeasuredNote(l.Details)
	if len(standing) > 0 {
		l.Outcome, l.Reason = "fail", joinNote(fmt.Sprintf("%d verdict(s) not replaced by a ledger entry: %s", len(standing), summary(standing)), note)
		return l
	}
	l.Outcome, l.Reason = "pass", joinNote(notJudged, note)
	return l
}

// standingRow is the failure a row of the CRAP layer's violations stands for in a
// partial run: a function that is not measured and over the cap always, and a scored
// function over the cap that has no entry in the ledger.
func standingRow(row map[string]any, p Partial) (Failure, bool) {
	key := text(row["func"])
	if key == "" {
		return Failure{}, false
	}
	if row["crap"] == nil {
		return Failure{
			Kind: "not measured", Key: key,
			Why: fmt.Sprintf("not measured on this run, and its complexity %d is over the cap", number(row["cc"])),
		}, true
	}
	score, ok := asFloat(row["crap"])
	if _, held := ledgerFunction(p.Ledger, key); held || !ok || score <= p.Cap {
		return Failure{}, false
	}
	return Failure{
		Kind: "functions", Key: key,
		Why: fmt.Sprintf("scores %.2f, over the cap %g, and has no ledger entry", score, p.Cap),
	}, true
}

func ledgerFunction(l *Ledger, key string) (float64, bool) {
	if l == nil {
		return 0, false
	}
	v, ok := l.Functions[key]
	return v, ok
}

func partialCoverage(l report.Layer, p Partial) report.Layer {
	if hasKey(l.Details, "error") || !hasKey(l.Details, "statements_pct") {
		return l
	}
	var standing []Failure
	for _, path := range slices.Sorted(maps.Keys(p.Packages)) {
		if p.Ledger != nil {
			if _, held := p.Ledger.Packages[path]; held {
				continue
			}
		}
		pk := p.Packages[path]
		standing = append(standing, Failure{
			Kind: "packages", Key: path,
			Why: fmt.Sprintf("covers %.1f%%, under the floor, and has no ledger entry", pk.StatementsPct),
		})
	}
	switch {
	case len(standing) > 0:
		l.Outcome, l.Reason = "fail", fmt.Sprintf("%d package(s) not replaced by a ledger entry: %s", len(standing), summary(standing))
	case l.Outcome == "fail" || l.Reason == floorReplaced:
		l.Outcome, l.Reason = "pass", notJudged
	}
	return l
}

func partialHygiene(l report.Layer, p Partial) report.Layer {
	// Findings are nil when the lint output could not be read. A layer that failed
	// on findings then has nothing that says they are within the ledger, so it stays
	// failed rather than becoming "not judged".
	if p.Findings == nil && lintFindingCount(l) > 0 {
		return l
	}
	var standing []Failure
	for _, name := range slices.Sorted(maps.Keys(p.Findings)) {
		for _, file := range slices.Sorted(maps.Keys(p.Findings[name])) {
			if !p.Ledger.holdsFinding(name, file) {
				standing = append(standing, Failure{
					Kind: "findings", Key: name + " " + file,
					Why: fmt.Sprintf("counts %d, and the ledger has no entry", p.Findings[name][file]),
				})
			}
		}
	}
	judged := judgeLint(l, Judgment{Outcome: Pass, Failures: standing})
	if judged.Outcome == "pass" && l.Outcome == "fail" {
		judged.Reason = notJudged
	}
	return judged
}

// holdsFinding reports whether the ledger has an entry for the findings of a linter
// in a file; a missing ledger has none.
func (l *Ledger) holdsFinding(name, file string) bool {
	if l == nil {
		return false
	}
	_, ok := l.Findings[name][file]
	return ok
}

// lintFindingCount is the number of lint findings the hygiene layer reports.
func lintFindingCount(l report.Layer) int {
	lint, ok := l.Details["lint"].(map[string]any)
	if !ok {
		return 0
	}
	return number(lint["findings"])
}
