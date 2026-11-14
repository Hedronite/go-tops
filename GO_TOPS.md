# Go-TOPS: Go Testing and Optimization Protocol Suite

**Spec status:** Draft 0.3.0 · **Date:** 2026-11-14 · **Owner:** Hedronite
**Modeled on:** [Rust-TOPS](https://github.com/Hedronite/rust-tops) (protocol 1.1.0, `cargo-tops` 0.2.0)
**Companion doc:** `GO_BUILD_SUITE.md`

> **Changes from 0.2.0:** One CRAP cap per class; authorship marking removed (§5.2, §6.2).
> Test jobs run with cgo, because `-race` requires it on Linux (§3.1, G-26). An enabled layer
> with no command is `not-run` and fails the gate (§3.3, §7). The error-guard discount applies
> in every class and only to guards that return the error; blank discards are caught (§6.3).
> Enforcement modes, with `ratchet` the adopter default (§6.7). The Bend2 parity layer and Lean
> recheck are removed; the native analyzers are the laws layer (§8.1). Protocol version bumped.

> **Changes from 0.1.0 to 0.2.0:** Hybrid laws architecture (native `go/analysis` primary floor + optional Bend2 parity layer + Lean recheck of catalog and pure invariants). Open decisions in §16 resolved where consensus was reached (err_guard_discount defaults, authorship trailers, Bend optional, etc.). Formal layers guidance added. Protocol version bumped.

> **How to read this.** Go-TOPS reuses Rust-TOPS's *shape* (a written spec, a JSON Schema,
> per-class profiles, an orchestrator CLI, a CI floor, and a laws layer) and re-derives the
> *content* for Go. Every place this spec intentionally differs from Rust-TOPS is called out in
> §14 ("Deltas from Rust-TOPS"). Everything Rust-TOPS defines that is reused here is described
> from the public repo page as captured in November 2026; confirm against the repo before
> claiming parity. Values labeled **proposed** are design defaults to be tuned on real code.
> Items marked **[verify]** name Go tools whose current behavior must be confirmed at pin time.

---

## 1. What Go-TOPS is

**Go-TOPS** is "a protocol and drop-in kit for Go that is correct, dense, and fast." It has
three parts, mirroring Rust-TOPS:

1. **A written spec** (this document, destined to become `GO_TOPS.md`) defining gates, tiers,
   classes, thresholds, and rules.
2. **A JSON Schema** (`schema/go-tops.schema.json`, plus a gate-report schema) that makes the
   config and the report machine-checkable. When prose and schema disagree, fix the schema, then
   the prose.
3. **Per-class profiles** (`profiles/*.yaml`) that set floors and requirements for different
   kinds of Go code.

Plus a practice layer in `POLICIES.md` (human and agent working rules) and an orchestrator CLI,
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
| Protocol version | **0.3.0** (draft; becomes 1.0.0 when the gate runs through tier 1 on the five launch classes) |
| Package version | 0.0.0 (unreleased) |
| Language floor ("MSRV" analogue) | `go 1.26.0` in `go.mod`; matrix tests floor and current (current: Go 1.27) |
| Toolchain pin | `toolchain` directive in `go.mod` plus `GOTOOLCHAIN=local` in CI |
| Tool lock | `go-tops.lock` (see §10) |
| golangci-lint | v2 line; a version built with a Go at least as new as the project toolchain (v2.14.0 bundles gofumpt v0.12.0 and supports Go 1.27) |
| Other tools | `gotestsum`, `govulncheck`, `benchstat`, mutation engine, `goleak`: pin exact versions at adoption **[verify]** |

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
| `go-tops.lock` | Tool pins and checksums (§10) | generated |
| `AGENTS.md` | Agent-facing commands and rules (template: `config/AGENTS.md.tmpl`) | `{MODULE}`, `{CLASS}`, `{ENFORCE}`, `{COVERAGE_FLOOR}`, `{CRAP_CAP}`, `{SURVIVORS_MAX}` |
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

- Source: the merged profile from unit tests (`go test -coverprofile -coverpkg=./...`) and
  integration runs (`go build -cover` + `GOCOVERDIR` + `go tool covdata merge/textfmt`).
- Floors per class (§5.2) live in `go-tops.yaml`.
- Gate fails when the profile is missing or empty (`test -s`).
- Coverage is computed on the module's own packages; generated files (`*.gen.go`,
  `// Code generated ... DO NOT EDIT.`) are excluded by default, with an explicit allowlist for
  inclusion.

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

### 8.2 Law catalog (draft: 41 laws, `G-01`…`G-41`)

Severity: **R** = Refuse, **A** = Ack. Reader: **an** = analyzer, **tl** = delegated tool,
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
`not-evaluated`. The report states the count, e.g., "18 of 41 laws judged."

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
| `baseline` | Writes `.go-tops/baseline.json` (§6.7) after a run in which every layer that feeds it passed. Refuses otherwise. The only command that writes the baseline. |
| `gate` | Runs enabled layers at or below `--max-tier` (default 1). Options: `--max-tier N`, `--dry-run` (refused in CI), `--base <ref>`, `--changed`, `--keep-going`, `--json`, `--only <layer>`. Missing mapped tool fails. Laws run last. |
| `laws [list \| run \| explain G-NN]` | Lists the catalog with readers, runs the laws layer alone, explains a law. |
| `report [--format text \| json \| junit \| sarif]` | Renders the last report. |
| `lock [--verify \| --update]` | Verifies or updates tool pins. |
| `doctor` | Diagnoses environment: Go version vs floor, tool presence, `GOFLAGS`, cgo state. |
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

| Area | Rust-TOPS (per public repo page) | Go-TOPS (this spec) | Why |
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
| Unmapped layer | Prints `skip`; gate continues | `not-run`; fails the gate | An enabled layer that ran nothing proves nothing |
| Enforcement | Absolute floors and caps | `hard`, `ratchet` (adopter default), `advisory` with a baseline file (§6.7) | Adopt on existing code without splitting functions to pass |
| Tiers | Only tier 0 (laws) and default max tier 1 are stated publicly | Tiers 0–3 defined (proposed) | Make nightly and release layers explicit |
| Classes | 11 in schema, 5 with profiles | 12 in schema, 5 with profiles (adds `tui-app`) | Hedronite ships TUIs and services |
| `check` | Does not validate against the schema | Full JSON Schema validation | Close a stated gap |
| Report | Optional report schema exists; CLI does not write it | Always writes `report.json` | Make results inspectable |
| `init` | Does not write the lock file | Writes the lock file | First gate run is already pinned |
| CI floor | Mutants/Darwin/fuzz jobs only as comments | Real jobs on tiers, off only by config | Unrun layers prove nothing |
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
| **M3** laws v0.1 | 18 laws judged (native); `law-diff` CI | Report shows "18 of 41 judged" |
| **M4** deep layers | Fuzz, goleak, synctest, benchstat; tier 2 jobs | Nightly workflow green on examples |
| **M5** release tier | Reproducible build, binary `govulncheck`, API-compat | Tier 3 passes on a tagged release |
| **M6** protocol 1.0 | Calibrated thresholds; frozen law IDs; five classes proven on real repos | Three Hedronite repos adopt it |
| **M7** extras | `parser-codec` profile, SARIF/JUnit, Nix flake | As prioritized |

---

## 20. Contributing, credits, license

- **Contributing:** read this spec and the schemas first; follow `POLICIES.md` and `AGENTS.md`;
  do not weaken laws; protocol-affecting PRs carry the `protocol-change` label.
- **Credits:** Hedronite. Structure and many rules adapted from Rust-TOPS. CRAP is credited to
  the 2007 "Change Risk Anti-Patterns" metric (Savoia and Evans).
- **License (proposed):** MIT OR Apache-2.0, matching Rust-TOPS.

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

- Rust-TOPS repository page (<https://github.com/Hedronite/rust-tops>), as read November 2026
- Go coverage for integration tests (<https://go.dev/doc/build-cover>)
- Go `tool` directive proposal (<https://go.googlesource.com/proposal/+/master/design/48429-go-tool-modules.md>)
- `testing/synctest` (<https://pkg.go.dev/testing/synctest>)
- gocyclo counting rules (<https://pkg.go.dev/github.com/fzipp/gocyclo>)
- Mutation tools: gremlins (<https://github.com/go-gremlins/gremlins>), go-mutesting v2 fork
  (<https://pkg.go.dev/github.com/jonbaldie/go-mutesting/v2>), mutest, ooze (ooze: <https://github.com/gtramontina/ooze>)
- CRAP tools: crap4go (<https://github.com/unclebob/crap4go>)
- golangci-lint v2 notes (<https://golangci-lint.run>; community setup write-ups)
