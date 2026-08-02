#!/usr/bin/env bash
#
# check-coverage.sh enforces exact 100% statement coverage.
#
# Coverage is counted from the profile, statement by statement, rather than read
# from go test's "coverage: N%" summary lines. Those are rounded to one decimal
# place, so a package at 99.96% prints "100.0%" and a gate reading them cannot
# see a single uncovered statement.
#
# Packages with no statements and packages with no test files contribute no
# profile blocks, so they pass without a special case.
set -euo pipefail

cover_file="${COVER_FILE:-cover.out}"

# Bound the failure output: a broken build can leave hundreds of uncovered
# blocks, and the first screenful is what gets read.
max_reported=25

# -timeout is explicit rather than left to go test's 10-minute default. The
# compiler refuses documents that would otherwise hang the third-party resolver,
# so a regression in that refusal is a test that never returns, not one that
# fails. Ninety seconds is several times the suite's normal wall time and turns
# that failure mode into a prompt stack dump naming the stuck goroutine.
go test ./... -timeout 90s -covermode=atomic -coverprofile="$cover_file"

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
