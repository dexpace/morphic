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
#
# Usage:
#   scripts/check-coverage.sh             # run the suite, then count its profile
#   scripts/check-coverage.sh <profile>   # count an existing profile, running nothing
#
# The second form is not a gate — it runs no tests. It exists so the counting below
# can be driven over profiles a real run will not produce, which is what
# scripts/verify-coverage-count.sh does. CI uses the first form.
#
# $COVER_FILE names where the first form writes its profile; the default is a
# temporary file this script removes on exit. An argument supersedes it, since
# nothing is written in that case.
set -euo pipefail

# Bound the failure output: a broken build can leave hundreds of uncovered
# blocks, and the first screenful is what gets read.
max_reported=25

if [ "$#" -gt 1 ]; then
	echo "usage: $(basename "$0") [profile]" >&2
	exit 2
fi

# Where the profile comes from is settled once, here, because only one of these
# three paths owns the file. An argument names the caller's, which this script
# must leave exactly as it found it — a cleanup trap installed before this point
# would expand $cover_file at exit and delete it.
if [ "$#" -eq 1 ]; then
	cover_file="$1"
	if [ ! -r "$cover_file" ]; then
		echo "COVERAGE FAIL: cannot read profile $cover_file"
		exit 1
	fi
else
	# The minted path is unique per invocation. `go test -coverprofile` truncates
	# the file when it starts and appends each package's blocks as that package
	# finishes, so two runs sharing one path overwrite each other mid-write and
	# each ends up reading a profile that is partly the other's.
	#
	# The merge below counts a block once however many times it appears, so this
	# no longer inflates the total the way it did before that merge landed. What
	# is left is a run judging a set of blocks that is not the set it produced,
	# which reads as a pass whenever the blocks it lost were the uncovered ones.
	#
	# COVER_FILE names a profile to keep for inspection; a path this script mints
	# is its own and is removed on exit.
	if [ -n "${COVER_FILE:-}" ]; then
		cover_file="$COVER_FILE"
	else
		cover_file="$(mktemp "${TMPDIR:-/tmp}/morphic-cover.XXXXXX")"
		trap 'rm -f "$cover_file"' EXIT
	fi

	# Run from the repo root, derived from this script's own location rather than
	# from the caller's cwd: invoked from a subdirectory, `go test ./...` would
	# otherwise measure only that subtree and still report a pass. Same derivation
	# as verify-atomic-output.sh. It happens here and not at the top of the file
	# because the argument form above takes a path from the caller, and a relative
	# one would then be resolved against the wrong directory.
	cd "$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"

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
fi

# Profile body, one block per line: "<import-path>/<file>.go:<span> <stmts> <count>".
#
# Blocks are merged by identity — the "<file>.go:<span>" field — before anything is
# counted, because the same block can arrive several times over. go test builds the
# -coverprofile output by concatenating each package's fragment, and cmd/go appends a
# cached fragment before the checks that decide whether that cached result is usable.
# A package whose fragment is found but whose cached result is then rejected therefore
# contributes its blocks once before the test re-runs and again afterwards, so a count
# over the raw lines reports how warm the test cache is rather than how large the tree
# is. With no cached fragment to find — a runner whose build cache is cold, which is
# CI's usual state — the lookup returns before the merge and no repeat appears at all.
#
# Merging is what go tool cover does when it reads a profile; an explicit -coverpkg
# would not help, since the repeats are re-emissions of a package's own fragment.
#
# The merge keeps only whether some fragment ran a block, which is the one question
# the gate asks. That has a consequence worth stating, because the fragments need not
# come from the same run: a block a replayed fragment covered and this run did not now
# counts as covered. The raw count turned that divergence into a failure instead, by
# charging the block's statements to the total twice and to the hits once. A block no
# fragment ran still merges to 0, and is still reported and still fails.
merge='NR == 1 {
	# Without this, a body-only file has its first block eaten as the header and
	# silently dropped — which reads as a pass when that block is the uncovered one.
	if ($0 !~ /^mode: /) {
		print "COVERAGE FAIL: the first line is not a \"mode:\" header"
		print "Coverage gate failed: the profile could not be read."
		bad = 1
		exit 1
	}
	next
}
{
	# Every block line is "<id> <stmts> <count>". Anything shorter would still be
	# read positionally into those three, quietly counting as a 0-statement block.
	if (NF != 3) {
		printf "COVERAGE FAIL: line %d has %d field(s), want 3: %s\n", NR, NF, $0
		print "Coverage gate failed: the profile could not be read."
		bad = 1
		exit 1
	}
	block = $1
	if (!(block in stmts)) {
		stmts[block] = $2 + 0
		ran[block] = ($3 + 0 > 0)
		next
	}
	# A span identifies one block of one file, so its statement count cannot
	# differ between copies. If it does, the profile is not one build and the
	# total would depend on line order.
	if (stmts[block] != $2 + 0) {
		printf "COVERAGE FAIL: %s reports %d and %d statements\n", block, stmts[block], $2
		print "Coverage gate failed: the profile does not describe a single build."
		bad = 1
		exit 1
	}
	if ($3 + 0 > 0) {
		ran[block] = 1
	}
}
END {
	if (bad) {
		exit 1
	}
	for (id in stmts) {
		print id, stmts[id], ran[id]
	}
}'

# A refused profile aborts the merge before it prints any block, so the capture holds
# its COVERAGE FAIL line and summary instead. Echo them rather than dying with an empty
# stdout: every other failure here reports on stdout, and a caller that captures only
# stdout should not have to guess why the gate stopped.
if ! merged="$(awk "$merge" "$cover_file")"; then
	printf '%s\n' "$merged"
	exit 1
fi

if [ -z "$merged" ]; then
	echo "COVERAGE FAIL: the profile records no statements"
	exit 1
fi

# Sorted so the same failure reads the same way on every run, and under LC_ALL=C so it
# reads the same way on every machine: collation is locale-dependent — en_US.UTF-8
# folds case and skips punctuation where C compares bytes — and with the listing capped
# it is not only the order that would vary but which blocks a reader is shown at all.
printf '%s\n' "$merged" | LC_ALL=C sort | awk -v max="$max_reported" '
	{
		# A block with no statements cannot be uncovered, and contributes nothing
		# either way. go emits these for an empty body — an unreached "case x:",
		# say — and listing one would print a COVERAGE FAIL beside a passing
		# verdict.
		if ($2 + 0 == 0) {
			next
		}
		total += $2
		if ($3 == 0) {
			blocks++
			missed += $2
			if (blocks <= max) {
				printf "COVERAGE FAIL: %s (%s statement(s) uncovered)\n", $1, $2
			}
		}
	}
	END {
		if (total == 0) {
			print "COVERAGE FAIL: the profile records no statements"
			exit 1
		}
		if (missed > 0) {
			if (blocks > max) {
				printf "  ... and %d more uncovered block(s)\n", blocks - max
			}
			printf "Coverage gate failed: %d of %d statements uncovered; 100%% is required.\n", missed, total
			exit 1
		}
		printf "Coverage gate passed: all %d statements covered.\n", total
	}'
