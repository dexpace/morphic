#!/usr/bin/env bash
#
# bench.sh runs every benchmark in the module.
#
# With no argument it measures — that is `make bench`, and the numbers are worth
# reading. With `1x` it runs each benchmark exactly once, which is what the gate
# does: one iteration says nothing about speed and everything about whether the
# benchmarks still build, still find their fixtures, and still complete.
#
# The gate needs that because `go test -bench` reports success for a run that
# measured nothing at all. A benchmark whose fixture path is wrong prints a
# SKIP under -v and nothing without it, and exits 0 either way — which is how
# this module's only benchmark went unexecuted while looking green.
#
# Usage: bench.sh [benchtime]
set -euo pipefail

cd "$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"

benchtime="${1:-}"

# The packages holding a benchmark, derived from the source as fuzz.sh derives
# its targets, so a new benchmark needs no list kept. Running ./... would link and
# start every other test binary to run nothing in it. go list says what ./...
# means, so a file git does not track yet counts, and testdata and a nested module
# do not. Its result is held first, so that a load that fails ends the script.
dirs="$(go list -f '{{.Dir}}' ./...)"
packages=()
while IFS= read -r dir; do
	if grep -qs '^func Benchmark' "$dir"/*_test.go; then
		packages+=("$dir")
	fi
done <<<"$dirs"

# With no package, go test would read the root directory and blame it for holding
# no Go files, and bash 3.2 stops at the empty list under set -u. Say what is wrong.
if [ "${#packages[@]}" -eq 0 ]; then
	echo "bench.sh: found no benchmark; this run would prove nothing" >&2
	exit 1
fi

# -run '^$' selects no tests: the suite is the coverage step's job, and running
# it again here would double the gate's wall time for nothing.
args=(test "${packages[@]}" -run '^$' -bench . -benchmem -v)
if [ -n "$benchtime" ]; then
	args+=("-benchtime=$benchtime")
fi

# Stream the run and keep a copy: `make bench` is minutes of output somebody is
# watching, and the checks below need the whole of it.
log="$(mktemp "${TMPDIR:-/tmp}/morphic-bench.XXXXXX")"
trap 'rm -f "$log"' EXIT

set +e
go "${args[@]}" 2>&1 | tee "$log"
status=${PIPESTATUS[0]}
set -e
if [ "$status" -ne 0 ]; then
	exit "$status"
fi

skipped="$(grep '^--- SKIP: Benchmark' "$log" || true)"
if [ -n "$skipped" ]; then
	echo "bench.sh: these benchmarks skipped, so they measured nothing:" >&2
	printf '%s\n' "$skipped" >&2
	exit 1
fi

# A -bench pattern that stopped matching reads exactly like a clean run. Require
# evidence that a benchmark actually reported.
if ! grep -q 'ns/op' "$log"; then
	echo "bench.sh: no benchmark reported a result; this run proved nothing" >&2
	exit 1
fi
