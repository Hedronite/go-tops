# Go-TOPS adoption record

This repository adopts [Go-TOPS](https://github.com/Hedronite/go-tops). `go-tops.yaml` is the
authority: a line here that disagrees with it is wrong.

1. **Adoption.** Go-TOPS, the Go testing and quality protocol.
2. **Protocol version.** 0.4.3.
3. **`gotops` and pinned tools.** `gotops` v0.1.0. Pins: toolchain go1.27.4 and golangci-lint 2.15.0 (go-tops.lock); go-licenses v2.0.1, gotestsum v1.13.0, govulncheck v1.9.0 (tools/go.mod).
4. **Class and thresholds.** `binary-service`: statement coverage floor 75%, CRAP cap 10, `enforce: ratchet`.
5. **Run the gate locally.** `just gate`, or `go tool -modfile=tools/go.mod gotops gate`.
6. **Baseline and ledgers.** The baseline is `.go-tops/baseline.json`; only `gotops baseline` writes it, never by hand. It records the build it was measured under, and the run with that build is the ledger leg: this repository's is the `ubuntu` leg of `.github/workflows/go-tops.yml`, and CI fails when that leg reports a ratchet outcome of `skip`. The baseline commands run only on the ledger leg's build. Run the workflow by hand (`workflow_dispatch`) and pick `init` to write the first ledger with `gotops baseline --init`, or `refresh` to lower entries with `gotops baseline`; the ubuntu leg runs it and uploads the file as the `go-tops-baseline` artifact, which you commit. A refresh only lowers entries. Raising or adding an entry is manual, with `gotops baseline --accept-regression --reason <text> --owner <handle>`: run it locally on the ledger leg's build, which on another platform is a `linux/amd64` Docker container of `golang:1.27.4` with the repository mounted at `/repo`, `GOTOOLCHAIN=local`, and the pinned golangci-lint 2.15.0 on `PATH` (the workflow's install step shows the archive and its SHA-256). Inside the container run `git config --global --add safe.directory /repo` first, because a checkout mounted on Linux can fail git's dubious-ownership check. The run needs network: the hygiene layer reads the vulnerability database. The mutation survivor ledger is `.go-tops/survivors.yaml`; the acknowledgement ledger is `.go-tops/acks.yaml`.
7. **Exemptions in force.** None.
8. **Known deviations.** The mutation gate is off until 2027-03-01, because the kit builds it in a later milestone; it carries its reason and expiry in `go-tops.yaml`.
