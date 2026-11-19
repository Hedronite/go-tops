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

**Outputs.** [`results.json`](results.json) holds every function (key, file, comp, cov, CRAP) and [`tables.md`](tables.md) holds the three tables below. [`crosscheck.md`](crosscheck.md) holds the cross-check.

**Cross-check.** On one repo (vigil) `run.sh` also runs `gotops gate` at caps 6, 8 and 10, reads the CRAP layer's `violations` from each report, and the test fails if the count differs from the scorer's count of functions over that cap. The gate's report is therefore agreeing with the table, not a second copy of it.

**Reproduce.** `./run.sh [output-dir]` needs git, go and network access to github.com. It uses the local toolchain only (`GOTOOLCHAIN=local`), keeps Go's caches inside the temporary directory, and removes everything on exit.

## Results

*Pending the first run.* The counts over caps 6, 8 and 10 against the probe's 192, 136 and 90 of 761 (guard-neutral row), each difference explained; the per-repo distributions; the five top scorers per repo; the cap statement.

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

*Pending.* gaze runs from `tools/go.mod` on a fixture module and the differences are explained in a note.
