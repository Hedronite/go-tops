#!/bin/sh
# Runs gaze, pinned in tools/go.mod, over testdata/gaze-crosscheck and writes
# gaze.json (default: next to this script; or into the directory given as the
# first argument). gaze reads the same kind of coverage profile the gate does
# (the whole module, -coverpkg=./...) and reports complexity, coverage and CRAP
# per function at threshold 10. Needs go and network access for the module
# cache; its caches live in a temporary directory that is removed on exit.
set -eu

HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/../.." && pwd)
OUT=${1:-$HERE}

W=$(mktemp -d)
trap 'chmod -R u+w "$W" 2>/dev/null; rm -rf "$W"' EXIT
export GOMODCACHE="$W/gomod" GOCACHE="$W/gocache" GOTMPDIR="$W/gotmp" GOTOOLCHAIN=local
mkdir -p "$W/gotmp" "$OUT"

cd "$ROOT/testdata/gaze-crosscheck"
go test -count=1 -coverprofile="$W/check.cover" -coverpkg=./... ./... >/dev/null
go tool -modfile="$ROOT/tools/go.mod" gaze crap ./... \
	--coverprofile="$W/check.cover" --format json --crap-threshold 10 >"$OUT/gaze.json" 2>"$W/gaze.err"
echo "wrote $OUT/gaze.json"
