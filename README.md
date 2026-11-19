# Go-TOPS

**A protocol and drop-in kit for Go that is correct, dense, and fast.**
A written spec, a JSON Schema, per-class profiles, a laws layer of `go/analysis` checks, and a thin orchestrator (`gotops`) for the Go tools you already run.

[Protocol](GO_TOPS.md) · [Agent harness](AGENTS.md) · [Changelog](CHANGELOG.md)

## Status

Draft. Protocol **0.4.4**. The package is unreleased (0.0.0), and no tag exists.

Milestones M1 and M2 are done: the `gotops` CLI (`version`, `check`, `init`, `gate`, `report`, `lock`, `doctor`, `baseline`) judges hygiene, tests, coverage and native CRAP at tier 1, and under `ratchet` it holds a legacy repo to a debt ledger, `.go-tops/baseline.json`. This repo's own CI gates it with `go run ./cmd/gotops gate`. Go-TOPS runs `advisory` on itself until its laws layer lands at M5 (§6.7), and its CI asserts the gates that exist. The next milestone is M3: the first adopter, `vigil`, gated under `ratchet`. Mutation and the laws layer do not run yet, no adopter has used the kit, and nothing here is released or ready to copy into CI.

| Milestone | Contents |
|---|---|
| **M0, M0.5** spec | `GO_TOPS.md` 0.4.0, three schemas, profiles for three launch classes, a calibration record, docs CI |
| **M1** CLI (done) | `init`, `check`, `gate` at tier 1 (hygiene, tests, coverage), `report`, `lock`, `doctor`, `report.json`; Go-TOPS gates itself under the advisory bootstrap rule |
| **M2** CRAP and ratchet (done) | Native CRAP: complexity, guard-neutral coverage, exemptions; `baseline` and the ratchet judge; Go-TOPS gates itself with CRAP on |
| M3 (next) | First adopter: `vigil` gated under `ratchet` |
| M4 | Mutation: a gremlins adapter and the survivor ledger |
| M5 | Laws v0.1: 16 of 43 laws judged by analyzers; the kit switches its own gate to `hard` |
| M6–M7 | Deep layers and the second and third adopters; the release tier and protocol 1.0 |

The full roadmap is in [`GO_TOPS.md` §19](GO_TOPS.md#19-roadmap).

### What 0.4.0 changed

- The ratchet is a debt ledger: only what is over the line is recorded, counts only fall, and `baseline --init` writes the first ledger.
- CRAP covers every production function on every run; the caps are 10, 10 and 8, taken from a calibration record of five repos.
- Layers end in an outcome and laws in a verdict, with one exit table; a required law that is not judged fails.
- One test run, one law pass with a kit-embedded configuration, and a one-job CI drop-in.
- Schemas, launch profiles and examples are files, and docs CI keeps every example block identical to its file and valid against its schema.

The full list is in [`CHANGELOG.md`](CHANGELOG.md).

### What 0.3.0 changed

- One CRAP cap per class, with no authorship trailer.
- Test jobs run with cgo, because `-race` needs cgo on Linux.
- An enabled layer with no command fails the gate, instead of being skipped.
- The error-guard discount applies in every class, and blank error discards are caught.
- Adopters are gated by ratchet against a baseline file.
- No Bend2 or Lean layer; the `go/analysis` analyzers are the laws layer.

## What is here

| Path | What it is |
|---|---|
| [`GO_TOPS.md`](GO_TOPS.md) | The protocol, 0.4.4 |
| [`schema/`](schema/) | The JSON Schemas: configuration, gate report, tool lock, and the shared core definitions |
| [`cmd/gotops/`](cmd/gotops/), [`internal/`](internal/) | The `gotops` CLI: one file per command, and the layers, runner, lock, report and `init` packages behind them |
| [`go-tops.yaml`](go-tops.yaml), [`go-tops.lock`](go-tops.lock), [`tools/`](tools/) | Go-TOPS gating itself: its configuration, its tool pins and its pinned module tools |
| [`.github/workflows/ci.yml`](.github/workflows/ci.yml) | The one `gate` job: Linux, macOS and the language floor, then the report assertion |
| [`profiles/`](profiles/) | The launch profiles: `binary-cli`, `binary-service`, `library-core` |
| [`calibration/2026-11-probe/`](calibration/2026-11-probe/) | The calibration record behind the CRAP caps, with the probe and the script that reproduces it |
| [`scripts/docs/`](scripts/docs/) | The docs checker that docs CI runs |
| [`config/AGENTS.template.md`](config/AGENTS.template.md) | The `AGENTS.md` that `gotops init` will write into an adopting repo |
| [`AGENTS.md`](AGENTS.md) | Standing orders for agents working on this repo ([`CLAUDE.md`](CLAUDE.md) imports it) |

## Contributing

Read `GO_TOPS.md` and `AGENTS.md` first. Work on a branch and open a PR ready for review when the step is complete. A change to laws, schema, profiles, thresholds or normative spec text is a protocol change: it carries the `protocol-change` label, a changelog entry, and a protocol version bump.

## Credits

Built by [Hedronite](https://github.com/Hedronite). Structure and many rules are adapted from [Rust-TOPS](https://github.com/Hedronite/rust-tops) (protocol 1.2.0 on `main`; latest release tag `cargo-tops` v0.3.0). CRAP scores use the 2007 Change Risk Anti-Patterns formula (Savoia and Evans); this kit did not originate that metric.

## License

Licensed under either of

- Apache License, Version 2.0 ([LICENSE-APACHE](LICENSE-APACHE) or <http://www.apache.org/licenses/LICENSE-2.0>)
- MIT license ([LICENSE-MIT](LICENSE-MIT) or <http://opensource.org/licenses/MIT>)

at your option.

Unless you explicitly state otherwise, any contribution intentionally submitted for inclusion in this work, as defined in the Apache-2.0 license, shall be dual licensed as above, without any additional terms or conditions.
