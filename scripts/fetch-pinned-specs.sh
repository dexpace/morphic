#!/usr/bin/env bash
#
# fetch-pinned-specs.sh fetches each spec internal/harness/pinnedspecs lists, at
# the commit the list pins, into a directory outside the repository, where the
# package's sweep test reads it with -pinned. The specs are not ours to commit,
# so a directory inside the checkout is refused.
#
# It checks no hash: the test checks each before compiling a byte of it
# (pinnedspecs.Spec.Read), so the check has one implementation, which the gate's
# tests hold. Each spec is written to <dir>/<name>/<its file's own name>, the
# path pinnedspecs.Spec.File names. A list other than the committed one is
# read in its place, as the package's tests read one to hold this script's
# reading of a list to pinnedspecs.ReadList's.
#
# Usage: fetch-pinned-specs.sh <dir> [<list>]        (dir must exist)
set -euo pipefail

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ] || [ ! -d "$1" ] || { [ "$#" -eq 2 ] && [ ! -f "$2" ]; }; then
	echo "usage: fetch-pinned-specs.sh <an existing directory> [<a list file>]" >&2
	exit 2
fi
dir="$(cd "$1" && pwd -P)"
list=internal/harness/pinnedspecs/testdata/specs.txt
if [ "$#" -eq 2 ]; then
	list="$(cd "$(dirname "$2")" && pwd -P)/$(basename "$2")"
fi

cd "$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
root="$(pwd -P)"
case "$dir/" in
"$root"/*)
	echo "fetch-pinned-specs: $dir is inside the checkout; fetch outside it" >&2
	exit 2
	;;
esac

fetched=0
while read -r name repository commit path _; do
	case "$name" in '' | '#'*) continue ;; esac
	url="https://raw.githubusercontent.com/$repository/$commit/$path"
	mkdir -p "$dir/$name"
	if ! curl --fail --silent --show-error --location --proto '=https' --retry 3 \
		--max-time 120 --output "$dir/$name/${path##*/}" "$url"; then
		echo "fetch-pinned-specs: $name: cannot fetch $url" >&2
		exit 1
	fi
	echo "fetch-pinned-specs: $name: $repository@${commit:0:7} $path"
	fetched=$((fetched + 1))
done <"$list"

if [ "$fetched" -eq 0 ]; then
	echo "fetch-pinned-specs: $list names no spec" >&2
	exit 1
fi
