#!/usr/bin/env bash
#
# go-cache.sh keeps a restored Go build cache whole for a CI run, and trims
# what main saves to the entries that run used.
#
# Go judges an entry by its mtime: a hit refreshes it once it is an hour old,
# and at most once a day a go command deletes, as it exits, every entry five
# days old. A restored cache keeps the mtimes it was saved with, so after five
# quiet days on main the first go command deletes all it did not use itself —
# and an entry no run uses any more rides along in every save for five days, so
# a cache saved on each merge would carry every archive built that week.
#
#   age    back-date every entry two hours, so a hit refreshes it and no entry
#          is old enough to trim
#   prune  delete the entries still back-dated, and the fuzz corpus, so each
#          run fuzzes from the committed seeds alone
#
# An entry is what Go's own trim removes: $GOCACHE/xx/<hash>-a or -d, where a -d
# may be a directory holding a cached executable. The cache is the one
# `go env GOCACHE` names; asking does not trim it.
#
# Usage: go-cache.sh age|prune
set -euo pipefail

usage() {
	echo "usage: $(basename "$0") age|prune" >&2
	exit 2
}

[ "$#" -eq 1 ] || usage
mode="$1"
dir="$(go env GOCACHE)"
if [ ! -d "$dir" ]; then
	echo "go-cache.sh: no build cache at $dir" >&2
	exit 1
fi

# Carries the back-dated time from age to prune: an entry no newer than it is
# one this run neither hit nor wrote.
marker="$dir/morphic-aged"

entries() {
	find "$dir" -mindepth 2 -maxdepth 2 -path "$dir/[0-9a-f][0-9a-f]/*" -name '*-[ad]' "$@"
}

case "$mode" in
age)
	# GNU date, then BSD date: CI runs Linux, a developer machine may not.
	stamp="$(date -u -d '2 hours ago' +%Y%m%d%H%M.%S 2>/dev/null ||
		date -u -v-2H +%Y%m%d%H%M.%S)"
	TZ=UTC touch -t "$stamp" "$marker"
	entries -exec touch -r "$marker" {} +

	aged="$(entries | wc -l | tr -d ' ')"
	echo "go-cache.sh: aged $aged entries"
	# Only a restored cache is aged, and a restored cache is never empty, so no
	# match means the pattern above no longer describes Go's layout.
	if [ "$aged" -eq 0 ]; then
		echo "::warning::go-cache.sh: no cache entries under $dir; has Go changed its cache layout?"
	fi
	;;
prune)
	total="$(entries | wc -l | tr -d ' ')"
	# No marker means nothing was restored, so every entry is this run's own.
	if [ -e "$marker" ]; then
		entries ! -newer "$marker" -exec rm -rf {} +
		rm -f "$marker"
	fi
	rm -rf "$dir/fuzz"
	echo "go-cache.sh: kept $(entries | wc -l | tr -d ' ') of $total entries"
	;;
*)
	usage
	;;
esac
