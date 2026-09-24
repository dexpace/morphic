#!/usr/bin/env bash
#
# fuzz.sh runs the module's fuzz targets under a bounded search.
#
# `go test` executes a target's seed corpus on every ordinary run but never
# mutates; only -fuzz searches, and -fuzz takes one package and one target per
# invocation. So the gate needs a loop, and the loop derives its pairs from the
# source: a target written tomorrow is fuzzed the moment it lands, and there is
# no list here to go stale.
#
# Usage: fuzz.sh [fuzztime]        (default 10s per target, or $FUZZTIME)
set -euo pipefail

cd "$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"

fuzztime="${1:-${FUZZTIME:-10s}}"

# Targets held out of the search, each naming the issue that must close before
# it comes back. A search is not the place to rediscover a bug that is already
# filed: it spends its whole budget re-finding one input and reddens every
# unrelated change until the fix lands.
#
# Closing the issue is what retires the entry: nothing here can check that the
# reason still holds, and a quarantine that outlives its bug reads as if it were
# still protecting something while it quietly stops a target from ever running.
# When an issue named above closes, delete its line and let the search prove it.
#
# dexpace/morphic#416 (an anyOf whose only branch was {"type":"null"} lowering
# to a union with no variants) is fixed, so nothing is held back right now.
#
# Ordinary `go test` still runs every one of these targets' seeds, quarantined
# or not; what is held back is the mutation.
quarantined=""

# The gate's fuzz budget is targets x fuzztime, so the target count is what
# bounds its wall time. A cap makes that bound explicit: crossing it is a
# deliberate decision about how long every CI run should take, not something a
# new target does by accident.
readonly max_targets=16

# Minimizing a found crash is bounded too. Left at its 60s default it would
# dominate the run on the one occasion it matters, and the reproducer is written
# out either way.
readonly minimize_time=10s

# The go command, which scripts/verify-fuzz-retry.sh replaces with one that plays
# out each way a search can end.
readonly go_cmd="${FUZZ_GO:-go}"

# Each search's output, kept for the one question asked of it after a failure.
search_log="$(mktemp)"
readonly search_log
trap 'rm -f "$search_log"' EXIT

# corpus_files lists the reproducers <pkg>'s corpus holds for <target>, which is
# where a search writes the input it failed on.
corpus_files() {
	local dir="$1/testdata/fuzz/$2"
	if [ -d "$dir" ]; then
		find "$dir" -type f | LC_ALL=C sort
	fi
}

# search runs one bounded search of <target> in <pkg>, showing its output as it
# runs and keeping it in $search_log.
search() {
	"$go_cmd" test "$1" -run '^$' -fuzz "^${2}\$" \
		-fuzztime="$fuzztime" -fuzzminimizetime="$minimize_time" 2>&1 | tee "$search_log"
	return "${PIPESTATUS[0]}"
}

# fuzz_target searches <target> in <pkg>, and searches once more when the first
# search failed on nothing but its own deadline (GitHub #466).
#
# The fuzz coordinator can hit -fuzztime while a worker is still executing an
# input, and then reports "context deadline exceeded" as a failure instead of
# stopping. Nothing was found: no reproducer is written, and the same code
# passes on the next run. Under runner load it happens often enough to redden
# unrelated changes, and a reader of the red step learns only by opening the
# log that there is nothing in it.
#
# A finding writes its input under testdata/fuzz/<target>/ before the search
# exits, so a failure that wrote one is a finding whatever else its output
# says, and is never retried. Nor is any failure that does not name the
# deadline. The retry is one search, not a loop: a coordinator that times out
# twice running is a problem with the runner worth seeing, and the budget
# targets x fuzztime stays the bound on a clean run.
fuzz_target() {
	local pkg="$1" target="$2" before
	before="$(corpus_files "$pkg" "$target")"
	if search "$pkg" "$target"; then
		return 0
	fi
	if ! grep -q 'context deadline exceeded' "$search_log" ||
		[ "$(corpus_files "$pkg" "$target")" != "$before" ]; then
		return 1
	fi
	printf 'fuzz.sh: %s stopped on the fuzz coordinator'"'"'s own deadline and wrote no reproducer; searching once more (GitHub #466)\n' "$target"
	search "$pkg" "$target"
}

found=0
fuzzed=0
seen=""
while IFS=: read -r file _ decl; do
	target="${decl#func }"
	target="${target%%(*}"
	pkg="./$(dirname "$file")"

	found=$((found + 1))
	seen="$seen $target"
	if [ "$found" -gt "$max_targets" ]; then
		echo "fuzz.sh: more than $max_targets fuzz targets; raise max_targets deliberately" >&2
		exit 1
	fi

	case " $quarantined " in
	*" $target "*)
		printf '=== skip %s (quarantined — see scripts/fuzz.sh)\n' "$target"
		continue
		;;
	esac

	printf '=== fuzz %s (%s) for %s\n' "$target" "$pkg" "$fuzztime"
	fuzz_target "$pkg" "$target"
	fuzzed=$((fuzzed + 1))
done < <(git grep -n '^func Fuzz' -- '*_test.go')

# A sweep that matched nothing exits 0 from an empty loop and reads exactly like
# a clean run. Refuse to report one: either the grammar above stopped matching
# the declarations, or the targets are gone.
if [ "$fuzzed" -eq 0 ]; then
	echo "fuzz.sh: no fuzz target was searched" >&2
	exit 1
fi

# A quarantine entry naming a target that no longer exists silently holds back
# nothing, and reads as if it still does.
for held in $quarantined; do
	case " $seen " in
	*" $held "*) ;;
	*)
		echo "fuzz.sh: quarantine names $held, which is not a fuzz target; remove it" >&2
		exit 1
		;;
	esac
done

printf 'fuzzed %d of %d target(s), %s each\n' "$fuzzed" "$found" "$fuzztime"
