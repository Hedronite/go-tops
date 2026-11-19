# Calibration record: native CRAP over the five repos (2026-11)

The 2026-11 probe ([`../2026-11-probe/`](../2026-11-probe/README.md)) measured CRAP with a syntactic stand-in. This record measures it again with the native code that `gotops gate` runs (GO_TOPS.md §6.2, §6.3), over the same five repos at the same commits, and compares the two. It is the evidence for the cap check of M2 (G5).

> **Draft.** The scorer behind `scoreModule` lands with G1. The sections marked *pending* are filled from the first real run, and this notice goes with them.

## What was measured

| Repo | Pinned commit |
|---|---|
| augur | `fa72aa71aef2e86a4ece01d5188ecc50aced7b55` |
| simplex | `873de8602a74b95bfe38af6d33a9d31594df5726` |
| vigil | `4a71f03ff8e52736963731b2dd088db643c13e03` |
| cubit | `7f6a776932e816842b6d60c949560d6084caebfe` |
| scry | `a33d4de203f6fc06f60982736947ec2e0c9099f5` |

The five are public Hedronite repos. Each is cloned shallow at its commit in a temporary directory; nothing is written to any repo.

## Method

**Coverage.** `go test -count=1 -coverprofile=<repo>.cover -coverpkg=./... ./...` in each clone: the same run as the tests layer (`.go-tops/cover.out`, whole-module statement coverage). A red suite stops the run.

**Scoring.** [`calibrate_test.go`](calibrate_test.go), built only with `-tags calibration`, calls the native scorer for each repo and writes every production function with its complexity, guard-neutral coverage and CRAP. The scorer is the one the CRAP layer uses, with its default knobs (`err_guard_discount: true`, no `wrap_functions`), so the total, the distribution and the counts over each cap are the layer's own, package-level function literals included. A `_test.go` file is outside CRAP scope, so the calibration adds no function to the kit's own score.

**Outputs.** [`results.json`](results.json) holds every function (key, file, comp, cov, CRAP) and [`tables.md`](tables.md) holds the four tables of the record: counts over each cap against the probe, the functions over each cap by complexity alone, the CRAP histogram and the five top scorers per repo. [`crosscheck.md`](crosscheck.md) holds the gate cross-check. [`gaze.json`](gaze.json) holds gaze's figures on the fixture (below).

**Cross-check.** On one repo (vigil) `run.sh` also runs `gotops gate` at caps 6, 8 and 10, reads the CRAP layer's `violations` from each report, and the test fails if the count differs from the scorer's count of functions over that cap. The gate's report is therefore agreeing with the table, not a second copy of it.

**Reproduce.** `./run.sh [output-dir]` needs git, go and network access to github.com. It uses the local toolchain only (`GOTOOLCHAIN=local`), keeps Go's caches inside the temporary directory, and removes everything on exit.

## Results

*Pending the first run.* The counts over caps 6, 8 and 10 against the probe's 192, 136 and 90 of 761 (guard-neutral row), each difference explained; the per-repo distributions; the five top scorers per repo; the cap statement.

## Cap statement

*Pending the first run.* The caps of §5.2 are `binary-cli` 10, `binary-service` 10 and `library-core` 8, the last provisional until this record repeats the probe with the native code. The probe's shares were 25% of functions over 6 (192 of 761), 18% over 8 (136) and 12% over 10 (90), with 67 of the 90 over 10 by complexity alone.

**Criterion, fixed before the run.** A cap moves under X2 only if the share of functions over that cap, counted natively, differs from the probe's by more than five percentage points *after* the differences listed below are accounted for. Otherwise the caps stand and this section says so with the numbers. A move is a separate `protocol-change` proposal to PM, never an edit in this record's PR.

Fill in one of these two forms from `tables.md`, and delete the other:

- **Agreement.** The native code finds *N* of *M* functions over cap 6, *A* over 8 and *B* over 10 (probe: 192, 136 and 90 of 761); *C* of the *B* are over 10 by complexity alone (probe: 67). Each difference is explained below. The caps stand, and the `library-core` cap of 8 is supported by *…*.
- **Proposal.** At cap *X* the native share is *p*% against the probe's *q*%, which is above the criterion after the explained differences. Proposed: *cap Y for class Z*, because *…*. Filed to PM as a `protocol-change` proposal.

## Where the native counts can differ from the probe

To be confirmed against the run, one by one:

- **Typed guards.** The probe called an `if x != nil { return … }` a guard by its shape; the native count requires `x` to have type `error` and the return to carry it, wrapped with `%w` or by a `wrap_functions` entry (§6.3).
- **Package-level function literals.** The probe skipped them; the native layer scores each on its own, so the function total can exceed 761.
- **Functions with no statements left** score `cov = 1` and a function with no profile block `cov = 0` (§6.2); the probe's rule for these differs in edge cases.
- **Identity and scope.** Test files, generated files and `testdata/` are out in both; the native scope also counts every `init`.

## Limits

- One platform and one set of build tags; no `-race`.
- Statement coverage from one `go test` run, not merged across platforms.
- Five repos written in the same month: the adopters this protocol is for, not a sample of Go in general.

## gaze cross-check

[gaze](https://github.com/unbound-force/gaze) `v1.9.0` is pinned in `tools/go.mod` (never in the root `go.mod`). [`gaze.sh`](gaze.sh) runs `gaze crap` on [`testdata/gaze-crosscheck/`](../../testdata/gaze-crosscheck), a small module with one function for each place the two can differ, using a whole-module coverage profile like the gate's, and writes [`gaze.json`](gaze.json). gaze takes its complexity from gocyclo, so it counts every error guard, and it maps coverage to function declarations.

Per function: gaze's figures as measured, and what GO_TOPS.md §6.2 and §6.3 give for the same profile, worked by hand. The native column is filled from the first run of the real scorer, and a row where it differs from the contract column is a finding for G1.

| Function | gaze comp | gaze cov | gaze CRAP | Contract comp | Contract cov | Contract CRAP | Native (G1) | Why they differ |
|---|---|---|---|---|---|---|---|---|
| `Parse` | 4 | 66.7% | 4.593 | 2 | 85.7% (6 of 7) | 2.012 | *pending* | both error guards qualify (a bare return of `err`, and `%w`), so they weigh 0 and their bodies leave coverage |
| `Wrapv` | 2 | 75.0% | 2.062 | 2 | 75.0% | 2.062 | *pending* | none: `%v` does not wrap, so the guard counts |
| `Classify` | 3 | 40.0% | 4.944 | 3 | 40.0% | 4.944 | *pending* | none: `errors.Is` and a return of something else are decisions |
| `Dispatch` | 5 | 50.0% | 8.125 | 5 | 50.0% | 8.125 | *pending* | none: the `default` is not counted by either |
| `Sum` | 4 | 87.5% | 4.031 | 4 | 87.5% | 4.031 | *pending* | none: the closure counts inside `Sum` in both |
| `Double` (package-level literal) | 2 | 0.0% | 6.000 | 2 | 66.7% (2 of 3) | 2.148 | *pending* | gaze lists the literal but gives it no coverage, though `TestDouble` runs it (the profile holds three blocks for it, two hit) |
| `Max` | 2 | 66.7% | 2.148 | 2 | 66.7% | 2.148 | *pending* | none |
| `(*Stack).Push` | 1 | 100.0% | 1.000 | 1 | 100.0% | 1.000 | *pending* | none |
| `(*Stack).Pop` | 2 | 83.3% | 2.019 | 2 | 83.3% | 2.019 | *pending* | none |
| `Noop` | 1 | 0.0% | 2.000 | 1 | 100.0% (no statements) | 1.000 | *pending* | §6.2: a function with no statements has `cov = 1`; gaze reads its empty block as uncovered |
| `Untested` | 2 | 0.0% | 6.000 | 2 | 0.0% | 6.000 | *pending* | none |
| `init` (first) | 1 | 100.0% | 1.000 | 1 | 100.0% | 1.000 | *pending* | none; the native identity is `init#1` |
| `init` (second) | 2 | 66.7% | 2.148 | 2 | 66.7% | 2.148 | *pending* | none; the native identity is `init#2` |

gaze lists 13 functions and the contract has the same 13. Three rows differ, each for one reason: the error-guard discount (§6.3), a package-level literal's coverage, and a function with no statements. On the other ten rows gaze, the contract and (once measured) the native code should agree to three decimals.
