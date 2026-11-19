#!/bin/sh
# Regenerates internal/complexity/testdata/gocyclo.golden: gocyclo's own figures
# for the parity corpus (GO_TOPS.md §6.3, "Baseline (matches gocyclo)").
#
# gocyclo is a tool of tools/go.mod only; it is never a dependency of the
# gotops module. Run from anywhere:
#
#   scripts/goldens/gocyclo.sh           rewrite the golden
#   scripts/goldens/gocyclo.sh --check   fail if the committed golden differs
#
# The output is sorted in the C locale, so it is the same on every host.
set -eu

root=$(cd "$(dirname "$0")/../.." && pwd)
corpus="$root/internal/complexity/testdata/corpus"
golden="$root/internal/complexity/testdata/gocyclo.golden"
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT

# Paths in the output are relative to the corpus, so no host path is recorded.
(cd "$corpus" && go tool -modfile="$root/tools/go.mod" gocyclo .) | LC_ALL=C sort >"$tmp"

if [ "${1:-}" = "--check" ]; then
  if ! cmp -s "$tmp" "$golden"; then
    echo "gocyclo.golden is out of date; run scripts/goldens/gocyclo.sh" >&2
    diff "$golden" "$tmp" >&2 || true
    exit 1
  fi
  echo "gocyclo.golden is up to date"
  exit 0
fi
cp "$tmp" "$golden"
echo "wrote $golden"
