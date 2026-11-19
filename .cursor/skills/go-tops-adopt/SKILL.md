---
name: go-tops-adopt
description: Adopt Go-TOPS on a Go module. Use when choosing a class or running gotops init, baseline --init and check. Requires Nushell. Run the script; do not re-implement init.
---

# go-tops-adopt

Nushell (`nu`) is required. Run this script. Do not read it and do not re-implement `gotops`.

```bash
nu go-tops-adopt.nu adopt --class <class> [--path .] [--owner <handle>] [--force] [--offline] [--allow-host-ledger] [--gotops "<argv>"]
nu go-tops-adopt.nu check [--path .] [--gotops "<argv>"]
```

`<class>` is `binary-cli`, `binary-service` or `library-core`.

`adopt` runs `gotops init`, then `gotops baseline --init`, then `gotops check`, and stops at the first that exits non-zero. It prints one JSON object. `command` is the exact argv of the three, joined by ` && `. `ok` is true only when `init` and `baseline --init` exit 0, the ledger was written on the ledger leg's build (linux/amd64, cgo on, no tags), and `check` exits 0 or names only the baseline's commit: under `ratchet`, `check` reads a ledger only once git holds it. In that case `data.commit_required` is true and `data.next` says what to commit. The script commits and stages nothing. After the commit, run `check`, which runs `gotops check` alone and is `ok` only when it exits 0.

`data.steps` has each command's exit code and output tail; `data.ledger` has the ledger's `build` and `on_ledger_leg_build`; `data.commit_ledger` says whether the ledger may be committed.

A ratchet `skip` is judged by build (ruling C3b), and the ledger leg's build is the one that judges the ratchet: a skip on the ledger's own build fails. A ledger written on another build (a Mac) is never committed: it would make the ledger leg skip the ratchet and CI would fail. The script gives `ok: false` for it and says how to write the ledger on the ledger leg: run the drop-in workflow by hand with `mode` `init`, then commit the file it uploads. `--allow-host-ledger` is for a dry run on this host; commit nothing it wrote under `.go-tops/`.

`gotops` is found from `--gotops`, then `$GOTOPS`, then the module's `tools/go.mod` (`go tool -modfile=tools/go.mod gotops`), then PATH. A missing `gotops` is `ok: false`.

If `nu` is not on PATH, there is no JSON. Treat `nu: command not found` as `{"ok":false,"command":"nu go-tops-adopt.nu","error":"nu: command not found"}`. Do not write the kit files yourself.
