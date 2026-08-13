#!/usr/bin/env bash
#
# verify-coverage-count.sh checks how scripts/check-coverage.sh counts a profile.
#
# The gate script runs the suite and then counts what it produced. This drives only
# the counting half, over profiles a real run will not hand it: a block repeated three
# times, a block one fragment covered and another did not, a span two fragments give
# different statement counts. Those shapes are why the merge exists, and no committed
# spec produces them, so without this file the merge has no test at all.
#
# Each case is followed by a mutation that must break it. A case that stays green
# against a deliberately broken counter is not testing the counter — and this file is
# the only thing standing between a wrong count and a gate that reports it as fact.
#
# Usage:
#   scripts/verify-coverage-count.sh
set -euo pipefail

repo_root="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
script="$repo_root/scripts/check-coverage.sh"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

failures=0

# fail records a failed check and keeps going, so one run reports every problem
# rather than only the first.
fail() {
	printf '  FAIL: %s\n' "$1"
	failures=$((failures + 1))
}

pass() { printf '  ok: %s\n' "$1"; }

# profile writes $work/<name>.out with a coverage header and the given body lines.
profile() {
	local name="$1"
	shift
	printf 'mode: atomic\n' >"$work/$name.out"
	if [ "$#" -gt 0 ]; then
		printf '%s\n' "$@" >>"$work/$name.out"
	fi
}

profile sanity 'x/a.go:1.1,2.2 1 1'
profile triple \
	'x/a.go:1.1,2.2 3 1' \
	'x/a.go:1.1,2.2 3 1' \
	'x/a.go:1.1,2.2 3 1' \
	'x/b.go:5.1,6.2 2 4'
profile partial 'x/a.go:1.1,2.2 3 0' 'x/a.go:1.1,2.2 3 2'
profile dead 'x/a.go:1.1,2.2 3 0' 'x/a.go:1.1,2.2 3 0'
profile distinct 'x/a.go:1.1,2.2 1 1' 'x/a.go:3.1,4.2 1 0'
profile conflict 'x/a.go:1.1,2.2 3 1' 'x/a.go:1.1,2.2 4 1'
profile empty
profile zerostmt 'x/a.go:1.1,2.2 0 0'
profile badfields 'x/a.go:1.1,2.2 5'
# go emits a numstmt=0 block for an empty body, such as an unreached "case x:". It
# cannot be uncovered, so it must not be listed beside a passing verdict.
profile empty-block 'x/a.go:1.1,2.2 5 1' 'x/b.go:9.1,9.9 0 0'

# No "mode:" line at all, so this one cannot go through profile(). Its first block is
# the uncovered one: a reader that eats a body line as the header calls this a pass.
printf 'x/a.go:1.1,2.2 5 0\nx/b.go:1.1,2.2 5 1\n' >"$work/noheader.out"

# 30 uncovered blocks, five past the 25 the failure listing prints.
capped=()
for i in $(seq 30); do capped+=("x/c.go:$i.1,$i.9 1 0"); done
profile capped "${capped[@]}"

# One record per case: <profile> | <want exit status> | <substring stdout must hold>.
# A substring prefixed with "!" must be absent instead. The mutation sweep below looks
# cases up by profile name, so the first record for a name is the one a mutation has to
# break — which is why the "!" records come first where both kinds exist.
cases=(
	'triple|0|Coverage gate passed: all 5 statements covered.'
	'partial|0|Coverage gate passed: all 3 statements covered.'
	'distinct|1|Coverage gate failed: 1 of 2 statements uncovered'
	'conflict|1|!x/a.go:1.1,2.2 3 1'
	'conflict|1|COVERAGE FAIL: x/a.go:1.1,2.2 reports 3 and 4 statements'
	'conflict|1|Coverage gate failed: the profile does not describe a single build.'
	'dead|1|COVERAGE FAIL: x/a.go:1.1,2.2 (3 statement(s) uncovered)'
	'dead|1|Coverage gate failed: 3 of 3 statements uncovered'
	'empty|1|COVERAGE FAIL: the profile records no statements'
	'zerostmt|1|COVERAGE FAIL: the profile records no statements'
	'noheader|1|COVERAGE FAIL: the first line is not a "mode:" header'
	'noheader|1|Coverage gate failed: the profile could not be read.'
	'badfields|1|COVERAGE FAIL: line 2 has 2 field(s), want 3: x/a.go:1.1,2.2 5'
	'empty-block|0|!COVERAGE FAIL'
	'empty-block|0|Coverage gate passed: all 5 statements covered.'
	'capped|1|... and 5 more uncovered block(s)'
	'capped|1|Coverage gate failed: 30 of 30 statements uncovered'
	'sanity|0|Coverage gate passed: all 1 statements covered.'
)

# check_case drives one record through <counter> and echoes why it did not hold,
# echoing nothing and returning 0 when it did.
check_case() {
	local record="$1" counter="$2"
	local name="${record%%|*}" rest="${record#*|}"
	local want_status="${rest%%|*}" want="${rest#*|}"

	# A leading "!" inverts the check: the substring must be absent.
	local absent=""
	if [ "${want#!}" != "$want" ]; then
		absent=1
		want="${want#!}"
	fi

	# A case naming a profile nothing built would run against a missing file, and the
	# counter's "cannot read profile" answer carries both a COVERAGE FAIL and exit 1 —
	# so a typo here would pass, quietly retiring the check it was meant to be.
	if [ ! -f "$work/$name.out" ]; then
		printf 'no profile named %s.out was built' "$name"
		return 1
	fi

	# stdout only: what the counter reports there is the contract being checked, and
	# a mutant broken outright would otherwise bury the run in awk parse errors.
	local out status=0
	out="$("$counter" "$work/$name.out" 2>/dev/null)" || status=$?
	if [ "$status" -ne "$want_status" ]; then
		printf 'exit %d, want %s' "$status" "$want_status"
		return 1
	fi

	local held=""
	case "$out" in
	*"$want"*) held=1 ;;
	esac
	if [ -n "$absent" ]; then
		if [ -n "$held" ]; then
			printf 'stdout holds "%s", which it must not' "$want"
			return 1
		fi
		return 0
	fi
	if [ -z "$held" ]; then
		printf 'stdout does not hold "%s"' "$want"
		return 1
	fi
	return 0
}

# case_record echoes the first case record for <profile>.
case_record() {
	local record
	for record in "${cases[@]}"; do
		if [ "${record%%|*}" = "$1" ]; then
			printf '%s' "$record"
			return 0
		fi
	done
	printf 'no case uses profile %s\n' "$1" >&2
	return 1
}

printf 'counting\n'
for record in "${cases[@]}"; do
	if reason="$(check_case "$record" "$script")"; then
		pass "${record%%|*}: ${record##*|}"
	else
		fail "${record%%|*}: $reason"
	fi
done

# mutate echoes the gate script with the sole occurrence of <from> replaced by <to>.
# Exact text, not a regex: the mutants have to read the same under BSD and GNU
# userlands, and a pattern that quietly means something else under one of them is
# indistinguishable from a case that catches nothing.
mutate() {
	awk -v from="$1" -v to="$2" '
		index($0, from) {
			$0 = substr($0, 1, index($0, from) - 1) to substr($0, index($0, from) + length(from))
		}
		{ print }
	' "$script"
}

# Each mutation removes one decision the counter makes, and names the case that has to
# notice. The first four anchor in the merge program, the last three in what refuses a
# profile and what reports one.
#
# <name> | <case the mutation must break> | <text to replace> | <replacement>
mutations=(
	'unmerged|triple|block = $1|block = NR'
	'keyed-by-file|distinct|block = $1|block = substr($1, 1, index($1, ":"))'
	'no-conflict-guard|conflict|stmts[block] != $2 + 0|0'
	'no-covered-merge|partial|if ($3 + 0 > 0) {|if (0) {'
	'no-header-guard|noheader|$0 !~ /^mode: /|0'
	'no-field-guard|badfields|NF != 3|0'
	'lists-empty-blocks|empty-block|if ($2 + 0 == 0) {|if (0) {'
	'no-abort-flag|conflict|if (bad) {|if (0) {'
)

sanity_record="$(case_record sanity)"

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
		fail "$name: names a case no profile above builds"
		continue
	fi

	# An anchor that no longer appears mutates nothing, and a case that "survives"
	# that looks exactly like a case that catches nothing. One that appears twice
	# would mutate both, so the case would not say which decision it caught.
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
		fail "$name: its anchor appears $hits times in $(basename "$script"), want 1"
		continue
	fi

	mutate "$from" "$to" >"$mutant"
	chmod +x "$mutant"

	if cmp -s "$script" "$mutant"; then
		fail "$name: changed nothing despite a unique anchor"
		continue
	fi

	# The single-block case reaches no decision any mutation here alters. If it goes
	# red the mutant is broken outright — an awk parse error, say — and its effect on
	# the target case would say nothing about the decision the mutation removed.
	if ! check_case "$sanity_record" "$mutant" >/dev/null; then
		fail "$name: breaks even the single-block case, so it proves nothing"
		continue
	fi

	if check_case "$target_record" "$mutant" >/dev/null; then
		fail "$name: '$target' still passes with $name applied, so it does not test it"
	else
		pass "$name turns '$target' red"
	fi
done

# The failure listing is sorted, and sort's collation follows the locale: en_US.UTF-8
# folds case and skips punctuation where C compares bytes. With the listing capped, an
# unpinned sort changes which blocks a reader is shown, not merely their order — so the
# same failure would read differently on two machines. Checked by running the same
# profile under two locales rather than by inspecting the pin.
printf '\nlocale independence\n'

profile collate \
	'x/a_b.go:1.1,2.2 1 0' \
	'x/aB.go:1.1,2.2 1 0' \
	'x/a-c.go:1.1,2.2 1 0' \
	'x/aa.go:1.1,2.2 1 0'

# A locale that is not installed silently falls back to C, which would make the whole
# check pass without comparing anything. Find one by its effect on sort, not by name.
c_order="$(printf 'a-c\naB\na_b\naa\n' | LC_ALL=C sort | tr '\n' ' ')"
alt_locale=""
for candidate in en_US.UTF-8 en_US.utf8 C.UTF-8; do
	if [ "$(printf 'a-c\naB\na_b\naa\n' | LC_ALL="$candidate" sort 2>/dev/null | tr '\n' ' ')" != "$c_order" ]; then
		alt_locale="$candidate"
		break
	fi
done

if [ -z "$alt_locale" ]; then
	printf '  skip: no installed locale collates differently from C\n'
else
	under_c="$(LC_ALL=C "$script" "$work/collate.out" 2>/dev/null || true)"
	under_alt="$(LC_ALL="$alt_locale" "$script" "$work/collate.out" 2>/dev/null || true)"

	# Without the pin the two runs must disagree, or this proves nothing about it.
	unpinned="$work/mutant-unpinned.sh"
	mutate 'LC_ALL=C sort' 'sort' >"$unpinned"
	chmod +x "$unpinned"
	loose_c="$(LC_ALL=C "$unpinned" "$work/collate.out" 2>/dev/null || true)"
	loose_alt="$(LC_ALL="$alt_locale" "$unpinned" "$work/collate.out" 2>/dev/null || true)"

	if [ "$loose_c" = "$loose_alt" ]; then
		fail "an unpinned sort agrees under C and $alt_locale, so this cannot detect one"
	elif [ "$under_c" = "$under_alt" ]; then
		pass "the listing is identical under C and $alt_locale"
	else
		fail "the listing differs between C and $alt_locale"
	fi
fi

printf '\n'
if ((failures > 0)); then
	printf '%d check(s) failed\n' "$failures" >&2
	exit 1
fi
printf 'all checks passed\n'
