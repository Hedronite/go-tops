#!/bin/sh
# Reproduces the 2026-11 CRAP calibration from clean clones.
#
# Clones the five repos at the pinned commits into a temporary directory, runs
# their tests with a coverage profile, then runs the native CRAP scorer over
# every production function (calibrate_test.go, build tag `calibration`) and
# writes results.json and tables.md (default: next to this script; or into the
# directory given as the first argument). It also runs `gotops gate` on one
# repo at caps 6, 8 and 10 and checks that the layer's violations equal the
# scorer's over-cap counts (the cross-check of the record).
#
# Nothing is written to any repo outside the temporary directory, which is
# removed on exit. Needs git, go and network access to github.com; it uses the
# local toolchain only (GOTOOLCHAIN=local), keeps Go's caches inside the
# temporary directory, and compares the counts with the probe record in
# ../2026-11-probe/results.json.
set -eu

HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/../.." && pwd)
OUT=${1:-$HERE}
XREPO=vigil
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
mkdir -p "$W/gotmp" "$W/xcheck"
export GOMODCACHE="$W/gomod" GOCACHE="$W/gocache" GOTMPDIR="$W/gotmp" GOTOOLCHAIN=local

: >"$W/repos.txt"
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
	# The profile the gate's tests layer writes: the whole module, -coverpkg=./... .
	# The run of the gate's tests layer: -race, cgo on, the whole module's profile. The
	# scorer loads the module under that same build, so the profile and the load agree.
	if ! (cd "$W/$name" && CGO_ENABLED=1 GOFLAGS="-mod=mod -count=1" go test -race -count=1 -coverprofile="$W/$name.cover" -coverpkg=./... ./... >"$W/$name.test.log" 2>&1); then
		echo "run.sh: tests failed in $name; the scorer needs green suites. Last lines:" >&2
		tail -n 20 "$W/$name.test.log" >&2
		exit 1
	fi
	echo "$name $sha tests ok"
	echo "$name $sha" >>"$W/repos.txt"
done

# Cross-check: the gate's CRAP layer at three caps on one repo. Hygiene may be
# red here (the repo has no kit config of its own) and that does not matter:
# only the crap layer's violations are read, from the report the run wrote.
(cd "$ROOT" && go build -o "$W/gotops" ./cmd/gotops)
mod=$(cd "$W/$XREPO" && go list -m)
(cd "$W/$XREPO" && "$W/gotops" init --class binary-cli --offline --force >/dev/null)
for cap in 6 8 10; do
	cat >"$W/$XREPO/go-tops.yaml" <<EOF
protocol: "0.4.2"
version: "0.0.0"
module: $mod
class: binary-cli
enforce: advisory
advisory:
  reason: calibration run, nothing here is enforced
  owner: "@calibration"
  expires: "2099-01-01"
toolchain:
  go_floor: "1.26"
  go_current: "1.27"
  cgo: false
gates:
  hygiene:
    golangci_lint: { config: .golangci.yml }
    govulncheck:   { fail_on: reachable }
    deps:          { depguard: true, licenses: { allow: [MIT, Apache-2.0, BSD-3-Clause, BSD-2-Clause, ISC] } }
  tests:
    race: true
    shuffle: true
    count: 1
  coverage:
    statements_floor: 0
    exclude_generated: true
  crap:
    cap: $cap
  mutation:
    enabled: false
    reason: calibration measures CRAP only
    expires: "2099-01-01"
layers:
  fuzz:  { enabled: false, reason: "calibration measures CRAP only" }
  bench: { enabled: false, reason: "calibration measures CRAP only" }
EOF
	(cd "$W/$XREPO" && "$W/gotops" gate --max-tier 1 >"$W/xcheck/gate-$cap.txt" 2>&1 || true)
	if [ ! -s "$W/$XREPO/.go-tops/report.json" ]; then
		echo "run.sh: the gate wrote no report at cap $cap. Last lines:" >&2
		tail -n 20 "$W/xcheck/gate-$cap.txt" >&2
		exit 1
	fi
	cp "$W/$XREPO/.go-tops/report.json" "$W/xcheck/report-$cap.json"
done

mkdir -p "$OUT"
(cd "$ROOT" && CALIBRATION_WORK="$W" CALIBRATION_OUT="$OUT" CALIBRATION_XREPO="$XREPO" CALIBRATION_XDIR="$W/xcheck" \
	CALIBRATION_PROBE="$HERE/../2026-11-probe/results.json" \
	go test -tags calibration -count=1 -run TestCalibrate ./calibration/2026-11-crap/)
echo "wrote $OUT/results.json and $OUT/tables.md"
