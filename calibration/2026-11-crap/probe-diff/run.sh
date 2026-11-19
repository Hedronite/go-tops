#!/bin/sh
# Dumps the probe's figures for every function of the five repos, so the native
# record can be compared with the probe one function at a time (see diff.py).
#
# The probe of ../../2026-11-probe prints only aggregates and the twelve worst
# functions per repo. This runs a patched COPY of it, in a temporary directory,
# that also writes one line per function to the file given as the first
# argument: repo, file, name, line, discounted complexity, guard-neutral
# statements, guard-neutral covered statements, raw complexity, guards. The
# committed probe is not changed. The suites run as the tests layer runs them
# (race on, cgo on), the same as ../run.sh, so the profiles are the same.
# Needs git, go and network access to github.com.
set -eu

HERE=$(cd "$(dirname "$0")" && pwd)
PROBE=$(cd "$HERE/../../2026-11-probe" && pwd)
OUT=${1:?usage: run.sh <dump-file>}
case $OUT in /*) ;; *) OUT=$PWD/$OUT ;; esac
: >"$OUT"

W=$(mktemp -d)
trap 'chmod -R u+w "$W" 2>/dev/null; rm -rf "$W"' EXIT
mkdir -p "$W/gotmp"
export GOMODCACHE="$W/gomod" GOCACHE="$W/gocache" GOTMPDIR="$W/gotmp" GOTOOLCHAIN=local

names=""
for entry in \
	augur:fa72aa71aef2e86a4ece01d5188ecc50aced7b55 \
	simplex:873de8602a74b95bfe38af6d33a9d31594df5726 \
	vigil:4a71f03ff8e52736963731b2dd088db643c13e03 \
	cubit:7f6a776932e816842b6d60c949560d6084caebfe \
	scry:a33d4de203f6fc06f60982736947ec2e0c9099f5; do
	name=${entry%%:*}
	sha=${entry##*:}
	git init -q "$W/$name"
	git -C "$W/$name" fetch -q --depth 1 "https://github.com/Hedronite/$name.git" "$sha"
	git -C "$W/$name" checkout -q --detach FETCH_HEAD
	(cd "$W/$name" && CGO_ENABLED=1 GOFLAGS="-mod=mod -count=1" go test -race -count=1 -coverprofile="$W/$name.cover" -coverpkg=./... ./... >"$W/$name.test.log" 2>&1) || {
		echo "run.sh: tests failed in $name" >&2
		exit 1
	}
	names="$names $name"
done

mkdir "$W/pd" && cp "$PROBE/probe/main.go" "$PROBE/probe/go.mod" "$W/pd/"
cat >"$W/patch.awk" <<'AWK'
/^\t\tvar st, cv, gst, gcv, guards, ccSum, ccdSum, unmeasured int$/ {
	print "\t\tif dp := os.Getenv(\"PROBE_DUMP\"); dp != \"\" {"
	print "\t\t\tdf, _ := os.OpenFile(dp, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)"
	print "\t\t\tfor _, f := range fns {"
	print "\t\t\t\tfmt.Fprintf(df, \"%s\\t%s\\t%s\\t%d\\t%d\\t%d\\t%d\\t%d\\t%d\\n\", repo, f.File, f.Name, f.Line, f.CC-f.Guards, f.Stmts-f.GStmts, f.Cov-f.GCov, f.CC, f.Guards)"
	print "\t\t\t}"
	print "\t\t\tdf.Close()"
	print "\t\t}"
}
{ print }
AWK
awk -f "$W/patch.awk" "$W/pd/main.go" >"$W/pd/main.patched" && mv "$W/pd/main.patched" "$W/pd/main.go"
grep -q PROBE_DUMP "$W/pd/main.go" || { echo "run.sh: the probe no longer has the line this patch anchors on" >&2; exit 1; }
(cd "$W/pd" && go build -o "$W/probe-dump" .)
# The probe reads <name>.cover and <name>/go.mod relative to its working directory.
(cd "$W" && PROBE_DUMP="$OUT" ./probe-dump $names >/dev/null)
echo "wrote $OUT"
