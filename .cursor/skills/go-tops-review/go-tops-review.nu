#!/usr/bin/env nu
# Reads a gate report and a diff for a reviewer. It runs no gate and gives no
# verdict: `ok` says the inputs were read, never that the change is good. For each
# Go function the diff touches it lists CRAP, complexity, coverage and the ratchet
# state against the ledger the base commit holds.

const NOISE = 0.01

# The review rules no tool decides (GO_TOPS.md sections 3.4 and 13, and the rules
# of config/AGENTS.template.md marked [review]). The reviewer answers each one.
const JUDGMENT = [
  {id: "ORACLE", rule: "Tests come from the contract, not the function body (13.1, G-28)."}
  {id: "SCAR", rule: "No function is split only to lower its CRAP score: look at a touched function whose state is lowered beside a new small one (13.2)."}
  {id: "WEAKEN", rule: "No law, linter setting, threshold or ledger entry is weakened to pass; a raised entry has a recorded regression and the protocol-change label (13.3)."}
  {id: "GENERATED", rule: "No generated file is edited; the generator is fixed (13.8)."}
  {id: "DELETE", rule: "Deleting code was preferred to adding configuration (13.9)."}
  {id: "RECORD", rule: "AGENTS.md and GO-TOPS.md still say what the config says (13.10, 13)."}
  {id: "SUPPRESS", rule: "Every //nolint names its linter and a reason; every //tops:ack has a reason and an owner (13.6)."}
  {id: "WRITE-CLOSE", rule: "The error from Close, Flush and Sync on a file written to is kept, not discarded."}
  {id: "GOROUTINES", rule: "No unbounded goroutine; concurrent work is bounded (errgroup with SetLimit)."}
  {id: "INTERFACES", rule: "Interfaces are small and live in the consumer; constructors return concrete types."}
  {id: "SYNC", rule: "Channels for ownership transfer, mutexes for shared state; no struct holding a lock is copied."}
  {id: "TESTS", rule: "Tests are table-driven where they repeat, clean up with t.Cleanup, and are deterministic: no sleep, no wall clock, no live network."}
]

const PROTOCOL_PATHS = ["GO_TOPS.md" "schema/" "profiles/" "laws/" "config/AGENTS.template.md"]

def have [bin: string] {
  not ((which $bin | where type == external) | is-empty)
}

def clip [text: string] {
  let t = ($text | str trim)
  if ($t | str length) > 400 { $t | str substring 0..<400 } else { $t }
}

def emit [ok: bool, command: string, error: string, data: record] {
  {ok: $ok, command: $command, error: $error, data: $data} | to json
}

def git-run [path: string, args: list<string>] {
  do { cd $path; ^git ...$args } | complete
}

def git-line [args: list<string>] {
  $"git ($args | str join ' ')"
}

# The fork point the diff is read from: the base and the head share it.
def fork-point [path: string, base: string] {
  let r = (git-run $path ["merge-base" $base "HEAD"])
  if $r.exit_code != 0 {
    return {ok: false, error: (clip $"git merge-base ($base) HEAD exited ($r.exit_code): ($r.stderr)")}
  }
  {ok: true, sha: ($r.stdout | str trim)}
}

# Files changed since the fork point: committed, and unless committed-only the
# working tree (tracked edits and untracked files).
def changed-files [path: string, sha: string, committed_only: bool] {
  let range = if $committed_only { [$sha "HEAD"] } else { [$sha] }
  let tracked = (git-run $path (["diff" "--name-status" "--no-renames"] | append $range))
  if $tracked.exit_code != 0 {
    return {ok: false, error: (clip $"git diff exited ($tracked.exit_code): ($tracked.stderr)")}
  }
  let rows = ($tracked.stdout | lines | where {|l| $l != "" } | each {|l|
    let p = ($l | split row "\t")
    {status: ($p | first), path: ($p | last)}
  })
  let fresh = if $committed_only { [] } else {
    let u = (git-run $path ["ls-files" "--others" "--exclude-standard"])
    $u.stdout | lines | where {|l| $l != "" } | each {|f| {status: "A", path: $f} }
  }
  {ok: true, files: ($rows | append $fresh)}
}

def is-go-source [p: string] {
  ($p | str ends-with ".go") and not ($p | str ends-with "_test.go")
}

def needs-label [files: list] {
  $files | where {|f| $PROTOCOL_PATHS | any {|p| $f.path == $p or (($p | str ends-with "/") and ($f.path | str starts-with $p)) } } | get path
}

# One hunk header "@@ -a,b +c,d @@" as the new side's first and last line. A hunk
# that only deletes (d = 0) touches the line it sits after.
def hunk-range [header: string] {
  let m = ($header | parse --regex '^@@ -\d+(?:,\d+)? \+(?<start>\d+)(?:,(?<len>\d+))? @@' | get 0?)
  if $m == null { return null }
  let start = ($m.start | into int)
  let len = if ($m.len? | default "") == "" { 1 } else { $m.len | into int }
  if $len == 0 { {first: $start, last: $start} } else { {first: $start, last: ($start + $len - 1)} }
}

# The touched line ranges of each Go source file, from a zero-context diff.
def touched-ranges [path: string, sha: string, committed_only: bool] {
  let range = if $committed_only { [$sha "HEAD"] } else { [$sha] }
  let r = (git-run $path (["diff" "-U0" "--no-color" "--no-renames"] | append $range | append ["--" "*.go"]))
  if $r.exit_code != 0 { return {ok: false, error: (clip $"git diff -U0 exited ($r.exit_code): ($r.stderr)")} }
  mut file = ""
  mut out = []
  for l in ($r.stdout | lines) {
    if ($l | str starts-with "+++ ") {
      $file = (if $l == "+++ /dev/null" { "" } else { $l | str substring 6.. })
    } else if ($l | str starts-with "@@ ") and $file != "" {
      let h = (hunk-range $l)
      if $h != null { $out = ($out | append {path: $file, first: $h.first, last: $h.last}) }
    }
  }
  {ok: true, ranges: $out}
}

# The top-level functions of a gofmt'd file: a function ends at the first closing
# brace in column 0, or on its own line when it is one line.
def funcs-of [lines: list<string>] {
  let starts = ($lines | enumerate | where {|x| $x.item | str starts-with "func " })
  $starts | each {|s|
    let sig = ($s.item | parse --regex '^func\s+(?:\(\s*(?:\w+\s+)?\*?(?<recv>\w+)(?:\[[^\]]*\])?\s*\)\s*)?(?<name>\w+)' | get 0?)
    if $sig == null { null } else {
      let one_line = ($s.item | str trim | str ends-with "}")
      let end = if $one_line { $s.index } else {
        let closing = ($lines | enumerate | skip $s.index | where {|x| $x.item | str starts-with "}" } | get 0?)
        if $closing == null { ($lines | length) - 1 } else { $closing.index }
      }
      {name: $sig.name, recv: ($sig.recv? | default ""), first: ($s.index + 1), last: ($end + 1)}
    }
  } | where {|f| $f != null }
}

def file-lines [path: string, file: string, committed_only: bool] {
  if $committed_only {
    let r = (git-run $path ["show" $"HEAD:($file)"])
    if $r.exit_code != 0 { [] } else { $r.stdout | lines }
  } else {
    let full = ($path | path join $file)
    if ($full | path exists) { open --raw $full | lines } else { [] }
  }
}

# The functions the diff touches, each with the key the report and ledger use.
def touched-functions [path: string, module: string, ranges: list, extra_files: list, committed_only: bool] {
  let whole = ($extra_files | each {|f| {path: $f, first: 1, last: 2147483647} })
  let all = ($ranges | append $whole)
  let files = ($all | get path | uniq)
  $files | each {|file|
    let mine = ($all | where path == $file)
    let dir = ($file | path dirname)
    let pkg = if $dir == "." or $dir == "" { $module } else { $"($module)/($dir)" }
    funcs-of (file-lines $path $file $committed_only) | where {|f|
      $mine | any {|h| $h.first <= $f.last and $h.last >= $f.first }
    } | each {|f|
      let base = if $f.recv == "" { $"($pkg).($f.name)" } else { $"($pkg).($f.recv).($f.name)" }
      {file: $file, name: $f.name, recv: $f.recv, first: $f.first, last: $f.last, key: $base}
    }
  } | flatten
}

# A report or ledger key is the base key, with "@file#n" when the name repeats.
def key-matches [key: string, base: string] {
  $key == $base or ($key | str starts-with $"($base)@")
}

def state-of [run: any, entry: any] {
  if $run == null and $entry == null { return "under-cap" }
  if $run == null { return "lowered" }
  if $entry == null { return "new" }
  if $run > ($entry + $NOISE) { "raised" } else if $run < ($entry - $NOISE) { "lowered" } else { "held" }
}

def function-row [f: record, violations: list, ledger: record] {
  let row = ($violations | where {|v| key-matches ($v.func? | default "") $f.key } | get 0?)
  let entry_key = ($ledger | columns | where {|k| key-matches $k $f.key } | get 0?)
  let entry = if $entry_key == null { null } else { $ledger | get $entry_key }
  let run = if $row == null { null } else { $row.crap? | default null }
  {
    file: $f.file
    function: (if $f.recv == "" { $f.name } else { $"($f.recv).($f.name)" })
    lines: [$f.first $f.last]
    crap: $run
    complexity: ($row.cc? | default null)
    coverage_pct: ($row.cov_pct? | default null)
    ledger: $entry
    state: (state-of $run $entry)
  }
}

# The report's mutation layer: what it did, and its surviving mutants with what
# explains each (a ledger entry, a mutation exemption, or nothing).
def mutation-of [rep: record] {
  let layer = ($rep.layers | where {|l| ($l.id? | default "") == "mutation" } | get 0?)
  let exempt = ($layer.details?.exempt? | default [])
  let survivors = ($layer.details?.survivors? | default [] | each {|s|
    let by = if ($s.key? | default "") in $exempt { "exemption" } else { $s.explained? | default "" }
    {key: ($s.key? | default ""), file: ($s.file? | default ""), function: ($s.function? | default ""), mutator: ($s.mutator? | default ""), line: $s.line?, status: ($s.status? | default ""), explained: $by}
  })
  {outcome: ($layer.outcome? | default ""), error: ($layer.details?.error? | default ""), survivors: $survivors}
}

def with-survivors [row: record, survivors: list] {
  let mine = ($survivors | where {|s| $s.file == $row.file and $s.function == $row.function } | each {|s| $s | reject file function })
  $row | insert survivors $mine
}

# The survivors in no listed function: a package-level initializer, for one.
def outside [survivors: list, rows: list] {
  $survivors | where {|s| not ($rows | any {|r| $r.file == $s.file and $r.function == $s.function }) }
}

def needs-reviewer [row: record] {
  $row.state in ["new" "raised"] or ($row.survivors | any {|s| $s.explained == "" })
}

# The ledger the fork point holds, or the working copy when the fork point has none.
def base-ledger [path: string, sha: string] {
  let r = (git-run $path ["show" $"($sha):.go-tops/baseline.json"])
  let work = ($path | path join ".go-tops" "baseline.json")
  let text = if $r.exit_code == 0 { $r.stdout } else if ($work | path exists) { open --raw $work } else { "" }
  let source = if $r.exit_code == 0 { "fork point" } else if $text != "" { "working tree" } else { "none" }
  let parsed = (try { $text | from json } catch { null })
  let fns = ($parsed.ledger?.functions? | default {})
  {source: $source, functions: $fns}
}

def read-report [file: string] {
  if not ($file | path exists) { return null }
  let parsed = (try { open --raw $file | from json } catch { null })
  if (($parsed | describe) | str starts-with "record") { $parsed } else { null }
}

def head-of [path: string] {
  (git-run $path ["rev-parse" "HEAD"]).stdout | str trim
}

# The report is for this head, or the functions listed would be another change's.
def report-problem [rep: any, head: string, report: string] {
  if $rep == null { return $"the report ($report) is missing or not a JSON report: run go-tops-gate first" }
  let rh = ($rep.git?.head? | default "")
  if $rh == "" { return "the report names no git head" }
  if not ($head | str starts-with $rh) and not ($rh | str starts-with $head) {
    return $"the report is for ($rh), HEAD is ($head): run go-tops-gate again"
  }
  if ($rep.layers? | default [] | where {|l| $l.id? == "crap" } | is-empty) {
    return "the report has no crap layer"
  }
  ""
}

def sub-files [path: string, base: string, committed_only: bool] {
  let fp = (fork-point $path $base)
  if not $fp.ok { return $fp }
  let cf = (changed-files $path $fp.sha $committed_only)
  if not $cf.ok { return $cf }
  {ok: true, sha: $fp.sha, files: $cf.files}
}

def "main files" [--base: string = "origin/main", --path: string = ".", --committed-only] {
  let cmd = (git-line ["diff" "--name-status" "--no-renames" $"($base)...HEAD"])
  if not (have "git") { return (emit false $cmd "git: command not found" {files: []}) }
  let r = (sub-files $path $base $committed_only)
  if not $r.ok { return (emit false $cmd $r.error {files: []}) }
  let label = (needs-label $r.files)
  emit true $cmd "" {
    base: $base
    fork_point: $r.sha
    files: $r.files
    go_sources: ($r.files | where {|f| is-go-source $f.path } | get path)
    protocol_change: {label: (not ($label | is-empty)), paths: $label}
  }
}

def "main diff" [
  --base: string = "origin/main"
  --path: string = "."
  --report: string = ".go-tops/report.json"
  --committed-only
] {
  let cmd = (git-line ["diff" "-U0" "--no-color" $"($base)...HEAD"])
  if not (have "git") { return (emit false $cmd "git: command not found" {functions: [], judgment: $JUDGMENT}) }
  let rep = (read-report ($path | path join $report))
  let head = (head-of $path)
  let bad = (report-problem $rep $head $report)
  if $bad != "" { return (emit false $cmd $bad {functions: [], judgment: $JUDGMENT}) }
  let fp = (sub-files $path $base $committed_only)
  if not $fp.ok { return (emit false $cmd $fp.error {functions: [], judgment: $JUDGMENT}) }
  let hunks = (touched-ranges $path $fp.sha $committed_only)
  if not $hunks.ok { return (emit false $cmd $hunks.error {functions: [], judgment: $JUDGMENT}) }
  let added = ($fp.files | where {|f| $f.status == "A" and (is-go-source $f.path) } | get path)
  let sources = ($hunks.ranges | where {|h| is-go-source $h.path })
  let fns = (touched-functions $path ($rep.module? | default "") $sources $added $committed_only)
  let ledger = (base-ledger $path $fp.sha)
  let violations = ($rep.layers | where id == "crap" | first | get details?.violations? | default [])
  let mutation = (mutation-of $rep)
  let rows = ($fns | each {|f| with-survivors (function-row $f $violations $ledger.functions) $mutation.survivors })
  emit true $cmd "" {
    base: $base
    fork_point: $fp.sha
    report: {
      path: $report
      head: ($rep.git?.head? | default "")
      dirty: ($rep.git?.dirty? | default null)
      enforce: ($rep.enforce? | default "")
      ratchet: ($rep.ratchet?.outcome? | default null)
      cap: ($rep.layers | where id == "crap" | first | get details?.cap? | default null)
      summary: ($rep.summary? | default null)
    }
    ledger_source: $ledger.source
    functions: $rows
    attention: ($rows | where {|r| needs-reviewer $r })
    mutation: {outcome: $mutation.outcome, error: $mutation.error}
    survivors_elsewhere: (outside $mutation.survivors $rows)
    note: "CRAP, complexity and coverage are listed only for functions over the cap: the report names no others. under-cap means the function is not in the report."
    judgment: $JUDGMENT
  }
}

def answer-problems [answers: any] {
  if not (($answers | describe) | str starts-with "record") { return ["the file has no [answers] table"] }
  let ids = ($JUDGMENT | get id)
  let unknown = ($answers | columns | where {|k| $k not-in $ids } | each {|k| $"unknown rule ($k)" })
  let missing = ($ids | where {|i| $i not-in ($answers | columns) } | each {|i| $"no answer for ($i)" })
  let bad = ($answers | columns | where {|k| $k in $ids } | each {|k|
    let a = ($answers | get $k)
    let status = ($a.status? | default "")
    let note = ($a.note? | default "")
    if $status not-in ["ok" "finding" "n/a"] { $"($k): status must be ok, finding or n/a" } else if $status == "finding" and ($note | str trim | is-empty) { $"($k): a finding needs a note" } else { "" }
  } | where {|p| $p != "" })
  $unknown | append $missing | append $bad
}

def "main answers" [file: string] {
  let cmd = $"nu go-tops-review.nu answers ($file)"
  if not ($file | path exists) { return (emit false $cmd $"($file) does not exist" {problems: []}) }
  let parsed = (try { open $file } catch { null })
  let answers = ($parsed.answers? | default null)
  let problems = (answer-problems $answers)
  if not ($problems | is-empty) { return (emit false $cmd (clip ($problems | str join "; ")) {problems: $problems}) }
  let findings = ($answers | columns | each {|k| {id: $k, status: ($answers | get $k | get status), note: ($answers | get $k | get note? | default "")} } | where status == "finding")
  emit true $cmd "" {problems: [], findings: $findings}
}

def main [] {
  emit false "nu go-tops-review.nu" "name a command: files, diff or answers" {judgment: $JUDGMENT}
}
