#!/usr/bin/env bash
#
# lint-planted.sh holds the rules .golangci.yml configures, which a clean tree
# cannot prove, to the tree and to findings planted for them. It runs
# golangci-lint with the repository's configuration and the linters in $linters
# only, over the module and the packages under
# internal/lintrules/testdata/planted, where each line ending in
# `// want "<message> (<linter>)"` holds something a rule exists to refuse, a
# quote in the message written \". It fails unless the plants draw exactly the
# marked findings and the module draws none.
#
# The module alone cannot prove a rule: one that recognizes nothing leaves it as
# clean as one that works. The plants also pin what must draw nothing, such as
# encoding/json/v2 or a test file's forced assertion, so a rule cannot pass by
# matching everything either.
#
# It reads golangci-lint's JSON output with jq, which CI's runner and recent
# macOS ship.
#
# Usage: lint-planted.sh
set -euo pipefail
export LC_ALL=C # one byte order for both sorted lists

# The run is the host's alone, so an argument, such as a port to lint as well,
# is refused rather than read as checked.
if [[ $# -ne 0 ]]; then
	echo "usage: lint-planted.sh" >&2
	exit 2
fi

cd "$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
testdata=internal/lintrules/testdata
linters=bidichk,containedctx,depguard,durationcheck,errname,exhaustive,fatcontext,forbidigo,forcetypeassert,gocheckcompilerdirectives,gochecknoinits,godot,makezero,nilnesserr,predeclared,reassign,revive,tagliatelle,testifylint

# --enable-only runs these linters whatever .golangci.yml enables, so the
# configuration is asked as well: a linter it leaves off is one `make lint` and
# the lint action never run, however its plants fare here. awk reads to the end
# rather than exit at the list's end: golangci-lint still writing to a closed
# pipe dies of SIGPIPE, which pipefail turns into this script's failure.
enabled="$(golangci-lint linters | awk '/^Enabled by your configuration/ { on = 1; next }
	on && /^$/ { on = 0 }
	on { sub(/:.*/, ""); print }')"
for linter in ${linters//,/ }; do
	if [[ $'\n'"$enabled"$'\n' != *$'\n'"$linter"$'\n'* ]]; then
		echo "lint-planted: .golangci.yml does not enable $linter, so lint never runs its rules" >&2
		exit 1
	fi
done

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# The findings marked in the plants, as "<file>:<line>: <message>"; `// want "`
# is nine bytes. Only \" is read as an escape: a pattern quoted in a message
# keeps its other backslashes.
want="$(find "$testdata/planted" -name '*.go' -exec awk 'match($0, /\/\/ want "([^"\\]|\\.)*"$/) {
	text = substr($0, RSTART + 9, RLENGTH - 10)
	gsub(/\\"/, "\"", text)
	print FILENAME ":" FNR ": " text
}' {} + | sort)"
if [[ -z "$want" ]]; then
	echo "lint-planted: no // want markers under $testdata/planted, so a clean run proves nothing" >&2
	exit 1
fi

# The module and the plants are linted in one go; ./... does not reach into
# testdata, so the plants are named apart. Exit codes past --issues-exit-code, a
# configuration that does not load among them, end the script. A finding's file
# and line are fields of the JSON, so none goes unread, and one with no file is
# a stray. The warnings about exclusions for linters this run leaves out are
# dropped.
if ! golangci-lint run --enable-only "$linters" \
	--issues-exit-code 0 --max-issues-per-linter 0 --max-same-issues 0 \
	--output.json.path "$work/issues.json" ./... "./$testdata/planted/..." >/dev/null 2>"$work/stderr"; then
	cat "$work/stderr" >&2
	exit 1
fi
grep -v 'Skipped 0 issues by rules' "$work/stderr" >&2 || true
got="$(jq -r '(.Issues // [])[] | "\(.Pos.Filename):\(.Pos.Line): \(.Text) (\(.FromLinter))"' \
	"$work/issues.json" | sort)"

if [[ "$got" != "$want" ]]; then
	echo "lint-planted: the findings are not the marked ones (< want, > got):" >&2
	diff <(printf '%s\n' "$want") <(printf '%s\n' "$got") >&2 || true
	exit 1
fi
echo "lint-planted: the module is clean and the $(printf '%s\n' "$want" | wc -l | tr -d ' ') planted findings are drawn, nothing else"
