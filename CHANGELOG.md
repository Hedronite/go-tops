# Changelog

All notable changes to this project are listed here. The protocol version and the package version are separate (see `GO_TOPS.md` §2). Package versions follow [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Changed

- **Protocol 0.4.0** (`GO_TOPS.md`, `schema/`, `profiles/`, `config/`). R-numbers are findings of the 2026-11-16 assessment; X-numbers are the 2026-11-17 decisions.
  - Facts and pins:
    - R28: the header and §1 cite Rust-TOPS protocol 1.2.0 on `main` and the release tag `cargo-tops` v0.3.0; "destined to become" and "(proposed)" are removed; §14 is rechecked against `rust-tops` `main`, with five corrections and the tiers row.
    - R28 / Q12: the build-suite companion line is removed; Appendix B cites avito-tech go-mutesting v2.3.1.
    - R29: §2 pins golangci-lint v2.15.0, gotestsum v1.13.0, govulncheck v1.9.0, gremlins v0.6.0 and goleak v1.3.0, with benchstat `[verify]`; template G-08 names `synctest.Sleep` and `httptest.NewTestServer`.
  - Ratchet:
    - R1: a function at or under the cap may change up to the cap; one over it may not exceed its baseline plus 0.01; a function absent from the baseline is new and meets the cap.
    - R2: the baseline is a debt ledger of what is over the line; an empty ledger means `hard`, with suppression and exemption counts held at zero.
    - R3: a first-ledger rule for a diff base with no baseline; `baseline --init` measures without judging when tests are green and the feeding output is non-empty; later writes only lower; a raise needs `--accept-regression` and the `protocol-change` label; `baseline` is refused under `advisory`.
    - R4: per-law, per-file finding counts are ledgered, plus per-linter counts for findings no law maps.
    - R5: a package under its floor fails only if its percentage falls and its uncovered-statement count rises.
    - R6: per-file counts of `//nolint`, `//tops:ack` and `acks.yaml` entries are ledgered.
    - R8: the judged-law count may not fall.
    - R19: function identity covers repeated names; renames go through `git diff -M` or `baseline --rename`.
    - The exemption count per repo and the per-file tier 2 unexplained-survivor count are ledgered; a new `advisory` key (reason, owner, expiry); the template states the rule.
  - CRAP and mutation:
    - R15: CRAP covers every production function on every run; `scope: diff` is removed for CRAP (§6.2).
    - R16: guard-neutral coverage; the error guard is narrowed to `err != nil` with one `return` of `err` or `fmt.Errorf` with `%w`; a new `wrap_functions` key (§6.3).
    - R17: a new §6.8 Exemptions with a required expiry; expired or malformed records fail; the table-shaped `switch`; the exemption count is tied to the §6.7 ledger.
    - R18 / X2: caps `binary-cli` 10, `binary-service` 10 and `library-core` 8 (provisional), 8 (provisional) for classes without a launch profile, citing the calibration record; the cap is stated as a complexity ceiling (§5.2, §6.2).
    - R20: mutation statuses are mapped; a survivor has a key; the survivor ledger needs an expiry; `unexplained_survivors_max: 0` in every class.
    - R21 (§6.4 half): no merge base or a shallow clone fails; an empty diff is `vacuous`.
  - Verdicts, run order and exits:
    - R7: two vocabularies, layer outcomes and law verdicts with reason codes; one casing rule; `enabled: false` needs a reason; `ack` is defined against the §6.7 suppressions count; one exit table, with a missing tool exit 1 and a missing or dirty baseline, an expired `advisory` or a lock mismatch exit 2.
    - R7 / X3: a required law that is `not-evaluated` fails whatever the reason; other laws are counted; a required law above `--max-tier` is listed as out of range; a run that leaves one out ends `partial`, never `pass`, and is refused in CI (exit 4).
    - R9: every enabled layer in range runs, with dependency short circuits; `--fail-fast` is opt-in; fewer layers run than enabled fails; `--changed` and `--keep-going` are removed; §3.2 states what tier 0 judges.
    - R14: G-42 (leak check in `TestMain`) and G-43 (fuzz target per declared glob); §7 lists only command layers; the catalog goes from 41 to 43 laws.
    - R29 (§7 half): the `goroutineleak` profile for Go 1.27 and later, `goleak` for 1.26.
  - Mechanics:
    - R10: one test run, `go test -json -race -shuffle=on -count=1` with coverage, parsed natively; coverage in statements, with the key renamed `statements_floor`.
    - R11: one adopter-style lint pass and no standalone `go vet`; G-03 is retired into G-33.
    - R12, R13: a kit-embedded law pass with a law, linter and settings table; a fixture for every enabled law; applicability from each law's classes list.
    - R22, R21 (§11 half), X4: one gate job plus scheduled and tag jobs; a macOS leg required, no Windows leg; no `law-diff` in the adopter drop-in; `fetch-depth: 0`.
    - R23: module tools in `tools/go.mod`; `go-tops.lock` for downloaded binaries and the toolchain; `lock --verify` reads `go version -m`.
    - R24: the floor is a language version in Go's order; GOVERSION is checked against the pin; the toolchain line is optional.
    - R25: the vulnerability DB timestamp is recorded, the layer is marked time-dependent and `-db` is supported; the license tool is `go-licenses` v2.0.1.
    - R26: allocation counts at tier 1; time per op at tier 2 on a named host.
    - R27: launch profiles `binary-cli`, `binary-service` and `library-core`; G-10 is out of v0.1.
    - R30: `init` markers and a `GO-TOPS.md` adoption record; the errcheck `exclude-functions` is `(io.ReadCloser).Close`, and a read-only `*os.File` takes a counted `//nolint:errcheck` with a reason.
    - Counts: 16 active v0.1 laws of 43.
  - Schemas and profiles:
    - R31: `schema/go-tops.schema.json`, `schema/gate-report.schema.json` and `schema/core.schema.json` (JSON Schema 2020-12, strict objects, protocol constant 0.4.0, shared `$defs`); the three launch profiles; `config/go-tops.example.yaml` and `config/gate-report.example.json`; the §5.3, §9 and §12.3 blocks are marked and byte-identical to their files.
    - R31: one shape for profile and config; `enabled: false` needs a reason; `advisory` is conditional on `enforce: advisory`; `laws.enable` is removed; `laws.required` names applicable laws.
    - X1: the shared-core seam, `schema/core.schema.json` with a sync `$comment`, and the run outcome `pass`, `fail` or `partial`.
    - R10: `gates.coverage.statements_floor` replaces `lines_floor`, and the report's `lines_pct` is `statements_pct`.
    - R4 (report): catalog-level and class-level law counts, with `applicable_judged`.
    - R14: `layers.goleak`, `layers.synctest` and `layers.fuzz.replay_seed_corpus` are removed and `layers.fuzz.targets` is added.
    - R25: `gates.hygiene.govulncheck.db`. R10: `gates.tests.race`, `shuffle` and `count` are fixed by the schema. R31: `check` validates survivor and ack entries; the report types its layer details and lists every exemption in force.
    - R18 / A11: a class with no column takes cap 8 (provisional) and the strictest floor, and `check` rejects a config looser than its class.
    - A run must measure something: `hygiene`, `tests` and `coverage` cannot be off; `crap` and `mutation` may be off only with a reason and a dated expiry, and every disabled gate is listed in the report.
    - Keys removed: `gates.mutation.scope` goes alongside `gates.crap.scope` (tier 1 mutates the diff, tier 2 the whole module), and `laws.enable`, `layers.goleak`, `layers.synctest` and `layers.fuzz.replay_seed_corpus` go with them.
    - §16 records the 0.4.0 decisions; §17 shows the real tree; §18 points to the schema files; §19 is the plan of record with one 1.0 criterion; Appendix A shows one test run.
- **Protocol 0.3.0** (`GO_TOPS.md`, schema sketch, `config/AGENTS.template.md`):
  - One CRAP cap per class; the `Authored-By: agent` trailer and the separate agent cap are removed (§5.2, §6.2).
  - Test jobs run with `CGO_ENABLED=1`, because `-race` requires cgo on Linux; G-26 exempts test jobs (§3.1).
  - New verdict `not-run`: an enabled layer with no command fails the gate. Kani is no longer a skipped layer (§3.3, §7).
  - `err_guard_discount` defaults to `true` in every class and is defined narrowly. errcheck `check-blank` and `check-type-assertions` are required, and G-12 covers blank discards (§6.3).
  - New `enforce` key (`hard`, `ratchet`, `advisory`) with a `.go-tops/baseline.json` ratchet and a `gotops baseline` command. Adopters default to `ratchet` (§6.7, §12.1).
  - Laws with a `tool` reader report `not-evaluated` when the linter or setting they depend on is off (§8.1).
  - The Bend2 parity layer and Lean recheck are removed (§8.1, §17, §19).
  - The AGENTS template names the check behind each rule, drops `require.Eventually`, narrows the `Close` rule, pairs `io.ReadAll` with a bounded reader, and verifies with `go tool gotops gate` alone.
  - The CRAP value in the report example is corrected to 35.7 (CC 14, 52% coverage).
- The AGENTS template is renamed `config/AGENTS.template.md`, so it renders and counts as Markdown, and is not loaded as a nested `AGENTS.md` by agent harnesses.

### Added

- Docs CI (`.github/workflows/docs.yml`, M0.5-2): read-only, every action pinned by commit SHA, superseded runs cancelled, one aggregate `docs` check that fails unless every job succeeded; the markdown lint tool is pinned through its action's committed bundle.
- Docs checker (`scripts/docs/`, its own module with two direct dependencies): offline relative links and anchors; every count stated in `GO_TOPS.md` and `README.md` against the law catalog; `go-tops:example` blocks byte-identical to their files and valid against their schemas, with a YAML or JSON block in §5.3, §9 or §12.3 that lacks a marker failing; balanced and named `tops-core` markers; a reason on every disabled lint rule; the workflow floor; and one protocol version in the header, §2 and both schemas. Self-tests plant each failure.
- `.github/CODEOWNERS` on the protocol, schema, profiles, config, calibration, scripts and `.github`.
- Calibration record `calibration/2026-11-probe/` (M0.5-5, record half): five Hedronite repos at pinned commits, 761 functions, the functions-over-cap table by rule and cap, the per-repo table, the Go version and the gremlins trial counts, with the standard-library probe and `run.sh`, which reproduces `results.json` from clean clones. At cap 6, 199 of 761 functions fail the 0.3.0 rule and 156 of those are complexity alone; at cap 4, 298 fail.
- `GO_TOPS.md`: the Go-TOPS protocol, draft 0.2.0.
- `config/AGENTS.template.md`: the `AGENTS.md` that `gotops init` will write.
- `AGENTS.md` and `CLAUDE.md`: standing orders for agents working on this repo.
- Dual license, MIT OR Apache-2.0.
