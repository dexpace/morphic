#!/usr/bin/env bash
#
# fuzz.sh runs the module's fuzz targets under a bounded search.
#
# `go test` executes a target's seed corpus on every ordinary run but never
# mutates; only -fuzz searches, and -fuzz takes one package and one target per
# invocation. So a search needs a loop, and the loop derives its pairs from the
# packages `./...` names: a target written tomorrow is fuzzed the moment it
# lands, there is no list here to go stale, and nothing under testdata counts.
#
# The search runs nightly and by hand, not on a pull request. The gate runs
# `check`, which holds the targets to the limits below and searches nothing.
#
# Usage: fuzz.sh [fuzztime]    search each target (default 5s, or $FUZZTIME)
#        fuzz.sh check         hold the targets to the limits, searching nothing
#        fuzz.sh prune         bound the corpus Go keeps under $GOCACHE/fuzz
set -euo pipefail

cd "$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"

mode="${1:-${FUZZTIME:-5s}}"

# Targets held out of the search, each naming the issue that must close before
# it comes back. A search is not the place to rediscover a bug that is already
# filed: it spends its whole budget re-finding one input and reddens every
# night until the fix lands.
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

# A search's budget is targets x fuzztime, so the target count is what bounds
# its wall time, and the nightly job has a timeout to stay inside. A cap makes
# that bound explicit: crossing it is a deliberate decision about how long the
# search takes, not something a new target does by accident.
readonly max_targets=16

# Minimizing is bounded tightly. The flag governs every new input that adds
# coverage as well as a found crash, a worker minimizing runs no search, and left
# at its 60s default it would dominate the run. The reproducer of a crash is
# written out either way, so a shorter bound costs at most a larger reproducer.
readonly minimize_time=1s

# What prune keeps of each target's corpus. Go never trims the directory, so
# these two bound what a search restores and what the cache holds: at most
# targets x max_corpus_entries x max_entry_kb. The count sits above what a target
# still builds on, since FuzzCycleDetector neared 1,000 entries in five nights
# with its coverage still rising, and below where re-running every entry, which
# a search does inside its budget, would leave it little time to search.
readonly max_corpus_entries=3000
readonly max_entry_kb=64

# list_targets prints "<import path> <target> <directory>", one line per target.
list_targets() {
	local import dir decl
	go list -f '{{.ImportPath}} {{.Dir}}' ./... | while read -r import dir; do
		{ grep -h '^func Fuzz' "$dir"/*_test.go 2>/dev/null || true; } | while read -r _ decl; do
			printf '%s %s %s\n' "$import" "${decl%%(*}" "$dir"
		done
	done
}

# is_quarantined reports whether the quarantine holds the target back.
is_quarantined() {
	case " $quarantined " in
	*" $1 "*) return 0 ;;
	*) return 1 ;;
	esac
}

# check_targets holds the target list to the limits, so a pull request hears
# about a breach rather than a night's search.
check_targets() {
	local import target _dir found=0 searchable=0 seen="" held
	while read -r import target _dir; do
		[ -n "$target" ] || continue
		found=$((found + 1))
		seen="$seen $target"
		is_quarantined "$target" || searchable=$((searchable + 1))
	done <<<"$targets"

	if [ "$found" -gt "$max_targets" ]; then
		echo "fuzz.sh: more than $max_targets fuzz targets; raise max_targets deliberately" >&2
		exit 1
	fi

	# A list that matched nothing reads exactly like a clean run. Refuse one:
	# either the grammar above stopped matching the declarations, or the targets
	# are gone.
	if [ "$searchable" -eq 0 ]; then
		echo "fuzz.sh: no fuzz target would be searched" >&2
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

	printf '%d fuzz target(s), %d searchable\n' "$found" "$searchable"
}

# trim_corpus keeps max_corpus_entries of one target's directory, and says so
# when that costs entries: a target at the cap has stopped adding up. Entries are
# named for their contents, so sorting by name evicts an arbitrary subset.
trim_corpus() {
	local evicted
	evicted="$(find "$1" -type f | LC_ALL=C sort | tail -n +$((max_corpus_entries + 1)))"
	[ -n "$evicted" ] || return 0
	printf '%s\n' "$evicted" | while IFS= read -r entry; do
		rm -f "$entry"
	done
	printf 'fuzz.sh: %s is past %d entries; dropped %d\n' \
		"${1##*/}" "$max_corpus_entries" "$(printf '%s\n' "$evicted" | wc -l | tr -d ' ')"
}

# prune_corpus drops every target that no longer exists, and every entry over the
# size limit, then trims what is left. A target that moved package is a target
# that no longer exists at its old import path.
prune_corpus() {
	local root import target _dir keep="" dir
	root="$(go env GOCACHE)/fuzz"
	if [ ! -d "$root" ]; then
		echo "fuzz.sh: no corpus under $root"
		return 0
	fi
	while read -r import target _dir; do
		keep="$keep$import/$target"$'\n'
	done <<<"$targets"

	find "$root" -type f -size +"${max_entry_kb}k" -exec rm -f {} +
	while IFS= read -r dir; do
		# Not a pipe: under pipefail, grep -q leaving early would read as no match.
		if grep -qxF "${dir#"$root"/}" <<<"$keep"; then
			trim_corpus "$dir"
		else
			rm -rf "$dir"
		fi
	done < <(find "$root" -type f | sed 's#/[^/]*$##' | LC_ALL=C sort -u)

	printf 'corpus: %s kB in %s entries\n' \
		"$(du -sk "$root" | cut -f1)" "$(find "$root" -type f | wc -l | tr -d ' ')"
}

# search runs every target, one after another, and carries on past a failure: a
# finding in one target must not cost the rest of the night's search.
search() {
	local fuzztime="$1" import target _dir fuzzed=0 failed=""

	# go test handles an interrupt and exits, which reads as a failed target, so
	# without this the loop would carry on to the next one. The shell runs it once
	# go test has gone.
	trap 'echo "fuzz.sh: interrupted" >&2; exit 130' INT

	while read -r import target _dir; do
		if is_quarantined "$target"; then
			printf '=== skip %s (quarantined — see scripts/fuzz.sh)\n' "$target"
			continue
		fi
		printf '=== fuzz %s (%s) for %s\n' "$target" "$import" "$fuzztime"
		fuzzed=$((fuzzed + 1))
		go test "$import" -run '^$' -fuzz "^${target}\$" \
			-fuzztime="$fuzztime" -fuzzminimizetime="$minimize_time" || failed="$failed $target"
	done <<<"$targets"

	if [ -n "$failed" ]; then
		echo "fuzz.sh: failed:$failed" >&2
		exit 1
	fi
	printf 'fuzzed %d target(s), %s each\n' "$fuzzed" "$fuzztime"
}

targets="$(list_targets)"
check_targets
case "$mode" in
check) ;;
prune) prune_corpus ;;
*) search "$mode" ;;
esac
