# Go-TOPS

**A protocol and drop-in kit for Go that is correct, dense, and fast.**
A written spec, a JSON Schema, per-class profiles, a laws layer of `go/analysis` checks, and a thin orchestrator (`gotops`) for the Go tools you already run.

[Protocol](GO_TOPS.md) · [Agent harness](AGENTS.md) · [Changelog](CHANGELOG.md)

## Status

Draft. Protocol **0.3.0**. The package is unreleased (0.0.0). There is no code yet.

This repo is at milestone M0: the spec. `go tool gotops` does not exist yet, and nothing here is ready to copy into CI.

| Milestone | Contents |
|---|---|
| **M0** spec | `GO_TOPS.md`, schemas, profiles for five launch classes |
| M1 CLI | `init`, `check`, `gate` at tiers 0–1, `report.json`; Go-TOPS gates itself |
| M2 | Native CRAP, one mutation-engine adapter, survivor ledger |
| M3 | Laws v0.1: 19 of 41 laws judged by analyzers |
| M4–M7 | Deep layers, release tier, protocol 1.0, extras |

The full roadmap is in [`GO_TOPS.md` §19](GO_TOPS.md#19-roadmap).

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
| [`GO_TOPS.md`](GO_TOPS.md) | The protocol, draft 0.3.0 |
| [`config/AGENTS.template.md`](config/AGENTS.template.md) | The `AGENTS.md` that `gotops init` will write into an adopting repo |
| [`AGENTS.md`](AGENTS.md) | Standing orders for agents working on this repo ([`CLAUDE.md`](CLAUDE.md) imports it) |

## Contributing

Read `GO_TOPS.md` and `AGENTS.md` first. Work on a branch and open a draft PR. A change to laws, schema, profiles, thresholds or normative spec text is a protocol change: it carries the `protocol-change` label, a changelog entry, and a protocol version bump.

## Credits

Built by [Hedronite](https://github.com/Hedronite). Structure and many rules are adapted from [Rust-TOPS](https://github.com/Hedronite/rust-tops). CRAP scores use the 2007 Change Risk Anti-Patterns formula (Savoia and Evans); this kit did not originate that metric.

## License

Licensed under either of

- Apache License, Version 2.0 ([LICENSE-APACHE](LICENSE-APACHE) or <http://www.apache.org/licenses/LICENSE-2.0>)
- MIT license ([LICENSE-MIT](LICENSE-MIT) or <http://opensource.org/licenses/MIT>)

at your option.

Unless you explicitly state otherwise, any contribution intentionally submitted for inclusion in this work, as defined in the Apache-2.0 license, shall be dual licensed as above, without any additional terms or conditions.
