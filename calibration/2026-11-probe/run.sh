#!/bin/sh
# Reproduces the 2026-11 calibration probe from clean clones.
#
# Clones the five repos at the pinned commits into a temporary directory, runs
# `go test` with a coverage profile in each, runs the probe over all five, and
# writes results.json (default: next to this script; or into the directory
# given as the first argument). Nothing is written to any repo and the
# temporary directory is removed on exit. Needs git, go and network access to
# github.com. Everything Go keeps (module cache, build cache) lives inside the
# temporary directory, so the first run downloads dependencies.
#
# The toolchain is the local one (GOTOOLCHAIN=local): a Go older than a repo's
# go directive fails here instead of downloading another toolchain, so the Go
# version recorded in results.json is the one that ran.
set -eu

HERE=$(cd "$(dirname "$0")" && pwd)
OUT=${1:-$HERE}
REPOS="
augur:fa72aa71aef2e86a4ece01d5188ecc50aced7b55
simplex:873de8602a74b95bfe38af6d33a9d31594df5726
vigil:4a71f03ff8e52736963731b2dd088db643c13e03
cubit:7f6a776932e816842b6d60c949560d6084caebfe
scry:a33d4de203f6fc06f60982736947ec2e0c9099f5
"

W=$(mktemp -d)
# Go makes its module cache read-only; make it writable so the cleanup works.
trap 'chmod -R u+w "$W" 2>/dev/null; rm -rf "$W"' EXIT
mkdir -p "$W/gotmp"
export GOMODCACHE="$W/gomod" GOCACHE="$W/gocache" GOTMPDIR="$W/gotmp"
export GOTOOLCHAIN=local GOFLAGS=-mod=mod

names=""
commits=""
for entry in $REPOS; do
	name=${entry%%:*}
	sha=${entry##*:}
	git init -q "$W/$name"
	git -C "$W/$name" fetch -q --depth 1 "https://github.com/Hedronite/$name.git" "$sha"
	git -C "$W/$name" checkout -q --detach FETCH_HEAD
	if [ "$(git -C "$W/$name" rev-parse HEAD)" != "$sha" ]; then
		echo "run.sh: $name is not at $sha" >&2
		exit 1
	fi
	if ! (cd "$W/$name" && go test -count=1 -coverprofile="$W/$name.cover" -coverpkg=./... ./... >"$W/$name.test.log" 2>&1); then
		echo "run.sh: tests failed in $name; the probe needs green suites. Last lines:" >&2
		tail -n 20 "$W/$name.test.log" >&2
		exit 1
	fi
	echo "$name $sha tests ok"
	names="$names $name"
	commits="${commits:+$commits, }\"$name\": \"$sha\""
done

(cd "$HERE/probe" && go build -o "$W/probe-bin" .)
# The probe reads <name>.cover and <name>/go.mod relative to its working directory.
(cd "$W" && ./probe-bin -json "$W/probe.json" $names)

mkdir -p "$OUT"
{
	printf '{\n  "go_version": "%s",\n  "platform": "%s/%s",\n' "$(go env GOVERSION)" "$(go env GOOS)" "$(go env GOARCH)"
	printf '  "commits": {%s},\n  "probe": ' "$commits"
	cat "$W/probe.json"
	printf '}\n'
} >"$OUT/results.json"
echo "wrote $OUT/results.json"
