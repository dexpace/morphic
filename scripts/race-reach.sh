#!/usr/bin/env bash
#
# race-reach.sh runs the go test command it is given over the one race planted
# in internal/archtest/testdata/racereach, and requires the run to fail with a
# data race whose stack names the plant's file. The Makefile passes the command
# `make race` runs, so that shows the command runs the race detector at all: one
# without -race, or a build that hides the plant, would leave the listed packages
# just as green.
#
# Usage: race-reach.sh go test [flags]
set -euo pipefail

# With no command there is nothing to show reaches the plant.
if [[ $# -lt 2 || "$2" != "test" ]]; then
	echo "usage: race-reach.sh go test [flags]" >&2
	exit 2
fi

cd "$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
plant=internal/archtest/testdata/racereach

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# -count=1 keeps a cached verdict from standing in for a run.
status=0
"$@" -count=1 "./$plant" >"$work/race.out" 2>&1 || status=$?
if [[ "$status" -eq 0 ]] ||
	! grep -q '^WARNING: DATA RACE$' "$work/race.out" ||
	! grep -qE "$plant/racereach_test\.go:[0-9]+" "$work/race.out"; then
	echo "race-reach: $plant must fail with a data race in $plant/racereach_test.go (exit $status):" >&2
	cat "$work/race.out" >&2
	exit 1
fi
echo "race-reach: the race detector reports the race planted in $plant/racereach_test.go"
