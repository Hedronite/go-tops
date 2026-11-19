| Repo | Commit | Functions | Probe | Δ | Not measured | Over 6 | Probe | Δ | Over 8 | Probe | Δ | Over 10 | Probe | Δ |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| augur | `fa72aa7` | 140 | 140 | 0 | 0 | 36 | 35 | +1 | 28 | 28 | 0 | 20 | 19 | +1 |
| simplex | `873de86` | 186 | 186 | 0 | 0 | 46 | 45 | +1 | 34 | 33 | +1 | 28 | 27 | +1 |
| vigil | `4a71f03` | 100 | 100 | 0 | 0 | 14 | 14 | 0 | 10 | 9 | +1 | 8 | 8 | 0 |
| cubit | `7f6a776` | 212 | 210 | +2 | 0 | 69 | 69 | 0 | 48 | 47 | +1 | 28 | 28 | 0 |
| scry | `a33d4de` | 127 | 125 | +2 | 0 | 32 | 29 | +3 | 20 | 19 | +1 | 8 | 8 | 0 |
| **all** | | **765** | **761** | +4 | **0** | **197** | **192** | +5 | **140** | **136** | +4 | **92** | **90** | +2 |

Over the cap by complexity alone (no test can change these):

| Repo | Comp > 6 | Probe | Δ | Comp > 8 | Probe | Δ | Comp > 10 | Probe | Δ |
|---|---|---|---|---|---|---|---|---|---|
| augur | 32 | 31 | +1 | 24 | 24 | 0 | 17 | 16 | +1 |
| simplex | 38 | 36 | +2 | 30 | 27 | +3 | 19 | 19 | 0 |
| vigil | 12 | 11 | +1 | 9 | 9 | 0 | 8 | 7 | +1 |
| cubit | 55 | 55 | 0 | 34 | 34 | 0 | 21 | 21 | 0 |
| scry | 24 | 23 | +1 | 13 | 12 | +1 | 5 | 4 | +1 |
| **all** | **161** | **156** | +5 | **110** | **106** | +4 | **70** | **67** | +3 |

| Repo | (<=4) | (<=6) | (<=8) | (<=10) | (<=15) | (<=30) | (>30) |
|---|---|---|---|---|---|---|---|
| augur | 75 | 29 | 8 | 8 | 11 | 8 | 1 |
| simplex | 113 | 27 | 12 | 6 | 12 | 11 | 5 |
| vigil | 73 | 13 | 4 | 2 | 6 | 1 | 1 |
| cubit | 122 | 21 | 21 | 20 | 14 | 11 | 3 |
| scry | 80 | 15 | 12 | 12 | 5 | 3 | 0 |
| **all** | **463** | **105** | **57** | **48** | **48** | **34** | **10** |

| Repo | Function | File | Comp | Coverage | CRAP |
|---|---|---|---|---|---|
| augur | `github.com/Hedronite/augur/internal/policy.validator.escalate` | `internal/policy/policy.go` | 13 | 46.4% | 39.0 |
| augur | `github.com/Hedronite/augur/scripts/mkarchive.run` | `scripts/mkarchive/main.go` | 24 | 78.3% | 29.9 |
| augur | `github.com/Hedronite/augur/internal/policy.validator.hold` | `internal/policy/policy.go` | 19 | 75.6% | 24.2 |
| augur | `github.com/Hedronite/augur/internal/policy.validator.document` | `internal/policy/policy.go` | 24 | 93.8% | 24.1 |
| augur | `github.com/Hedronite/augur/internal/engine.Evaluate` | `internal/engine/engine.go` | 20 | 97.9% | 20.0 |
| simplex | `github.com/Hedronite/simplex/internal/vantage.NetworkLister.HasTailnetAddress` | `internal/vantage/net.go` | 9 | 0.0% | 90.0 |
| simplex | `github.com/Hedronite/simplex/internal/overlay.nodeDesc` | `internal/overlay/loader.go` | 9 | 27.3% | 40.2 |
| simplex | `github.com/Hedronite/simplex/internal/config.parseChecks` | `internal/config/config.go` | 29 | 85.2% | 31.7 |
| simplex | `github.com/Hedronite/simplex/internal/overlay.parseOverrides` | `internal/overlay/loader.go` | 27 | 82.4% | 31.0 |
| simplex | `github.com/Hedronite/simplex/internal/config.parseHolds` | `internal/config/config.go` | 26 | 80.8% | 30.8 |
| vigil | `github.com/Hedronite/vigil/internal/config.Config.validate` | `internal/config/config.go` | 37 | 84.7% | 41.9 |
| vigil | `github.com/Hedronite/vigil/internal/promclient.Query` | `internal/promclient/promclient.go` | 23 | 94.5% | 23.1 |
| vigil | `github.com/Hedronite/vigil/internal/httpx.Client.Get` | `internal/httpx/httpx.go` | 14 | 94.6% | 14.0 |
| vigil | `github.com/Hedronite/vigil/cmd/vigil.newRoot` | `cmd/vigil/cli.go` | 14 | 100.0% | 14.0 |
| vigil | `github.com/Hedronite/vigil/internal/httpx.validPath` | `internal/httpx/httpx.go` | 14 | 100.0% | 14.0 |
| cubit | `github.com/Hedronite/cubit/internal/textfile.Open` | `internal/textfile/textfile.go` | 30 | 75.9% | 42.6 |
| cubit | `github.com/Hedronite/cubit/internal/scan.scanRoot` | `internal/scan/scan.go` | 36 | 91.2% | 36.9 |
| cubit | `github.com/Hedronite/cubit/internal/config.Check` | `internal/config/check.go` | 32 | 85.9% | 34.9 |
| cubit | `github.com/Hedronite/cubit/internal/config.classify` | `internal/config/findings.go` | 26 | 97.7% | 26.0 |
| cubit | `github.com/Hedronite/cubit/internal/collect.Collector.render` | `internal/collect/collect.go` | 23 | 100.0% | 23.0 |
| scry | `github.com/Hedronite/scry/hack/mkarchive.run` | `hack/mkarchive/main.go` | 23 | 82.6% | 25.8 |
| scry | `github.com/Hedronite/scry/internal/report.Render` | `internal/report/table.go` | 20 | 96.1% | 20.0 |
| scry | `github.com/Hedronite/scry/internal/report.verdict` | `internal/report/build.go` | 16 | 96.8% | 16.0 |
| scry | `github.com/Hedronite/scry/internal/kube.parseReadyz` | `internal/kube/readyz.go` | 13 | 100.0% | 13.0 |
| scry | `github.com/Hedronite/scry/internal/wait.RealClock.Sleep` | `internal/wait/wait.go` | 3 | 0.0% | 12.0 |
