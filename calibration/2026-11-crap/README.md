# Calibration record: native CRAP over the five repos (2026-11)

The 2026-11 probe ([`../2026-11-probe/`](../2026-11-probe/README.md)) measured CRAP with a syntactic stand-in. This record measures it again with the native code that `gotops gate` runs (GO_TOPS.md §6.2, §6.3), over the same five repos at the same commits, and compares the two. It is the evidence for the cap check of M2 (G5).

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

**Scoring.** [`calibrate_test.go`](calibrate_test.go), built only with `-tags calibration`, calls the native scorer for each repo and writes every production function with its complexity, guard-neutral coverage and CRAP. The scorer is the one the CRAP layer uses (`internal/crap`, under the build the tests layer runs, with its default knobs: `err_guard_discount: true`, no `wrap_functions`), so the total, the distribution and the counts over each cap are the layer's own, package-level function literals included. A function in a file the run's build does not compile is not scored; it is listed with its complexity and counted over a cap by complexity alone, as the layer judges it (§6.2). A `_test.go` file is outside CRAP scope, so the calibration adds no function to the kit's own score.

**Outputs.** [`results.json`](results.json) holds every function (key, file, comp, cov, CRAP) and [`tables.md`](tables.md) holds the four tables of the record: counts over each cap against the probe, the functions over each cap by complexity alone, the CRAP histogram and the five top scorers per repo. [`crosscheck.md`](crosscheck.md) holds the gate cross-check. [`gaze.json`](gaze.json) holds gaze's figures on the fixture (below).

**Cross-check.** On one repo (vigil) `run.sh` also runs `gotops gate` at caps 6, 8 and 10, reads the CRAP layer's `violations` from each report, and the test fails if the count differs from the scorer's count of functions over that cap. The gate's report is therefore agreeing with the table, not a second copy of it.

**Per-function comparison with the probe.** [`probe-diff/run.sh`](probe-diff/run.sh) runs a patched copy of the probe that dumps its figures for every function, and [`probe-diff/diff.py`](probe-diff/diff.py) joins them with `results.json` by file and function name. Its output for this record is [`probe-diff/diff.txt`](probe-diff/diff.txt). The committed probe is not changed.

**Reproduce.** `./run.sh [output-dir]` needs git, go and network access to github.com. It uses the local toolchain only (`GOTOOLCHAIN=local`), keeps Go's caches inside the temporary directory, and removes everything on exit.

## Results

Run on go1.27.4, darwin/arm64, cgo on and the race tag, as the tests layer runs. All five suites are green.

**Functions and functions over each cap, all five repos** (per repo: [`tables.md`](tables.md)). The probe columns are its guard-neutral row, the one §5.2 cites.

| | Native | Probe | Δ |
|---|---|---|---|
| Production functions | 765 | 761 | +4 |
| Over cap 6 | 197 (25.8%) | 192 (25.2%) | +5 |
| Over cap 8 | 140 (18.3%) | 136 (17.9%) | +4 |
| Over cap 10 | 92 (12.0%) | 90 (11.8%) | +2 |
| Over cap 6 by complexity alone | 161 | 156 | +5 |
| Over cap 8 by complexity alone | 110 | 106 | +4 |
| Over cap 10 by complexity alone | 70 | 67 | +3 |
| Not measured on this build | 0 | n/a | |

**Distribution of CRAP**, all five repos, in bins up to and including each edge: 463 functions at 4 or less, 105 above 4 and up to 6, 57 up to 8, 48 up to 10, 48 up to 15, 34 up to 30 and 10 above 30. The per-repo rows and the five top scorers of each repo are in [`tables.md`](tables.md).

**Cross-check with the gate.** On vigil, `gotops gate` reports 14, 10 and 8 CRAP violations at caps 6, 8 and 10, and the scorer finds 14, 10 and 8 functions over those caps ([`crosscheck.md`](crosscheck.md)). The layer and the table are one count.

**No function is not measured here.** None of the five repos has a production file outside this build (no other-platform file, no tagged file), so the build rule of §6.2 changed no figure of this record.

## Differences from the probe, explained

[`probe-diff/diff.txt`](probe-diff/diff.txt) joins the two lists function by function. There are two kinds of difference and no others.

**Four functions only the native code has.** They are package-level function literals: cubit `resolver` and `fsyncFile`, scry `newObserver` and `syncDir`. §6.3 scores a package-level literal on its own, and the probe skipped them (its limits say so). They score 6.0, 1.0, 1.0 and 2.1, so none is over cap 6, and they account for the +4 in the function count and for none of the counts over a cap.

**31 functions with a higher complexity, by 1 to 3** (augur 7, simplex 8, vigil 7, cubit 2, scry 7). In every one the native complexity lies between the probe's discounted figure and the raw gocyclo count: the native code discounts a subset of the guards the probe discounted. The probe called any `if err != nil` whose body returns a call that takes the error a guard. §6.3 discounts only a bare `return err`, an `fmt.Errorf` with a literal format that wraps the error with `%w`, or a call to a function listed in `complexity.wrap_functions`, and the list is empty here. Vigil's `config.Load` shows it: its three guards each return `usageErr(path, "", "read config: %v", err)`, a helper that is not listed, so natively they count (comp 3) and the probe did not (comp 1).

**Every difference in the counts follows from those 31.** Of them, 5 cross cap 6, 4 cross cap 8 and 2 cross cap 10, which is exactly the +5, +4 and +2 over the caps; no function changes side because of coverage. By complexity alone 5, 4 and 3 cross caps 6, 8 and 10, the +5, +4 and +3 of the table. The functions are listed by cap in `probe-diff/diff.txt`.

## Cap statement

**Agreement.** The native code finds 197 of 765 functions over cap 6, 140 over 8 and 92 over 10 (probe: 192, 136 and 90 of 761); 70 of the 92 over 10 are over it by complexity alone (probe: 67 of 90). The shares differ from the probe's by 0.5, 0.4 and 0.2 percentage points, against a criterion fixed before the run of 5, and every difference is explained above. The caps of §5.2 stand. In particular the measurement the `library-core` cap of 8 waited on is repeated and agrees: 18.3% of functions over 8 natively against 17.9%. Whether §5.2 still calls that cap provisional is a protocol-change question for PM, not an edit in this record.

The criterion, for the record: a cap moves under X2 only if the native share of functions over it differs from the probe's by more than five percentage points after the differences listed above are accounted for.

## Limits

- One platform and one set of build tags (the race tag is on, as the gate runs). The not-measured count is for this platform only.
- Statement coverage from one `go test` run, not merged across platforms.
- Five repos written in the same month: the adopters this protocol is for, not a sample of Go in general.

## gaze cross-check

[gaze](https://github.com/unbound-force/gaze) `v1.9.0` is pinned in `tools/go.mod` (never in the root `go.mod`). [`gaze.sh`](gaze.sh) runs `gaze crap` on [`testdata/gaze-crosscheck/`](../../testdata/gaze-crosscheck), a small module with one function for each place the two can differ, using a whole-module coverage profile like the gate's, and writes [`gaze.json`](gaze.json). gaze takes its complexity from gocyclo, so it counts every error guard, and it maps coverage to function declarations.

Per function: gaze's figures as measured, what GO_TOPS.md §6.2 and §6.3 give for the same profile, worked by hand, and the native scorer's figures. [`gaze_test.go`](gaze_test.go) (build tag `calibration`) scores the fixture with the native code and fails unless each function's complexity equals the contract's and its CRAP is within a millionth of it, so the last two columns agree by construction of the test, and the test is the check that G1 meets the contract on this fixture.

| Function | gaze comp | gaze cov | gaze CRAP | Contract comp | Contract cov | Contract CRAP | Native: comp · cov · CRAP | Why they differ |
|---|---|---|---|---|---|---|---|---|
| `Parse` | 4 | 66.7% | 4.593 | 2 | 85.7% (6 of 7) | 2.012 | 2 · 85.7% · 2.012 | both error guards qualify (a bare return of `err`, and `%w`), so they weigh 0 and their bodies leave coverage |
| `Wrapv` | 2 | 75.0% | 2.062 | 2 | 75.0% | 2.062 | 2 · 75.0% · 2.062 | none: `%v` does not wrap, so the guard counts |
| `Classify` | 3 | 40.0% | 4.944 | 3 | 40.0% | 4.944 | 3 · 40.0% · 4.944 | none: `errors.Is` and a return of something else are decisions |
| `Dispatch` | 5 | 50.0% | 8.125 | 5 | 50.0% | 8.125 | 5 · 50.0% · 8.125 | none: the `default` is not counted by either |
| `Sum` | 4 | 87.5% | 4.031 | 4 | 87.5% | 4.031 | 4 · 87.5% · 4.031 | none: the closure counts inside `Sum` in both |
| `Double` (package-level literal) | 2 | 0.0% | 6.000 | 2 | 66.7% (2 of 3) | 2.148 | 2 · 66.7% · 2.148 | gaze lists the literal but gives it no coverage, though `TestDouble` runs it (the profile holds three blocks for it, two hit) |
| `Max` | 2 | 66.7% | 2.148 | 2 | 66.7% | 2.148 | 2 · 66.7% · 2.148 | none |
| `(*Stack).Push` | 1 | 100.0% | 1.000 | 1 | 100.0% | 1.000 | 1 · 100.0% · 1.000 | none |
| `(*Stack).Pop` | 2 | 83.3% | 2.019 | 2 | 83.3% | 2.019 | 2 · 83.3% · 2.019 | none |
| `Noop` | 1 | 0.0% | 2.000 | 1 | 100.0% (no statements) | 1.000 | 1 · 100.0% · 1.000 | §6.2: a function with no statements has `cov = 1`; gaze reads its empty block as uncovered |
| `Untested` | 2 | 0.0% | 6.000 | 2 | 0.0% | 6.000 | 2 · 0.0% · 6.000 | none |
| `init` (first) | 1 | 100.0% | 1.000 | 1 | 100.0% | 1.000 | 1 · 100.0% · 1.000 | none; the native identity is `init#1` |
| `init` (second) | 2 | 66.7% | 2.148 | 2 | 66.7% | 2.148 | 2 · 66.7% · 2.148 | none; the native identity is `init#2` |

gaze lists 13 functions and the contract and the native code have the same 13. Three rows differ, each for one reason: the error-guard discount (§6.3), a package-level literal's coverage, and a function with no statements. On the other ten rows gaze, the contract and the native code agree to three decimals.
