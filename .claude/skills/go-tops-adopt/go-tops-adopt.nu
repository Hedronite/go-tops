#!/usr/bin/env nu
# Wraps `gotops init`, `gotops baseline --init` and `gotops check`. Does not
# re-implement them. `ok` is true only when all three exit 0 and the ledger was
# written on the ledger leg's build: a ledger from any other build is never
# committed (GO_TOPS.md section 6.7, ruling C3), so it is not an adoption.

# The build every ledger leg of the drop-in runs: the ubuntu runner, cgo on for the
# race detector, no tags.
const LEDGER_LEG = {goos: "linux", goarch: "amd64", tags: [], cgo_enabled: true}

const CLASSES = ["binary-cli" "binary-service" "library-core"]

def have [bin: string] {
  not ((which $bin | where type == external) | is-empty)
}

def clip [text: string] {
  let t = ($text | str trim)
  if ($t | str length) > 400 { $t | str substring 0..<400 } else { $t }
}

def quote-arg [a: string] {
  if ($a | str contains " ") { $"\"($a)\"" } else { $a }
}

def argv-line [args: list<string>] {
  $args | each {|a| quote-arg $a } | str join " "
}

# The gotops command: --gotops, else $GOTOPS, else the adopter's pinned tool
# (tools/go.mod), else gotops on PATH. A fresh module has no tools/go.mod yet.
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

def emit [ok: bool, command: string, error: string, data: record] {
  {ok: $ok, command: $command, error: $error, data: $data} | to json
}

# One step: the exact argv, its exit code and the tail of its output.
def run-step [path: string, argv: list<string>, args: list<string>] {
  let full = ($argv | append $args)
  let r = (do { cd $path; ^($argv | first) ...($argv | skip 1) ...$args } | complete)
  let out = (if ($r.stderr | str trim | is-empty) { $r.stdout } else { $r.stderr })
  {command: (argv-line $full), exit: $r.exit_code, tail: (clip $out)}
}

# The ledger as written: its build, and whether that is the ledger leg's build.
def ledger-info [path: string] {
  let file = ($path | path join ".go-tops" "baseline.json")
  if not ($file | path exists) { return {path: $file, written: false} }
  let parsed = (try { open --raw $file | from json } catch { null })
  let build = ($parsed.build? | default null)
  if $build == null {
    return {path: $file, written: true, build: null, on_ledger_leg_build: false}
  }
  let same = (
    ($build.goos? | default "") == $LEDGER_LEG.goos
    and ($build.goarch? | default "") == $LEDGER_LEG.goarch
    and ($build.cgo_enabled? | default null) == $LEDGER_LEG.cgo_enabled
    and (($build.tags? | default []) | is-empty)
  )
  {path: $file, written: true, build: $build, on_ledger_leg_build: $same}
}

def ledger-problem [ledger: record, allow_host: bool] {
  if $allow_host or ($ledger.on_ledger_leg_build? | default false) { return "" }
  let b = ($ledger.build? | default null)
  let host = if $b == null { "an unknown build" } else { $"($b.goos)/($b.goarch), cgo ($b.cgo_enabled)" }
  $"the ledger was written on ($host), not the ledger leg's linux/amd64 with cgo: do not commit it. Run the drop-in workflow by hand with mode init on its ubuntu leg and commit that file. Pass --allow-host-ledger for a dry run that commits nothing."
}

# The first check of an adoption. Under ratchet `check` reads only a ledger that is
# committed (GO_TOPS.md section 6.7), and nothing is committed yet: the only issues
# it may name are the baseline's. Those are the commit still to make, not a failure.
def commit-only [r: record] {
  let issues = ($r.tail | lines | where {|l| $l | str trim | is-not-empty })
  ($r.exit == 2) and not ($issues | is-empty) and ($issues | all {|l| ($l | str trim | str ends-with "(baseline)") and (($l | str contains "tracked by git") or ($l | str contains "differs from HEAD")) })
}

# The error of the check step, and whether only the commit is left.
def check-verdict [checked: record] {
  if $checked.exit == 0 { return {failed: "", commit_required: false} }
  if (commit-only $checked) { return {failed: "", commit_required: true} }
  {failed: $"($checked.command) exited ($checked.exit): ($checked.tail)", commit_required: false}
}

const NEXT = "commit go-tops.yaml, go-tops.lock, tools/, .golangci.yml, the workflow and .go-tops/baseline.json together, then run gotops check again: it must exit 0"

def "main check" [--path: string = ".", --gotops: string = ""] {
  let argv = (gotops-argv $path $gotops)
  let command = (argv-line ($argv | append ["check"]))
  if not (have ($argv | first)) { return (emit false $command $"($argv | first): command not found" {steps: []}) }
  let s = (run-step $path $argv ["check"])
  let error = if $s.exit == 0 { "" } else { $"($s.command) exited ($s.exit): ($s.tail)" }
  emit ($error == "") $command $error {steps: [$s]}
}

def "main adopt" [
  --class: string = ""  # binary-cli, binary-service or library-core
  --path: string = "."
  --owner: string = ""
  --force
  --offline
  --allow-host-ledger  # a dry run: accept a ledger from this host's build, never to be committed
  --gotops: string = ""
] {
  let argv = (gotops-argv $path $gotops)
  let init_args = (["init" "--class" $class] | append (if $owner != "" { ["--owner" $owner] } else { [] }) | append (if $force { ["--force"] } else { [] }) | append (if $offline { ["--offline"] } else { [] }))
  let plan = [$init_args ["baseline" "--init"] ["check"]]
  let command = ($plan | each {|a| argv-line ($argv | append $a) } | str join " && ")
  if $class not-in $CLASSES {
    return (emit false $command $"--class must be one of ($CLASSES | str join ', ')" {steps: []})
  }
  let bin = ($argv | first)
  if not (have $bin) {
    return (emit false $command $"($bin): command not found" {steps: []})
  }
  # Stop at the first step that fails: a later step reads what it did not write.
  let made = (run-step $path $argv $init_args)
  let based = if $made.exit == 0 { run-step $path $argv ["baseline" "--init"] } else { null }
  let steps = ([$made, $based] | where {|s| $s != null })
  let ledger = (ledger-info $path)
  let early = if $made.exit != 0 { $"($made.command) exited ($made.exit): ($made.tail)" } else if $based.exit != 0 { $"($based.command) exited ($based.exit): ($based.tail)" } else { ledger-problem $ledger $allow_host_ledger }
  if $early != "" { return (emit false $command $early {steps: $steps, ledger: $ledger, commit_ledger: false}) }
  let checked = (run-step $path $argv ["check"])
  let verdict = (check-verdict $checked)
  let data = {steps: ($steps | append $checked), ledger: $ledger, commit_ledger: ($ledger.on_ledger_leg_build? | default false), commit_required: $verdict.commit_required}
  emit ($verdict.failed == "") $command $verdict.failed (if $verdict.commit_required { $data | insert next $NEXT } else { $data })
}

def main [] {
  emit false "nu go-tops-adopt.nu" "name a command: adopt or check" {steps: []}
}
