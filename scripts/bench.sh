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

# -run '^$' selects no tests: the suite is the coverage step's job, and running
# it again here would double the gate's wall time for nothing.
args=(test ./... -run '^$' -bench . -benchmem -v)
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

# A tree with no benchmarks left, or a -bench pattern that stopped matching,
# reads exactly like a clean run. Require evidence that one actually reported.
if ! grep -q 'ns/op' "$log"; then
	echo "bench.sh: no benchmark reported a result; this run proved nothing" >&2
	exit 1
fi
