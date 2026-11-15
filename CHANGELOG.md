# Changelog

All notable changes to this project are listed here. The protocol version and the package version are separate (see `GO_TOPS.md` §2). Package versions follow [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Changed

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

- `GO_TOPS.md`: the Go-TOPS protocol, draft 0.2.0.
- `config/AGENTS.template.md`: the `AGENTS.md` that `gotops init` will write.
- `AGENTS.md` and `CLAUDE.md`: standing orders for agents working on this repo.
- Dual license, MIT OR Apache-2.0.
