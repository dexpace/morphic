#!/usr/bin/env bash
#
# check-coverage.sh runs the whole suite under the race detector and enforces
# exact 100% statement coverage.
#
# Coverage is counted from the profile, statement by statement, rather than read
# from go test's "coverage: N%" summary lines. Those are rounded to one decimal
# place, so a package at 99.96% prints "100.0%" and a gate reading them cannot
# see a single uncovered statement.
#
# Packages with no statements and packages with no test files contribute no
# profile blocks, so they pass without a special case.
set -euo pipefail

# Work from the repo root, derived from this script's own location rather than
# from the caller's cwd. Invoked from a subdirectory it would otherwise measure
# only that subtree and still report a pass. Same derivation as
# verify-atomic-output.sh.
cd "$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"

# The profile path is unique per invocation. `go test -coverprofile` truncates
# the file when it starts and appends each package's blocks as that package
# finishes, so two runs sharing one path interleave into a profile that is
# neither run's, with every block counted once per run that reached it. That can
# never fail a fully covered tree — hit and total inflate together — but the
# total a human reads to judge the gate is then several times the real one.
#
# COVER_FILE names a profile to keep for inspection; a path this script mints is
# its own and is removed on exit.
if [ -n "${COVER_FILE:-}" ]; then
	cover_file="$COVER_FILE"
else
	cover_file="$(mktemp "${TMPDIR:-/tmp}/morphic-cover.XXXXXX")"
	trap 'rm -f "$cover_file"' EXIT
fi

# Bound the failure output: a broken build can leave hundreds of uncovered
# blocks, and the first screenful is what gets read.
max_reported=25

# -race rides along with the coverage run rather than getting a `go test ./...`
# of its own. The two want the same execution of the same tests, and running the
# suite twice to collect two properties from it costs a second full run for
# nothing: the profile is identical with the detector on and off.
#
# -timeout is explicit rather than left to go test's 10-minute default. The
# compiler refuses documents that would otherwise hang the third-party resolver,
# so a regression in that refusal is a test that never returns, not one that
# fails. The bound clears the slowest package under -race by more than an order
# of magnitude, which leaves room for a CI runner several times slower than a
# developer machine while still turning a hang into a prompt stack dump naming
# the stuck goroutine.
go test ./... -race -timeout 300s -covermode=atomic -coverprofile="$cover_file"

# Profile body, one block per line: "<import-path>/<file>.go:<span> <stmts> <count>".
# Sorted so the same failure reads the same way on every run.
uncovered="$(tail -n +2 "$cover_file" | awk '$3 == 0' | sort)"

read -r hit total <<<"$(awk 'NR > 1 { total += $2; if ($3 > 0) hit += $2 } END { print hit + 0, total + 0 }' "$cover_file")"

if [ "$total" -eq 0 ]; then
	echo "COVERAGE FAIL: the profile records no statements"
	exit 1
fi

if [ "$hit" -ne "$total" ]; then
	printf '%s\n' "$uncovered" | awk -v max="$max_reported" '
		NR <= max { printf "COVERAGE FAIL: %s (%s statement(s) uncovered)\n", $1, $2 }
		END { if (NR > max) printf "  ... and %d more uncovered block(s)\n", NR - max }'
	echo "Coverage gate failed: $((total - hit)) of $total statements uncovered; 100% is required."
	exit 1
fi

echo "Coverage gate passed: all $total statements covered."
