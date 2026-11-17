# Go-TOPS: Go Testing and Optimization Protocol Suite

**Spec status:** 0.4.0-dev (in progress; 0.3.0 was the last draft) · **Date:** 2026-11-17 · **Owner:** Hedronite
**Modeled on:** [Rust-TOPS](https://github.com/Hedronite/rust-tops) (protocol 1.2.0 on `main`; the latest release tag, `cargo-tops` v0.3.0, carries protocol 1.1.0, and `main` holds package 0.4.0, untagged; read 2026-11-17)

> **Changes from 0.2.0 to 0.3.0:** One CRAP cap per class; authorship marking removed (§5.2, §6.2).
> Test jobs run with cgo, because `-race` requires it on Linux (§3.1, G-26). An enabled layer
> with no command is `not-run` and fails the gate (§3.3, §7). The error-guard discount applies
> in every class and only to guards that return the error; blank discards are caught (§6.3).
> Enforcement modes, with `ratchet` the adopter default (§6.7). The Bend2 parity layer and Lean
> recheck are removed; the native analyzers are the laws layer (§8.1). Protocol version bumped.
>
> **Changes from 0.1.0 to 0.2.0:** Hybrid laws architecture (native `go/analysis` primary floor + optional Bend2 parity layer + Lean recheck of catalog and pure invariants). Open decisions in §16 resolved where consensus was reached (err_guard_discount defaults, authorship trailers, Bend optional, etc.). Formal layers guidance added. Protocol version bumped.
>
> **How to read this.** Go-TOPS reuses Rust-TOPS's *shape* (a written spec, a JSON Schema,
> per-class profiles, an orchestrator CLI, a CI floor, and a laws layer) and re-derives the
> *content* for Go. Every place this spec intentionally differs from Rust-TOPS is called out in
> §14 ("Deltas from Rust-TOPS"). Everything Rust-TOPS defines that is reused here is described
> from `rust-tops` `main` as read on 2026-11-17; confirm against the repo before claiming
> parity. Values labeled **proposed** are design defaults to be tuned on real code.
> Items marked **[verify]** name Go tools whose current behavior must be confirmed at pin time.

---

## 1. What Go-TOPS is

**Go-TOPS** is "a protocol and drop-in kit for Go that is correct, dense, and fast." It has
three parts, mirroring Rust-TOPS:

1. **A written spec** (this document, `GO_TOPS.md`) defining gates, tiers,
   classes, thresholds, and rules.
2. **A JSON Schema** (`schema/go-tops.schema.json`, plus a gate-report schema) that makes the
   config and the report machine-checkable. When prose and schema disagree, fix the schema, then
   the prose.
3. **Per-class profiles** (`profiles/*.yaml`) that set floors and requirements for different
   kinds of Go code.

Plus a practice layer (human and agent working rules, §13 and `AGENTS.md`) and an orchestrator CLI,
**`gotops`**, that runs the gates.

### 1.1 Goals

- Make "done" measurable: a change is acceptable when the gate passes, and the gate is the same
  for humans and agents.
- Catch what coverage cannot: complexity-weighted risk (CRAP) and mutation survival on changed
  code.
- Encode Go's real hazards (goroutine leaks, context misuse, unbounded I/O, resource leaks, race
  conditions) as checkable **laws**, not folklore.
- Be hermetic: pinned tools, reproducible results, no network surprises.
- Be honest: a layer that was not evaluated is reported as not evaluated, never as a pass.

### 1.2 Non-goals

- Not a test framework. Go-TOPS shells out to `go test`, `gotestsum`, fuzzing, and friends; it
  does not replace them.
- Not a style guide. Formatting and idiom checks belong to `gofumpt`/golangci-lint; Go-TOPS only
  requires that they pass.
- Not a replacement for review. Several layers (oracle-first testing, scar-guard) are policy
  that tooling can only partially observe.

### 1.3 Name and binary

- Protocol: **Go-TOPS**. Binary: **`gotops`**. Repo (proposed): `github.com/Hedronite/go-tops`.
- Go has no `cargo`-style external-subcommand mechanism, so there is no `go tops`. Run it with the
  tool directive: `go get -tool github.com/Hedronite/go-tops/cmd/gotops@<ver>` then
  `go tool gotops gate`. (Also installable with `go install`.)

---

## 2. Pins

| Item | Value |
|---|---|
| Protocol version | **0.4.0-dev** (in progress; 0.3.0 was the last draft; 1.0.0 is set by the criterion in §19) |
| Package version | 0.0.0 (unreleased) |
| Language floor ("MSRV" analogue) | `go 1.26.0` in `go.mod`; matrix tests floor and current (current: Go 1.27) |
| Toolchain pin | `toolchain` directive in `go.mod` plus `GOTOOLCHAIN=local` in CI |
| Tool lock | `go-tops.lock` (see §10) |
| golangci-lint | **v2.15.0** (exact; latest on 2026-11-17, released 2026-10-16; Go 1.27 support since v2.13.0) |
| `gotestsum` | **v1.13.0** (exact; optional display wrapper over `go test -json`) |
| `govulncheck` | **v1.9.0** (exact) |
| `benchstat` | `golang.org/x/perf/cmd/benchstat` has no release tags: a pseudo-version is pinned at adoption **[verify]** G6 (R26 places it at tier 2) |
| Mutation engine | `gremlins` **v0.6.0** (exact; the starting engine, 0.x, flags may change; accounting rules are G4's, R20) |
| `goleak` | `go.uber.org/goleak` **v1.3.0** (exact; the leak check on Go 1.26; on 1.27 and later the `goroutineleak` profile in `runtime/pprof` is the stdlib alternative, and G5 states which layer uses which) |

Tool versions were read from upstream release tags on 2026-11-17.

Protocol and package versions are **separate planes**, as in Rust-TOPS: a package release can
change code without changing the protocol, and a protocol change must bump the protocol version
and schema together.

---

## 3. Protocol model

### 3.1 Gate order

Gates run in this order. A failing gate stops the run unless `--keep-going` is set.

| # | Gate | Command(s) | Pass condition |
|---|---|---|---|
| 1 | **Hygiene** | `go mod tidy -diff`; `go mod verify`; `golangci-lint fmt --diff`; `go vet ./...`; `golangci-lint run`; `govulncheck ./...`; dependency policy (`depguard`, license check) | All clean; no reachable vulnerability |
| 2 | **Tests** | `CGO_ENABLED=1 gotestsum -- -race -shuffle=on -count=1 ./...` (includes `Example*` functions, the doc-test analogue). `-race` requires cgo on Linux; only darwin runs it without cgo | All pass; no race |
| 3 | **Coverage** | tests with `-coverprofile -coverpkg=./...`; merge integration data (`go build -cover`, `GOCOVERDIR`, `go tool covdata`); then **`test -s`** on the merged profile | Profile exists and is non-empty; class floors met |
| 4 | **CRAP** | native CRAP over merged profile + cyclomatic complexity | No function above its cap (§6) |
| 5 | **Mutation (diff)** | mutation engine restricted to lines changed in `origin/main...HEAD` | Unexplained survivors ≤ `unexplained_survivors_max` |
| 6 | **Class layers** | fuzz, leak detection, synctest-required tests, benchmark regression, cgo/unsafe checks, alloc budgets, as the class requires | Per-class (§5) |
| ★ | **Laws** | Go-TOPS analyzers (§8) | No `Refuse`; `not-evaluated` never counts as a pass |

★ The laws layer is **tier 0** (it defines the floor) but **executes last**, matching Rust-TOPS,
where the laws layer is tier 0 and runs after the mapped commands.

Step 6 mirrors Rust-TOPS's step 6 ("property tests, fuzz, Miri, Kani, Callgrind when the crate
class requires them") with Go-appropriate equivalents (§7).

### 3.2 Tiers

`gotops gate` runs every enabled layer **at or below `--max-tier`** (default **1**). A layer or
law above `--max-tier` is out of range: it is not run and is not counted against the run (§3.5).

Rust-TOPS defines tiers 0–5 (`RUST_TOPS.md` §3). Go-TOPS uses four:

| Tier | Purpose | Typical runtime | Contents |
|---|---|---|---|
| **0** | Floor, always on | seconds | Config and schema validation (`check`); the laws whose reader is `analyzer` |
| **1** | **Merge gate** (default) | minutes | Gates 1–5; the laws whose reader is `tool`; class layers that run a command at tier 1. Seed-corpus fuzz replay, leak checks and `synctest` tests run inside the test run of gate 2 and are not layers (§7) |
| **2** | **Deep / nightly** | tens of minutes to hours | Timed fuzzing; full-module mutation; long `-race -count=N` stress; benchmark regression vs baseline; full OS/arch/tag matrix |
| **3** | **Release** | variable | Reproducible-build verification; vulnerability re-scan of the artifact (`govulncheck -mode=binary`); SBOM and signature checks; API-compat check (`gorelease`/`apidiff`-class **[verify]**) |

**What `--max-tier 0` judges.** Tier 0 judges the laws whose reader is `analyzer`: each reads
files itself (`go.mod`, the config, source syntax) and needs no other layer's output. It does
not judge the laws whose reader is `tool`. Those map the output of a tier 1 layer (G-12 reads
the lint JSON, G-40 the `govulncheck` output) and are judged at tier 1. In a `--max-tier 0` run
they are out of range: listed as `not-evaluated` with reason `input_missing` and in the report's
out-of-range list, and they do not fail that run (§3.3). Exit 0 from `--max-tier 0` therefore means the tier 0 checks passed, not
that the gate did.

### 3.3 Verdicts

<!-- tops-core:begin verdicts -->
There are two vocabularies: **outcomes** for layers and **verdicts** for laws. They share no
value, so a value never means two things.

**Casing.** Every enum value is lower case. Words in an outcome or a verdict are joined with `-`
(`not-run`, `not-evaluated`), words in a reason code with `_` (`no_reader`), and JSON object keys
use `snake_case`. Prose, config and the report spell each value the same way.

**Layer outcomes** say what one layer did in one run.

| Outcome | Meaning | Effect on the gate |
|---|---|---|
| `pass` | The layer ran and its rule is met | none |
| `fail` | The layer ran and its rule is broken | **fails** |
| `skip` | The class does not require the layer, or `go-tops.yaml` sets it `enabled: false` with a `reason` | Printed with the reason; the layer is not enabled, so it is not counted |
| `not-run` | The layer is enabled and in range but has no command mapped | **fails** |
| `not-reached` | The layer is enabled and in range and has a command, but a layer it needs did not pass, or `--fail-fast` stopped the run before it (§3.5) | **fails** |
| `vacuous` | The layer ran and had nothing to judge, in the one place this spec allows that: an empty mutation diff (§6.4) | Printed and counted for nothing: neither a pass nor a failure |
| `tool-missing` | A tool the layer maps could not be found | **fails** |

Everywhere else an empty result is `fail` (§3.4 rule 5).

**Law verdicts** say what one law found.

| Verdict | Meaning | Effect on the gate |
|---|---|---|
| `allow` | The law was judged and the code meets it | none |
| `ack` | The law found an instance of a soft rule and the instance carries a recorded acknowledgement (below) | none, within the limit below |
| `refuse` | The law found an instance it forbids, or an instance of a soft rule with no acknowledgement | **fails** |
| `not-evaluated` | The law could not be judged. It carries one reason code | **fails** when the law is required (below); otherwise counted and never blocking |

**Reason codes.** Every `not-evaluated` carries exactly one.

| Code | Meaning |
|---|---|
| `no_reader` | No reader can judge the law in this run: it is `attested` and not yet attested, it is `tbd`, or its analyzer is not built |
| `config_off` | The linter, setting or layer the law reads is switched off: a `tool` reader that finds errcheck, or its `check-blank` setting, off for G-12; a feeding layer set `enabled: false` |
| `input_missing` | The output the reader needs was not produced: the layer that makes it was `not-run`, `not-reached` or `tool-missing`, or is above `--max-tier` |

**Required laws.** The class profile's `laws.required` lists the laws the class cannot go
without. A required law that is `not-evaluated` **fails the gate, whatever the reason**. A law
outside the list that is `not-evaluated` is counted in the report and never blocks. A required
law above `--max-tier` is out of range: the report lists it as out of range, and that run's
verdict covers its range only (§3.2). A profile's `laws.required` holds only laws at or below the
drop-in's maximum tier, 1, so the merge gate judges every required law and this rule binds in
full there. Switching off the linter behind a required law therefore fails the gate, and a
`not-evaluated` law is never read as `allow`.

**Severity and acknowledgement.** Each law has a severity in §8.2: **R** or **A**. An instance of
an R law is `refuse`. An instance of an A law is `ack` when it carries a recorded
acknowledgement and `refuse` when it does not. A recorded acknowledgement is an inline
`//tops:ack G-NN reason="…" owner="…"` comment, or an entry (law, file, reason, owner) in
`.go-tops/acks.yaml` (`laws.ack_ledger`); both need a reason and an owner (§13 rule 6). Under
`hard` an `ack` does not fail. Under `ratchet` the acknowledgements for a law in a file are
counted against that file's `suppressions` entry for the law (§6.7): up to the entry they are
`ack`, and when the count is above the entry the law's verdict for that file is `refuse`.
<!-- tops-core:end verdicts -->

**Keys.** The rules above add these configuration keys.

| Key | Meaning |
|---|---|
| `laws.required` | Profile key: a list of law IDs, a subset of `laws.enable`. Each must have an `analyzer` or `tool` reader, because an `attested` or `tbd` law can never be judged, and each must be a law at tier 1 or below. `go-tops.yaml` may add IDs and may not remove or override the profile's |
| `enabled`, `reason` | On every layer. `enabled: false` is the only way to turn a layer off, and it requires a non-empty `reason`, which is printed with the `skip`. `check` fails an `enabled: false` with no `reason` |

### 3.4 Stated rules (policy)

These are inherited in spirit from Rust-TOPS and stated as protocol rules:

1. **Oracle first.** Write the oracle from the *contract* before the code. Tests derived from a
   function body freeze current behavior, including bugs.
2. **Scar-guard.** Do not split functions only to lower a CRAP score. Reviewers may reject a
   change whose only effect is to move complexity across function boundaries.
3. **Do not weaken a law** so a check passes. Fix the code, or change the law through the protocol
   change process (§13), never inline.
4. **Schema precedence.** When prose and schema disagree, fix the schema first, then the prose.
5. **No silent empties.** A coverage profile, CRAP report, or test result that is empty fails the
   gate. A green run that measured nothing is a defect. `vacuous` is for the empty mutation diff
   only (§3.3).
6. **No dry gates in CI.** `--dry-run` and any "names-only" mode must be refused when `CI=true`.
7. **Required means judged.** A law the profile requires that was not judged fails the gate,
   whatever the reason (§3.3).

### 3.5 Run order

`gotops gate` runs **every** enabled layer in range, in the order of §3.1 with the laws last, so
that one run shows every refusal. A layer that fails does not stop the layers that do not depend
on it. This replaces the stop-at-the-first-failure behavior of §3.1.

A layer that needs another is `not-reached` when the other is not `pass`:

| Layer | Needs |
|---|---|
| Coverage | Tests |
| CRAP | Coverage |
| Mutation | Tests (green) |
| A law whose reader is `tool` | The layer whose output it maps. Only that law is affected: it is `not-evaluated` with reason `input_missing`, and the laws layer still runs |

Hygiene, tests and the class layers need nothing.

- `--fail-fast` stops the run after the first layer that is `fail`, `not-run` or `tool-missing`.
  Every enabled layer in range that has not run is then `not-reached`.
- **Fewer ran than enabled fails.** The report records the number of layers enabled in range and
  the number that ran (outcome `pass`, `fail` or `vacuous`). The gate fails when they differ.
- `--only <layer>` runs that layer and the layers it needs. The others are out of range.
- There is no `--changed`. The CRAP layer covers every production function on every run, and the
  mutation layer is scoped by `--base` (§6.4).

---

## 4. Quick start

### 4.1 Copy the files

For a new repo, copy the drop-in set and replace the placeholders:

| File | Purpose | Placeholders |
|---|---|---|
| `go-tops.yaml` | Protocol config (§9) | `{MODULE}`, `{CLASS}` |
| `go-tops.lock` | Tool pins and checksums (§10) | generated |
| `AGENTS.md` | Agent-facing commands and rules (template: `config/AGENTS.template.md`) | `{MODULE}`, `{CLASS}`, `{ENFORCE}`, `{COVERAGE_FLOOR}`, `{CRAP_CAP}`, `{SURVIVORS_MAX}` |
| `.golangci.yml` | golangci-lint v2 config; errcheck `check-blank` and `check-type-assertions` on (§6.3) | `{MODULE}` |
| `.go-tops/baseline.json` | Ratchet baseline (§6.7); written by `gotops baseline`, not by hand | generated |
| `.gremlins.yaml` or engine config | Mutation settings **[verify]** | |
| `.github/workflows/go-tops.yml` | CI floor (§11) | `{GO_FLOOR}`, `{GO_CURRENT}`, `{PRIMARY_PKG}` |
| `go.mod` (`go`, `toolchain`, `tool` lines) | Toolchain pin | `{GO_FLOOR}` |
| `justfile` | Front door recipes | |

### 4.2 Or install the orchestrator

```bash
go get -tool github.com/Hedronite/go-tops/cmd/gotops@<ver>
go tool gotops init --class binary-cli     # writes the drop-in set
go tool gotops check                       # validates config
go tool gotops gate                        # runs the gates (tier ≤ 1)
```

`init` writes `go-tops.yaml`, `go-tops.lock`, `AGENTS.md`, drop-in configs, a class workflow, and
the laws config, and (unlike Rust-TOPS, which does not write `rust-tops.lock`) generates the
lock file so the first `gate` is already pinned.

### 4.3 Nix (optional)

An optional flake overlay exposes `gotops` and the analyzer bundle, and a `nix flake check`
verifies the law catalog names. Low priority; ship after the CLI is stable (§15).

---

## 5. Classes and profiles

A **class** describes what kind of Go code a module is. The class selects the profile: floors,
required layers, and CRAP caps. A repo declares one class per module (workspace roots use
`workspace-root` and delegate to member modules).

### 5.1 Class catalog

The schema permits twelve classes (Rust-TOPS's schema permits eleven). **Launch profiles** ship
for the first five; the rest are schema-valid but profile-less until written, exactly as in
Rust-TOPS where five of eleven classes have profiles.

| Class | Typical code | Launch profile? |
|---|---|---|
| `library-core` (default) | Reusable library with a stable API | **Yes** |
| `binary-cli` | Command-line programs | **Yes** |
| `tui-app` | Bubble Tea (or similar) terminal programs | **Yes** |
| `binary-service` | HTTP/gRPC services, exporters, daemons | **Yes** |
| `concurrency-runtime` | Schedulers, pools, pipelines, queue/worker frameworks | **Yes** |
| `library-util` | Small helper packages, low API stability | No |
| `parser-codec` | Parsers, encoders/decoders, protocol code | No (high priority next) |
| `unsafe-kernel` | Heavy `unsafe` / low-level memory code | No |
| `cgo-bridge` | cgo and foreign-function boundaries | No |
| `codegen` | Generators, analyzers, `go generate` tools (the proc-macro analogue) | No |
| `embedded-tinygo` | TinyGo / constrained targets (the `no-std-embedded` analogue) | No |
| `workspace-root` | `go.work` root coordinating modules | No |

### 5.2 Proposed floors by class

All numbers are **proposed** defaults. Tune on real repositories before 1.0.

| | `library-core` | `binary-cli` | `tui-app` | `binary-service` | `concurrency-runtime` |
|---|---|---|---|---|---|
| Statement coverage floor | 85% | 70% | 65% (model/update logic) | 75% | 90% |
| CRAP cap | 8 (provisional) | 10 | 8 (provisional) | 10 | 8 (provisional) |
| `unexplained_survivors_max` | **0** | **0** | **0** | **0** | **0** |
| Race detector | required | required | required | required | required (+ stress) |
| Leak detection (`goroutineleak` profile on Go 1.27, `goleak` on 1.26) | if goroutines | if goroutines | required | required | required |
| `synctest` for time/concurrency | if applicable | if applicable | if timers | if timers | required |
| Fuzz targets | for exported decoders | for flag/arg parsers | for input parsing | for request decoders | for queue/codec boundaries |
| Benchmark regression gate | tier 2 | no | no | tier 2 | tier 2 |
| API-compat check | tier 3 | no | no | no | tier 3 |
| Extra laws | G-21–G-24 | G-41 | **G-10** | G-19, G-20 | G-01–G-09 |

There is one CRAP cap per class, the same for every author: the gate is the same for humans and
agents (§1.1). What a cap means is stated in §6.2: at full coverage it is a ceiling on
complexity. How the cap binds under each enforcement mode, including what an existing function
above it may do, is stated once, in §6.7. No class is uncapped: `tui-app` and
`concurrency-runtime` have no launch profile (§5.1) and no measurement, so each takes 8, the
strictest launch cap, as a provisional value until a repo of the class is measured.

**Where the caps come from.** The launch caps are `binary-cli` 10, `binary-service` 10 and
`library-core` 8. They come from a measurement, not from another protocol:
`calibration/2026-11-probe/` records a syntactic probe over five Hedronite Go repos, 761
production functions at 88% statement coverage with every suite green. Under the 0.4.0 rule
(error guards discounted, guard bodies left out of coverage) 192 of the 761 functions are over a
cap of 6, 136 are over 8 and 90 are over 10. Of the functions over 10, 67 are over it by
complexity alone, which no test can change. A cap of 6 would fail about one function in four in
well-covered repos; near 10 the coverage term starts to do work (§6.2, worked examples).
`library-core` keeps the stricter value, as it did when the caps were 4 and 6. The `library-core`
cap is **provisional** until slice M2-5 repeats the measurement with the native implementation,
and so are the caps of `tui-app` and `concurrency-runtime`, which take 8 because nothing has
been measured for them. A higher number is not a weaker gate by itself: the ratchet (§6.7) and
mutation (§6.4) carry the rest.

### 5.3 Profile file shape

```yaml
# profiles/binary-cli.yaml
class: binary-cli
protocol: "0.3.0"
gates:
  coverage: { lines_floor: 70 }
  crap:     { cap: 6, complexity: { err_guard_discount: true } }
  mutation: { unexplained_survivors_max: 2, scope: diff }
layers:
  race: required
  goleak: when-goroutines
  synctest: when-timers
  fuzz: { required_for: [arg-parsers], tier: 1, replay_seed_corpus: true }
laws:
  enable: [G-11, G-12, G-16, G-17, G-18, G-19, G-33, G-34, G-40, G-41]
```

---

## 6. Scoring and reporting model

### 6.1 Coverage

- Source: the merged profile from unit tests (`go test -coverprofile -coverpkg=./...`) and
  integration runs (`go build -cover` + `GOCOVERDIR` + `go tool covdata merge/textfmt`).
- Floors per class (§5.2) live in `go-tops.yaml`.
- Gate fails when the profile is missing or empty (`test -s`).
- Coverage is computed on the module's own packages; generated files (`*.gen.go`,
  `// Code generated ... DO NOT EDIT.`) are excluded by default, with an explicit allowlist for
  inclusion.

### 6.2 CRAP

```text
CRAP(f) = comp(f)² × (1 − cov(f))³ + comp(f)          cov(f) ∈ [0,1]
```

- `comp(f)` = cyclomatic complexity of function `f` computed from the AST (§6.3).
- `cov(f)` = statement coverage of `f` from the merged profile, guard-neutral (§6.3): the sum of
  `NumStmt` over the profile blocks inside `f` whose count is above zero, divided by the sum of
  `NumStmt` over all blocks inside `f`, closures included. A function that has statements and no
  block in the profile has `cov(f) = 0`. A function with no statements left after §6.3 has
  `cov(f) = 1`. A profile that holds no block of any production file fails the layer (§3.4,
  rule 5).
- **Cap:** one per class (§5.2), the same for every author. There is no authorship marking: a
  marker that changes the cap is a marker an agent can choose. A function meets the cap when
  `CRAP(f) ≤ cap`, computed in `float64` and compared unrounded.
- **What the cap means.** At full coverage `cov(f) = 1` and `CRAP(f) = comp(f)`, so the cap is a
  ceiling on complexity. A function whose `comp(f)` is above the cap fails at every coverage
  level. A function whose `comp(f)` equals the cap passes only at `cov(f) = 1`. A function below
  the cap passes once its coverage is high enough, and needs less the further below the cap it
  sits. Coverage lowers the score of a function inside the ceiling; it never lifts one over it.
- **Scope:** every production function of the module's own packages, on every run, at every tier
  that runs the layer. Excluded: `_test.go` files, generated files (as in §6.1: `*.gen.go` and
  files with a `// Code generated ... DO NOT EDIT.` header) and `testdata/` directories. Nothing
  else is excluded; a function leaves the cap check only through an exemption (§6.8). There is no
  diff scope for CRAP: a function's score also moves when a test is deleted, and the function
  itself does not change. The module-wide report is the output of every run, not a separate mode.
- **Enforcement** is set by `enforce` and stated once, in §6.7. This section defines the score,
  the cap and the scope; it does not say which functions may exceed the cap under which mode.
- **Implementation:** native (not shelled out), so the formula, counting rule, and thresholds are
  protocol-controlled. Existing tools (`crap4go`, `gaze`) are useful cross-checks **[verify]**.

**Worked examples.** Every value is `CRAP = comp² × (1 − cov)³ + comp`, shown to one decimal;
a function is judged on the unrounded value.

| # | Case | Cap | `comp(f)` | Covered / total statements | `cov(f)` | `CRAP(f)` | Result |
|---|---|---|---|---|---|---|---|
| 1 | Complex and half-tested | 10 | 14 | 52 / 100 | 52.0% | 35.7 | fails |
| 2 | Under the cap, tested enough | 10 | 8 | 70 / 100 | 70.0% | 9.7 | meets |
| 3 | The same function, fewer tests | 10 | 8 | 68 / 100 | 68.0% | 10.1 | fails |
| 4 | At the cap, fully covered | 10 | 10 | 40 / 40 | 100.0% | 10.0 | meets |
| 5 | Above the cap, fully covered | 10 | 11 | 44 / 44 | 100.0% | 11.0 | fails at every coverage |
| 6 | `library-core`, at the cap, fully covered | 8 | 8 | 40 / 40 | 100.0% | 8.0 | meets |
| 7 | The same function, one statement short | 8 | 8 | 99 / 100 | 99.0% | 8.0 (8.000064) | fails: compared unrounded |
| 8 | Three guards discounted, guard bodies untested, guard-neutral coverage | 8 | 6 | 14 / 17 | 82.4% | 6.2 | meets |
| 9 | Row 8 with the guard bodies kept in coverage | 8 | 6 | 14 / 20 | 70.0% | 7.0 | meets, charged for what the discount forgave |
| 10 | Row 8 with `err_guard_discount: false` | 8 | 9 | 14 / 20 | 70.0% | 11.2 | fails |
| 11 | Dispatch table with `shape: table-switch` (§6.8) | 10 | 30 | 90 / 90 | 100.0% | 30.0 | over the cap; exempt, still printed |

Rows 8 to 10 are one function of 20 statements: 3 sit in the bodies of guards that meet §6.3, and
none of those 3 is covered; 14 of the other 17 are covered. Row 8 leaves the 3 out of both terms
(14 of 17). Row 9 shows what leaving them in would charge (14 of 20). Row 10 turns the discount
off, so the function has comp 9 and its guard bodies count.

The coverage a function needs follows from the formula: it meets a cap when
`cov(f) ≥ 1 − ((cap − comp(f)) / comp(f)²)^(1/3)`. Rounded up to 0.1%:

| Cap | `comp(f)` | Minimum `cov(f)` |
|---|---|---|
| 10 | 5 | 41.6% |
| 10 | 8 | 68.6% |
| 10 | 9 | 76.9% |
| 10 | 10 | 100.0% |
| 8 | 5 | 50.7% |
| 8 | 6 | 61.9% |
| 8 | 7 | 72.7% |
| 8 | 8 | 100.0% |

Keys (G7 folds this table into §9 and the schema, then deletes it):

| Key | Change |
|---|---|
| `gates.crap.scope` | **Removed.** CRAP has no diff scope. A config that still sets it fails `check` |

### 6.3 Complexity counting rule

Baseline (matches `gocyclo`): `1 + #(if) + #(for) + #(range) + #(case) + #(comm) + #(&&) + #(||)`.

- `default:` clauses in `switch` and `select` are not counted, so an n-way branch adds n − 1,
  as in classical McCabe.
- Function literals are counted inside the function that contains them. A closure is not a
  place to hide branches. Package-level function literals are scored on their own.

**Error guards.** Counting every `if err != nil { return err }` as a decision charges the most
for handling errors properly, and rewards the shortcuts that avoid the guard: `x, _ := f()`,
`must(f())`, and `panic`. The profile key `complexity.err_guard_discount` (default **`true`** in
every class) gives weight 0 to a guard that meets all of these:

- it is an `if` statement with no `else`, whose condition is exactly `err != nil`, where `err` is
  an error value assigned by the statement just before the `if` or in its init clause;
- its body is exactly one `return` statement, and the last result of that `return` is one of:
  that `err`; a call to `fmt.Errorf` whose format is a string literal and whose operand for one
  of its `%w` verbs is that `err`; or a call to a function listed in `complexity.wrap_functions`
  with that `err` as an argument. A bare `return` does not qualify.

A guard that logs, swallows the error, does other work, or classifies the error is counted
normally (and `nilerr` flags the swallowing case). A condition that tests `errors.Is`,
`errors.As` or a comparison with a sentinel is a decision and counts. A `fmt.Errorf` that formats
`err` with `%v`, or with no verb, does not wrap it and counts.

**Wrap functions.** `complexity.wrap_functions` lists the functions a repo calls instead of
`fmt.Errorf` to wrap an error. An entry is `import/path.Name`, matched against the import path of
the called function's package, as the file imports it, and its name. The default is the empty
list. List only a function whose result wraps its error argument, so that `errors.Is` finds that
argument through the result. The list sits in `go-tops.yaml`, which CODEOWNERS covers (§11).

**Guard-neutral coverage.** The discount applies to both terms of CRAP. The statements inside the
body of a discounted guard leave the numerator and the denominator of `cov(f)`: the profile
blocks that lie within the guard's body are not counted for `f`. A function is not charged in
coverage for what it was forgiven in complexity. Guard bodies are the least-tested statements in
real Go, and without this rule the untested error return the discount forgives in `comp(f)` is
charged again in `cov(f)`. When `err_guard_discount` is `false` no guard is discounted: its
decision counts in `comp(f)` and its body counts in `cov(f)`.

The discount only makes sense if discarding the error is caught. The drop-in `.golangci.yml`
therefore sets errcheck `check-blank: true` and `check-type-assertions: true`, and G-12 judges
those settings (§8.1).

Keep the key explicit in every config so scores are comparable across repos.

Keys (G7 folds this table into §9 and the schema, then deletes it):

| Key | Type | Default | Change |
|---|---|---|---|
| `gates.crap.complexity.err_guard_discount` | bool | `true` | Existing. Now also removes the guard's body from `cov(f)` |
| `gates.crap.complexity.wrap_functions` | list of strings | `[]` | New. Wrap functions that count as `fmt.Errorf` with `%w` for the guard rule |

### 6.4 Mutation

- **Engine:** pluggable adapter. The starting engine is **gremlins v0.6.0**
  (`github.com/go-gremlins/gremlins`); a `go install` build of it reports its version as `dev`,
  so the pin is read with `go version -m` (§10). It exits 0 when mutants survive unless a
  threshold flag is set, so the adapter judges the parsed statuses, never the exit code. Other
  engines (`go-mutesting` forks, `mutest`, an in-house `-overlay` runner) are admitted only
  through an adapter that maps their statuses to the classes below and meets every rule in this
  section; none is pinned in 0.4.0. Engine output that is missing or does not parse is `fail`
  (§3.4, rule 5). The adapter must support restricting mutation to changed lines or files; if the
  chosen engine cannot, Go-TOPS filters to changed files and post-filters survivors to changed
  lines.
- **Diff base:** the merge base of `origin/main` and `HEAD` (`origin/main...HEAD`, three-dot),
  overridable with `--base`. The diff is written to `.go-tops/mutation.diff` as
  `git diff --unified=0 <base>...HEAD`, minus the files §6.2 excludes (`_test.go`, generated
  files, `testdata/`) and every non-Go file.
- **Diff outcomes:**
  - No merge base, or a shallow clone (`git rev-parse --is-shallow-repository` prints `true`):
    the layer is `fail`, and its message names the missing history and `fetch-depth: 0` (§11).
  - No changed line in a production Go file: the layer is `vacuous`. It prints that it had
    nothing to look at and counts for nothing: not as a pass, not as a failure.
  - A non-empty diff that yields no mutants is a `pass` with `0 mutants` printed. An empty diff
    is the only vacuous case.
- **Statuses:** every mutant the engine reports is mapped; a status that is not in this table is
  `fail`, never dropped.

  | Engine status (gremlins) | Class | Effect |
  |---|---|---|
  | `KILLED` | killed | Counted as killed |
  | `LIVED` | survivor | Unexplained unless explained below |
  | `NOT COVERED` | survivor | As `LIVED`. No test reaches the changed code, which is what "changed code without a test" means |
  | `TIMED OUT` | killed, listed | Counted as killed. Its key is listed in the report, so a reviewer sees which mutants hung instead of failing a test |
  | `NOT VIABLE` | dropped | Does not compile; counted nowhere. The report prints the total dropped |

- **Survivor key:** file, enclosing function, mutator, and a hash of the normalized mutated line.
  - `file`: module-relative, slash-separated.
  - `function`: the enclosing function, by receiver type and name as in §6.7; a function literal
    belongs to its parent function (§6.3).
  - `mutator`: the engine's mutator name, such as `CONDITIONALS_BOUNDARY`.
  - `line_hash`: SHA-256, in hex, of the mutated line: the source line with the mutant applied at
    the position the engine reports, tokenized with `go/scanner`, comments and automatic
    semicolons dropped, tokens joined by one space. The key holds no line number, because a line number changes on the next edit.
    Two mutants of one mutator on one line differ in the mutated line, so they have different
    keys.
- **Survivor accounting:** a survivor is *explained* when an unexpired entry in
  `.go-tops/survivors.yaml` has its key, or an unexpired `mutation` exemption (§6.8) covers its
  file or function. Only *unexplained* survivors count against `unexplained_survivors_max`. An
  entry has these fields, all required:

  ```yaml
  survivors:
    - file: internal/limits/parse.go
      function: parseLimit
      mutator: CONDITIONALS_BOUNDARY
      line_hash: "<sha-256 of the mutated line>"
      reason: equivalent at the boundary, the next line rejects the same input
      owner: platform
      expires: "2027-01-15"
  ```

  `reason` has at least three words and says why the mutant is accepted: why no test can tell it
  from the original, or which test is owed. `expires` is a `YYYY-MM-DD` date. An entry with a
  missing field, an unknown field, a `reason` of fewer than three words, or an `expires` that is
  not a real day, or that is today (UTC) or earlier, explains nothing and fails the gate at tier
  0, named in the output. An entry that matches no survivor in this run is not an error: a
  diff-scoped run cannot see survivors outside the diff.
- **Allowance:** `unexplained_survivors_max` is **`0` in every class**. Any unexplained survivor
  fails the layer. The key stays in `go-tops.yaml` so the report states the limit; `0` is its only
  valid value.
- Tier 2 runs the whole module on a schedule, with the same statuses and key; engine run time can
  reach hours on large modules. The unexplained survivors of that run are held by the `survivors`
  ledger kind of §6.7: one count per file, and a count above the ledger's value fails. A legacy
  survivor is debt that may only fall; a new survivor, or one in a file the ledger does not list,
  fails (an absent entry means 0). The diff-scoped run at tier 1 has no such allowance:
  `unexplained_survivors_max` stays 0.

Keys (G7 folds this table into §9 and the schema, then deletes it):

| Key | Change |
|---|---|
| `gates.mutation.unexplained_survivors_max` | Existing. The only valid value is `0`, in every class |
| `gates.mutation.ledger` | Existing (`.go-tops/survivors.yaml`). Entry fields are now the record above, `expires` required |

### 6.5 Law verdicts

A law ends a run as `allow`, `ack`, `refuse` or `not-evaluated` with a reason code (§3.3). The
report lists, per law, its reader (§8.2) so a reader can see what was actually judged, and for a
`not-evaluated` law the reason. A required law that is `not-evaluated` fails the gate.

### 6.6 Gate report

<!-- tops-core:begin report -->
Unlike Rust-TOPS, whose CLI prints text and does not write the optional report object that its
schema describes, `gotops gate` **always** writes `.go-tops/report.json` (and prints text). Every
run that gets as far as running a layer writes it, whether the run passes, fails or was stopped by
`--fail-fast`; a run refused before any layer runs (exit 2, 3 or 4) prints its reason and writes
none. The report names every layer in range with its outcome and every law with its verdict
(§3.3). It counts `not-run`, `not-reached`, `vacuous` and `not-evaluated` separately and never
folds them into `pass` or `allow`. It records the numbers §3.5 and §3.3 judge by: layers enabled
in range and layers that ran; laws required, judged and `not-evaluated`, with the count by reason; and the laws
out of range, with the run's verdict covering its range only. See §12.3.
<!-- tops-core:end report -->

### 6.7 Enforcement modes

<!-- tops-core:begin enforcement -->
The top-level key `enforce` sets how floors and caps are applied. Adopters default to
`ratchet`. Go-TOPS runs `hard` on itself.

| Mode | What fails |
|---|---|
| `hard` | A production function above the CRAP cap; a package below its coverage floor; any lint finding; any law verdict `refuse`; an unexplained mutation survivor |
| `ratchet` | The `hard` rules, except where the ledger below holds an entry for the thing that failed: that entry's limit applies instead. Two further limits exist only here: suppression counts and the exemption count |
| `advisory` | Nothing. Needs the `advisory` key (Keys, below); the report prints the run as not enforced. The baseline cannot be written |

**CRAP.** A function at or under the cap may change up to the cap. A function over the cap
may not exceed its baseline score plus 0.01 (a rise of 0.01 or less is noise). A function absent
from the baseline is new and meets the cap. So a function scoring 2 under a cap of 10 may rise to
10, and one scoring 14.2 under a cap of 10 may not pass 14.21. Under `ratchet` no function within
the cap is held stricter than under `hard`.

**The baseline is a debt ledger.** `.go-tops/baseline.json` lists only what is over the line
today. Everything the ledger does not list is held to its `hard` rule, and an empty ledger means
`hard`, with the suppression and exemption counts held at zero. That is stricter than `hard`,
and it is allowed: a new suppression or exemption is a regression that needs the path below. The file is tracked, written
with keys sorted and one entry per line so that parallel branches merge without conflict, and
must be clean against `HEAD`.

| Ledger kind | Entry key | Entry value | The ratchet checks |
|---|---|---|---|
| `functions` | function identity (below) | CRAP score when written | score ≤ value + 0.01. Only a function over the cap has an entry |
| `packages` | package import path | statement coverage % and uncovered-statement count | A package under the floor fails only when its percentage falls **and** its uncovered-statement count rises. Deleting tested code can lower the percentage without raising the uncovered count, so it does not fail. A package at or over the floor, or absent, is held to the floor |
| `findings` | linter, file | count of that linter's findings that no law maps | count ≤ value |
| `laws` | law ID, file | count of findings that make that law `refuse` | count ≤ value; `refuse` then means "no new instance" under `ratchet` and "none" under `hard` |
| `suppressions` | linter and file for `//nolint`; law ID and file for `//tops:ack` comments and `.go-tops/acks.yaml` entries | count of such comments and entries | count ≤ value; absent means 0. No `hard` form: under `hard` a suppression needs only its reason (§13 rule 6) |
| `exemptions` | none (one count per repo) | number of live exemption records (§6.8) | count ≤ value; absent means 0. No `hard` form: under `hard` an exemption is bounded by its expiry  |
| `survivors` | file | count of unexplained survivors from the tier 2 whole-module mutation run (§6.4) | count ≤ value; absent means 0, so a legacy survivor is debt and a new one fails  |

A finding is counted once: a finding that a law maps counts under `laws`, not under `findings`. A
count is by file, never by module, so removing a finding in one file does not pay for adding one
in another. A new file starts at 0; a deleted file's allowance does not move to another file.

Beside the ledger the file holds `judged_laws`, the number of laws the run judged (applicable to
the class, verdict other than `not-evaluated`). It is a floor, not a debt: the count may not fall
between baseline and run, so a configuration edit cannot quietly shrink what is judged.

**Writing the baseline.** Only `gotops baseline` writes the file (§12.1).

- `baseline --init` writes the first ledger. It runs the layers that feed the ledger (tests,
  coverage, CRAP, hygiene, the laws) as `gate` would and judges none of them: the ledger records
  what was **measured**, not what **passed**. It writes when the tests are green and every
  feeding layer produced non-empty output (a non-empty coverage profile, a CRAP report with at
  least one production function, saved lint output). Red thresholds do not stop it; red tests or
  an empty output do. It refuses when a ledger exists and under `advisory`. A repo with nothing
  over the line gets a valid file with an empty ledger.
- A ledger kind fed only by tier 2 (`survivors`) is recorded the same way, from a tier 2 run:
  `baseline --init --kind survivors`. Until it is recorded, that kind is absent and holds at its
  `hard` form.
- Every later write only lowers. It lowers a value, removes an entry that is no longer over
  the line, and raises `judged_laws`. It never adds an entry and never raises one.
- Raising an entry, adding one, or lowering `judged_laws` needs
  `baseline --accept-regression --reason <text> --owner <handle>` and a PR that carries the
  `protocol-change` label. The flag records the change under `regressions` in the file. `gate`
  compares the file with its copy at the diff base (§6.4) and fails a raised or added entry, or
  a lowered `judged_laws`, that has no matching `regressions` record. A record is removed by the
  write that pays the entry back to its earlier value.
- **First ledger.** When the diff base has no `.go-tops/baseline.json`, the file is a first
  ledger, and there is no copy to compare it with. `gate` accepts it without `regressions`
  records only if every entry equals what this run measures (a `functions` value within 0.01;
  counts, percentages and uncovered-statement counts exactly), no entry exists for anything the
  run measures within the line, and `judged_laws` equals the run's count. That is what
  `baseline --init` writes on that commit, so it is the only first ledger that passes. Any entry
  above its measured value fails, so a hand-written ledger cannot carry extra allowance. A kind
  that the base copy lacks (the tier 2 `survivors` kind, recorded later) is a first ledger for
  that kind and is checked the same way against the run that recorded it.
- Under `ratchet` a missing baseline fails; deleting the file is not a reset.
- Legacy findings stay in the ledger. They are not wrapped in `//nolint` or `//tops:ack` to zero
  the count: the `suppressions` limit holds that route shut.
<!-- tops-core:end enforcement -->

**Function identity.** A function is identified by import path, receiver type (if any)
and name. Where that key occurs more than once in a package (several `init` functions, or
`foo_linux.go` and `foo_darwin.go` twins; every `.go` file in the directory counts, whatever its
build tags), the key adds the file's name, and where it still repeats within one file, its
position in source order (`init#2`). A function moved to another file keeps its entry when
`git diff -M` against the diff base pairs the two files and the receiver and name are unchanged.
For any other rename or move, `baseline --rename <old-key> <new-key>` rewrites one key (function
or package) and never changes a value; it refuses unless the old key has vanished from the code and
the new key exists. The same command moves a key that gained a file name when a twin of the
function was added. The result is reviewed as part of the PR. An unpaired rename without that
command is a new function, and a new function meets the cap.

**Keys.** The rules above add one configuration key. `enforce` and `baseline` are in §9 already.

| Key | Meaning |
|---|---|
| `advisory.reason`, `advisory.owner`, `advisory.expires` | Required when `enforce: advisory` and invalid otherwise. `expires` is a date; `check` fails once it has passed. |

#### First adoption of a legacy repo

The repo has 100 production functions, 8 of them over the `binary-cli` cap of 10 under the 0.4.0
rule, which counts coverage guard-neutral (vigil's counts in `calibration/2026-11-probe/`), and
no baseline. The ledger numbers
below other than those two are illustrative. Every step passes by the rule named beside it; the
measuring run is `baseline --init` itself, so no step is a red gate.

1. `gotops init --class binary-cli` writes `go-tops.yaml` with `enforce: ratchet`, and no
   baseline (§12.1). `init` runs no gate, so there is nothing to fail.
2. `gotops baseline --init` runs the feeding layers and judges none. Rule: measured, not
   passed. Tests are green and the profile and the CRAP report are non-empty, so it writes
   a ledger with 8 `functions` entries (each at its current score), no `packages` entry (every
   package meets its floor), and whatever
   `findings`, `laws`, `suppressions` and `exemptions` counts the code has today, for example 3
   `//nolint` in two files. `judged_laws` is the run's count.
3. The ledger and `go-tops.yaml` are committed on the adoption branch. Rule: the baseline is
   tracked and clean against `HEAD`. The diff base, `origin/main`, has no baseline, so the
   ledger is a first ledger (§6.7).
4. `gotops check` passes: the file exists and is clean (§12.1).
5. `gotops gate` passes. Rule: first ledger. Each entry equals this run's measurement, so no
   `regressions` record is needed. Each of the 8 functions equals its entry (limit: entry +
   0.01); the other 92 were at or under 10 when measured and are unchanged, so they meet the
   cap; every count equals its entry; `judged_laws` equals its recorded value; no tier 2 kind
   is judged at tier 1. This is the first green run, with no source edit.
6. A later PR edits one of the 8 functions, which scores 21.0, to 21.005. Rule: a rise of 0.01
   or less is noise. An edit to 22.0 fails by the same rule; that refusal is the rule working on
   new debt, not an adoption step.
7. A PR refactors one of the 8 down to 8. `gate` passes (the score fell), and
   `gotops baseline` removes its entry (writes only lower). From then on it is held to the cap
   of 10, like the other 92.
8. A PR deletes tested code. In a package at or over its floor the PR passes while the package
   stays at or over it. In a package the ledger lists as under its floor, the percentage may
   fall but the uncovered-statement count does not rise, so the package does not fail.
9. Before the nightly tier 2 job is enabled, one tier 2 run records the legacy survivors:
   `gotops baseline --init --kind survivors`. The nightly then fails only on a survivor
   that is new or in a file whose count rose.
10. CI runs `gotops gate` (§11) on the adoption PR. The base still has no baseline, so the
    first-ledger rule applies again and the run is green on step 5's commit.

### 6.8 Exemptions

<!-- tops-core:begin exemptions -->

Every skip is data. An exemption takes functions, or mutants, out of one layer's check, in the
open and for a limited time. It is the only way to do so: a cap, a threshold or a survivor
allowance is never changed for one function.

```yaml
exemptions:
  - path: internal/wire/dispatch.go
    symbol: Server.handleOp
    layer: crap
    shape: table-switch
    reason: opcode dispatch, every arm is one call; stays a table or answers to the cap
    owner: platform
    expires: "2027-03-01"
  - path: internal/legacy/**
    layer: mutation
    reason: frozen vendor port, replaced by the new codec in the January release
    owner: protocol
    expires: "2027-01-31"
```

Each entry has these fields:

| Field | Required | Meaning |
|---|---|---|
| `path` | yes | Glob relative to the module root, slash-separated. `*` stays inside one path element; `**` crosses elements |
| `symbol` | no | One function in the files `path` matches: `Name` for a package-level function, `Type.Name` for a method (no `*`, no parentheses, no type parameters). Without `symbol` the entry covers every function in those files |
| `layer` | yes | `crap` or `mutation`. Any other value is malformed |
| `shape` | no | `table-switch`. Only with `layer: crap` and a `symbol` |
| `reason` | yes | At least three words, saying why the layer cannot judge this code |
| `owner` | yes | A non-empty person or team handle |
| `expires` | yes | A quoted `YYYY-MM-DD` date, UTC |

**Expiry is required.** An entry is in force while today (UTC) is before `expires`. On that date
and after, or when `expires` is missing or not a real calendar day, the entry exempts nothing
and the gate fails with the entry named. There is no permanent exemption.

**Malformed fails.** An entry with an unknown field, a missing required field, a `layer` outside
the table, a `shape` without `layer: crap` and `symbol`, a `symbol` without `path`, a `reason`
of fewer than three words, or a `path` that is not a valid glob exempts nothing and fails the
gate. Exemptions are read at tier 0 (§3.2), before any layer runs, so form and expiry fail first.
The layer an entry names then checks that it still matches: a `path` that matches no production
file, or a `symbol` that names no function in the matching files, fails, so an exemption leaves
the file with the code it covered.

**What an exemption does.** Under `layer: crap` the covered functions are still scored and
printed, marked exempt, and are not judged against the cap. Under `layer: mutation` the mutants
in the covered code are not counted as survivors, and the report lists them as exempt. An
exemption changes no other layer, no law and no coverage floor; a law is acknowledged with
`//tops:ack` (§3.3). The gate prints every exemption in force, with its expiry, on every run.

**The table-shaped `switch`.** A function that is a dispatch table has a large cyclomatic
complexity and no real branching. Splitting it only to bring each part under the cap is a
scar-guard violation (§3.4, rule 2); it takes a `crap` exemption with `shape: table-switch`
instead. A function is table-shaped when:

- its body is one `switch` or type `switch` statement, optionally followed by one `return`;
- every clause of that `switch`, `default` included, has a body of exactly one statement: a
  `return`, an assignment, an expression statement or a channel send;
- that statement contains no `if`, `for`, `range`, `switch`, `select`, `go`, `defer`, function
  literal, `&&` or `||`, and at most one call expression.

With `shape: table-switch` the gate checks the shape on every run. The exemption holds only while
the function stays a table: an arm that grows a guard, a nested branch, or a second call is real
branching, the entry stops matching, and the function answers to the cap. A `switch` that is not
table-shaped has no exemption under this rule; the general rules above apply to it, as to any
function.

<!-- tops-core:end exemptions -->

**The exemption count.** The number of live exemption records, those in force on the day of the
run, is the `exemptions` ledger kind of §6.7: one count per repo. Under `ratchet` that count may
only fall, so a new exemption is a regression that §6.7 gates; under `hard` an exemption is
bounded by its expiry. This section defines the record and §6.7 defines how the count moves.

Keys (G7 folds this table into §9 and the schema, then deletes it):

| Key | Type | Default | Change |
|---|---|---|---|
| `exemptions` | list of records (fields above) | `[]` | New. Lives in `go-tops.yaml`, which CODEOWNERS covers (§11) |

---

## 7. Class layers: Go equivalents of Rust's deep tools

Rust-TOPS lists fuzz, Miri, Kani, loom, sanitizers, and Callgrind. Go has different failure
modes and different tools. **This section lists only layers that run a command.** A condition
with no command of its own is a law with a decidable predicate (below), and anything that runs
inside the single test run of gate 2 is part of that run, not a layer.

| Rust-TOPS layer | Go-TOPS layer | Command | Tier |
|---|---|---|---|
| Fuzz | Timed fuzzing, one run per target | `go test -run='^$' -fuzz='^FuzzName$' -fuzztime=…` | 2 |
| Miri / sanitizers (UB detection) | Stress repetition; sanitizer legs for cgo classes | `go test -race -count=N`; `go test -msan` / `-asan` | 2 |
| Kani (formal) | *No Go equivalent.* Not a Go-TOPS layer, so it does not appear in config or the report | n/a | n/a |
| Callgrind (perf) | Benchmark regression vs baseline; allocation budgets | `go test -run='^$' -bench=. -benchmem`, `benchstat` | 2 |
| `cargo deny` | Dependency policy and vulnerabilities | `govulncheck ./...`, `golangci-lint run` (`depguard`), `go mod verify` | 1 |
| MSRV job | Language-floor job: build and test on the `go` floor | `go test ./...` on the floor toolchain (CI matrix, §11) | 1 |
| Feature matrix | Build-tag × `CGO_ENABLED` × OS/arch matrix | `go test ./...` per leg (CI matrix, §11) | 2 |

**What is not a layer, and where it went.**

| Was | Now |
|---|---|
| Property tests (`rapid` / `gopter` **[verify]**) | `Test*` functions in the single test run of gate 2 |
| Seed-corpus fuzz replay | The test run: `go test` replays committed seeds whether or not `-fuzz` is set |
| Deterministic concurrency tests (`testing/synctest`) | The test run. G-08 forbids `time.Sleep` as a wait |
| Leak checks, "if goroutines" | Law **G-42** (§8.2): a package with a non-test `go` statement has a leak check in `TestMain` |
| Fuzz targets, "for flag/arg parsers", "for request decoders" | Law **G-43** (§8.2): a fuzz target exists for each function matching a declared `layers.fuzz.targets` glob |
| Race detection | A flag on the test run: `-race` |

**Leak checks by Go version.** On Go 1.27 and later the `goroutineleak` profile in
`runtime/pprof` is the standard-library leak check: it needs no dependency and reports goroutines
that can never run again, but it can miss a leak that a global still reaches. On Go 1.26, the
floor, the profile is not available and the check is `goleak` (`go.uber.org/goleak`). G-42
accepts either form where the module's `go` directive allows it.

**An enabled layer with no command is `not-run` and fails the gate**, and **a mapped tool that
is missing is `tool-missing` and fails the gate** (§3.3). A layer drops out of a run only when
the class does not require it or `go-tops.yaml` sets it `enabled: false` with a `reason`; either
way it is `skip`, printed with the reason. `init` writes `enabled: false` with a reason for
layers the class does not map, rather than leaving them enabled and empty. Because Go-TOPS
resolves tools through the `tool` directive and the lock file, a missing tool is rare and is a
configuration error, not an environment accident.

---

## 8. The Laws layer

### 8.1 Model

Laws encode Go-specific hazards as named, numbered rules. Rust-TOPS 1.1.0 defines 53 laws
(H-01…H-53) in a Bend2-based layer where 15 have fact readers judged by `bend` and 38 report
`not-evaluated`. Go-TOPS uses the same honesty model (`Refuse` fails, `Ack` requires
acknowledgement, `not-evaluated` never counts as a pass), implemented natively: the laws layer
is a `go/analysis` multichecker (`gotops-laws`), tier 0, always on.

There is no Bend2 or Lean layer. A proof over a table of facts checks the table, not the code;
the analyzers check the code. A formal layer is reconsidered only with evidence that it judges
something the analyzers do not, and only through the protocol-change process (§13).

- **Native.** Laws are implemented as `go/analysis` analyzers in one multichecker binary
  (`gotops-laws`, run via `go vet -vettool` or directly). Many laws map onto existing analyzers
  (vet, errcheck, bodyclose, noctx, contextcheck, gosec, depguard); the rest are custom.
- **Readers.** Each law declares how it is judged:
  - `analyzer`: a Go-TOPS analyzer produces a verdict;
  - `tool`: delegated to a named external tool, mapped into a verdict. A `tool` reader
    reads the tool's effective configuration first. If the linter or setting the law depends
    on is off (for example errcheck, or its `check-blank` setting, for G-12), the law is
    `not-evaluated`, not `pass`;
  - `attested`: judged by human/agent attestation (e.g., oracle-first); reported
    `not-evaluated` until attested;
  - `tbd`: not yet implemented; reported `not-evaluated`.
- **Honesty preserved.** A law with no fact reader (or missing tool) is reported `not-evaluated`, never as a pass.

### 8.2 Law catalog (draft: 43 laws, `G-01`…`G-43`)

Severity: **R** = a violation is `refuse`, **A** = a violation is `ack` when acknowledged and `refuse` when not (§3.3). Reader: **an** = analyzer, **tl** = delegated tool,
**at** = attested, **tbd** = not yet implemented. "v0.1" marks laws targeted for the first
mechanical release.

**Concurrency**

| ID | Law | Sev | Reader | v0.1 |
|---|---|---|---|---|
| G-01 | Every `go` statement has a documented owner and an exit path (context, done channel, or finite work) | A | an (heuristic) | |
| G-02 | `wg.Add` is never called inside the goroutine it counts | R | tl (vet family) | ✓ |
| G-03 | Module language version ≥ 1.22 (per-iteration loop variables) | R | an | ✓ |
| G-04 | Channels are closed only by their owning sender, never by a receiver | A | an (heuristic) | |
| G-05 | No unbuffered send that can strand a goroutine on early return (leak pattern) | A | an + goleak | |
| G-06 | No mutex held across blocking I/O or channel operations | A | an (heuristic) | |
| G-07 | Sync primitives are never copied | R | tl (vet `copylocks`) | ✓ |
| G-08 | Tests never call `time.Sleep` to wait for concurrency; use `synctest` | R | an | ✓ |
| G-09 | Fire-and-forget work from a request uses `context.WithoutCancel`, a bound, and a shutdown path | A | an (heuristic) | |
| G-10 | (`tui-app`) The Bubble Tea model is mutated only inside `Update`; no model writes from goroutines or `tea.Cmd` closures | R | an | ✓ |
| G-42 | A package with a `go` statement in non-test code has a leak check in `TestMain`: a call to `goleak.VerifyTestMain` or `goleak.VerifyNone`, or, where the module's `go` directive is 1.27 or later, a `pprof.Lookup("goroutineleak")` check after `m.Run`. The predicate is syntactic: goroutines that a called library starts are not seen | R | an | |

**Errors**

| ID | Law | Sev | Reader | v0.1 |
|---|---|---|---|---|
| G-11 | Package-boundary errors wrap with `%w`; no matching on `err.Error()` strings | R | tl (errorlint) + an | ✓ |
| G-12 | No ignored errors outside the allowlist, including blank assignments (`_ =`, `x, _ :=`) and unchecked type assertions | R | tl (errcheck with `check-blank`, `check-type-assertions`) | ✓ |
| G-13 | Library code does not `panic` or call `Must*`-style helpers for recoverable conditions; each panic outside `init` and tests carries an ack naming the programmer error | A | an (flags every call) | |
| G-14 | Exported sentinel/typed errors are documented as API | A | at | |

**Context**

| ID | Law | Sev | Reader | v0.1 |
|---|---|---|---|---|
| G-15 | `context.Context` is the first parameter and is not stored in a struct | A | an | |
| G-16 | Every outbound network or database call takes a context | R | tl (noctx, contextcheck) | ✓ |
| G-17 | `context.Background()`/`TODO()` appear only in `main`, wiring, and tests | R | an | ✓ |

**Resources**

| ID | Law | Sev | Reader | v0.1 |
|---|---|---|---|---|
| G-18 | Acquired resources are released: response bodies, rows (and `rows.Err()` checked), files, timers | R | tl (bodyclose, sqlclosecheck, rowserrcheck) | ✓ |
| G-19 | HTTP clients and servers set timeouts; no `http.DefaultClient` in production code | R | an | ✓ |
| G-20 | Reads of untrusted input are bounded (`io.LimitReader`, `http.MaxBytesReader`); no `io.ReadAll` on unbounded input | A | an (heuristic) | |

**API and types**

| ID | Law | Sev | Reader | v0.1 |
|---|---|---|---|---|
| G-21 | Exported identifiers have doc comments | A | tl (revive) | |
| G-22 | The public API surface is recorded and checked for compatibility; breaking changes require a major version | R | tl (gorelease/apidiff **[verify]**) | tier 3 |
| G-23 | `init()` performs registration only; no I/O or hidden state | A | an | |
| G-24 | No package-level mutable state in `library-core` | A | an | |

**Unsafe and cgo**

| ID | Law | Sev | Reader | v0.1 |
|---|---|---|---|---|
| G-25 | `unsafe` is confined to declared packages, each use carrying a `SAFETY:` comment | R | an | ✓ |
| G-26 | cgo appears only in `cgo-bridge` modules; all other classes build and release with `CGO_ENABLED=0`. Test jobs are exempt: `-race` requires cgo on Linux | R | an + build | ✓ |
| G-27 | Packages using `unsafe` or cgo are tested under `-race` and, for cgo, a sanitizer leg | A | an | |

**Testing**

| ID | Law | Sev | Reader | v0.1 |
|---|---|---|---|---|
| G-28 | Oracle-first: tests derive from the contract, not the function body | A | **at** | |
| G-29 | Every exported decoder/parser in `parser-codec` has a fuzz target and a committed seed corpus | R | an | |
| G-30 | Golden files change only via an explicit `-update` run and are reviewed | A | at | |
| G-31 | Tests do not depend on wall clock, network, or unseeded randomness | R | an (heuristic) | |
| G-32 | Benchmarks use `b.Loop` and report allocations | A | an | |
| G-43 | Every function that matches a glob in `layers.fuzz.targets` has a `Fuzz*` function in its package that calls it, and a committed seed under `testdata/fuzz/`. A glob is a `go list` package pattern, a colon, and a function-name glob in `path.Match` syntax (`./internal/spec:Parse*`). G-29 is the `parser-codec` case | R | an + config | |

**Build and determinism**

| ID | Law | Sev | Reader | v0.1 |
|---|---|---|---|---|
| G-33 | `go` directive ≥ protocol floor; `toolchain` pinned | R | an | ✓ |
| G-34 | `go mod tidy -diff` is clean and `go mod verify` passes | R | tl | ✓ |
| G-35 | Release builds are reproducible (`-trimpath`, no embedded timestamps) | A | tl (tier 3) | |

**Security**

| ID | Law | Sev | Reader | v0.1 |
|---|---|---|---|---|
| G-36 | No secrets in source or fixtures | R | tl (secrets scanner **[verify]**) | |
| G-37 | No `math/rand` for security-sensitive values; stdlib crypto only | R | tl (gosec) | ✓ |
| G-38 | SQL is parameterized; no string concatenation into `Query`/`Exec` | R | an | ✓ |
| G-39 | No `template.HTML`/unescaped output from untrusted input | A | an (heuristic) | |
| G-40 | Zero reachable vulnerabilities per `govulncheck` | R | tl | ✓ |

**Interface contracts**

| ID | Law | Sev | Reader | v0.1 |
|---|---|---|---|---|
| G-41 | (`binary-cli`) Exit codes are documented and stable; `--json` output matches a versioned schema | A | at | |

**v0.1 target:** judge 18 laws mechanically (the ✓ rows), mirroring Rust-TOPS's
approach of shipping a minority of laws with real readers and reporting the rest honestly as
`not-evaluated`. The report states the count, e.g., "18 of 43 laws judged."

### 8.3 Law metadata

Each law lives in `laws/G-NN.yaml`:

```yaml
id: G-17
title: "No context.Background outside main, wiring, and tests"
severity: Refuse
classes: [library-core, binary-service, concurrency-runtime, binary-cli, tui-app]
reader: analyzer
analyzer: gotops/ctxroot
allow_paths: ["cmd/**/main.go", "internal/app/wire*.go", "**/*_test.go"]
rationale: >
  Detached contexts defeat cancellation and tracing; they hide leaks.
fix: "Accept a context parameter and pass it down."
```

---

## 9. Configuration: `go-tops.yaml`

```yaml
protocol: "0.3.0"            # must match the schema's protocol id
version: "0.1.0"             # this config's version (non-empty)
module: github.com/acme/myapp
class: binary-cli            # one of the schema's classes
enforce: ratchet             # hard | ratchet | advisory (§6.7); adopters default to ratchet
baseline: .go-tops/baseline.json

toolchain:
  go_floor: "1.26.0"
  go_current: "1.27"
  cgo: false                 # builds and releases; true only for cgo-bridge.
                             # Test jobs always run with cgo for -race (§3.1)

gates:
  hygiene:
    golangci_lint: { config: .golangci.yml }
    govulncheck:   { fail_on: reachable }
    deps:          { depguard: true, licenses: { allow: [MIT, Apache-2.0, BSD-3-Clause, BSD-2-Clause, ISC] } }
  tests:
    race: true
    shuffle: true
    count: 1
  coverage:
    lines_floor: 70
    exclude_generated: true
    integration: { enabled: true, packages: ["./cmd/..."] }
  crap:
    cap: 6
    scope: diff
    complexity: { err_guard_discount: true }
  mutation:
    engine: gremlins          # gremlins | go-mutesting | mutest | overlay
    scope: diff
    base: origin/main
    unexplained_survivors_max: 2
    ledger: .go-tops/survivors.yaml

layers:
  fuzz:      { tier: 1, replay_seed_corpus: true, timed: { tier: 2, duration: 5m } }
  goleak:    { when: goroutines }
  synctest:  { when: timers }
  bench:     { tier: 2, baseline: .bench/baseline.txt, regression_pct: 5 }

laws:
  enable: default             # or an explicit list
  ack_ledger: .go-tops/acks.yaml

report:
  path: .go-tops/report.json
```

Config files the drop-in set may include: `go-tops.yaml`, `go-tops.lock`, `AGENTS.md`,
`.golangci.yml`, mutation-engine config, `.github/workflows/go-tops.yml`, `justfile`, `.go-tops/`
(ledgers and report).

---

## 10. Tool lock: `go-tops.lock`

Purpose: make the gate reproducible. Contents:

- Exact versions of Go, `golangci-lint`, `gotestsum`, `govulncheck`, `benchstat`, the mutation
  engine, the secrets scanner, and `gotops` itself.
- For downloaded binaries: URL template and SHA-256 per OS/arch.
- For `tool`-directive tools: the module version (the `go.sum` entry provides the hash).
- **Trust model:** checksum-verified. Where an upstream release is mutable or unsigned, use
  trust-on-first-use and record that in the lock (Rust-TOPS does this for Bend and Lean).
  `gotops lock --verify` re-checks every pin; `gotops lock --update` is a reviewed change, never
  an automatic one.

---

## 11. CI floor

The drop-in workflow `.github/workflows/go-tops.yml` provides these jobs (placeholders
`{GO_FLOOR}`, `{GO_CURRENT}`, `{PRIMARY_PKG}`):

| Job | Content |
|---|---|
| `hygiene` | `go mod tidy -diff`, `go mod verify`, fmt check, vet, golangci-lint, govulncheck |
| `tests` | `CGO_ENABLED=1 gotestsum -- -race -shuffle=on -count=1` on `{GO_FLOOR}` and `{GO_CURRENT}` |
| `language-floor` | Build and test pinned at `go` floor (the MSRV analogue) |
| `matrix` | Build-tag × `CGO_ENABLED=0/1` × GOOS/GOARCH (tier 2 on schedule; floor subset per PR) |
| `coverage-crap` | Merged profile → CRAP; **fails if the profile or CRAP JSON is empty** |
| `mutation-diff` | Diff-scoped mutation vs `origin/main...HEAD` |
| `laws` | `gotops laws` (tier 0), uploads report |
| `law-diff` | Separate workflow: fails PRs that weaken or remove a law without a protocol-change label |
| `fuzz-nightly`, `bench-nightly`, `mutation-full` | Tier 2, scheduled |
| `release-verify` | Tier 3, on tag: reproducible build, binary `govulncheck`, SBOM/signature check |

CI rules (carried from Rust-TOPS's lessons):

- CI **refuses** `--dry-run` and any names-only mode.
- The coverage job fails on an empty profile or empty CRAP output.
- Unlike Rust-TOPS's drop-in workflow, which comments out the mutants, Darwin, and fuzz jobs,
  Go-TOPS ships them as real jobs on the appropriate tier, disabled only by explicit config.
- Pin actions by commit SHA; minimal `permissions:`; cache `GOMODCACHE`/`GOCACHE` by `go.sum`.

---

## 12. The `gotops` CLI

### 12.1 Subcommands

| Command | Behavior |
|---|---|
| `init --class <c>` | Writes `go-tops.yaml`, `go-tops.lock`, `AGENTS.md`, drop-in configs, the class workflow, `justfile`, and the laws config. Refuses to overwrite without `--force`. |
| `check` | Validates `go-tops.yaml` against the **JSON Schema** (full validation, an improvement over Rust-TOPS's partial checks), plus semantic checks: protocol id, non-empty version, module name, `crap.cap ≥ 1`, `library-core`/`concurrency-runtime` keep `unexplained_survivors_max = 0`, language floor ≤ current, `advisory` carries a dated exemption, and under `ratchet` the baseline file exists and is clean against `HEAD`. |
| `baseline` | Writes `.go-tops/baseline.json` (§6.7), the only command that does. `--init` measures and writes the first ledger when the tests are green and every feeding layer produced non-empty output (thresholds may be red); `--init --kind survivors` records the tier 2 survivor counts the same way. A plain `baseline` only lowers entries and removes paid ones. `--accept-regression --reason <text> --owner <handle>` is the one way to raise an entry or lower `judged_laws` (the PR carries the `protocol-change` label). `--rename <old-key> <new-key>` moves one key. Refused under `advisory`, and `--init` is refused when a ledger exists. |
| `gate` | Runs every enabled layer at or below `--max-tier` (default 1), in order, each judged (§3.5), laws last. Options: `--max-tier N`, `--fail-fast`, `--dry-run` (refused in CI), `--base <ref>`, `--json`, `--only <layer>`. A mapped tool that is missing is `tool-missing` and exits 1. Exit codes: §12.2. |
| `laws [list \| run \| explain G-NN]` | Lists the catalog with readers, runs the laws layer alone, explains a law. |
| `report [--format text \| json \| junit \| sarif]` | Renders the last report. |
| `lock [--verify \| --update]` | Verifies or updates tool pins. |
| `doctor` | Diagnoses environment: Go version vs floor, tool presence, `GOFLAGS`, cgo state. |
| `survivors [list \| explain \| expire]` | Manages the mutation survivor ledger. |
| `version` | Protocol, package, and pinned-tool versions. |

### 12.2 Exit codes

<!-- tops-core:begin exit-codes -->
One table maps every outcome and verdict (§3.3) to an exit code. Codes 2, 3 and 4 are decided
before any layer runs. A run that starts returns 0 or 1.

| Code | When |
|---|---|
| 0 | Every layer in range is `pass`, `skip` or `vacuous`; every law is `allow`, `ack` (within the ratchet limit, §3.3) or `not-evaluated` and either not required or out of range |
| 1 | Any layer is `fail`, `not-run`, `not-reached` or `tool-missing`; any law is `refuse`; any required law in range is `not-evaluated`; fewer layers ran than are enabled in range; a ratchet limit (§6.7) is exceeded |
| 2 | The configuration or its custody is invalid: a schema or semantic failure of `check` (including a missing or dirty baseline under `ratchet` and an expired `advisory`), or a tool lock mismatch |
| 3 | An environment error that is not a missing tool: an unreadable repository, no C compiler where `-race` needs one |
| 4 | Refused by policy: `--dry-run` or a names-only mode when `CI=true` |

A missing tool has one code: 1.
<!-- tops-core:end exit-codes -->

### 12.3 Report: `.go-tops/report.json`

Written on every `gate` run. Schema file: `schema/gate-report.schema.json`.

```json
{
  "protocol": "0.3.0",
  "tool_version": "0.0.0",
  "module": "github.com/acme/myapp",
  "class": "binary-cli",
  "enforce": "ratchet",
  "git": { "head": "abc1234", "base": "origin/main", "dirty": false },
  "max_tier": 1,
  "started_at": "2026-11-14T18:00:00Z",
  "duration_ms": 184211,
  "layers": [
    {
      "id": "tests", "tier": 1, "outcome": "pass",
      "details": { "packages": 41, "tests": 812, "race": true, "shuffle_seed": 91823 }
    },
    {
      "id": "coverage", "tier": 1, "outcome": "pass",
      "details": { "lines_pct": 78.4, "floor": 70, "profile_bytes": 48211 }
    },
    {
      "id": "crap", "tier": 1, "outcome": "fail",
      "details": { "cap": 6,
        "violations": [ { "func": "ParseSpec", "file": "internal/spec/parse.go:41",
                          "cc": 14, "cov_pct": 52.0, "crap": 35.7, "baseline": null } ] }
    },
    { "id": "mutation", "tier": 1, "outcome": "pass",
      "details": { "mutants": 63, "killed": 59, "survived_explained": 3, "survived_unexplained": 1, "max": 2 } }
  ],
  "layer_counts": { "enabled_in_range": 4, "ran": 4 },
  "laws": {
    "catalog": 43, "judged": 18, "required": 10, "required_not_evaluated": 0,
    "not_evaluated": 25,
    "not_evaluated_by_reason": { "no_reader": 22, "config_off": 0, "input_missing": 3 },
    "out_of_range": [],
    "results": [
      { "id": "G-17", "verdict": "allow", "reader": "analyzer" },
      { "id": "G-28", "verdict": "not-evaluated", "reason": "no_reader", "reader": "attested" }
    ]
  },
  "summary": { "outcome": "fail", "exit_code": 1, "failed": ["crap"] }
}
```

Report rules: every layer in range is present with an `outcome` from §3.3, and a `skip` carries
its `reason`. A layer that is `not-run`, `not-reached` or `tool-missing` is present and fails the
run. A law carries a `verdict`, and a `not-evaluated` law carries its reason code; the laws counts
(`not_evaluated`, `not_evaluated_by_reason`, `required_not_evaluated`) are separate and are never folded into
`allow`. `out_of_range` lists the laws above `max_tier`; the run's `summary` covers the range
`max_tier` selects and no more. `layer_counts` holds the layers enabled in range and the layers that ran; the run fails
when they differ (§3.5). A CRAP violation with `"baseline": null` is a new function; otherwise
`baseline` is the score it must not exceed.

---

## 13. Policies (`POLICIES.md` and `AGENTS.md`)

Practice rules for humans and agents, enforced by review and, where possible, by the laws layer:

1. Write the oracle from the contract before the code (G-28).
2. Never split a function only to lower its CRAP score (scar-guard).
3. Never weaken a law, linter config, or threshold to make a change pass. Change them through the
   protocol-change process: a PR that touches `laws/`, `schema/`, `profiles/`, or thresholds
   carries a `protocol-change` label and a changelog entry, and is reviewed by a protocol owner.
4. Fix schema first, then prose.
5. Explain every accepted mutation survivor (reason, owner, optional expiry).
6. No `//nolint` without a reason; no `//tops:ack` without a reason and an owner.
7. Agent-authored code meets the same gate and the same CRAP cap as human code.
8. Do not edit generated files; fix the generator.
9. Prefer deleting code to adding configuration.
10. Keep `AGENTS.md` current: it is the contract between the repo and its agents.

`AGENTS.md` (generated by `init`) lists: the `just` recipes; the exact `gotops` commands; the
class and its floors; the laws enabled; the survivor/ack ledgers; and the rules above.

---

## 14. Deltas from Rust-TOPS

| Area | Rust-TOPS (`rust-tops` `main`, read 2026-11-17) | Go-TOPS (this spec) | Why |
|---|---|---|---|
| Invocation | `cargo tops` external subcommand | `go tool gotops` / `gotops` | Go has no external-subcommand mechanism |
| Test runner | `cargo nextest` + `cargo test --doc` | `gotestsum` over `go test` incl. `Example*` | Native equivalents |
| Coverage | `cargo llvm-cov` → LCOV | `go test -coverprofile` + `covdata` merge; LCOV optional | Go's own format; integration via `GOCOVERDIR` |
| CRAP | external `cargo crap --lcov` | **Native** implementation | Control of formula and counting rule; no mature single Go tool |
| Mutation | `cargo mutants --in-diff` | Pluggable engine adapter + diff filter | Go engines vary in diff support |
| Deps/supply chain | `cargo deny check` | `govulncheck` + `depguard` + license scan + `go mod verify` | Go splits these concerns |
| Deep layers | fuzz, Miri, Kani, loom, sanitizers, Callgrind (mostly unmapped) | fuzz, race, goleak, synctest, benchstat, pprof budgets (**mapped**) | Go's tools are in the stdlib/ecosystem and cheap to run |
| Laws engine | Bend2 `LAWS.bend` (53 laws, 15 judged) | Native `go/analysis` multichecker (43 laws); no Bend or Lean layer | Analyzers judge the code itself; a proof over a fact table judges the table |
| CRAP caps | Human and agent caps | One cap per class | A cap the author can select by marking is not a cap |
| Unmapped layer | `NotRun`; fails the gate | `not-run`; fails the gate | Same rule: an enabled layer that ran nothing proves nothing |
| Enforcement | `gate` judges the coverage floors and the CRAP threshold on new and changed functions; a CRAP baseline holds existing ones (§2.1, §16); no mode setting | `hard`, `ratchet` (adopter default), `advisory`, with a baseline file (§6.7) | Adopt on existing code without splitting functions to pass |
| Tiers | Tiers 0–5 with a cadence per layer (§3); `gate` default `--max-tier 1` | Tiers 0–3 defined in §3.2 | Four cadences cover Go's layers: laws, merge gate, nightly, release |
| Classes | 11 in schema, 8 with profiles | 12 in schema, 5 with profiles (adds `tui-app`) | Hedronite ships TUIs and services |
| `check` | Validates `rust-tops.yaml` against the schema | Full JSON Schema validation | Same rule |
| Report | Optional report schema exists; CLI does not write it | Always writes `report.json` | Make results inspectable |
| `init` | Does not write the lock file | Writes the lock file | First gate run is already pinned |
| CI floor | The drop-in runs `cargo tops gate --max-tier 1` with `fetch-depth: 0`; mutation and Darwin lanes are overlay comments | Real jobs on tiers, off only by config | Unrun layers prove nothing |
| Missing tool | Missing mapped tool fails gate | Same; tools are resolved via `tool` directive + lock | Hermeticity |
| Toolchain pin | `rust-toolchain.toml`, MSRV | `go` + `toolchain` directives, language-floor job | Go equivalent |
| Reproducible env | Nix flake | Optional Nix flake (later) | Lower priority |

---

## 15. What you get / what you do not get

### 15.1 What you get

| | Go alone | + Go-TOPS |
|---|---|---|
| Format, vet, lint | ✓ (manual) | pinned, config included |
| Race-checked shuffled tests | manual | enforced |
| Merged unit + integration coverage | manual | automated, non-empty enforced |
| Complexity-weighted risk (CRAP) | ✗ | native, capped per class |
| Diff-scoped mutation testing | ✗ | adapter + survivor ledger |
| Goroutine-leak and concurrency discipline | convention | laws + `goleak`/`synctest` layers |
| Reachability-based vulnerability gate | manual | enforced |
| Machine-readable gate report | ✗ | `report.json` |
| Agent contract | ✗ | `AGENTS.md` + policies |

### 15.2 What you do not get (v0.1 honesty)

- No built-in test engine; Go-TOPS orchestrates existing tools.
- Only a minority of laws are judged mechanically; the rest report `not-evaluated`.
- No formal-verification layer (Kani analogue); it is not a Go-TOPS layer.
- Mutation testing quality depends on the chosen engine; engines are pre-1.0 or fork-maintained
  and may change flags between releases.
- Only five of twelve classes have profiles at launch.
- Coverage statistics reflect statement coverage; Go has no native branch coverage.
- No Windows/macOS-specific law coverage at v0.1; matrix jobs cover builds and tests only.
- Published `examples/` are not packaged as importable modules.

---

## 16. Decisions (resolved for v0.3.0; remaining calibration items noted)

1. **Complexity rule (`err_guard_discount`):** **true** in every class, for guards that only return the error (wrapped or not). errcheck `check-blank` and `check-type-assertions` are required, so discarding the error is not cheaper than handling it. Explicit in every profile. (§6.3) *Changed in 0.3.0; 0.2.0 turned it off for `library-core` and `concurrency-runtime`.*
2. **Authorship marking:** None. One CRAP cap per class for every author. (§5.2, §6.2) *Changed in 0.3.0; 0.2.0 used an `Authored-By: agent` trailer that raised the cap.*
3. **Mutation engine:** Pluggable adapter. Start with gremlins (or strongest existing with diff filtering). Plan thin in-house `-overlay` runner later for full control. Not blocking v0.1/v0.2.
4. **Formal layers:** No Bend2 parity layer and no Lean recheck. The native analyzers are the laws layer. Reconsidered only with evidence that a formal layer judges something the analyzers do not. (§8.1) *Changed in 0.3.0.*
5. **Thresholds:** Remain **proposed**. Calibrate on three real Hedronite repos (CLI, TUI, service) before protocol 1.0. (§5.2)
6. **`parser-codec` priority:** High priority immediately after the five launch classes.
7. **Laws numbering:** Freeze IDs at 1.0. Retire rather than renumber. Prefix `G-NN`.
8. **Report format extras:** Defer SARIF/JUnit to M7 or later. `report.json` sufficient for v0.2.
9. **Distribution:** Module + `go install` / `go tool` first. GoReleaser binaries and Nix flake later (M7).
10. **Naming:** Confirmed **Go-TOPS** / `gotops` / `github.com/Hedronite/go-tops`.
11. **Enforcement:** `enforce` is `hard`, `ratchet` or `advisory`. Adopters default to `ratchet`; Go-TOPS runs `hard` on itself. (§6.7) *New in 0.3.0.*
12. **Race and cgo:** Test jobs run with `CGO_ENABLED=1` because `-race` requires cgo on Linux. Builds and releases stay `CGO_ENABLED=0` outside `cgo-bridge`. (§3.1, G-26) *New in 0.3.0.*
13. **Unmapped layers:** An enabled layer with no command is `not-run` and fails the gate. (§3.3, §7) *New in 0.3.0.*

---

## 17. Repository layout (proposed)

```
go-tops/
├── cmd/gotops/                  # CLI entry
├── internal/
│   ├── config/                  # go-tops.yaml load + schema validation
│   ├── gate/                    # layer runner, tiers, verdicts
│   ├── layers/                  # hygiene, tests, coverage, crap, mutation, fuzz, bench...
│   ├── cover/                   # profile parse/merge (unit + covdata)
│   ├── complexity/              # AST cyclomatic counting
│   ├── crap/                    # scoring + diff scoping
│   ├── mutate/                  # engine adapters + survivor ledger
│   ├── laws/                    # catalog loader
│   ├── analyzers/               # go/analysis analyzers (G-NN)
│   ├── lock/                    # go-tops.lock verify/update
│   └── report/                  # JSON/text/JUnit/SARIF
├── laws/                        # G-NN.yaml metadata
├── profiles/                    # class profiles
├── schema/                      # go-tops.schema.json, gate-report.schema.json
├── config/                      # drop-in templates (golangci, workflow, justfile, AGENTS.md)
├── examples/                    # one repo per launch class
├── docs/brand/
├── GO_TOPS.md                   # the protocol (this spec, finalized)
├── POLICIES.md
├── AGENTS.md
├── CHANGELOG.md
├── go-tops.yaml                 # Go-TOPS gates itself
├── go-tops.lock
├── go.mod
├── LICENSE  LICENSE-APACHE  LICENSE-MIT
└── README.md
```

**Self-hosting:** Go-TOPS must pass its own gate at tier 1 before any release; its own CRAP
output and survivor ledger are published in CI artifacts.

---

## 18. JSON Schema sketch (`schema/go-tops.schema.json`)

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://hedronite.com/schemas/go-tops/0.3.0/go-tops.schema.json",
  "title": "Go-TOPS configuration",
  "type": "object",
  "required": ["protocol", "version", "module", "class"],
  "additionalProperties": false,
  "properties": {
    "protocol": { "const": "0.3.0" },
    "version":  { "type": "string", "minLength": 1 },
    "module":   { "type": "string", "minLength": 1 },
    "enforce":  { "enum": ["hard", "ratchet", "advisory"], "default": "ratchet" },
    "baseline": { "type": "string", "minLength": 1 },
    "class": {
      "enum": ["library-core", "library-util", "parser-codec", "embedded-tinygo",
               "unsafe-kernel", "binary-cli", "tui-app", "binary-service",
               "concurrency-runtime", "cgo-bridge", "codegen", "workspace-root"]
    },
    "toolchain": {
      "type": "object",
      "properties": {
        "go_floor":   { "type": "string", "pattern": "^1\\.[0-9]+(\\.[0-9]+)?$" },
        "go_current": { "type": "string", "pattern": "^1\\.[0-9]+(\\.[0-9]+)?$" },
        "cgo":        { "type": "boolean" }
      }
    },
    "gates": {
      "type": "object",
      "properties": {
        "coverage": {
          "type": "object",
          "properties": { "lines_floor": { "type": "number", "minimum": 0, "maximum": 100 } }
        },
        "crap": {
          "type": "object",
          "properties": {
            "cap":   { "type": "number", "minimum": 1 },
            "scope": { "enum": ["diff", "all"] },
            "complexity": {
              "type": "object",
              "properties": { "err_guard_discount": { "type": "boolean" } }
            }
          }
        },
        "mutation": {
          "type": "object",
          "properties": {
            "engine": { "enum": ["gremlins", "go-mutesting", "mutest", "overlay"] },
            "scope":  { "enum": ["diff", "all"] },
            "unexplained_survivors_max": { "type": "integer", "minimum": 0 }
          }
        }
      }
    }
  },
  "allOf": [
    {
      "if":   { "properties": { "class": { "enum": ["library-core", "concurrency-runtime"] } } },
      "then": { "properties": { "gates": { "properties": { "mutation": {
                 "properties": { "unexplained_survivors_max": { "const": 0 } } } } } } }
    }
  ]
}
```

(Abbreviated; the full schema covers every key in §9.)

---

## 19. Roadmap

| Milestone | Contents | Exit criterion |
|---|---|---|
| **M0** spec | This doc + schemas + profiles for five classes | Reviewed; thresholds marked for calibration |
| **M1** CLI skeleton | `init`, `check`, `gate` (tier 0–1: hygiene, tests, coverage, `report.json`) | Self-hosting green |
| **M2** CRAP + mutation | Native CRAP; one mutation engine adapter; survivor ledger | Gate fails on a deliberately weak test |
| **M3** laws v0.1 | 18 laws judged (native); `law-diff` CI | Report shows "18 of 43 judged" |
| **M4** deep layers | Fuzz, goleak, synctest, benchstat; tier 2 jobs | Nightly workflow green on examples |
| **M5** release tier | Reproducible build, binary `govulncheck`, API-compat | Tier 3 passes on a tagged release |
| **M6** protocol 1.0 | Calibrated thresholds; frozen law IDs; five classes proven on real repos | Three Hedronite repos adopt it |
| **M7** extras | `parser-codec` profile, SARIF/JUnit, Nix flake | As prioritized |

---

## 20. Contributing, credits, license

- **Contributing:** read this spec and the schemas first; follow the policies in §13 and
  `AGENTS.md`; do not weaken laws; protocol-affecting PRs carry the `protocol-change` label.
- **Credits:** Hedronite. Structure and many rules adapted from Rust-TOPS. CRAP is credited to
  the 2007 "Change Risk Anti-Patterns" metric (Savoia and Evans).
- **License:** MIT OR Apache-2.0 (`LICENSE-MIT`, `LICENSE-APACHE`), matching Rust-TOPS.

---

## Appendix A: Gate order at a glance

```
tier 1 (merge gate)
  1 hygiene   → mod tidy -diff · mod verify · fmt · vet · lint · govulncheck · deps
  2 tests     → gotestsum -race -shuffle -count=1 (+ Examples)
  3 coverage  → merged profile · test -s · floors
  4 crap      → native CRAP · one cap per class · ratchet vs baseline
  5 mutation  → diff-scoped · unexplained survivors ≤ max
  6 class     → fuzz seed replay · goleak · synctest · alloc checks
tier 0 (executes last)
  ★ laws      → G-NN analyzers · Refuse fails · not-evaluated ≠ pass
tier 2 (nightly)  timed fuzz · full mutation · race stress · bench regression · full matrix
tier 3 (release)  reproducible build · binary govulncheck · SBOM/sig · API compat
```

## Appendix B: Sources

The Rust-TOPS repository, the tool versions and the Go 1.27 APIs were read on 2026-11-17 (Go APIs against go1.27.4); other pages are cited as in 0.3.0.

- Rust-TOPS repository (<https://github.com/Hedronite/rust-tops>), `main` at `77056c9`; latest release tag `v0.3.0` (2026-11-15)
- Go coverage for integration tests (<https://go.dev/doc/build-cover>)
- Go `tool` directive proposal (<https://go.googlesource.com/proposal/+/master/design/48429-go-tool-modules.md>)
- `testing/synctest` (<https://pkg.go.dev/testing/synctest>)
- gocyclo counting rules (<https://pkg.go.dev/github.com/fzipp/gocyclo>)
- Mutation tools: gremlins v0.6.0, 2025-12-06 (<https://github.com/go-gremlins/gremlins>);
  go-mutesting, maintained fork `avito-tech/go-mutesting` v2.3.1, 2025-12-26
  (<https://github.com/avito-tech/go-mutesting>); mutest (<https://github.com/fchimpan/mutest>);
  ooze (<https://github.com/gtramontina/ooze>)
- CRAP tools: crap4go (<https://github.com/unclebob/crap4go>)
- golangci-lint v2.15.0, 2026-10-16 (<https://golangci-lint.run>; release notes on
  <https://github.com/golangci/golangci-lint/releases>)
- Go 1.27 APIs named in the template: `synctest.Sleep`
  (<https://pkg.go.dev/testing/synctest#Sleep>), `httptest.NewTestServer`
  (<https://pkg.go.dev/net/http/httptest#NewTestServer>), the `goroutineleak` profile
  (<https://pkg.go.dev/runtime/pprof>)
