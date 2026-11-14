# Agent standing orders (Go-TOPS 0.2.0, draft)

Goal: correct, dense, fast Go.
Production: the fewest lines that keep the named invariants and measured performance.
Tests: as many as the contract needs. Never write tests by reading the implementation.

The protocol text is [`GO_TOPS.md`](GO_TOPS.md). This file is the harness for work on this repo.
[`config/AGENTS.md.tmpl`](config/AGENTS.md.tmpl) is the file `gotops init` will write into an adopting repo. It is not the harness for this repo.

## State

Milestone M0: the spec. There is no Go code, no `go.mod`, no CLI and no CI yet. `go tool gotops` does not exist. Do not document, run or claim a command that has not landed.

Draft 0.2.0 still says otherwise on these points. They are revised in `GO_TOPS.md`, in a protocol-change PR, before M1 code starts:

1. The CRAP cap does not depend on who wrote the code. There is no authorship trailer.
2. `-race` needs cgo on Linux. Test jobs run with `CGO_ENABLED=1`; builds and releases use `CGO_ENABLED=0`.
3. An enabled layer with no command fails the gate. Only `enabled: false` in config turns a layer off.
4. The error-guard discount applies in every class, and only to guards that return the error. Blank discards (`_ =`, `x, _ :=`) are caught.
5. Adopters are gated by ratchet: no function gets worse, new functions meet the cap. This repo gates itself `hard`.
6. The Bend2 parity layer and Lean recheck are out until there is evidence they judge something the analyzers do not.

## Do this

1. Read the section of `GO_TOPS.md` you are changing, and the schema once it exists. Fix the schema first, then the prose (§3.4).
2. A change to `laws/`, `schema/`, `profiles/`, a threshold, or normative text in `GO_TOPS.md` is a protocol change. It carries the `protocol-change` label and a `CHANGELOG.md` entry, and bumps the protocol version together with the schema (§2, §13).
3. Once code lands: write the oracle from the contract first, then the smallest shape that satisfies it, then run the gate. Go-TOPS gates itself (§17). Nothing is released until its own gate is green at tier 1.

## Git

- Work on a branch and open a draft PR. Never push `main`, force-push, tag or publish a release.
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
