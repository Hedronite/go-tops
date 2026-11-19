package ratchet

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

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
	l.Outcome, l.Reason = hygieneOutcome(details)
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

// hygieneOutcome folds the checks back into the layer's outcome and reason, as
// the hygiene layer does.
func hygieneOutcome(details map[string]any) (string, string) {
	var bad, absent []string
	total := 0
	for _, key := range []string{"tidy", "verify", "toolchain", "format", "lint", "vulnerabilities", "licenses", "depguard"} {
		check, ok := details[key].(map[string]any)
		if !ok {
			continue
		}
		total++
		msg := text(check["message"])
		switch check["outcome"] {
		case "fail":
			bad = append(bad, key+": "+msg)
		case "tool-missing":
			absent = append(absent, key+": "+msg)
		}
	}
	switch {
	case len(absent) > 0:
		return "tool-missing", fmt.Sprintf("%d check(s) lack a tool: %s", len(absent), strings.Join(absent, "; "))
	case len(bad) > 0:
		return "fail", fmt.Sprintf("%d of %d checks failed: %s", len(bad), total, strings.Join(bad, "; "))
	}
	return "pass", ""
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

// ApplyPartial gives the layers of a partial run under ratchet (ruling C3e): the
// range holds fewer layers than the ledger is measured from, so the thresholds the
// ledger replaces are not judged at all. The CRAP layer's functions over the cap, the
// coverage layer's floor and the lint findings are reported and booked as not judged.
// What the ledger does not replace stands: a function that is not measured and over
// the cap, an exemption that matches nothing (problems), and every other hygiene
// check. It returns a copy as Apply does, with only the verdict fields changed.
func ApplyPartial(layers []report.Layer, problems []Failure) []report.Layer {
	out := slices.Clone(layers)
	for i, read := range plain(layers) {
		switch read.ID {
		case "crap":
			out[i] = merge(layers[i], read, partialCrap(read, problems))
		case "coverage":
			out[i] = merge(layers[i], read, partialCoverage(read))
		case "hygiene":
			out[i] = merge(layers[i], read, partialHygiene(read), "lint")
		}
	}
	return out
}

func partialCrap(l report.Layer, problems []Failure) report.Layer {
	if l.Outcome != "fail" || hasKey(l.Details, "error") || !hasKey(l.Details, "violations") {
		return l
	}
	var standing []Failure
	for _, row := range listOf(l.Details["violations"]) {
		if row["crap"] == nil && text(row["func"]) != "" {
			standing = append(standing, Failure{
				Kind: "not measured", Key: text(row["func"]),
				Why: fmt.Sprintf("not measured on this run, and its complexity %d is over the cap", number(row["cc"])),
			})
		}
	}
	standing = append(standing, problems...)
	note := notMeasuredNote(l.Details)
	if len(standing) > 0 {
		l.Outcome, l.Reason = "fail", joinNote(fmt.Sprintf("%d verdict(s) the ledger does not replace: %s", len(standing), summary(standing)), note)
		return l
	}
	l.Outcome, l.Reason = "pass", joinNote(notJudged, note)
	return l
}

func partialCoverage(l report.Layer) report.Layer {
	if hasKey(l.Details, "error") || !hasKey(l.Details, "statements_pct") {
		return l
	}
	if l.Outcome == "fail" || l.Reason == floorReplaced {
		l.Outcome, l.Reason = "pass", notJudged
	}
	return l
}

func partialHygiene(l report.Layer) report.Layer {
	judged := judgeLint(l, Judgment{Outcome: Pass})
	if judged.Outcome == "pass" && l.Outcome == "fail" {
		judged.Reason = notJudged
	}
	return judged
}
