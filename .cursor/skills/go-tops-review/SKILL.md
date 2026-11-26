---
name: go-tops-review
description: Review a Go diff with the Go-TOPS gate report. Use for a fresh review, including each fix commit. Lists each touched function's CRAP, complexity, coverage, ratchet state and mutation survivors, and the review rules no tool decides. Requires Nushell. Run the script.
---

# go-tops-review

Nushell (`nu`) is required. The reviewer did not write the diff. Run the script. Do not read it instead of reviewing.

```bash
nu go-tops-review.nu files [--base origin/main] [--path .] [--committed-only]
nu go-tops-review.nu diff [--base origin/main] [--path .] [--report .go-tops/report.json] [--committed-only]
nu go-tops-review.nu answers <file>
```

Run `go-tops-gate` first: `diff` reads its `.go-tops/report.json` and fails with `ok: false` when the report is missing, is not JSON, or is for another HEAD. The script gives no verdict. `ok` says the inputs were read, not that the change is good.

1. `files` lists the files changed since the fork point with the base, the Go sources among them, and whether the diff touches the protocol (`GO_TOPS.md`, `schema/`, `profiles/`, `laws/`, `config/AGENTS.template.md`) and so needs the `protocol-change` label.
2. `diff` lists each Go function the diff touches, in `data.functions`: `crap`, `complexity` and `coverage_pct` from the report, the `ledger` entry the fork point holds, and `state`: `new` (over the cap, no entry), `raised` (over its entry), `held`, `lowered` (under its entry, or paid and under the cap) or `under-cap`. The report names only functions over the cap, so `under-cap` has no numbers. Each function also lists its `survivors` from the report's mutation layer: `key`, `mutator`, `line`, `status` and `explained` (`ledger`, `exemption`, or empty when nothing explains it). `data.survivors_elsewhere` holds those in no listed function, such as a package-level initializer, and `data.mutation` the layer's `outcome` (`vacuous` when the diff changed no production line) and `error`. `data.attention` is the `new` and `raised` functions and those with an unexplained survivor. Read each against the diff: an unexplained survivor is a missing test or a ledger entry whose reason you must judge.
3. `data.judgment` lists the rules no tool decides, as `{id, rule}`. Answer every one against the diff.
4. Write the answers as TOML, then run `answers <file>`:

```toml
[answers]
"ORACLE" = { status = "ok", note = "" }
"SCAR" = { status = "finding", note = "internal/x/y.go:40 splits parse for its score only" }
```

`status` is `ok`, `finding` or `n/a`. A `finding` needs a note. `answers` gives `ok: false` for a missing or unknown ID. Report each `finding` as a review finding.

`data.report.ratchet` is the report's ratchet outcome. A `skip` is allowed only when the run's build differs from the ledger's build (ruling C3b); a `skip` on the ledger's own build is a failure, and `go-tops-gate` says so. CI's judge requires `pass` on the ledger leg; the skills do not know which leg they run on and allow a skip on another build.

By default the range is the fork point with `--base` plus the working tree: tracked edits and untracked files. `--committed-only` omits the working tree. Functions are found by the gofmt column-0 rule, so run gofmt first.

Each command prints JSON. `command` is the exact git argv, or the script's own invocation for `answers`. A missing `git` is `ok: false`.

If `nu` is not on PATH, there is no JSON. Treat `nu: command not found` as `{"ok":false,"command":"nu go-tops-review.nu","error":"nu: command not found"}`.
