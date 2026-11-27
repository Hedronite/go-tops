---
name: go-tops-gate
description: Run gotops gate and report each layer and law under ruling C9. Use when checking or fixing a Go-TOPS gate. not-evaluated is not a pass. Requires Nushell. Run the script.
---

# go-tops-gate

Nushell (`nu`) is required. Run this script. Do not read it to grade the gate, and do not trust a `report.json` left from an earlier run: the script deletes it before the run.

```bash
nu go-tops-gate.nu [--path .] [--max-tier 1] [--base <ref>] [--report .go-tops/report.json] [--gotops "<argv>"]
```

It prints one JSON object. `command` is the exact `gotops gate` argv. `ok` is judged from the report and the exit code, never from the exit code alone, under ruling C9: before M5 judges the required laws, exit 1 on them alone is a pass; once `laws.judged` is non-zero, the gate must pass with every required law judged and none refused. `ok` is true only when:

- the exit code is 0 or 1 (2, 3 and 4 are errors, not verdicts) and is the `summary.exit_code` the report records;
- the report exists, is JSON and has its layers, laws, summary and enforce fields;
- every layer is `pass` or `skip`, or for `mutation` alone `vacuous` (the diff changed no production line), and the run was not partial;
- the mutation layer lists no unexplained survivor, counts none when it is `pass`, and is off (`skip`) only until a day after today (UTC);
- the only failed item, if any, is `laws`, with every required law reported not evaluated, none judged and none refused;
- under `enforce: ratchet` the ratchet is `pass`, or `skip` on a build that is not the ledger's, never `fail`.

A partial run, and an advisory run with a failing layer, are `ok: false` although the gate exits 0: the report is read, not the exit code alone.

A ratchet `skip` is judged by build (ruling C3b): it is allowed only when the run's build differs from the ledger's build, which the report names in `ratchet.run_build` and `ratchet.ledger_build`. A `skip` on the ledger's own build fails: the ratchet judged nothing where it must. A `skip` elsewhere says CI judges the ratchet on the ledger leg. The script does not know which leg it runs on: CI's judge requires `pass` on the ledger leg, which a skip on another build never is.

A law that is `not-evaluated` never turns a failure into a pass. A law that is not required and not evaluated does not fail the laws layer, so it does not make `ok` false: it is listed in `data.laws.gaps`.

`data.mutation` has the layer's outcome, the counts (`mutants`, `killed`, `survived_explained`, `survived_unexplained`, `dropped`), `timed_out_keys`, `exempt` and `unexplained`: each survivor nothing explains, with its `key`, `line` and `status`. Kill it with a test, or accept it with `gotops survivors explain <key>` and say why. A `vacuous` layer judged nothing and is not a failure.

`data` has `mutation`, `layers`, `ratchet`, `summary`, the `laws` counts and `laws.gaps` (every law that is not allowed, with its reason), and `problems`, each reason `ok` is false. A `not-evaluated` law is a gap, never a pass beyond the required laws C9 names.

`gotops` is found from `--gotops`, then `$GOTOPS`, then the module's `tools/go.mod` (`go tool -modfile=tools/go.mod gotops`), then PATH. A missing `gotops`, a missing report or a report that is not JSON is `ok: false`.

If `nu` is not on PATH, there is no JSON. Treat `nu: command not found` as `{"ok":false,"command":"nu go-tops-gate.nu","error":"nu: command not found"}`.
