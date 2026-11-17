# Go-TOPS adoption record

This repository adopts [Go-TOPS](https://github.com/Hedronite/go-tops). `go-tops.yaml` is the
authority: a line here that disagrees with it is wrong.

1. **Adoption.** Go-TOPS, the Go testing and quality protocol.
2. **Protocol version.** {PROTOCOL}.
3. **`gotops` and pinned tools.** `gotops` {KIT_VERSION}. Pins: {TOOL_VERSIONS}.
4. **Class and thresholds.** `{CLASS}`: statement coverage floor {COVERAGE_FLOOR}%, CRAP cap {CRAP_CAP}, `enforce: {ENFORCE}`.
5. **Run the gate locally.** `just gate`, or `go tool -modfile=tools/go.mod gotops gate`.
6. **Baseline and ledgers.** The baseline is `.go-tops/baseline.json`; only `gotops baseline` writes it, so refresh it there and never by hand. The mutation survivor ledger is `.go-tops/survivors.yaml`; the acknowledgement ledger is `.go-tops/acks.yaml`.
7. **Exemptions in force.** None.
8. **Known deviations.** The CRAP gate is off until {EXPIRES_M2} and the mutation gate until {EXPIRES_M4}, because the kit builds them in later milestones; each carries its reason and expiry in `go-tops.yaml`.
