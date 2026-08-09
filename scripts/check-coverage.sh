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
#
# Blocks are merged by identity — the "<file>.go:<span>" field — before anything is
# counted, because the same block can arrive several times over. go test builds the
# -coverprofile output by concatenating each package's fragment, and cmd/go appends a
# cached fragment before the checks that decide whether that cached result is usable,
# once for each of the two keys it tries. Immediately after `go clean -testcache` every
# block therefore lands three times, and a count over the raw lines reports how warm
# the test cache is rather than how large the tree is. Merging is what go tool cover
# does when it reads a profile; an explicit -coverpkg would not help, since the repeats
# are re-emissions of a package's own fragment.
#
# Counts merge by max rather than the sum go tool cover uses in atomic mode, because
# the repeats are one run's data re-emitted: summing would multiply real execution
# counts by however many times a fragment happened to be appended. The verdict is the
# same under either rule, since the gate only asks whether a block ever ran.
#
# One consequence is deliberate: a block that ran in one fragment and not in another
# now counts as covered, the plain meaning of "some test executed this statement". It
# retires a false failure the raw count could produce, where such a block added 2 to
# the total and 1 to the hits. A block no fragment ran still merges to 0, and is still
# reported and still fails.
#
# Sorted so the same failure reads the same way on every run.
merged="$(awk 'NR > 1 {
	if (!($1 in stmts)) {
		stmts[$1] = $2 + 0
		count[$1] = $3 + 0
		next
	}
	# A span identifies one block of one file, so its statement count cannot
	# differ between copies. If it does, the profile is not one build and the
	# total would depend on line order.
	if (stmts[$1] != $2 + 0) {
		printf "COVERAGE FAIL: %s reports %d and %d statements\n", $1, stmts[$1], $2 > "/dev/stderr"
		conflict = 1
		exit 1
	}
	if ($3 + 0 > count[$1]) {
		count[$1] = $3 + 0
	}
}
END {
	if (conflict) {
		exit 1
	}
	for (block in stmts) {
		print block, stmts[block], count[block]
	}
}' "$cover_file" | sort)"

uncovered="$(printf '%s\n' "$merged" | awk '$3 == 0')"

read -r hit total <<<"$(printf '%s\n' "$merged" | awk '{ total += $2; if ($3 > 0) hit += $2 } END { print hit + 0, total + 0 }')"

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
