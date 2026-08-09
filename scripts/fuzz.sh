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
#   FuzzCanonicalWords_Properties — dexpace/morphic#336, CanonicalWords is not
#   idempotent for "ℤℤA". Reached within seconds of mutation from the committed
#   seeds, so the target has nothing else to report until #336 is fixed.
#
# Ordinary `go test` still runs every one of these targets' seeds, quarantined
# or not; what is held back is the mutation.
quarantined="FuzzCanonicalWords_Properties"

# The gate's fuzz budget is targets x fuzztime, so the target count is what
# bounds its wall time. A cap makes that bound explicit: crossing it is a
# deliberate decision about how long every CI run should take, not something a
# new target does by accident.
readonly max_targets=16

# Minimizing a found crash is bounded too. Left at its 60s default it would
# dominate the run on the one occasion it matters, and the reproducer is written
# out either way.
readonly minimize_time=10s

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
	go test "$pkg" -run '^$' -fuzz "^${target}\$" \
		-fuzztime="$fuzztime" -fuzzminimizetime="$minimize_time"
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
