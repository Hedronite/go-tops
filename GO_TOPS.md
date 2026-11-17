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

- Protocol: **Go-TOPS**. Binary: **`gotops`**. Repo: `github.com/Hedronite/go-tops`.
- Go has no `cargo`-style external-subcommand mechanism, so there is no `go tops`. `gotops` is a
  module tool, pinned in `tools/go.mod` with every other tool (§10), so the adopter's own `go.mod`
  carries no tool dependency:
  `go get -tool -modfile=tools/go.mod github.com/Hedronite/go-tops/cmd/gotops@<ver>`, then
  `go tool -modfile=tools/go.mod gotops gate`. The `just` recipes in the drop-in hide the
  `-modfile` flag. `go install` also works, for trying the tool; a gate runs the pinned build.

---

## 2. Pins

| Item | Value |
|---|---|
| Protocol version | **0.4.0-dev** (in progress; 0.3.0 was the last draft; 1.0.0 is set by the criterion in §19) |
| Package version | 0.0.0 (unreleased) |
| Language floor ("MSRV" analogue) | Go **1.26**, the oldest Go release still supported upstream (a policy: the floor moves when upstream drops support). The floor is a language version, compared in Go's own order (`go/version.Lang`), so a `go 1.26` and a `go 1.26.4` directive both meet it. Matrix tests floor and current (current: Go 1.27) |
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

Gates run in this order. How a run proceeds past a failing gate, and which gates need which, is stated in §3.5.

| # | Gate | Command(s) | Pass condition |
|---|---|---|---|
| 1 | **Hygiene** | `go mod tidy -diff`; `go mod verify`; `go env GOVERSION` equals the pinned toolchain (§2); `golangci-lint fmt --diff`; `golangci-lint run` as the adopter-style pass (the adopter's `.golangci.yml`, JSON saved to `.go-tops/lint-style.json`); `govulncheck -json ./...`; dependency policy (`depguard` in that lint pass; `go-licenses check` for the license allow list) | All clean; no reachable vulnerability |
| 2 | **Tests** | **One run** feeds gates 2, 3 and 4: `CGO_ENABLED=1 go test -json -race -shuffle=on -count=1 -coverprofile=.go-tops/cover.out -coverpkg=./... ./...` (includes `Example*` functions, the doc-test analogue). `-race` requires cgo on Linux; only darwin runs it without cgo | All pass; no race; at least one test ran |
| 3 | **Coverage** | the profile of that run, merged with integration data (`go build -cover`, `GOCOVERDIR`, `go tool covdata`); then **`test -s`** on the merged profile | Profile exists and is non-empty; class floors met, in statements (§6.1) |
| 4 | **CRAP** | native CRAP over the same profile + cyclomatic complexity (§6.2) | No production function above its cap (§6.2, §6.7) |
| 5 | **Mutation (diff)** | mutation engine restricted to lines changed against the merge base of `origin/main` (§6.4) | No unexplained survivor (`unexplained_survivors_max` is 0, §6.4) |
| 6 | **Class layers** | fuzz, leak detection, synctest-required tests, benchmark regression, cgo/unsafe checks, alloc budgets, as the class requires. Allocation counts (`testing.AllocsPerRun`, `allocs/op`) are exact and gate at tier 1; time per op gates only at tier 2, on a named host, with `benchstat` significance | Per-class (§5) |
| ★ | **Laws** | Go-TOPS analyzers (§8) | No `refuse`; `not-evaluated` never counts as a pass |

★ The laws layer is **tier 0** (it defines the floor) but **executes last**, matching Rust-TOPS,
where the laws layer is tier 0 and runs after the mapped commands.

**One test run.** The suite runs once per gate run. Gate 2 parses the `go test -json` stream
natively: zero tests fails, skips are recorded, and so is the shuffle seed (the `-test.shuffle
<seed>` line each test binary prints). Gates 3 and 4 read the profile of the same run, so a
test cannot pass in one gate and be absent from another. With `-race` the cover mode is `atomic`.
`gotestsum` may wrap the same command for display; it is optional, and the gate reads the JSON,
not the wrapper's output.

**One lint pass per concern.** Gate 1 runs golangci-lint once, in the adopter's style, and saves
the JSON. There is no standalone `go vet`: golangci-lint's `govet` runs the same analyzers. The
laws layer runs its own pass with the kit's configuration (§8.1); the adopter's configuration
never reaches a law.

**The vulnerability layer is time-dependent.** `govulncheck` exits 0 with `-json` whatever it
finds, so the gate judges the parsed findings. The report records the database timestamp
(`db_last_modified` in the `config` message), and the layer is marked time-dependent: the same
commit can pass today and fail tomorrow, because the database moved. `-db <url>` points
`govulncheck` at a mirrored database, for a run that must be repeatable.

Step 6 mirrors Rust-TOPS's step 6 ("property tests, fuzz, Miri, Kani, Callgrind when the crate
class requires them") with Go-appropriate equivalents (§7).

### 3.2 Tiers

`gotops gate` runs every enabled layer **at or below `--max-tier`** (default **1**).

Rust-TOPS's public page states only that the laws layer is tier 0 and that the default max tier
is 1; it does not define the other tiers. Go-TOPS therefore **proposes** a definition:

| Tier | Purpose | Typical runtime | Contents |
|---|---|---|---|
| **0** | Floor, always on | seconds | Laws layer; config/schema validation |
| **1** | **Merge gate** (default) | minutes | Gates 1–5; class layers that are cheap (seed-corpus fuzz replay, leak checks, synctest tests) |
| **2** | **Deep / nightly** | tens of minutes to hours | Timed fuzzing; full-module mutation; long `-race -count=N` stress; benchmark regression vs baseline; full OS/arch/tag matrix |
| **3** | **Release** | variable | Reproducible-build verification; vulnerability re-scan of the artifact (`govulncheck -mode=binary`); SBOM and signature checks; API-compat check (`gorelease`/`apidiff`-class **[verify]**) |

### 3.3 Verdicts

Every layer and law produces one verdict:

| Verdict | Meaning | Effect |
|---|---|---|
| `pass` | Evaluated and satisfied | none |
| `Refuse` | Hard rule violated | **Fails the gate** |
| `Ack` | Soft rule: needs a recorded acknowledgement (inline `//tops:ack G-NN reason="..."` or ledger entry) | Fails the gate if unacknowledged |
| `skip` | The class does not require the layer, or `go-tops.yaml` sets it `enabled: false` | Printed with the reason; gate continues |
| `not-run` | The layer is enabled but has no command mapped | **Fails the gate** |
| `not-evaluated` | Reader or tool could not judge | Reported; **never counts as a pass** |

Hard rules encode as `Refuse`; soft rules as `Ack`. This is the same model Rust-TOPS uses for its
laws.

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
   gate. A green run that measured nothing is a defect.
6. **No dry gates in CI.** `--dry-run` and any "names-only" mode must be refused when `CI=true`.

---

## 4. Quick start

### 4.1 Copy the files

For a new repo, copy the drop-in set and replace the placeholders:

| File | Purpose | Placeholders |
|---|---|---|
| `go-tops.yaml` | Protocol config (§9) | `{MODULE}`, `{CLASS}` |
| `go-tops.lock` | Pins for downloaded binaries and the Go toolchain (§10) | generated |
| `tools/go.mod`, `tools/go.sum` | Pins for every module tool, run with `go tool -modfile=tools/go.mod` (§10) | generated |
| `AGENTS.md` | Agent-facing commands and rules (template: `config/AGENTS.template.md`). `init` writes its block between `<!-- go-tops:begin -->` and `<!-- go-tops:end -->` | `{MODULE}`, `{CLASS}`, `{ENFORCE}`, `{COVERAGE_FLOOR}`, `{CRAP_CAP}`, `{SURVIVORS_MAX}` |
| `GO-TOPS.md` | Adoption record (§13) | generated |
| `.golangci.yml` | The adopter's golangci-lint v2 config, **style only**: no law reads it (§8.1). It carries errcheck `check-blank`, `check-type-assertions` and the same `exclude-functions` list as the law pass (§8.1), so the editor and the gate agree | `{MODULE}` |
| `.go-tops/baseline.json` | Ratchet baseline (§6.7); written by `gotops baseline`, not by hand | generated |
| `.gremlins.yaml` or engine config | Mutation settings **[verify]** | |
| `.github/workflows/go-tops.yml` | CI floor (§11) | `{GO_FLOOR}`, `{GO_CURRENT}`, `{PRIMARY_PKG}` |
| `.github/CODEOWNERS` | A fragment covering the files that can weaken the gate (§11) | `{OWNER}` |
| `go.mod` | The `go` line (and, where the repo uses one, the `toolchain` line) | `{GO_FLOOR}` |
| `justfile` | Front door recipes | |

### 4.2 Or install the orchestrator

```bash
mkdir tools && go mod init -modfile=tools/go.mod <module>/tools
go get -tool -modfile=tools/go.mod github.com/Hedronite/go-tops/cmd/gotops@<ver>
go tool -modfile=tools/go.mod gotops init --class binary-cli   # writes the drop-in set
go tool -modfile=tools/go.mod gotops check                     # validates config
go tool -modfile=tools/go.mod gotops gate                      # runs the gates (tier ≤ 1)
```

`init` writes the drop-in set above, pins the tools in `tools/go.mod`, and generates
`go-tops.lock`, so the first `gate` is already pinned. It writes no laws config: the kit embeds
it (§8.1). Where `AGENTS.md` already exists, `init` touches only the block between its markers
(§12.1).

### 4.3 Nix (optional)

An optional flake overlay exposes `gotops` and the analyzer bundle, and a `nix flake check`
verifies the law catalog names. Low priority; ship after the CLI is stable (§15).

---

## 5. Classes and profiles

A **class** describes what kind of Go code a module is. The class selects the profile: floors,
required layers, and CRAP caps. A repo declares one class per module (workspace roots use
`workspace-root` and delegate to member modules).

### 5.1 Class catalog

The schema permits twelve classes. **Launch profiles** ship for the three classes the Hedronite
repos are: `binary-cli`, `binary-service` and `library-core`. The other nine, `tui-app` and
`concurrency-runtime` among them, are schema-valid but profile-less until a repo needs one; such
a repo sets `class` and states every threshold in its own `go-tops.yaml`.

| Class | Typical code | Launch profile? |
|---|---|---|
| `library-core` (default) | Reusable library with a stable API | **Yes** |
| `binary-cli` | Command-line programs | **Yes** |
| `tui-app` | Bubble Tea (or similar) terminal programs | No (schema-valid; written when a repo needs it) |
| `binary-service` | HTTP/gRPC services, exporters, daemons | **Yes** |
| `concurrency-runtime` | Schedulers, pools, pipelines, queue/worker frameworks | No (schema-valid; written when a repo needs it) |
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
| Line coverage floor | 85% | 70% | 65% (model/update logic) | 75% | 90% |
| CRAP cap | 4 | 6 | 6 | 5 | 4 |
| `unexplained_survivors_max` | **0** | 2 | 2 | 1 | **0** |
| Race detector | required | required | required | required | required (+ stress) |
| Leak detection (`goleak`) | if goroutines | if goroutines | required | required | required |
| `synctest` for time/concurrency | if applicable | if applicable | if timers | if timers | required |
| Fuzz targets | for exported decoders | for flag/arg parsers | for input parsing | for request decoders | for queue/codec boundaries |
| Benchmark regression gate | tier 2 | no | no | tier 2 | tier 2 |
| API-compat check | tier 3 | no | no | no | tier 3 |
| Extra laws | G-21–G-24 | G-41 | **G-10** | G-19, G-20 | G-01–G-09 |

There is one CRAP cap per class. It does not depend on who wrote the code: the gate is the same
for humans and agents (§1.1). Under `ratchet` (§6.7) the cap applies to new functions; existing
functions must not get worse. The `binary-cli` cap of 6 matches Rust-TOPS's `binary-cli` human
cap; the other values are new.

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

- Unit: **statements**, not lines. Go's profile counts statements (`NumStmt` per block), and
  every coverage figure, floor and key in this spec is a percentage of statements. Go has no
  native branch coverage (§15.2).
- Source: the profile of the one test run (§3.1: `-coverprofile` with `-coverpkg=./...`), merged
  with integration runs (`go build -cover` + `GOCOVERDIR` + `go tool covdata merge/textfmt`).
  With `-race` the cover mode is `atomic`. The recipe for merging a `go test` profile with
  `GOCOVERDIR` data is **[verify]** at M1; the route to test is
  `go test -cover ... -args -test.gocoverdir=DIR`, then `covdata merge`, then `textfmt`.
- Floors per class (§5.2) live in `go-tops.yaml` as `gates.coverage.statements_floor`.
- Gate fails when the profile is missing or empty (`test -s`), or holds no block of a production
  file (§3.4, rule 5).
- Coverage is computed on the module's own packages; generated files (`*.gen.go`,
  `// Code generated ... DO NOT EDIT.`) are excluded by default, with an explicit allowlist for
  inclusion.

Keys (G7 folds this table into §9 and the schema, then deletes it):

| Key | Change |
|---|---|
| `gates.coverage.lines_floor` | **Renamed** `gates.coverage.statements_floor`. The report's `lines_pct` becomes `statements_pct` |

### 6.2 CRAP

```
CRAP(f) = comp(f)² × (1 − cov(f))³ + comp(f)          cov(f) ∈ [0,1]
```

- `comp(f)` = cyclomatic complexity of function `f` computed from the AST.
- `cov(f)` = statement coverage of `f` from the merged profile.
- **Cap:** one per class (§5.2), the same for every author. Drop-in default: **6.0**. There is
  no authorship marking: a marker that changes the cap is a marker an agent can choose.
- **Enforcement:** under `hard`, every function in scope meets the cap. Under `ratchet`, new
  functions meet the cap and no existing function's score rises above its baseline (§6.7).
- **Scope:** the gate evaluates functions touched in the diff. A `--all` mode reports the whole
  module and is used in tier 2 for trend tracking.
- **Implementation:** native (not shelled out), so the formula, counting rule, and thresholds are
  protocol-controlled. Existing tools (`crap4go`, `gaze`) are useful cross-checks **[verify]**.

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

- the condition is `err != nil` (or `!errors.Is`/`errors.As` on the same error) for an error
  returned by the statement just before it or in its init clause;
- the body is a single `return` whose error result is that error, or that error wrapped with
  `fmt.Errorf("…%w…", err)` or a constructor that wraps it;
- there is no `else`.

A guard that logs and returns `nil`, swallows the error, or does other work is counted normally
(and `nilerr` flags the swallowing case).

The discount only makes sense if discarding the error is caught. The drop-in `.golangci.yml`
therefore sets errcheck `check-blank: true` and `check-type-assertions: true`, and G-12 reports
`not-evaluated` if those settings are off (§8.1, readers).

Keep the key explicit in every config so scores are comparable across repos.

### 6.4 Mutation

- Engine: pluggable adapter (`gremlins`, a `go-mutesting` fork, `mutest`, or an in-house
  `-overlay` runner) **[verify each]**. The adapter must support restricting mutation to changed
  lines or files; if the chosen engine cannot, Go-TOPS filters to changed files and post-filters
  survivors to changed lines.
- Diff base: `origin/main...HEAD` (three-dot, merge-base semantics), overridable with `--base`.
- **Survivor accounting:** each surviving mutant is either killed by a new test or listed in
  `.go-tops/survivors.yaml` with `reason`, `owner`, and an optional `expires` date. Only
  *unexplained* survivors count against `unexplained_survivors_max`.
- `library-core` and `concurrency-runtime` require **0** unexplained survivors, mirroring
  Rust-TOPS's `library-core` rule.
- Tier 2 runs the whole module on a schedule; engine run time can reach hours on large modules.

### 6.5 Law verdicts

`Refuse` (fails), `Ack` (soft; needs acknowledgement), `not-evaluated` (never a pass). The report
lists, per law, its reader (§8.2) so a reader can see what was actually judged.

### 6.6 Gate report

Unlike Rust-TOPS, whose CLI prints text and does not write the optional report object that its
schema describes, `gotops gate` **always** writes `.go-tops/report.json` (and prints text). See
§12.

### 6.7 Enforcement modes

The top-level key `enforce` sets how floors and caps are applied. Adopters default to
`ratchet`. Go-TOPS runs `hard` on itself.

| Mode | What fails |
|---|---|
| `hard` | Any function in scope above the CRAP cap; coverage below the floor; any lint finding |
| `ratchet` | A new function above the cap; an existing function whose CRAP rises above its baseline; a package whose coverage falls below its baseline (or below the floor, once it has reached it); a per-linter, per-file finding count that rises; an `Ack` count that rises |
| `advisory` | Nothing. Needs a dated exemption in `go-tops.yaml`; the report prints the run as not enforced |

Baseline rules:

- The baseline is `.go-tops/baseline.json`: CRAP per function, coverage per package, finding
  counts per linter per file, and the ack count.
- Only `gotops baseline` writes it, and only after a run in which every layer that feeds it
  passed. The file is tracked and must be clean against `HEAD`.
- Under `ratchet`, a missing baseline fails. Deleting the file is not a reset.
- A function is identified by package path, receiver and name. A renamed function is a new
  function. A new file starts at 0 findings; a deleted file's allowance does not move to
  another file.
- Lint counts are per linter, per file, not per module, so removing a finding in one file does
  not pay for adding one in another.
- Legacy findings stay in the baseline file. They are not wrapped in `//nolint` or
  `//tops:ack` to zero the count.

---

## 7. Class layers: Go equivalents of Rust's deep tools

Rust-TOPS lists fuzz, Miri, Kani, loom, sanitizers, and Callgrind. Go has different failure
modes and different tools:

| Rust-TOPS layer | Go-TOPS layer | Tool | Tier |
|---|---|---|---|
| Property tests | Property tests | `rapid` / `gopter` **[verify]** | 1 |
| Fuzz | Native fuzzing: replay seed corpus (tier 1), timed fuzz (tier 2) | `go test -fuzz` | 1 / 2 |
| loom (concurrency model checking) | Deterministic concurrency tests; leak checks | `testing/synctest`; `goleak` **[verify]** | 1 |
| Miri / sanitizers (UB detection) | Race detector (also enables pointer-safety checks); stress repetition | `go test -race -count=N`; `-msan`/`-asan` for cgo classes | 1 / 2 |
| Kani (formal) | *No Go equivalent.* Not a Go-TOPS layer, so it does not appear in config or the report | n/a | n/a |
| Callgrind (perf) | Benchmark regression vs baseline; allocation budgets | `testing.B` (+ `b.Loop`), `benchstat`, pprof | 2 |
| `cargo deny` | Dependency policy + vulnerabilities | `govulncheck`, `depguard`, license scan **[verify]**, `go mod verify` | 1 |
| MSRV job | Language-floor job: build and test on `go` floor | CI matrix | 1 |
| Feature matrix | Build-tag × `CGO_ENABLED` × OS/arch matrix | CI matrix | 2 |

**An enabled layer with no command is `not-run` and fails the gate**, and **a mapped tool that
is missing fails the gate**. A layer drops out of a run only when the class does not require it
or `go-tops.yaml` sets it `enabled: false`; either way it prints `skip` with the reason. `init`
writes `enabled: false` for layers the class does not map, rather than leaving them enabled and
empty. Because Go-TOPS resolves tools through the `tool` directive and the lock file, "missing
tool" is rare and is a configuration error, not an environment accident.

---

## 8. The Laws layer

### 8.1 Model

Laws encode Go-specific hazards as named, numbered rules. Rust-TOPS 1.1.0 defines 53 laws
(H-01…H-53) in a Bend2-based layer where 15 have fact readers judged by `bend` and 38 report
`not-evaluated`. Go-TOPS uses the same honesty model (`refuse` fails, `ack` requires
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
  - `tool`: delegated to a named external tool, mapped into a verdict. A delegated linter runs
    in the law pass below, under the kit's own configuration, so the linter and every setting
    the law depends on are on by construction;
  - `attested`: judged by human/agent attestation (e.g., oracle-first); reported
    `not-evaluated` until attested;
  - `tbd`: not yet implemented; reported `not-evaluated`.
- **Honesty preserved.** A law with no fact reader (or missing tool) is reported `not-evaluated`, never as a pass.

**The law pass.** The laws layer runs its delegated laws as its own golangci-lint pass, apart
from the adopter-style pass of gate 1 (§3.1). `gotops` embeds the law configuration in the
binary and writes it to `.go-tops/laws.golangci.yml` on every run. The pass is

```bash
golangci-lint run --config .go-tops/laws.golangci.yml --enable-only <linters in the table below> \
  --output.json.path=.go-tops/lint-laws.json ./...
```

and each delegated law reads its findings from the saved JSON. The adopter's `.golangci.yml` is
style only: the law pass never reads it, so no setting in it can weaken a law, and a law cannot
go quiet because a linter was switched off, so `config_off` cannot arise for a delegated law.
The pass runs when the laws whose reader is `tool` are in range, which is tier 1 (§3.2). A pass
that did not run leaves its laws `not-evaluated` with reason `input_missing`; a result file that
is missing or does not parse fails the layer (§3.4, rule 5); a run with no findings still writes
a valid document. The pass is a second lint run over the
same packages and shares the analysis cache with gate 1; its cost is measured at M5.

**Law → linter → settings.** The settings belong to the kit. Changing one is a protocol change
(§13).

| Law | Linter | Kit-owned settings |
|---|---|---|
| G-02 | `govet` | `enable: [waitgroup]` |
| G-07 | `govet` | `enable: [copylocks]` |
| G-11 | `errorlint` | `errorf: true`, `asserts: true`, `comparison: true`. Matching on `err.Error()` is judged by a `gotops-laws` analyzer, not a linter |
| G-12 | `errcheck` | `check-blank: true`, `check-type-assertions: true`, `exclude-functions: [(io.ReadCloser).Close, (*os.File).Close]` |
| G-16 | `noctx`, `contextcheck` | defaults |
| G-18 | `bodyclose`, `sqlclosecheck`, `rowserrcheck` | defaults |
| G-37 | `gosec` | `includes: [G401, G404, G501, G505]` |

`govet` runs with `disable-all: true` and exactly the analyzers its rows name. The G-12
`exclude-functions` list is what gives a read-only `Close` a passing form: `defer r.Close()` on a
reader passes, while `_ = f.Close()` and an unchecked `Write` still fail. A file written to is
still checked by review (`AGENTS.md`). `init` writes the same list into the adopter's
`.golangci.yml`. The delegated laws that are not golangci-lint linters are judged in gate 1:
G-34 (`go mod tidy -diff`, `go mod verify`) and G-40 (`govulncheck`). Laws with an `analyzer`
reader (G-08, G-17, G-19, G-25, G-38) and the facts G-26 and G-33 have no row here because no
linter setting decides them.

**Every enabled law has a fixture.** A law that has no fixture is not enabled.

- A custom analyzer is tested with `analysistest` and `// want` comments in its `testdata/src/`.
- A delegated linter is tested with a fixture module under `testdata/lints/` that breaks each
  rule once, the violating line carrying `// want G-NN`. A test runs the law pass over that
  module and diffs the findings against the markers: a marked line with no finding, or a finding
  on an unmarked line, fails. This is what proves each row of the settings table fires.
- Bumping a linter's pin re-runs the fixtures. That is the upkeep, and the point.

### 8.2 Law catalog (draft: 41 laws, `G-01`…`G-41`)

Severity: **R** = Refuse, **A** = Ack. Reader: **an** = analyzer, **tl** = delegated tool,
**at** = attested, **tbd** = not yet implemented. "v0.1" marks laws targeted for the first
mechanical release. A retired law keeps its ID and its row, its Law cell begins `Retired`, and it
counts in the catalog but is neither active nor v0.1.

**Concurrency**

| ID | Law | Sev | Reader | v0.1 |
|---|---|---|---|---|
| G-01 | Every `go` statement has a documented owner and an exit path (context, done channel, or finite work) | A | an (heuristic) | |
| G-02 | `wg.Add` is never called inside the goroutine it counts | R | tl (vet family) | ✓ |
| G-03 | Retired: absorbed by G-33. The language floor G-33 checks is above 1.22, where loop variables became per-iteration | R | an | |
| G-04 | Channels are closed only by their owning sender, never by a receiver | A | an (heuristic) | |
| G-05 | No unbuffered send that can strand a goroutine on early return (leak pattern) | A | an + goleak | |
| G-06 | No mutex held across blocking I/O or channel operations | A | an (heuristic) | |
| G-07 | Sync primitives are never copied | R | tl (vet `copylocks`) | ✓ |
| G-08 | Tests never call `time.Sleep` to wait for concurrency; use `synctest` | R | an | ✓ |
| G-09 | Fire-and-forget work from a request uses `context.WithoutCancel`, a bound, and a shutdown path | A | an (heuristic) | |
| G-10 | (`tui-app`) The Bubble Tea model is mutated only inside `Update`; no model writes from goroutines or `tea.Cmd` closures | R | an | |

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

**Build and determinism**

| ID | Law | Sev | Reader | v0.1 |
|---|---|---|---|---|
| G-33 | The `go` directive's language version is at least the protocol floor, compared in Go's order (§2), and `go env GOVERSION` equals the pinned toolchain. A missing `toolchain` line is accepted when the `go` line names a patch release | R | an | ✓ |
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

**v0.1 target:** judge 16 laws mechanically (the ✓ rows: 16 active v0.1 laws; G-03 is retired
into G-33, and G-10 waits for the `tui-app` profile), mirroring Rust-TOPS's approach of shipping
a minority of laws with real readers and reporting the rest honestly as `not-evaluated`. The
report states the count, e.g., "16 of 41 laws judged": 16 active v0.1 laws out of 41 catalog
rows, retired rows included.

### 8.3 Law metadata

Each law lives in `laws/G-NN.yaml`:

```yaml
id: G-17
title: "No context.Background outside main, wiring, and tests"
severity: refuse      # refuse (catalog R) | ack (catalog A)
status: active            # active | retired | planned
classes: [library-core, binary-service, concurrency-runtime, binary-cli, tui-app]
reader: analyzer
analyzer: gotops/ctxroot
fixture: testdata/src/ctxroot
allow_paths: ["cmd/**/main.go", "internal/app/wire*.go", "**/*_test.go"]
rationale: >
  Detached contexts defeat cancellation and tracing; they hide leaks.
fix: "Accept a context parameter and pass it down."
```

**One applicability source.** A law applies to a class when the class is in the law file's
`classes` list. Nothing else says which laws apply: a profile does not list laws, and
`go-tops.yaml` does not enable or disable one. The report derives its counts from the catalog
and that list:

- `catalog`: every law row, retired ones included (41);
- `applicable`: laws whose `status` is `active` and whose `classes` holds the class;
- `judged`: applicable laws with a verdict other than `not-evaluated`.

The class profile's `laws.required` names applicable laws whose verdict must be judged (§3.3);
every id in it must be in the catalog and apply to the class. `fixture` names the fixture of the
law (§8.1); an `active` law without one is invalid.

Keys (G7 folds this table into §9 and the schema, then deletes it):

| Key | Change |
|---|---|
| `laws.enable` | **Removed** from profile and config. Applicability is the `classes` list of each law file |

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

## 10. Tool pins: `tools/go.mod` and `go-tops.lock`

Purpose: make the gate reproducible, with one pin mechanism for each kind of tool.

- **Module tools** (`gotops`, `gotestsum`, `govulncheck`, `benchstat`, the mutation engine,
  `go-licenses` v2.0.1) live in `tools/go.mod` as `tool` lines and are run with
  `go tool -modfile=tools/go.mod <name>`. That file and its `go.sum` are the lock for them, and
  the Go checksum database verifies them. The adopter's own `go.mod` and `go.sum` stay free of
  tool dependencies. `go-tops.lock` does not repeat these versions.
- **`go-tops.lock`** holds only what a module cannot: the downloaded binaries (golangci-lint,
  for one; its project advises binary installs), each with its URL template and SHA-256 per
  OS/arch, and the Go toolchain with its exact version.
- **`gotops lock --verify`** re-checks every pin. For a module tool it builds the binary and
  reads the module version with `go version -m` (a `go install` build reports its own version as
  `dev`, so `--version` text is never compared), then compares it with `tools/go.mod`. For a
  downloaded binary it compares the SHA-256, and for the toolchain it compares
  `go env GOVERSION`.
- **Trust model:** checksum-verified. Where an upstream release is mutable or unsigned, use
  trust-on-first-use and record that in the lock. `gotops lock --update` is a reviewed change,
  never an automatic one.

---

## 11. CI floor

The drop-in workflow `.github/workflows/go-tops.yml` (placeholders `{GO_FLOOR}`, `{GO_CURRENT}`,
`{PRIMARY_PKG}`) runs the same command a developer runs. It repeats no tool as a step: a job that
runs tools one by one can be green while the gate is red.

| Job | Trigger | Content |
|---|---|---|
| `gate` | pull request, push to `main` | Checkout with `fetch-depth: 0` (the mutation diff needs history, §6.4); the pinned toolchain; `go tool -modfile=tools/go.mod gotops gate` (tier ≤ 1); upload `.go-tops/` (`report.json` and the saved lint and coverage output) with `if: always()`. Legs: `ubuntu-latest` at `{GO_CURRENT}` (the full gate); `macos-latest` at `{GO_CURRENT}` (the full gate); `ubuntu-latest` at `{GO_FLOOR}` with `GOTOOLCHAIN` set to the floor release and `--only tests` (build and test on the language floor) |
| `deep` | schedule | `gotops gate --max-tier 2`: timed fuzzing, full-module mutation, long `-race -count=N` stress, benchmark time per op on a named host with `benchstat` significance, and the build-tag × `CGO_ENABLED=0/1` × GOOS/GOARCH matrix |
| `release` | tag | `gotops gate --max-tier 3`: reproducible-build verification, binary `govulncheck`, SBOM and signature check |

CI rules (carried from Rust-TOPS's lessons):

- CI **refuses** `--dry-run` and any names-only mode.
- The gate fails on an empty coverage profile or empty CRAP output (§3.4), and so does CI.
- **Every leg of `gate` is a required check.** The macOS leg is required wherever the kit runs on
  Macs, which is every repo whose developers or users run macOS: Darwin differs from Linux in
  cgo, paths and the toolchain, and a gate that only ran on Linux says nothing about the machine
  a developer uses. A repo that never runs on macOS drops the leg by a CODEOWNERS-reviewed edit
  of the workflow. There is **no Windows leg**: Go-TOPS judges no Windows-specific behavior, and
  a repo that ships on Windows adds its own leg.
- **No `law-diff` job in the adopter drop-in.** An adopter has no law files to diff. The risk on
  the adopter's side is an edit to the files that set the gate, so the drop-in ships a CODEOWNERS
  fragment covering `go-tops.yaml`, `.golangci.yml`, `.go-tops/`, `go-tops.lock`, `tools/` and
  `.github/workflows/go-tops.yml`. The Go-TOPS repo keeps its own `law-diff` check, which fails
  PRs that weaken or remove a law without a protocol-change label.
- A layer the class requires runs in these jobs; none ships as a commented-out job. A layer is
  off only by `enabled: false` with a reason (§7).
- Pin actions by commit SHA; minimal `permissions:`; cache `GOMODCACHE`/`GOCACHE` by `go.sum` and
  `tools/go.sum`.

---

## 12. The `gotops` CLI

### 12.1 Subcommands

| Command | Behavior |
|---|---|
| `init --class <c>` | Writes `go-tops.yaml`, `go-tops.lock`, `tools/go.mod` with its pins, the adopter's `.golangci.yml` (style only), the workflow, the CODEOWNERS fragment, `justfile` and the adoption record `GO-TOPS.md`. It writes no laws config: the kit embeds it. Where `AGENTS.md` exists it writes only between `<!-- go-tops:begin -->` and `<!-- go-tops:end -->`, replacing the block on a re-run and appending the pair when absent; one marker without the other is a configuration error. Where `AGENTS.md` is absent it creates the file with the block. Refuses to overwrite any other existing file without `--force`. |
| `check` | Validates `go-tops.yaml` against the **JSON Schema** (full validation, an improvement over Rust-TOPS's partial checks), plus semantic checks: protocol id, non-empty version, module name, `crap.cap ≥ 1`, `library-core`/`concurrency-runtime` keep `unexplained_survivors_max = 0`, language floor ≤ current, `advisory` carries a dated exemption, and under `ratchet` the baseline file exists and is clean against `HEAD`. |
| `baseline` | Writes `.go-tops/baseline.json` (§6.7) after a run in which every layer that feeds it passed. Refuses otherwise. The only command that writes the baseline. |
| `gate` | Runs enabled layers at or below `--max-tier` (default 1). Options: `--max-tier N`, `--dry-run` (refused in CI), `--base <ref>`, `--changed`, `--keep-going`, `--json`, `--only <layer>`. Missing mapped tool fails. Laws run last. |
| `laws [list \| run \| explain G-NN]` | Lists the catalog with readers, runs the laws layer alone, explains a law. |
| `report [--format text \| json \| junit \| sarif]` | Renders the last report. |
| `lock [--verify \| --update]` | Verifies or updates tool pins (§10). `--verify` reads each module tool's version with `go version -m`, compares each downloaded binary's SHA-256, and compares `go env GOVERSION` with the pinned toolchain. `--update` is a reviewed change, never run in CI. |
| `doctor` | Diagnoses environment: Go version against the floor (compared in Go's order, §2) and against the pinned toolchain, a C compiler (cgo is required for `-race`), tool presence, `GOFLAGS`, `GOTOOLCHAIN`, cgo state. |
| `survivors [list \| explain \| expire]` | Manages the mutation survivor ledger. |
| `version` | Protocol, package, and pinned-tool versions. |

### 12.2 Exit codes

| Code | Meaning |
|---|---|
| 0 | All evaluated layers passed (and no `Refuse`) |
| 1 | Gate failed (`Refuse`, unmet floor, unexplained survivor, unacknowledged `Ack`) |
| 2 | Config invalid or tool lock mismatch |
| 3 | Environment error (missing tool, unreadable repo) |
| 4 | Refused by policy (dry-run in CI) |

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
      "id": "tests", "tier": 1, "verdict": "pass",
      "details": { "packages": 41, "tests": 812, "race": true, "shuffle_seed": 91823 }
    },
    {
      "id": "coverage", "tier": 1, "verdict": "pass",
      "details": { "lines_pct": 78.4, "floor": 70, "profile_bytes": 48211 }
    },
    {
      "id": "crap", "tier": 1, "verdict": "Refuse",
      "details": { "cap": 6,
        "violations": [ { "func": "ParseSpec", "file": "internal/spec/parse.go:41",
                          "cc": 14, "cov_pct": 52.0, "crap": 35.7, "baseline": null } ] }
    },
    { "id": "mutation", "tier": 1, "verdict": "pass",
      "details": { "mutants": 63, "killed": 59, "survived_explained": 3, "survived_unexplained": 1, "max": 2 } }
  ],
  "laws": {
    "catalog": 41, "judged": 18, "not_evaluated": 23,
    "results": [ { "id": "G-17", "verdict": "pass", "reader": "analyzer" } ]
  },
  "summary": { "verdict": "fail", "failed": ["crap"] }
}
```

Report rules: every layer present; skipped layers carry `verdict: "skip"` with a reason; an
enabled layer with no command carries `verdict: "not-run"` and fails the run; `not_evaluated`
is counted separately and never folded into `pass`. A CRAP violation with `"baseline": null`
is a new function; otherwise `baseline` is the score it must not exceed.

---

## 13. Policies

Practice rules for humans and agents, enforced by review and, where possible, by the laws layer. They
live in this section and `init` writes them into the adopting repo's `AGENTS.md`; there is no
separate policies file:

1. Write the oracle from the contract before the code (G-28).
2. Never split a function only to lower its CRAP score (scar-guard).
3. Never weaken a law, linter config, or threshold to make a change pass. Change them through the
   protocol-change process: a PR that touches `laws/`, `schema/`, `profiles/`, or thresholds
   carries a `protocol-change` label and a changelog entry, and is reviewed by a protocol owner.
4. Fix schema first, then prose.
5. Explain every accepted mutation survivor (reason, owner, expiry).
6. No `//nolint` without a reason; no `//tops:ack` without a reason and an owner.
7. Agent-authored code meets the same gate and the same CRAP cap as human code.
8. Do not edit generated files; fix the generator.
9. Prefer deleting code to adding configuration.
10. Keep `AGENTS.md` current: it is the contract between the repo and its agents.

`AGENTS.md` (written by `init` between `<!-- go-tops:begin -->` and `<!-- go-tops:end -->`, so
existing agent instructions outside the markers survive) lists: the `just` recipes; the exact
`gotops` commands; the class and its floors; the laws that apply to the class; the survivor/ack
ledgers; and the rules above.

**The adoption record `GO-TOPS.md`.** Every adopting repo keeps a `GO-TOPS.md` at its root: the
one page a reader or an agent opens to learn what the repo adopted. It records:

1. that the repo adopts Go-TOPS, with a link to <https://github.com/Hedronite/go-tops>;
2. the protocol version adopted;
3. the `gotops` version and the pinned tool versions, from `tools/go.mod` and `go-tops.lock`;
4. the class and its thresholds, from `go-tops.yaml`;
5. how to run the gate locally;
6. where the baseline and the survivor ledger live, and how to refresh the baseline;
7. every exemption in force (§6.8);
8. known deviations from the protocol.

`init` writes it with items 2 to 4 and 6 filled from the config. Keep it current when the
config, an exemption or a ledger path changes. A record that disagrees with `go-tops.yaml` is
wrong; the config wins.

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
| Laws engine | Bend2 `LAWS.bend` (53 laws, 15 judged) | Native `go/analysis` multichecker (41 laws); no Bend or Lean layer | Analyzers judge the code itself; a proof over a fact table judges the table |
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
| Race-checked shuffled tests | manual | enforced, in one run that also feeds coverage and CRAP |
| Merged unit + integration coverage | manual | automated, non-empty enforced |
| Complexity-weighted risk (CRAP) | ✗ | native, capped per class |
| Diff-scoped mutation testing | ✗ | adapter + survivor ledger |
| Goroutine-leak and concurrency discipline | convention | laws + `goleak`/`synctest` layers |
| Reachability-based vulnerability gate | manual | enforced |
| Machine-readable gate report | ✗ | `report.json` |
| Agent contract | ✗ | `AGENTS.md` + policies |
| One gate, locally and in CI | manual | `gotops gate`, one CI job |

### 15.2 What you do not get (v0.1 honesty)

- No built-in test engine; Go-TOPS orchestrates existing tools.
- Only a minority of laws are judged mechanically; the rest report `not-evaluated`.
- No formal-verification layer (Kani analogue); it is not a Go-TOPS layer.
- Mutation testing quality depends on the chosen engine; engines are pre-1.0 or fork-maintained
  and may change flags between releases.
- Only three of twelve classes have profiles at launch (`binary-cli`, `binary-service`, `library-core`).
- Coverage statistics reflect statement coverage; Go has no native branch coverage.
- No platform-specific law coverage at v0.1: the gate runs on Linux and macOS and judges the same laws on both; there is no Windows leg.
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
| **M3** laws v0.1 | 18 laws judged (native); `law-diff` CI | Report shows "18 of 41 judged" |
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
