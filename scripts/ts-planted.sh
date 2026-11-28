#!/usr/bin/env bash
#
# ts-planted.sh holds the TypeScript toolchain the gate builds generated
# TypeScript with to files planted for it. It runs `tsc --strict --noEmit` over
# scripts/testdata/ts-planted: every file under builds/ must draw nothing, and
# the files under fails/ must draw exactly the findings their lines mark, each
# line ending in `// want "<code>"` holding something tsc must refuse.
#
# A toolchain alone proves nothing: a tsc that reports nothing, or one run with
# strict checking off, leaves a tree of generated code as clean as one that works.
# The plants show the compiler on PATH refuses what a strict build must and
# accepts what it must.
#
# node and tsc are held to the releases the Makefile pins. Where the third
# argument is `required`, as the gate's workflow passes it, a toolchain that is
# absent or at another release fails. Otherwise an absent one is reported and
# the plants are skipped, and another release is reported and still run, though
# what it finds is not what the gate's would: a contributor's machine need not
# hold Node.js to run the rest of the gate, and a run that skipped says so.
#
# Usage: ts-planted.sh <node-version> <typescript-version> [required|optional]
set -euo pipefail
export LC_ALL=C # one byte order for both sorted lists

if [[ $# -lt 2 || $# -gt 3 ]]; then
	echo "usage: ts-planted.sh <node-version> <typescript-version> [required|optional]" >&2
	exit 2
fi
want_node="$1"
want_tsc="$2"
mode="${3:-optional}"
if [[ "$mode" != required && "$mode" != optional ]]; then
	echo "ts-planted: the third argument is required or optional, not $mode" >&2
	exit 2
fi

cd "$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
plants=scripts/testdata/ts-planted

# miss reports a toolchain that is absent or at another release: an error where
# the toolchain is required, a warning otherwise.
miss() {
	if [[ "$mode" == required ]]; then
		echo "ts-planted: $1" >&2
		exit 1
	fi
	echo "warning: $1" >&2
}

for tool in node tsc; do
	if ! command -v "$tool" >/dev/null; then
		miss "$tool is not on PATH, the gate pins Node.js $want_node and TypeScript $want_tsc"
		echo "warning:   the planted TypeScript files were not built" >&2
		echo "warning:   install Node.js $want_node, then: npm install --global typescript@$want_tsc" >&2
		exit 0
	fi
done

have_node="$(node --version)"
have_node="${have_node#v}"
if [[ "$have_node" != "$want_node" ]]; then
	miss "local Node.js is $have_node, the gate pins $want_node"
fi
have_tsc="$(tsc --version)"
have_tsc="${have_tsc#Version }"
if [[ "$have_tsc" != "$want_tsc" ]]; then
	miss "local TypeScript is $have_tsc, the gate pins $want_tsc: npm install --global typescript@$want_tsc"
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# The files are found before tsc runs, so a directory emptied by a move fails
# here rather than passing with nothing built. A glob, since macOS's bash has
# no mapfile.
shopt -s nullglob
builds=("$plants"/builds/*.ts)
fails=("$plants"/fails/*.ts)
shopt -u nullglob
if [[ ${#builds[@]} -eq 0 || ${#fails[@]} -eq 0 ]]; then
	echo "ts-planted: $plants holds no file under builds/ or none under fails/" >&2
	exit 1
fi

if ! tsc --strict --noEmit --pretty false "${builds[@]}" >"$work/builds.out" 2>&1; then
	echo "ts-planted: tsc refuses a file under $plants/builds:" >&2
	cat "$work/builds.out" >&2
	exit 1
fi

# The findings marked in the plants, as "<file>:<line>: <code>".
for file in "${fails[@]}"; do
	awk -v file="$file" 'match($0, /\/\/ want "TS[0-9]+"$/) {
		print file ":" FNR ": " substr($0, RSTART + 9, RLENGTH - 10)
	}' "$file"
done | sort >"$work/want"
if [[ ! -s "$work/want" ]]; then
	echo "ts-planted: no line under $plants/fails marks a finding" >&2
	exit 1
fi

# tsc prints "<file>(<line>,<column>): error <code>: <message>" for each, and
# exits non-zero when it reports any.
if tsc --strict --noEmit --pretty false "${fails[@]}" >"$work/fails.out" 2>&1; then
	echo "ts-planted: tsc accepts every file under $plants/fails" >&2
	exit 1
fi
sed -n -E 's/^([^(]+)\(([0-9]+),[0-9]+\): error (TS[0-9]+): .*/\1:\2: \3/p' "$work/fails.out" |
	sort >"$work/got"

if ! diff "$work/want" "$work/got" >"$work/diff"; then
	echo "ts-planted: tsc's findings are not the planted ones (< wanted, > reported):" >&2
	cat "$work/diff" >&2
	echo "ts-planted: tsc printed:" >&2
	cat "$work/fails.out" >&2
	exit 1
fi
echo "ts-planted: tsc $have_tsc on Node.js $have_node draws the $(wc -l <"$work/want" | tr -d ' ') planted findings"
