#!/usr/bin/env bash
#
# verify-fuzz-retry.sh checks how scripts/fuzz.sh answers a failed search.
#
# fuzz.sh searches a target once more when the search failed on nothing but the
# fuzz coordinator's own deadline (GitHub #466). That decision is only ever
# reached on a loaded CI runner, never on demand, so a real `go test -fuzz`
# cannot drive it. This drives the script with a stand-in for the go command
# that plays out each way a search can end, in a throwaway repository holding
# one fuzz target, and counts the searches each run made.
#
# Each case is followed by a mutation that must break it, for the reason
# verify-coverage-count.sh gives: a case that stays green against a
# deliberately broken script is not testing the script.
#
# Usage:
#   scripts/verify-fuzz-retry.sh
set -euo pipefail

repo_root="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
script="$repo_root/scripts/fuzz.sh"

work="$(mktemp -d)"
readonly work
trap 'rm -rf "$work"' EXIT

failures=0

fail() {
	printf '  FAIL: %s\n' "$1"
	failures=$((failures + 1))
}

pass() { printf '  ok: %s\n' "$1"; }

# The stand-in go command. Each call is one search: it appends to $CALLS, and
# SCENARIO says how that search ends. A finding writes its input where the
# real command does, under the package's testdata/fuzz/<target>/.
mkdir -p "$work/bin"
cat >"$work/bin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
echo call >>"$CALLS"
calls="$(wc -l <"$CALLS" | tr -d ' ')"
pkg="$2"
deadline() { printf -- '--- FAIL: FuzzX (10.05s)\n    context deadline exceeded\nFAIL\n'; exit 1; }
case "$SCENARIO" in
clean) echo ok ;;
deadline-once) [ "$calls" -gt 1 ] && echo ok || deadline ;;
deadline-always) deadline ;;
finding)
	mkdir -p "$pkg/testdata/fuzz/FuzzX"
	echo 'go test fuzz v1' >"$pkg/testdata/fuzz/FuzzX/0123"
	printf 'Failing input written to testdata/fuzz/FuzzX/0123\n'
	deadline
	;;
other) printf -- '--- FAIL: FuzzX (0.01s)\n    x_test.go:3: property broken\nFAIL\n'; exit 1 ;;
*) echo "unknown scenario $SCENARIO" >&2; exit 2 ;;
esac
EOF
chmod +x "$work/bin/go"

# run_scenario runs <fuzz script> under <scenario> in a fresh repository and
# echoes "<exit status> <searches made>", with the script's output left in
# $work/out.
run_scenario() {
	local fuzz="$1" scenario="$2" repo="$work/repo-$2"
	rm -rf "$repo"
	mkdir -p "$repo/p" "$repo/scripts"
	git -C "$repo" init -q
	printf 'package p\n\nimport "testing"\n\nfunc FuzzX(f *testing.F) {}\n' >"$repo/p/x_test.go"
	cp "$fuzz" "$repo/scripts/fuzz.sh"
	git -C "$repo" add -A

	local status=0
	: >"$work/calls"
	CALLS="$work/calls" SCENARIO="$scenario" FUZZ_GO="$work/bin/go" \
		bash "$repo/scripts/fuzz.sh" 1s >"$work/out" 2>&1 || status=$?
	printf '%d %d' "$status" "$(wc -l <"$work/calls" | tr -d ' ')"
}

# One record per case: <scenario> | <want exit status> | <want searches> |
# <substring the output must hold, or "" for none>.
cases=(
	'clean|0|1|fuzzed 1 of 1 target(s)'
	'deadline-once|0|2|stopped on the fuzz coordinator'"'"'s own deadline'
	'deadline-always|1|2|'
	'finding|1|1|Failing input written to'
	'other|1|1|property broken'
)

# check_case runs one record against <fuzz script> and echoes why it did not
# hold, echoing nothing and returning 0 when it did.
check_case() {
	local record="$1" fuzz="$2"
	local scenario="${record%%|*}" rest="${record#*|}"
	local want_status="${rest%%|*}"
	rest="${rest#*|}"
	local want_calls="${rest%%|*}" want="${rest#*|}"

	local got
	got="$(run_scenario "$fuzz" "$scenario")"
	if [ "$got" != "$want_status $want_calls" ]; then
		printf 'exit and searches %s, want %s %s' "$got" "$want_status" "$want_calls"
		return 1
	fi
	if [ -n "$want" ] && ! grep -qF -- "$want" "$work/out"; then
		printf 'output does not hold "%s"' "$want"
		return 1
	fi
	return 0
}

case_record() {
	local record
	for record in "${cases[@]}"; do
		if [ "${record%%|*}" = "$1" ]; then
			printf '%s' "$record"
			return 0
		fi
	done
	printf 'no case plays scenario %s\n' "$1" >&2
	return 1
}

printf 'searches\n'
for record in "${cases[@]}"; do
	if reason="$(check_case "$record" "$script")"; then
		pass "${record%%|*}"
	else
		fail "${record%%|*}: $reason"
	fi
done

# mutate echoes fuzz.sh with the sole occurrence of <from> replaced by <to>,
# as exact text, for the reason verify-coverage-count.sh gives.
mutate() {
	awk -v from="$1" -v to="$2" '
		index($0, from) {
			$0 = substr($0, 1, index($0, from) - 1) to substr($0, index($0, from) + length(from))
		}
		{ print }
	' "$script"
}

# Each mutation removes one decision fuzz_target makes, and names the case that
# has to notice.
#
# <name> | <case the mutation must break> | <text to replace> | <replacement>
#
# The retry is the one line that is a tab and then `search`: the first search
# is written `if search`, so the two anchors below that name it are unique. No
# anchor may hold a `|`, which is what separates a record's fields.
mutations=(
	'no-retry|deadline-once|	search "$pkg" "$target"|	return 1'
	'retries-a-finding|finding|[ "$(corpus_files "$pkg" "$target")" != "$before" ]|false'
	'retries-anything|other|! grep -q '"'"'context deadline exceeded'"'"' "$search_log"|false'
	'retries-forever|deadline-always|	search "$pkg" "$target"|	fuzz_target "$pkg" "$target"'
)

clean_record="$(case_record clean)"

printf '\nmutations, each of which must turn its case red\n'
for record in "${mutations[@]}"; do
	name="${record%%|*}"
	rest="${record#*|}"
	target="${rest%%|*}"
	rest="${rest#*|}"
	from="${rest%%|*}"
	to="${rest#*|}"
	mutant="$work/mutant-$name.sh"

	if ! target_record="$(case_record "$target")"; then
		fail "$name: names a case no scenario above plays"
		continue
	fi

	hits="$(awk -v from="$from" '
		{
			left = $0
			while ((at = index(left, from)) > 0) {
				n++
				left = substr(left, at + length(from))
			}
		}
		END { print n + 0 }' "$script")"
	if [ "$hits" -ne 1 ]; then
		fail "$name: its anchor appears $hits times in fuzz.sh, want 1"
		continue
	fi

	mutate "$from" "$to" >"$mutant"
	if cmp -s "$script" "$mutant"; then
		fail "$name: changed nothing despite a unique anchor"
		continue
	fi

	# A clean search reaches no decision a mutation here alters. If it goes red,
	# the mutant is broken outright, and its effect on the target case would say
	# nothing about the decision the mutation removed.
	if ! check_case "$clean_record" "$mutant" >/dev/null; then
		fail "$name: breaks even the clean search, so it proves nothing"
		continue
	fi

	if check_case "$target_record" "$mutant" >/dev/null; then
		fail "$name: '$target' still passes with $name applied, so it does not test it"
	else
		pass "$name turns '$target' red"
	fi
done

if [ "$failures" -gt 0 ]; then
	printf '\n%d check(s) failed\n' "$failures"
	exit 1
fi
printf '\nall checks passed\n'
