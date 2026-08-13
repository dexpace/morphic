#!/usr/bin/env bash
#
# verify-nolint-grammar.sh checks how scripts/check-nolint-linters.sh reads a
# //nolint directive.
#
# The gate script re-implements extractInlineRangeFromComment
# (pkg/result/processors/nolint_filter.go) in awk, and every verdict it reaches
# rests on that grammar agreeing with golangci-lint's. The tree carries a handful
# of directives, all of one shape, so the committed corpus exercises almost none
# of it: the forms that matter — a name that is merely disabled, an empty name, a
# blanket suppression spelled four different ways — never appear, and would be
# nobody's job to notice if the grammar drifted.
#
# It also drives the shapes around the grammar that decide whether the answer
# describes this repo at all: which config the enabled set is read from, a null
# Enabled list, and a path git grep -n cannot be split on.
#
# Each case is followed by a mutation that must break it. A case that stays green
# against a deliberately broken reader is not testing the reader — and a gate
# step nothing tests is one that can start passing everything without a sign.
#
# Usage:
#   scripts/verify-nolint-grammar.sh
set -euo pipefail

repo_root="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
script="$repo_root/scripts/check-nolint-linters.sh"

for tool in git jq golangci-lint; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		printf 'NOLINT VERIFY FAIL: %s is not on PATH\n' "$tool" >&2
		exit 1
	fi
done

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

# The fixture enables one linter and one formatter, so a directive can name an
# enabled linter (errorlint), an enabled formatter (gofmt), a real linter that is
# off (unparam), or nothing that exists — the four verdicts the script reaches.
standard_config='version: "2"
linters:
  default: none
  enable:
    - errorlint
formatters:
  enable:
    - gofmt
'

# make_repo creates $work/<name> as a tracked repo carrying the given config.
make_repo() {
	local name="$1" config="$2" dir="$work/$1"
	mkdir -p "$dir/pk" "$dir/scripts"
	printf '%s' "$config" >"$dir/.golangci.yml"
	printf 'package pk\n' >"$dir/pk/a.go"
	# The fixture must not inherit the caller's git identity, signing or default
	# branch: a global commit.gpgsign would fail the commit on a runner with no
	# key, and the whole suite would read as a broken script rather than a
	# missing fixture.
	git -c init.defaultBranch=main init -q "$dir"
	git -C "$dir" config user.email nolint@example.invalid
	git -C "$dir" config user.name nolint
	git -C "$dir" config commit.gpgsign false
	git -C "$dir" add -A
	git -C "$dir" commit -qm fixture
}

# plant rewrites the fixture's one Go file so it holds exactly the given lines.
# git grep reads the working tree, so no commit is needed between cases.
plant() {
	local dir="$work/$1"
	shift
	{
		printf 'package pk\n\n'
		printf '%s\n' "$@"
	} >"$dir/pk/a.go"
}

# plant_crlf is plant with CRLF line endings. go/scanner drops the trailing CR
# before the nolint filter reads the comment, so a directive there is live to
# golangci-lint. The gate's gofmt step would reject such a file, but this check
# must not depend on a step above it having run.
plant_crlf() {
	local dir="$work/$1"
	shift
	{
		printf 'package pk\r\n\r\n'
		printf '%s\r\n' "$@"
	} >"$dir/pk/a.go"
}

make_repo main "$standard_config"
make_repo noformatters 'version: "2"
linters:
  default: none
  enable:
    - errorlint
'
make_repo nolinters 'version: "2"
linters:
  default: none
formatters:
  enable:
    - gofmt
'

# A directory that is not the repo, holding a config that disagrees with it about
# every name the cases use. Anything reading the enabled set from the caller's
# directory answers for this file instead of the fixture's.
mkdir -p "$work/foreign"
printf 'version: "2"\nlinters:\n  default: none\n  enable:\n    - unparam\nformatters:\n  enable:\n    - gci\n' \
	>"$work/foreign/.golangci.yml"

# 30 bad directives, five past the 25 the failure listing prints.
capped_lines=()
for i in $(seq 30); do capped_lines+=("var _ = $i //nolint:bogus$i // r"); done

# One record per case: <name> | <repo> | <planted line> | <want exit> | <substring
# stdout must hold>. "@capped" plants the 30 lines above instead of one. The
# mutation sweep looks cases up by name, so each name appears once.
#
# Every "passed" case pins the directive count too: a grammar change that stops
# seeing a directive at all would otherwise read as a pass.
cases=(
	'clean|main|var _ = 1|0|nolint gate passed: 0 directive(s), 0 linter name(s), all enabled.'
	'enabled|main|var _ = 1 //nolint:errorlint // r|0|nolint gate passed: 1 directive(s), 1 linter name(s), all enabled.'
	'formatter|main|var _ = 1 //nolint:gofmt // r|0|nolint gate passed: 1 directive(s), 1 linter name(s), all enabled.'
	'disabled|main|var _ = 1 //nolint:unparam // r|1|//nolint names "unparam", which golangci-lint is not running'
	'unknown|main|var _ = 1 //nolint:notarealinter // r|1|//nolint names "notarealinter", which golangci-lint is not running'
	'bare|main|var _ = 1 //nolint // r|1|//nolint suppresses every enabled linter'
	'all|main|var _ = 1 //nolint:all // r|1|//nolint suppresses every enabled linter'
	'allprefix|main|var _ = 1 //nolint:allfoo // r|1|//nolint suppresses every enabled linter'
	'all-in-list|main|var _ = 1 //nolint:errorlint,all // r|1|//nolint suppresses every enabled linter'
	# golangci-lint tests HasPrefix("nolint:all") on the raw text and compares the
	# split-out names lower-cased, so the two disagree on case: "//nolint:allfoo"
	# blankets, "//nolint:AllFoo" is just an unknown name, and "//nolint:ALL"
	# blankets by the second test after failing the first.
	'all-uppercase|main|var _ = 1 //nolint:ALL // r|1|//nolint suppresses every enabled linter'
	'allprefix-mixedcase|main|var _ = 1 //nolint:AllFoo // r|1|//nolint names "allfoo", which golangci-lint is not running'
	'emptyname|main|var _ = 1 //nolint:|1|//nolint has an empty linter name, which suppresses nothing'
	'trailing-comma|main|var _ = 1 //nolint:errorlint,|1|//nolint has an empty linter name, which suppresses nothing'
	'mixedcase|main|var _ = 1 //nolint:ErrorLint // r|0|nolint gate passed: 1 directive(s), 1 linter name(s), all enabled.'
	'spacecolon|main|var _ = 1 //nolint: errorlint // r|0|nolint gate passed: 1 directive(s), 1 linter name(s), all enabled.'
	'leadingspace|main|var _ = 1 // nolint:errorlint // r|0|nolint gate passed: 1 directive(s), 1 linter name(s), all enabled.'
	'reasoncut|main|var _ = 1 //nolint:errorlint //notalinter|0|nolint gate passed: 1 directive(s), 1 linter name(s), all enabled.'
	'extraslashes|main|var _ = 1 ////nolint:notarealinter // r|1|nolint gate failed: 1 problem(s) across 1 directive(s).'
	'notdirective|main|var _ = 1 //nolintfoo:bar|0|nolint gate passed: 0 directive(s), 0 linter name(s), all enabled.'
	'blockcomment|main|var _ = 1 /*nolint:notarealinter*/|0|nolint gate passed: 0 directive(s), 0 linter name(s), all enabled.'
	# The documented over-report: golangci-lint never sees this one, since the
	# directive is inside a string literal. Telling it apart needs a Go parser,
	# and over-reporting is the direction this check has to fail in. Asserted on
	# the count, because the name carries the closing quote with it.
	'instring|main|var _ = "//nolint:notarealinter"|1|nolint gate failed: 1 problem(s) across 1 directive(s).'
	# A bare //nolint at the end of a CRLF line. go/scanner drops the CR, so
	# golangci-lint honours it — measured: the finding under it disappears — and a
	# grammar reading "nolint\r" would match neither anchor and miss it entirely.
	'crlf-bare|main|@crlf|1|//nolint suppresses every enabled linter'
	'capped|main|@capped|1|  ... and 5 more'
	'no-formatters-config|noformatters|var _ = 1 //nolint:errorlint // r|0|nolint gate passed: 1 directive(s), 1 linter name(s), all enabled.'
	'no-linters-config|nolinters|var _ = 1 //nolint:errorlint // r|1|NOLINT FAIL: golangci-lint reports no enabled linters, so nothing can be checked'
)

# check_case drives one record through <candidate script> and echoes why it did
# not hold, echoing nothing and returning 0 when it did.
check_case() {
	local record="$1" candidate="$2" from_dir="${3:-}"
	local rest="${record#*|}"
	local repo="${rest%%|*}"
	rest="${rest#*|}"
	local planted="${rest%%|*}"
	rest="${rest#*|}"
	local want_status="${rest%%|*}" want="${rest#*|}"

	local dir="$work/$repo"
	if [ ! -d "$dir/.git" ]; then
		printf 'no fixture repo named %s was built' "$repo"
		return 1
	fi

	case "$planted" in
	@capped) plant "$repo" "${capped_lines[@]}" ;;
	@crlf) plant_crlf "$repo" 'var _ = 1 //nolint' ;;
	*) plant "$repo" "$planted" ;;
	esac

	cp "$candidate" "$dir/scripts/check-nolint-linters.sh"
	chmod +x "$dir/scripts/check-nolint-linters.sh"

	# stdout only: what the script reports there is the contract being checked,
	# and a mutant broken outright would otherwise bury the run in awk errors.
	local out status=0
	out="$(cd "${from_dir:-$dir}" && "$dir/scripts/check-nolint-linters.sh" 2>/dev/null)" || status=$?
	if [ "$status" -ne "$want_status" ]; then
		printf 'exit %d, want %s' "$status" "$want_status"
		return 1
	fi

	case "$out" in
	*"$want"*) return 0 ;;
	esac
	printf 'stdout does not hold "%s"' "$want"
	return 1
}

# case_record echoes the case record named <name>.
case_record() {
	local record
	for record in "${cases[@]}"; do
		if [ "${record%%|*}" = "$1" ]; then
			printf '%s' "$record"
			return 0
		fi
	done
	printf 'no case named %s\n' "$1" >&2
	return 1
}

printf 'grammar and configuration\n'
for record in "${cases[@]}"; do
	if reason="$(check_case "$record" "$script")"; then
		pass "${record%%|*}"
	else
		fail "${record%%|*}: $reason"
	fi
done

# mutate echoes the gate script with the sole occurrence of <from> replaced by
# <to>. Exact text, not a regex: the mutants have to read the same under BSD and
# GNU userlands, and a pattern that quietly means something else under one of
# them is indistinguishable from a case that catches nothing.
mutate() {
	awk -v from="$1" -v to="$2" '
		index($0, from) {
			$0 = substr($0, 1, index($0, from) - 1) to substr($0, index($0, from) + length(from))
		}
		{ print }
	' "$script"
}

# Each mutation removes one decision the reader makes, and names the case that
# has to notice. An anchor holds no "|", which splits the record, and no
# backslash, which awk -v would turn into the control character it spells.
#
# <name> | <case the mutation must break> | <text to replace> | <replacement>
mutations=(
	'no-all-prefix|allprefix|substr(cand, 8, 3) == "all"|0'
	'no-all-in-list|all-in-list|name == "all"|0'
	'no-empty-split|emptyname|n = 1|n = 0'
	'no-empty-name|trailing-comma|name == ""|0'
	'no-lowercase|mixedcase|tolower(|('
	'no-trim|spacecolon|trim(parts[i])|parts[i]'
	'no-slash-strip|leadingspace|sub("^[/ ]+", "", cand)|sub("^[/]+", "", cand)'
	'no-reason-cut|reasoncut|if (cut > 0)|if (0)'
	'no-scan-advance|extraslashes|pos + length(lead) - length(cand)|pos + 2'
	'no-directive-anchor|notdirective|cand ~ "^nolint[ :]"|cand ~ "nolint"'
	'no-cr-strip|crlf-bare|$0 = stripcr($0)|$0 = $0'
	'no-enabled-lookup|disabled|!(name in enabled)|0'
	'no-formatters-list|formatter|>>"$work/enabled.txt"|>/dev/null'
	'no-cap|capped|max_reported=25|max_reported=100'
	'no-null-guard|no-formatters-config|(.Enabled // [])[].name'"'"' "$work/formatters.json"|.Enabled[].name'"'"' "$work/formatters.json"'
	'one-jq-call|no-linters-config|"$work/linters.json" >"$work/enabled.txt"|"$work/linters.json" "$work/formatters.json" >"$work/enabled.txt"'
)

sanity_record="$(case_record clean)"

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
		fail "$name: names a case the table does not carry"
		continue
	fi

	# An anchor that no longer appears mutates nothing, and a case that
	# "survives" that looks exactly like a case that catches nothing. One that
	# appears twice would mutate both, so the case would not say which decision
	# it caught.
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

	# The directive-free case reaches no decision any mutation here alters. If it
	# goes red the mutant is broken outright — an awk parse error, say — and its
	# effect on the target case would say nothing about the decision removed.
	if ! check_case "$sanity_record" "$mutant" >/dev/null; then
		fail "$name: breaks even the directive-free case, so it proves nothing"
		continue
	fi

	if check_case "$target_record" "$mutant" >/dev/null; then
		fail "$name: '$target' still passes with $name applied, so it does not test it"
	else
		pass "$name turns '$target' red"
	fi
done

# The directives are read from the repo the script lives in, but golangci-lint
# discovers .golangci.yml relative to its own working directory. Unless the two
# are tied together, running the script from anywhere else answers for a config
# that has nothing to do with the tree being scanned. Checked by running the same
# case from two directories rather than by inspecting the anchor.
printf '\nthe enabled set follows the repo, not the caller\n'

anchor_case="$(case_record enabled)"
if ! reason="$(check_case "$anchor_case" "$script" "$work/foreign")"; then
	fail "the enabled case does not hold when run from another directory: $reason"
else
	# Without the anchor the two runs must disagree, or this proves nothing.
	loose="$work/mutant-unanchored.sh"
	mutate 'cd "$repo_root" && golangci-lint linters --json' 'golangci-lint linters --json' >"$loose"
	chmod +x "$loose"
	if check_case "$anchor_case" "$loose" "$work/foreign" >/dev/null; then
		fail "an unanchored read still holds from another directory, so this cannot detect one"
	else
		pass "the verdict is the same from the repo and from an unrelated directory"
	fi
fi

# git grep -n output is split on its first two colons, which a path holding one
# would silently derail — the finding would keep its file but lose its line.
printf '\na path git grep -n output cannot be split\n'

colon_dir="$work/colon"
make_repo colon "$standard_config"
if ! printf 'package pk\n\nvar _ = 1 //nolint:notarealinter // r\n' >"$colon_dir/pk/a:b.go" 2>/dev/null; then
	printf '  skip: this filesystem will not hold a path with a colon\n'
else
	git -C "$colon_dir" add -A
	git -C "$colon_dir" commit -qm colon
	cp "$script" "$colon_dir/scripts/check-nolint-linters.sh"
	chmod +x "$colon_dir/scripts/check-nolint-linters.sh"

	out="$(cd "$colon_dir" && ./scripts/check-nolint-linters.sh 2>/dev/null)" || true
	case "$out" in
	*"a tracked path holds a colon"*)
		# Without the guard it must not refuse, or the refusal proves nothing.
		guardless="$work/mutant-guardless.sh"
		mutate 'if [ -n "$colon_paths" ]; then' 'if false; then' >"$guardless"
		chmod +x "$guardless"
		cp "$guardless" "$colon_dir/scripts/check-nolint-linters.sh"
		loose_out="$(cd "$colon_dir" && ./scripts/check-nolint-linters.sh 2>/dev/null)" || true
		case "$loose_out" in
		*"a tracked path holds a colon"*)
			fail "the tree is refused even without the guard, so the guard is not what refuses it"
			;;
		*"pk/a:b.go: //nolint names"*)
			# The misparse the guard exists to prevent: the finding keeps its
			# file and loses its line, "pk/a:b.go" standing where "path:line"
			# belongs. Seeing it here is what makes the refusal above load-bearing.
			pass "a colon in a tracked path is refused, and without the guard the line number is silently lost"
			;;
		*)
			fail "without the guard the run neither refused nor misparsed, so this pins nothing"
			;;
		esac
		;;
	*)
		fail "a colon in a tracked path was not refused"
		;;
	esac
fi

# Two guards no planted directive can reach. The first runs before the script has
# read anything; the second only fires when awk cannot read the file the shell
# just found non-empty, so no config produces it.
printf '\nguards no planted directive reaches\n'

# A PATH holding everything the script reaches before the tool loop, and jq only
# missing: bash for the shebang's `env bash`, dirname and git to find the repo.
mkdir -p "$work/bin"
for tool in bash dirname git golangci-lint; do
	ln -sf "$(command -v "$tool")" "$work/bin/$tool"
done
out="$(PATH="$work/bin" "$work/main/scripts/check-nolint-linters.sh" 2>/dev/null)" || true
case "$out" in
*"NOLINT FAIL: jq is not on PATH"*) pass "a missing tool is named rather than crashed on" ;;
*) fail "a PATH without jq did not name it: $out" ;;
esac

# The shell guard proves the file is non-empty, so the awk-side check can only
# fire on a file awk cannot read. Pointed at one that does not exist.
unreadable="$work/mutant-unreadable.sh"
mutate '-v enabled_file="$work/enabled.txt"' '-v enabled_file="$work/absent.txt"' >"$unreadable"
chmod +x "$unreadable"
cp "$unreadable" "$work/main/scripts/check-nolint-linters.sh"
plant main 'var _ = 1'
out="$(cd "$work/main" && ./scripts/check-nolint-linters.sh 2>/dev/null)" || true
case "$out" in
*"NOLINT FAIL: the enabled set came through empty"*)
	pass "an unreadable enabled set is refused, not read as nothing enabled"
	;;
*)
	fail "an unreadable enabled set did not fire the awk guard: $out"
	;;
esac

printf '\n'
if ((failures > 0)); then
	printf '%d check(s) failed\n' "$failures" >&2
	exit 1
fi
printf 'all checks passed\n'
