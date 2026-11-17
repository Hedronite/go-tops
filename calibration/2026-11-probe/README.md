# Calibration record: the 2026-11 probe

Go-TOPS 0.3.0 sets one CRAP cap per class, between 4 and 6 (`GO_TOPS.md` §5.2). This record measures caps in that range against real Go: five Hedronite repos, every function in them, every test suite green. It is the evidence the caps in §5.2 are calibrated against. The first run was on 2026-11-16; this record is the re-run of 2026-11-17, produced by [`run.sh`](run.sh), and its numbers are in [`results.json`](results.json).

## What was measured

| Repo | Pinned commit |
|---|---|
| augur | `fa72aa71aef2e86a4ece01d5188ecc50aced7b55` |
| simplex | `873de8602a74b95bfe38af6d33a9d31594df5726` |
| vigil | `4a71f03ff8e52736963731b2dd088db643c13e03` |
| cubit | `7f6a776932e816842b6d60c949560d6084caebfe` |
| scry | `a33d4de203f6fc06f60982736947ec2e0c9099f5` |

Toolchain: go1.27.4 on darwin/arm64. Each repo was cloned shallow at its commit; nothing was written to any repo.

## Method

**Coverage.** In each repo: `go test -count=1 -coverprofile=<repo>.cover -coverpkg=./... ./...`. All five suites exited 0. Coverage is statement coverage across the whole module (`-coverpkg=./...`).

**Probe.** [`probe/main.go`](probe/main.go) is one file in its own module ([`probe/go.mod`](probe/go.mod)), standard library only (`go/ast`, `go/parser`, `go/token`). For each function declaration in each non-test, non-generated `.go` file (directories named `testdata`, `vendor` and `.git` are skipped), it computes:

- **CC** by the gocyclo rule: 1, plus one for each `if`, `for`, `range`, non-default `case`, non-default `select` clause, `&&` and `||`. Closures count in their parent.
- **Error guards** (the 0.3.0 discount). A guard is discounted when its condition is `x != nil`, it has no `else`, its body is one `return` whose last result is `x` or a call that takes `x`, and `x` is assigned in the `if` init or the statement before it. This is the 0.3.0 text read syntactically, without the `errors.Is` forms.
- **Function coverage**: the profile blocks inside the function's span, weighted by statements. *Guard-neutral* coverage removes the blocks inside discounted guard bodies.
- **Table switches**: a `switch` whose every arm holds at most one simple statement. Its arms beyond the first are subtracted in the two "table" rows.
- **CRAP** = CC² × (1 − coverage)³ + CC, with CC either raw or discounted as the row says.

Five rows of results follow from those: CRAP with plain CC, with the guard discount, with the discount and guard-neutral coverage, with table switches counted once on top, and CC alone (the part no test can change).

**Reproduce.** `./run.sh [output-dir]` clones the five repos at the commits above into a temporary directory, runs coverage and the probe, and writes `results.json`. It needs git, go and network access to github.com; it uses the local toolchain only (`GOTOOLCHAIN=local`), keeps Go's caches inside the temporary directory, and removes everything on exit. The output is deterministic: two runs from clean copies gave byte-identical `results.json`.

## Limits

- The probe is syntactic: no type information, so a call is a guard only by its shape.
- Package-level function literals are skipped.
- One platform and one set of build tags; no `-race`.
- Statement coverage from one `go test` run, not merged across platforms.
- Five repos written by agent squads in the same month; they are the adopters this protocol is for, not a sample of Go in general.
- It reproduces the shape of the result. The native implementation planned for M2 replaces it.

## Results

5 repos, 761 production functions, 7,639 statements, 88.2% statement coverage. The sum of CC is 3,718 raw and 3,468 after the guard discount; 250 guards among 2,957 decisions; 109 of the 250 guard bodies are covered.

**Functions over the cap, all five repos.**

| Functions over the cap | cap 4 | cap 5 | cap 6 | cap 8 | cap 10 | cap 15 | cap 30 |
|---|---|---|---|---|---|---|---|
| CRAP, plain gocyclo count | 316 | 258 | 214 | 151 | 106 | 48 | 13 |
| CRAP, 0.3.0 rule (error guards discounted) | 298 | 245 | **199** | 136 | 91 | 42 | 11 |
| CRAP, discount plus guard bodies left out of coverage | 297 | 243 | 192 | 136 | 90 | 41 | 10 |
| Same, plus table-shaped switches counted once | 278 | 229 | 178 | 120 | 79 | 38 | 9 |
| CC alone above the cap (no test can fix) | 248 | 197 | **156** | 106 | 67 | 28 | 3 |

**Per repo, with the cap-6 columns under both rules.** "Over cap 6, 0.3.0 rule" is the second row of the table above, split by repo.

| Repo | Commit | Functions | Statements | Stmt coverage | Guards | Guard bodies covered | Over cap 6, raw | Over cap 6, 0.3.0 rule | That, as a share of functions | CC alone > 6 |
|---|---|---|---|---|---|---|---|---|---|---|
| augur | `fa72aa7` | 140 | 1564 | 89.7% | 44 | 52% | 40 | 37 | 26% | 31 |
| simplex | `873de86` | 186 | 1871 | 87.8% | 35 | 51% | 50 | 46 | 25% | 36 |
| vigil | `4a71f03` | 100 | 854 | 87.0% | 67 | 37% | 18 | 16 | 16% | 11 |
| cubit | `7f6a776` | 210 | 2261 | 88.0% | 57 | 49% | 71 | 70 | 33% | 55 |
| scry | `a33d4de` | 125 | 1089 | 87.9% | 47 | 32% | 35 | 30 | 24% | 23 |
| **all five** | | **761** | **7639** | **88.2%** | **250** | **44%** | **214** | **199** | **26%** | **156** |

**The five highest scorers in each repo**, for a sense of what the gate would name first. They are ranked by the probe's own key, the last column: CRAP with the error-guard discount and guard-neutral coverage. The column before it, CRAP under the 0.3.0 rule, uses the same discounted CC with ordinary coverage. `results.json` lists twelve per repo.

| Repo | Function | Location | CC | Guards | Coverage | Coverage without guard bodies | CRAP, 0.3.0 rule | CRAP, guard-neutral |
|---|---|---|---|---|---|---|---|---|
| augur | `validator.escalate` | `internal/policy/policy.go:553` | 13 | 0 | 46% | 46% | 39.0 | 39.0 |
| augur | `run` | `scripts/mkarchive/main.go:43` | 26 | 2 | 77% | 78% | 30.9 | 29.9 |
| augur | `validator.hold` | `internal/policy/policy.go:687` | 19 | 0 | 76% | 76% | 24.2 | 24.2 |
| augur | `validator.document` | `internal/policy/policy.go:254` | 24 | 0 | 94% | 94% | 24.1 | 24.1 |
| augur | `Evaluate` | `internal/engine/engine.go:23` | 20 | 0 | 98% | 98% | 20.0 | 20.0 |
| simplex | `NetworkLister.HasTailnetAddress` | `internal/vantage/net.go:13` | 9 | 0 | 0% | 0% | 90.0 | 90.0 |
| simplex | `nodeDesc` | `internal/overlay/loader.go:447` | 9 | 0 | 27% | 27% | 40.2 | 40.2 |
| simplex | `parseChecks` | `internal/config/config.go:625` | 29 | 0 | 85% | 85% | 31.7 | 31.7 |
| simplex | `parseOverrides` | `internal/overlay/loader.go:270` | 27 | 0 | 82% | 82% | 31.0 | 31.0 |
| simplex | `parseHolds` | `internal/config/config.go:703` | 26 | 0 | 81% | 81% | 30.8 | 30.8 |
| vigil | `Config.validate` | `internal/config/config.go:177` | 42 | 5 | 82% | 85% | 45.2 | 41.9 |
| vigil | `Query` | `internal/promclient/promclient.go:154` | 26 | 5 | 91% | 96% | 21.3 | 21.0 |
| vigil | `validPath` | `internal/httpx/httpx.go:211` | 14 | 0 | 100% | 100% | 14.0 | 14.0 |
| vigil | `newRoot` | `cmd/vigil/cli.go:97` | 15 | 1 | 100% | 100% | 14.0 | 14.0 |
| vigil | `Client.Get` | `internal/httpx/httpx.go:230` | 14 | 1 | 95% | 94% | 13.0 | 13.0 |
| cubit | `Open` | `internal/textfile/textfile.go:128` | 31 | 1 | 76% | 76% | 41.9 | 42.6 |
| cubit | `scanRoot` | `internal/scan/scan.go:150` | 37 | 1 | 91% | 91% | 36.9 | 36.9 |
| cubit | `Check` | `internal/config/check.go:50` | 32 | 0 | 86% | 86% | 34.9 | 34.9 |
| cubit | `classify` | `internal/config/findings.go:184` | 26 | 0 | 98% | 98% | 26.0 | 26.0 |
| cubit | `Collector.render` | `internal/collect/collect.go:226` | 23 | 0 | 100% | 100% | 23.0 | 23.0 |
| scry | `run` | `hack/mkarchive/main.go:41` | 25 | 2 | 79% | 83% | 27.8 | 25.8 |
| scry | `Render` | `internal/report/table.go:16` | 20 | 0 | 96% | 96% | 20.0 | 20.0 |
| scry | `verdict` | `internal/report/build.go:47` | 16 | 0 | 97% | 97% | 16.0 | 16.0 |
| scry | `parseReadyz` | `internal/kube/readyz.go:25` | 13 | 0 | 100% | 100% | 13.0 | 13.0 |
| scry | `RealClock.Sleep` | `internal/wait/wait.go:41` | 3 | 0 | 0% | 0% | 12.0 | 12.0 |

## What the numbers say

These follow from the tables above; no other claim is made here.

1. At cap 6, 199 of 761 functions (26%) fail the 0.3.0 rule, in repos with 88% coverage. At cap 4 it is 298 of 761 (39%).
2. 156 of those 199 cap-6 failures (78%) are complexity alone: their CC is above 6, so no amount of testing changes the verdict.
3. The error-guard discount moves 15 functions at cap 6 (214 to 199). Qualifying guards are 250 of 2,957 decisions (8.5%); vigil is the high case at 19%.
4. Guard bodies are covered 44% of the time (109 of 250), half the overall rate, and leaving them out of coverage moves 7 functions at cap 6.
5. Every repo's statement coverage sits between 87.0% and 89.7%.

## Mutation trial

A separate trial with gremlins, measured on 2026-11-16 and **not re-run** for this record. `go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0` built under go1.27.4 on darwin/arm64; `gremlins --version` prints `dev`, so the build is identified by that module version only. Both runs were `gremlins unleash --workers 6` and exited 0.

| Repo | Time | Mutants | Killed | Lived | Not covered | Timed out | Test efficacy | Mutator coverage |
|---|---|---|---|---|---|---|---|---|
| vigil | 38.3 s | 302 | 220 | 35 | 47 | 0 | 86.27% | 84.44% |
| augur | 8 min 29 s | 515 | 385 | 65 | 61 | 4 | 85.56% | 88.06% |

All 35 survivors in vigil came from three mutators: 19 conditionals-negation, 13 conditionals-boundary, 3 arithmetic-base. Augur took thirteen times vigil's wall time (509 s against 38.3 s) for 1.8 times the statements (1,564 against 854), so whole-module mutation on a repo of this size is already a tier 2 cost; only a diff-scoped run fits tier 1.

## Re-run against the first run

The tables above are the re-run's output. They were compared cell by cell with the figures recorded on 2026-11-16: the five rows by seven caps (35 cells), the per-repo rows (5 repos by 7 columns), the totals, and seven scorers the first run named (all within the twelve per repo in `results.json`). **Every cell is equal; no cell differs.** The commits, toolchain version and platform are the same as in the first run, and the probe's arithmetic is unchanged: its first commit here is the file as it ran, and its second only adds the `-json` output.

## Files

| Path | What it is |
|---|---|
| [`run.sh`](run.sh) | Clones at the pinned commits, runs coverage and the probe, writes `results.json` |
| [`results.json`](results.json) | The re-run's output: Go version, platform, commits, and the probe's numbers for each repo |
| [`probe/main.go`](probe/main.go) | The probe: standard library only |
| [`probe/go.mod`](probe/go.mod) | The probe's own module |
