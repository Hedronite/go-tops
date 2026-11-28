#!/usr/bin/env nu
# Wraps `gotops gate`. Does not re-implement the gate.
# It judges the run from .go-tops/report.json and the exit code under ruling C9:
# until the required laws are judged (M5), the gate exits 1 on them alone and that
# is a pass. Anything else that is not a pass is not ok, and not-evaluated is never
# a pass beyond those required laws. The judgment is the same as the drop-in
# workflow's judge step; the kit tests hold the two to each other.

def have [bin: string] {
  not ((which $bin | where type == external) | is-empty)
}

def clip [text: string] {
  let t = ($text | str trim)
  if ($t | str length) > 400 { $t | str substring 0..<400 } else { $t }
}

def is-list [v: any] {
  let kind = ($v | describe)
  ($kind | str starts-with "list") or ($kind | str starts-with "table")
}

def is-record [v: any] {
  ($v | describe) | str starts-with "record"
}

# The gotops command: --gotops, else $GOTOPS, else the adopter's pinned tool
# (tools/go.mod), else gotops on PATH.
def gotops-argv [path: string, given: string] {
  let chosen = if $given != "" { $given } else { $env.GOTOPS? | default "" }
  if $chosen != "" {
    return ($chosen | split row " " | where {|w| $w != "" })
  }
  let mod = ($path | path join "tools" "go.mod")
  if ($mod | path exists) and ((open --raw $mod) | str contains "github.com/Hedronite/go-tops") {
    return ["go" "tool" "-modfile=tools/go.mod" "gotops"]
  }
  ["gotops"]
}

def quote-arg [a: string] {
  if ($a | str contains " ") { $"\"($a)\"" } else { $a }
}

def argv-line [args: list<string>] {
  $args | each {|a| quote-arg $a } | str join " "
}

def c9-laws-ok [laws: record] {
  let req = ($laws.required? | default 0)
  let judged = ($laws.judged? | default 0)
  let ne = ($laws.required_not_evaluated? | default 0)
  let results = ($laws.results? | default [])
  let refused = ($results | where {|r| ($r.verdict? | default "") == "refuse" })
  if not ($refused | is-empty) { return false }
  if $req <= 0 { return false }
  if $judged == 0 { return ($req == $ne) }
  if $judged >= $req { return false }
  if $judged > 0 and $judged < $req {
    let bad_j = ($results | where {|r|
      let v = ($r.verdict? | default "")
      ($v != "not-evaluated") and ($v not-in ["allow" "ack"])
    })
    let bad_p = ($results | where {|r|
      ($r.verdict? | default "") == "not-evaluated" and (($r.reason? | default "") != "no_reader")
    })
    let ok = ($ne == ($req - $judged)) and ($bad_j | is-empty) and ($bad_p | is-empty)
    return $ok
  }
  false
}

def laws-problems [laws: record, failed_laws: bool] {
  if not $failed_laws { return [] }
  mut out = []
  let judged = ($laws.judged? | default 0)
  let req = ($laws.required? | default 0)
  if $req > 0 and $judged >= $req {
    let catalog = ($laws.catalog? | default 43)
    $out = ($out | append $"the required laws failed and ruling C9 has retired ($judged of ($catalog) laws judged)")
  } else if not (c9-laws-ok $laws) {
    $out = ($out | append "the laws block does not satisfy ruling C9 for this judged count")
  }
  $out
}

def shape-problems [r: record] {
  mut out = []
  if ($r.enforce? | describe) != "string" { $out = ($out | append "the report names no enforce mode") }
  if not (is-list $r.layers?) or ($r.layers? | default [] | is-empty) { $out = ($out | append "the report lists no layers") }
  if not (is-record $r.laws?) { $out = ($out | append "the report has no laws record") }
  let s = ($r.summary? | default null)
  if not (is-record $s) {
    $out = ($out | append "the report has no summary")
  } else {
    if not (is-list $s.failed?) { $out = ($out | append "the report summary has no failed list") }
  }
  $out
}

# The two builds of a ratchet judgment are the same build: GOOS, GOARCH, cgo and
# the set of tags (GO_TOPS.md section 6.7).
def same-build [a: any, b: any] {
  if not (is-record $a) or not (is-record $b) { return false }
  let flat = (($a.goos? | default "") == ($b.goos? | default "")) and (($a.goarch? | default "") == ($b.goarch? | default "")) and (($a.cgo_enabled? | default null) == ($b.cgo_enabled? | default null))
  $flat and ((($a.tags? | default []) | sort) == (($b.tags? | default []) | sort))
}

# A ratchet skip is judged by build (ruling C3b): it is the report that this run is
# not on the ledger's build, so it is allowed only when the two builds differ. A
# skip on the ledger's own build means the ratchet judged nothing where it must.
def ratchet-problems [r: record] {
  if ($r.enforce? | default "") != "ratchet" { return [] }
  let outcome = ($r.ratchet?.outcome? | default "")
  if $outcome not-in ["pass" "fail" "skip"] {
    return ["the report has no ratchet outcome under enforce: ratchet"]
  }
  if $outcome == "fail" { return ["the ratchet fails: a function is over its ledger entry"] }
  let both_named = ((is-record $r.ratchet?.ledger_build?) and (is-record $r.ratchet?.run_build?))
  if $outcome == "skip" and (not $both_named or (same-build $r.ratchet?.ledger_build? $r.ratchet?.run_build?)) {
    return ["the ratchet was skipped on the ledger's own build, so it judged nothing (C3b): the baseline was recorded under a build no leg runs, or the report names no builds"]
  }
  []
}

# pass and skip, and for the mutation layer alone vacuous: the diff changed no
# production line (GO_TOPS.md section 6.4).
def layer-passes [l: record] {
  let outcome = ($l.outcome? | default "")
  ($outcome in ["pass" "skip"]) or ($outcome == "vacuous" and ($l.id? | default "") == "mutation")
}

def layer-problems [r: record] {
  $r.layers | where {|l| not (layer-passes $l) } | each {|l|
    let said = $"layer ($l.id? | default '?') is ($l.outcome? | default 'missing')"
    let why = ($l.details?.error? | default "")
    if $why == "" { $said } else { $"($said): ($why)" }
  }
}

# The report's mutation layer, or null: this also reads a report that lacks layers.
def mutation-layer [r: record] {
  if not (is-list $r.layers?) { return null }
  $r.layers | where {|l| ($l.id? | default "") == "mutation" } | get 0?
}

# The survivors nothing explains: no ledger entry, and no mutation exemption.
def unexplained [m: any] {
  if not (is-record $m) { return [] }
  let exempt = ($m.details?.exempt? | default [])
  $m.details?.survivors? | default [] | where {|s| $s.explained? == null and ($s.key? | default "") not-in $exempt }
}

# A YYYY-MM-DD day after today (UTC).
def later-day [v: any] {
  if ($v | describe) != "string" { return false }
  if not ($v =~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$') { return false }
  $v > (date now | date to-timezone UTC | format date "%Y-%m-%d")
}

def mutation-problems [r: record] {
  let m = (mutation-layer $r)
  if $m == null { return [] }
  let outcome = ($m.outcome? | default "")
  mut out = (unexplained $m | each {|s| $"unexplained survivor ($s.key? | default '?') \(($s.status? | default '?'), line ($s.line? | default '?')\)" })
  if $outcome == "pass" and $m.details?.survived_unexplained? != 0 {
    $out = ($out | append "the mutation layer passed without a count of 0 unexplained survivors")
  }
  if $outcome == "skip" and not (later-day $m.expires?) {
    $out = ($out | append "the mutation gate is off without a date after today (UTC)")
  }
  $out
}

def mutation-data [r: record] {
  let m = (mutation-layer $r)
  if $m == null { return null }
  let d = ($m.details? | default {})
  {
    outcome: ($m.outcome? | default "")
    reason: ($m.reason? | default "")
    expires: ($m.expires? | default "")
    error: ($d.error? | default "")
    mutants: $d.mutants?
    killed: $d.killed?
    survived_explained: $d.survived_explained?
    survived_unexplained: $d.survived_unexplained?
    timed_out_keys: ($d.timed_out_keys? | default [])
    dropped: $d.dropped?
    exempt: ($d.exempt? | default [])
    unexplained: (unexplained $m | each {|s| {key: ($s.key? | default ""), line: $s.line?, status: ($s.status? | default "")} })
  }
}

def run-problems [code: int, r: record] {
  let failed = ($r.summary.failed | each {|f| $f | into string })
  mut out = []
  if ($r.summary.outcome? | default "") == "partial" or ($r.layer_counts?.enabled_in_range? | default (-1)) != ($r.layer_counts?.ran? | default (-2)) {
    $out = ($out | append "the run was partial: a layer in range did not run, so it is not a gate")
  }
  let others = ($failed | where {|f| $f != "laws" })
  if not ($others | is-empty) {
    $out = ($out | append $"failed: ($others | str join ', ')")
  }
  if ($r.summary.exit_code? | default null) != $code {
    $out = ($out | append $"the gate exited ($code) and its report says exit_code ($r.summary.exit_code? | default 'missing')")
  }
  if $code == 1 and ($failed | is-empty) {
    $out = ($out | append "the gate exited 1 but the report names no failed item")
  }
  $out
}

# judge is the C9 judgment of one run: every reason it is not a pass.
def judge [code: int, r: record] {
  let shape = (shape-problems $r)
  if not ($shape | is-empty) { return $shape }
  let failed_laws = ("laws" in $r.summary.failed)
  (mutation-problems $r)
    | append (layer-problems $r)
    | append (run-problems $code $r)
    | append (ratchet-problems $r)
    | append (laws-problems $r.laws $failed_laws)
}

def read-report [file: string] {
  if not ($file | path exists) { return {state: "missing", report: null} }
  let parsed = (try { open --raw $file | from json } catch { null })
  if not (is-record $parsed) { return {state: "bad", report: null} }
  {state: "ok", report: $parsed}
}

def law-gaps [r: record] {
  ($r.laws?.results? | default []) | where {|x| ($x.verdict? | default "") != "allow" and ($x.verdict? | default "") != "ack" } | each {|x|
    {id: ($x.id? | default ""), verdict: ($x.verdict? | default ""), reason: ($x.reason? | default "")}
  }
}

def data-of [code: int, r: record, problems: list, file: string] {
  let rat = ($r.ratchet? | default null)
  {
    exit: $code
    report: $file
    module: ($r.module? | default "")
    class: ($r.class? | default "")
    enforce: ($r.enforce? | default "")
    max_tier: ($r.max_tier? | default null)
    summary: ($r.summary? | default null)
    layers: (($r.layers? | default []) | each {|l| {id: ($l.id? | default ""), tier: ($l.tier? | default null), outcome: ($l.outcome? | default ""), reason: ($l.reason? | default "")} })
    mutation: (mutation-data $r)
    ratchet: (if $rat == null { null } else { {outcome: ($rat.outcome? | default ""), same_build: (same-build $rat.ledger_build? $rat.run_build?), judged: (($rat.outcome? | default "") in ["pass" "fail"])} })
    laws: {
      catalog: ($r.laws?.catalog? | default 43)
      judged: ($r.laws?.judged? | default 0)
      judged_label: (if ($r.laws?.judged? | default null) == null { null } else { $"($r.laws.judged) of ($r.laws?.catalog? | default 43) laws judged" })
      applicable: ($r.laws?.applicable? | default null)
      required: ($r.laws?.required? | default null)
      required_not_evaluated: ($r.laws?.required_not_evaluated? | default null)
      refused: (($r.laws?.results? | default []) | where {|x| ($x.verdict? | default "") == "refuse" } | each {|x| $x.id? | default ""})
      gaps: (law-gaps $r)
    }
    problems: $problems
  }
}

def emit [ok: bool, command: string, error: string, data: record] {
  {ok: $ok, command: $command, error: $error, data: $data} | to json
}

def main [
  --path: string = "."
  --max-tier: int = 1
  --base: string = ""
  --report: string = ".go-tops/report.json"
  --gotops: string = ""
] {
  let argv = (gotops-argv $path $gotops)
  let args = (["gate" "--max-tier" ($max_tier | into string)] | append (if $base != "" { ["--base" $base] } else { [] }))
  let command = (argv-line ($argv | append $args))
  let bin = ($argv | first)
  if not (have $bin) {
    return (emit false $command $"($bin): command not found" {exit: 127, problems: []})
  }
  let file = ($path | path join $report)
  rm -f $file
  let r = (do { cd $path; ^$bin ...($argv | skip 1) ...$args } | complete)
  let tail = (clip (if ($r.stderr | str trim | is-empty) { $r.stdout } else { $r.stderr }))
  if $r.exit_code not-in [0 1] {
    return (emit false $command $"gotops gate exited ($r.exit_code): ($tail)" {exit: $r.exit_code, problems: []})
  }
  let loaded = (read-report $file)
  if $loaded.state != "ok" {
    let why = if $loaded.state == "missing" { $"the gate wrote no ($report)" } else { $"($report) is not a JSON report" }
    return (emit false $command $why {exit: $r.exit_code, problems: [$why]})
  }
  let problems = (judge $r.exit_code $loaded.report)
  let ok = ($problems | is-empty)
  emit $ok $command (if $ok { "" } else { clip ($problems | str join "; ") }) (data-of $r.exit_code $loaded.report $problems $file)
}
