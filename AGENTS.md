# Agent standing orders (Go-TOPS 0.4.5)

Goal: correct, dense, fast Go.
Production: the fewest lines that keep the named invariants and measured performance.
Tests: as many as the contract needs. Never write tests by reading the implementation.

The protocol text is [`GO_TOPS.md`](GO_TOPS.md). This file is the harness for work on this repo.
[`config/AGENTS.template.md`](config/AGENTS.template.md) is the file `gotops init` will write into an adopting repo. It is not the harness for this repo.

## State

Protocol 0.4.5; M4 done; next M5. The Go code is the docs checker in `scripts/docs/` (its own `go.mod`) and the `gotops` CLI in the root module (`cmd/gotops/`, `internal/`): `version`, `check`, `init`, `gate`, `report`, `lock`, `doctor`, `baseline` and `survivors`. `gate` runs hygiene, tests, coverage, CRAP and diff-scoped mutation at tier 1 and, under `enforce: ratchet`, the ratchet judge against `.go-tops/baseline.json`; the laws layer is a stub until M5. Mutation is on in `go-tops.yaml`, so the kit gates its own diffs: a mutant that survives on a changed line fails. Kill it with a test first; `gotops survivors explain` records one that no test can tell from the original, or one whose test is owed. CI is docs CI (`.github/workflows/docs.yml`) and `.github/workflows/ci.yml`: its `gate` job runs `go run ./cmd/gotops gate` under `enforce: advisory` and then asserts the report (GO_TOPS.md §6.7), with hygiene, tests, coverage and CRAP all `pass` and mutation `pass` or `vacuous`; the `skills`, `bootstrap` and `mutation` jobs run the skills, the first-ledger route and the weak-test fixture; and a weekly `adoption` job runs the end-to-end adoption test and the skills against the real `gotops`. Every kit function meets the CRAP cap of 10 except the two exemptions in `go-tops.yaml`, which expire on 2027-03-31. Do not document, run or claim a command or layer that has not landed (`laws`).

Points from 0.3.0 that code must follow (see the change note at the top of `GO_TOPS.md`):

1. One CRAP cap per class, for every author. There is no authorship trailer.
2. Test jobs run with `CGO_ENABLED=1`, because `-race` needs cgo on Linux. Builds and releases use `CGO_ENABLED=0`.
3. An enabled layer with no command is `not-run` and fails the gate. Only `enabled: false` in config turns a layer off.
4. The error-guard discount applies in every class, and only to guards that return the error. Blank discards (`_ =`, `x, _ :=`) are caught by errcheck `check-blank`.
5. Adopters are gated by `ratchet` against `.go-tops/baseline.json`. This repo gates itself `advisory` until the laws layer lands at M5 (§6.7, protocol 0.4.1): its CI asserts that hygiene, tests and coverage pass in the report, and `go-tops.yaml` switches to `hard` at M5.
6. There is no Bend2 or Lean layer. The `go/analysis` analyzers are the laws layer.

## Do this

1. Read the section of `GO_TOPS.md` you are changing, and the schema once it exists. Fix the schema first, then the prose (§3.4).
2. A change to `laws/`, `schema/`, `profiles/`, a threshold, or normative text in `GO_TOPS.md` is a protocol change. It carries the `protocol-change` label and a `CHANGELOG.md` entry, and bumps the protocol version together with the schema (§2, §13).
3. Once code lands: write the oracle from the contract first, then the smallest shape that satisfies it, then run the gate. Go-TOPS gates itself (§17). Nothing is released until its own gate is green at tier 1.

## Git

- Work on a branch and open a PR ready for review when the step is complete. Never push `main`, force-push, tag or publish a release.
- Fixes are new commits on the PR branch.
- No secrets, tokens, local paths or internal notes in the repo.

## Forbidden

- Writing tests from the current function body.
- Splitting a function only to lower its CRAP score (scar-guard).
- Weakening a law, linter setting or threshold so a check passes.
- Counting `not-evaluated`, an unmapped layer, or an empty report as a pass.
- `//nolint` without the linter name and a reason.
- Discarding an error with `_` outside an allowlist with a reason.
- `context.Background()` or `context.TODO()` outside `main`, wiring and tests.
- `time.Sleep` in tests to wait on concurrency. Use `testing/synctest`.
- Editing generated files. Fix the generator.
- Adding a dependency that does not delete code or buy measured safety or performance.
