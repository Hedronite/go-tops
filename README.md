# Go-TOPS

<p align="center">
  <img src="docs/brand/go-tops-banner.jpg" alt="Go-TOPS product mark and wordmark" width="720" />
</p>

<p align="center"><strong>A protocol and drop-in kit for Go that is correct, dense, and fast.</strong><br/>
A written spec, a JSON Schema, per-class profiles, and a thin orchestrator (`gotops`) for the Go tools you already run.</p>

<p align="center">
  <a href="https://go.dev"><img alt="Go 1.26 language floor" src="https://img.shields.io/badge/go-1.26-00ADD8?style=flat-square&logo=go&logoColor=white&labelColor=0A0A0A" /></a>
  <a href="https://github.com/Hedronite"><img alt="Hedronite" src="https://img.shields.io/badge/org-Hedronite-08ACF0?style=flat-square&logo=github&logoColor=white&labelColor=0A0A0A" /></a>
  <a href="GO_TOPS.md"><img alt="Protocol 0.4.2" src="https://img.shields.io/badge/protocol-0.4.2-08ACF0?style=flat-square&labelColor=0A0A0A" /></a>
  <a href="LICENSE"><img alt="License: MIT OR Apache-2.0" src="https://img.shields.io/badge/license-MIT%20OR%20Apache--2.0-08ACF0?style=flat-square&labelColor=0A0A0A" /></a>
  <a href="https://github.com/Hedronite/go-tops/actions/workflows/ci.yml"><img alt="CI workflow: ci" src="https://img.shields.io/github/actions/workflow/status/Hedronite/go-tops/ci.yml?branch=main&style=flat-square&label=ci&labelColor=0A0A0A&color=08ACF0" /></a>
  <a href="https://github.com/Hedronite/go-tops/actions/workflows/docs.yml"><img alt="Docs workflow" src="https://img.shields.io/github/actions/workflow/status/Hedronite/go-tops/docs.yml?branch=main&style=flat-square&label=docs&labelColor=0A0A0A&color=08ACF0" /></a>
</p>

<p align="center">
  <a href="#quick-start">Quick start</a> ·
  <a href="#what-you-get">What you get</a> ·
  <a href="#how-it-works">How it works</a> ·
  <a href="#status">Status</a> ·
  <a href="GO_TOPS.md">Protocol</a> ·
  <a href="AGENTS.md">Harness</a> ·
  <a href="CHANGELOG.md">Changelog</a>
</p>

<p align="center">
  Built by <a href="https://github.com/Hedronite">Hedronite</a>. Structure adapted from <a href="https://github.com/Hedronite/rust-tops">Rust-TOPS</a>.<br/>
  CRAP scores use the 2007 Change Risk Anti-Patterns formula. This kit did not originate that metric.
</p>

---

Go-TOPS (Go Testing and Optimization Protocol Suite) is a protocol: a written spec, a JSON Schema, per-class profiles, and the `gotops` CLI that runs the Go tools you already use. This repository is the public kit for that protocol.

It is for authors of Go libraries and binaries, and for the people who own their CI, who want one gate order: hygiene, tests, and coverage, then the later layers the class names once those layers land.

The package is unreleased (`0.0.0-dev`). There is no git tag and no module version to install. Run `gotops` from this tree. It is not yet a drop-in for someone else's CI.

## Pins

| Plane | Pin | Where |
| --- | --- | --- |
| Protocol | 0.4.2 | [`GO_TOPS.md`](GO_TOPS.md), [`go-tops.yaml`](go-tops.yaml), and the schema `protocol` constants |
| Package | 0.0.0-dev | `gotops version`. No tag exists. A release will set this at link time |
| Language floor | Go 1.26 | [`go.mod`](go.mod) and `toolchain.go_floor` in [`go-tops.yaml`](go-tops.yaml) |
| Toolchain | go1.27.4 | [`go-tops.lock`](go-tops.lock) |
| Launch classes | three | [`profiles/`](profiles/): `binary-cli`, `binary-service`, `library-core` |

## Quick start

From a clone of this repository:

```bash
go run ./cmd/gotops version
go run ./cmd/gotops check
go run ./cmd/gotops gate
```

`check` validates `go-tops.yaml` against the schema, then the semantic checks. `gate` runs hygiene, tests, and coverage at tier 1. This repository's CI runs `go run ./cmd/gotops gate` and then asserts the report.

`gotops init --class` accepts `binary-cli`, `binary-service`, or `library-core`.

## What you get

| | `go test` and linters alone | This kit |
| --- | --- | --- |
| Contract | Docs and tests you arrange | Protocol 0.4.2 and JSON Schemas for the config, the gate report, and the tool lock |
| Class floors | None | Three launch profiles |
| Orchestrator | You sequence the commands | `gotops` `version`, `check`, `init`, `gate`, `report`, `lock`, `doctor` |
| CI on this repo | — | One `gate` job (Linux, macOS, and the language floor), then a report assertion, plus docs CI |

### What you do not get

| Claim | Reality |
| --- | --- |
| A released module or tag | The package is `0.0.0-dev`. Nothing is published, and this kit is not ready to copy into someone else's CI |
| Native CRAP, the ratchet, or mutation | Those layers do not run yet. They are the next milestones |
| A laws layer that judges this repo | Analyzer laws land later. Until then this repository gates itself under `advisory`, and CI asserts the gates that exist |
| A test framework | `gotops` runs `go test` and the pinned tools; it does not replace them |

## How it works

Write the oracle from the contract, then the smallest production code that meets it. A test taken from the function body freezes current behavior, including bugs.

`gotops gate` runs enabled layers at or below `--max-tier` (default 1). Today that is hygiene, tests, and coverage. An enabled layer with no command is `not-run` and fails the gate. A layer that was not evaluated is reported as not evaluated, never as a pass.

| Path | Role |
| --- | --- |
| [`GO_TOPS.md`](GO_TOPS.md) | The protocol, 0.4.2 |
| [`schema/`](schema/) | JSON Schemas: configuration, gate report, tool lock, and shared core definitions |
| [`cmd/gotops/`](cmd/gotops/), [`internal/`](internal/) | The `gotops` CLI |
| [`go-tops.yaml`](go-tops.yaml), [`go-tops.lock`](go-tops.lock), [`tools/`](tools/) | This repository gating itself: configuration, toolchain pin, and module tools |
| [`.github/workflows/ci.yml`](.github/workflows/ci.yml) | The one `gate` job |
| [`profiles/`](profiles/) | Launch profiles |
| [`calibration/2026-11-probe/`](calibration/2026-11-probe/) | The calibration record behind the planned CRAP caps |
| [`scripts/docs/`](scripts/docs/) | The docs checker that docs CI runs |
| [`config/AGENTS.template.md`](config/AGENTS.template.md) | The `AGENTS.md` section `gotops init` will write into an adopting repo |
| [`AGENTS.md`](AGENTS.md) | Editing harness for this repository ([`CLAUDE.md`](CLAUDE.md) imports it) |

## Status

Draft. Protocol **0.4.2**. Package **0.0.0-dev**. No tag.

Milestone M1 is done: the `gotops` CLI judges hygiene, tests, and coverage at tier 1, and this repository gates itself with `go run ./cmd/gotops gate`. Go-TOPS runs `advisory` on itself until the laws layer lands. CRAP, mutation, and the laws layer do not run yet. No adopter has used the kit.

| Milestone | Contents |
| --- | --- |
| **M0, M0.5** spec | `GO_TOPS.md` 0.4.0, three schemas, profiles for three launch classes, a calibration record, docs CI |
| **M1** CLI (done) | `init`, `check`, `gate` at tier 1 (hygiene, tests, coverage), `report`, `lock`, `doctor`, `report.json`; Go-TOPS gates itself under the advisory bootstrap rule |
| M2 (next) | Native CRAP and the ratchet: complexity, guard-neutral coverage, the baseline ledger, exemptions |
| M3 | First adopter under the `ratchet` mode |
| M4 | Mutation: a gremlins adapter and the survivor ledger |
| M5 | Laws v0.1: 16 of 43 laws judged by analyzers; the kit switches its own gate to `hard` |
| M6–M7 | Deep layers and further adopters; the release tier and protocol 1.0 |

The full roadmap is in [`GO_TOPS.md` §19](GO_TOPS.md#19-roadmap).

## Contributing

Read [`GO_TOPS.md`](GO_TOPS.md) and [`AGENTS.md`](AGENTS.md) first. Work on a branch and open a PR ready for review when the step is complete. A change to laws, schema, profiles, thresholds or normative spec text is a protocol change: it carries the `protocol-change` label, a changelog entry, and a protocol version bump.

## Credits and license

Built by [Hedronite](https://github.com/Hedronite). Structure and many rules are adapted from [Rust-TOPS](https://github.com/Hedronite/rust-tops) (protocol 1.2.0). CRAP scores use the 2007 Change Risk Anti-Patterns formula (Savoia and Evans); this kit did not originate that metric.

Licensed under MIT OR Apache-2.0, at your option. See [`LICENSE`](LICENSE), [`LICENSE-MIT`](LICENSE-MIT), and [`LICENSE-APACHE`](LICENSE-APACHE).

Unless you explicitly state otherwise, any contribution intentionally submitted for inclusion in this work, as defined in the Apache-2.0 license, shall be dual licensed as above, without any additional terms or conditions.
