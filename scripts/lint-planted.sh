#!/usr/bin/env bash
#
# lint-planted.sh holds the rules .golangci.yml configures, which a clean tree
# cannot prove, to the tree and to findings planted for them. It runs
# golangci-lint with the repository's configuration and the linters in $linters
# only, over the module and the packages under internal/lintrules/testdata, where
# each line ending in `// want "<message> (<linter>)"` holds something a rule
# exists to refuse. It fails unless the plants draw exactly the marked findings
# and the module draws none.
#
# The module alone cannot prove a rule: one that recognizes nothing leaves it as
# clean as one that works. The plants also pin what must draw nothing, such as
# encoding/json/v2 or a test file's forced assertion, so a rule cannot pass by
# matching everything either.
#
# Lint reads the files one port's build includes, so the run is repeated on each
# port given as GOOS/GOARCH. The plants under testdata/ports that a port alone
# builds are marked for that port's run, which shows each run reads its port.
#
# Usage: lint-planted.sh [GOOS/GOARCH ...]        (make lint-planted passes $(PORTS))
set -euo pipefail
export LC_ALL=C # one byte order for both sorted lists

cd "$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
testdata=internal/lintrules/testdata
linters=containedctx,depguard,errname,exhaustive,forbidigo,forcetypeassert,godot,predeclared

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

# The plant packages, named one by one: ./... does not reach into testdata.
pkgs=()
while IFS= read -r pkg; do
	pkgs+=("./$pkg")
done < <(find "$testdata/planted" "$testdata/ports" -name '*.go' -exec dirname {} \; | sort -u)

# markers prints each finding marked in the files under $1 named like $2, as
# "<file>:<line>: <message>"; `// want "` is nine bytes.
markers() {
	find "$1" -name "$2" -exec awk 'match($0, /\/\/ want "[^"]*"$/) {
		print FILENAME ":" FNR ": " substr($0, RSTART + 9, RLENGTH - 10)
	}' {} + | sort
}

planted="$(markers "$testdata/planted" '*.go')"
if [[ -z "$planted" ]]; then
	echo "lint-planted: no // want markers under $testdata/planted, so a clean run proves nothing" >&2
	exit 1
fi

# run lints the module and the plants on the port $1 names as GOOS/GOARCH, and
# fails unless the plants draw exactly the marked findings: those under planted,
# and those of the ports file of that port's GOOS, if there is one.
run() {
	local goos="${1%/*}" goarch="${1#*/}" want got
	want="$planted"
	if [[ -f "$testdata/ports/ports_$goos.go" ]]; then
		want="$(printf '%s\n%s\n' "$planted" "$(markers "$testdata/ports" "ports_$goos.go")" | sort)"
	fi

	# Exit codes past --issues-exit-code, a configuration that does not load
	# among them, end the script. A finding's file and line are fields of the
	# JSON, so none goes unread, and one with no file is a stray. The warnings
	# about exclusions for linters this run leaves out are dropped.
	if ! GOOS="$goos" GOARCH="$goarch" golangci-lint run --enable-only "$linters" \
		--issues-exit-code 0 --max-issues-per-linter 0 --max-same-issues 0 \
		--output.json.path "$work/issues.json" ./... "${pkgs[@]}" >/dev/null 2>"$work/stderr"; then
		cat "$work/stderr" >&2
		exit 1
	fi
	grep -v 'Skipped 0 issues by rules' "$work/stderr" >&2 || true
	got="$(jq -r '(.Issues // [])[] | "\(.Pos.Filename):\(.Pos.Line): \(.Text) (\(.FromLinter))"' \
		"$work/issues.json" | sort)"

	if [[ "$got" != "$want" ]]; then
		echo "lint-planted: on $1, the findings are not the marked ones (< want, > got):" >&2
		diff <(printf '%s\n' "$want") <(printf '%s\n' "$got") >&2 || true
		exit 1
	fi
	echo "lint-planted: on $1, the module is clean and the $(printf '%s\n' "$want" | wc -l | tr -d ' ') planted findings are drawn, nothing else"
}

for port in "$(go env GOOS)/$(go env GOARCH)" "$@"; do
	if [[ "$port" != */* ]]; then
		echo "lint-planted: $port is no GOOS/GOARCH pair" >&2
		exit 2
	fi
	run "$port"
done
