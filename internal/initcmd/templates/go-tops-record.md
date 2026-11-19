# Go-TOPS adoption record

This repository adopts [Go-TOPS](https://github.com/Hedronite/go-tops). `go-tops.yaml` is the
authority: a line here that disagrees with it is wrong.

1. **Adoption.** Go-TOPS, the Go testing and quality protocol.
2. **Protocol version.** {PROTOCOL}.
3. **`gotops` and pinned tools.** `gotops` {KIT_VERSION}. Pins: {TOOL_VERSIONS}.
4. **Class and thresholds.** `{CLASS}`: statement coverage floor {COVERAGE_FLOOR}%, CRAP cap {CRAP_CAP}, `enforce: {ENFORCE}`.
5. **Run the gate locally.** `just gate`, or `go tool -modfile=tools/go.mod gotops gate`.
6. **Baseline and ledgers.** The baseline is `.go-tops/baseline.json`; only `gotops baseline` writes it, never by hand. It records the build it was measured under, and the run with that build is the ledger leg: this repository's is the `ubuntu` leg of `.github/workflows/go-tops.yml`, and CI fails when that leg reports a ratchet outcome of `skip`. The baseline commands run only on the ledger leg's build. Run the workflow by hand (`workflow_dispatch`) and pick `init` to write the first ledger with `gotops baseline --init`, or `refresh` to lower entries with `gotops baseline`; the ubuntu leg runs it and uploads the file as the `go-tops-baseline` artifact, which you commit. A refresh only lowers entries. Raising or adding an entry is manual, with `gotops baseline --accept-regression --reason <text> --owner <handle>`: run it locally on the ledger leg's build, which on another platform is a `linux/amd64` Docker container of `golang:{TOOLCHAIN_NUM}` with the repository mounted at `/repo`, `GOTOOLCHAIN=local`, and the pinned golangci-lint {GOLANGCI_VERSION} on `PATH` (the workflow's install step shows the archive and its SHA-256). Inside the container run `git config --global --add safe.directory /repo` first, because a checkout mounted on Linux can fail git's dubious-ownership check. The run needs network: the hygiene layer reads the vulnerability database. The mutation survivor ledger is `.go-tops/survivors.yaml`; the acknowledgement ledger is `.go-tops/acks.yaml`.
7. **Exemptions in force.** None.
8. **Known deviations.** The mutation gate is off until {EXPIRES_M4}, because the kit builds it in a later milestone; it carries its reason and expiry in `go-tops.yaml`.
