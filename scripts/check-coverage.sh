#!/usr/bin/env bash
#
# check-coverage.sh enforces 100.0% statement coverage.
#
# It runs the test suite with a coverage profile and fails (exit 1) if the
# overall total, or any single package, reports less than 100.0% statement
# coverage. Packages with no statements to cover (e.g. pure marker/interface
# packages) report "[no statements]" and are treated as a pass.
set -euo pipefail

cover_file="${COVER_FILE:-cover.out}"

# Run the suite once, capturing the per-package summary lines while still
# streaming them to the log.
test_out="$(go test ./... -covermode=atomic -coverprofile="$cover_file")"
echo "$test_out"

fail=0

# Per-package check. Covered packages print:
#   ok   <pkg>   <time>   coverage: 100.0% of statements
# Zero-statement packages print "coverage: [no statements]" (skipped as pass);
# packages without tests print "[no test files]" (no coverage token, skipped).
while IFS= read -r line; do
	case "$line" in
	*"coverage: "*"% of statements")
		pkg="$(printf '%s\n' "$line" | awk '{print $2}')"
		pct="${line##*coverage: }"
		pct="${pct%%% of statements}"
		if awk "BEGIN{exit !($pct < 100.0)}"; then
			echo "COVERAGE FAIL: package $pkg at ${pct}% (< 100.0%)"
			fail=1
		fi
		;;
	esac
done <<<"$test_out"

# Total check, read from the func report's summary line.
func_out="$(go tool cover -func="$cover_file")"
total_pct="$(printf '%s\n' "$func_out" | awk '/^total:/{print $NF}' | tr -d '%')"
if [ -z "$total_pct" ]; then
	echo "COVERAGE FAIL: could not determine total coverage"
	exit 1
fi
if awk "BEGIN{exit !($total_pct < 100.0)}"; then
	echo "COVERAGE FAIL: total at ${total_pct}% (< 100.0%)"
	fail=1
fi

if [ "$fail" -ne 0 ]; then
	echo "Coverage gate failed: 100.0% statement coverage is required."
	exit 1
fi

echo "Coverage gate passed: total ${total_pct}%, every package at 100.0%."
