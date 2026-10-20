#!/usr/bin/env bash
#
# lint-planted.sh holds the lint rules a clean tree cannot prove to the tree and
# to the findings planted for them. It runs golangci-lint, with the repository's
# configuration and the linters in $linters only, over the module and over each
# package under internal/lintrules/testdata/planted, where each line ending in
# `// want "<message>"` holds something a rule exists to refuse. It fails unless
# the plants draw exactly the marked findings and the module draws none.
#
# The module alone cannot prove a rule: one that recognizes nothing leaves it as
# clean as one that works. The plants also pin what must draw nothing, such as
# encoding/json/v2 or a test file's forced assertion, so a rule cannot pass by
# matching everything either.
set -euo pipefail
export LC_ALL=C # one byte order for both sorted lists

repo_root="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
cd "$repo_root"
dir=internal/lintrules/testdata/planted
linters=containedctx,depguard,errname,exhaustive,forbidigo,forcetypeassert,godot,predeclared

# The packages under $dir. ./... does not reach into a testdata directory, so
# each is named.
pkgs=()
while IFS= read -r pkg; do
	pkgs+=("./$pkg")
done < <(find "$dir" -name '*.go' -exec dirname {} \; | sort -u)

# Each marker becomes "<file>:<line>: <message>"; `// want "` is nine bytes.
want="$(find "$dir" -name '*.go' -exec awk 'match($0, /\/\/ want "[^"]*"$/) {
	print FILENAME ":" FNR ": " substr($0, RSTART + 9, RLENGTH - 10)
}' {} + | sort)"
if [[ -z "$want" ]]; then
	echo "lint-planted: no // want markers under $dir" >&2
	exit 1
fi

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

# A finding prints as "<file>:<line>:<column>: <message> (<linter>)", the column
# only when it is known; it goes. A golangci-lint that cannot run, a
# configuration that does not load included, exits non-zero whatever
# --issues-exit-code says, and ends the script.
out="$(golangci-lint run --enable-only "$linters" \
	--issues-exit-code 0 --max-issues-per-linter 0 --max-same-issues 0 \
	--output.text.path stdout --output.text.print-issued-lines=false \
	--output.text.colors=false ./... "${pkgs[@]}")"
findings="$(printf '%s\n' "$out" | sed -nE 's|^([^ :]+\.go:[0-9]+)(:[0-9]+)?: |\1: |p' | sort)"
got="$(printf '%s\n' "$findings" | awk -v d="$dir/" 'index($0, d) == 1')"
stray="$(printf '%s\n' "$findings" | awk -v d="$dir/" 'NF && index($0, d) != 1')"

# golangci-lint ends with "<n> issues" whatever it found, so a finding this
# script could not read is a count that does not match, not a silent pass.
status=0
reported="$(printf '%s\n' "$out" | sed -nE 's/^([0-9]+) issues[:.]$/\1/p')"
read_count="$(printf '%s\n' "$findings" | awk 'NF' | wc -l | tr -d ' ')"
if [[ "$reported" != "$read_count" ]]; then
	echo "lint-planted: golangci-lint reported ${reported:-an unknown number of} issues; $read_count were read:" >&2
	printf '%s\n' "$out" >&2
	status=1
fi
if [[ -n "$stray" ]]; then
	echo "lint-planted: the module draws findings:" >&2
	printf '%s\n' "$stray" >&2
	status=1
fi
if [[ "$got" != "$want" ]]; then
	echo "lint-planted: the plants' findings are not the marked ones (< want, > got):" >&2
	diff <(printf '%s\n' "$want") <(printf '%s\n' "$got") >&2 || true
	status=1
fi
if ((status == 0)); then
	echo "lint-planted: the module is clean and the $(printf '%s\n' "$want" | wc -l | tr -d ' ') planted findings are drawn, nothing else"
fi
exit "$status"
