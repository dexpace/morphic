#!/usr/bin/env bash
#
# vet-rules.sh runs the repository's own lint rules, internal/lintrules, over the
# module through go vet and cmd/morphic-vet. The run must find the module clean,
# and must report the one finding planted in internal/lintrules/testdata/reach,
# which shows it reads the packages it is given: a tool that ran no rule would
# leave the module just as clean.
#
# go vet caches each package's result under the tool's build ID, so an edit to a
# rule reruns every package, and an unchanged tree costs little.
#
# Usage: vet-rules.sh
set -euo pipefail

# The run is the host's alone, so an argument, such as a port to vet as well,
# is refused rather than read as checked.
if [[ $# -ne 0 ]]; then
	echo "usage: vet-rules.sh" >&2
	exit 2
fi

cd "$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
reach=internal/lintrules/testdata/reach

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
tool="$work/morphic-vet"
go build -o "$tool" ./cmd/morphic-vet

# The module and the plant are vetted in one go: their findings, the file each
# is in, must be exactly the plant. A finding in the module, or an error, names
# a file of its own. go vet replays a cached finding with the path of the tree
# that produced it, so the plant's file is read by its path below the module root,
# whichever tree ran first.
status=0
go vet -vettool="$tool" ./... "./$reach" 2>"$work/vet.out" || status=$?
got="$(sed -nE 's/^([^#][^:]*):[0-9]+:[0-9]+: .*/\1/p' "$work/vet.out" |
	sed -E "s#^.*/($reach/reach\.go)\$#\1#")"
if [[ "$got" != "$reach/reach.go" || "$status" -ne 1 ]]; then
	echo "vet-rules: the one finding must be in $reach/reach.go (exit $status):" >&2
	cat "$work/vet.out" >&2
	exit 1
fi
echo "vet-rules: the module is clean, and $reach/reach.go draws its finding"
