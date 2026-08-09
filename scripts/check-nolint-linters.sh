#!/usr/bin/env bash
#
# check-nolint-linters.sh fails when a //nolint directive names a linter that
# golangci-lint is not running.
#
# nolintlint reports a directive that suppresses nothing, but only for a linter
# that is enabled: golangci-lint's nolint filter drops nolintlint's "unused
# directive" issue outright when the named linter is off, in shouldPassIssue
# (pkg/result/processors/nolint_filter.go), under the comment "don't expect
# disabled linters to cover their nolint statements". So a directive naming a
# disabled linter, or one that does not exist at all, suppresses nothing and
# fails nothing, while still reading as a live constraint on the code beneath it.
#
# A run can log "Found unknown linters in //nolint directives: ..." for a name it
# cannot resolve at all, but that reaches even less: the filter parses a file only
# when it has an issue in that file to filter, so a directive in a clean file is
# never read, and the warning is in any case printed by a run that exits 0. It
# also says nothing about a real linter that is merely disabled here.
#
# The enabled set is asked of golangci-lint rather than copied from
# .golangci.yml, so enabling or dropping a linter never needs an edit here.
set -euo pipefail

repo_root="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"

# Bound the failure output: bad directives arrive in blocks — dropping a linter
# from the config invalidates every directive naming it at once — and the first
# screenful is what gets read.
max_reported=25

for tool in git jq golangci-lint; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "NOLINT FAIL: $tool is not on PATH; the enabled set cannot be derived" >&2
		exit 1
	fi
done

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# Formatters are a separate list in golangci-lint v2, but `run` reports their
# findings under their own name ("File is not properly formatted (gci)"), so
# //nolint:gci is a real directive and both lists belong in the enabled set.
golangci-lint linters --json >"$work/linters.json"
golangci-lint formatters --json >"$work/formatters.json"
jq -r '.Enabled[].name' "$work/linters.json" "$work/formatters.json" | sort -u >"$work/enabled.txt"

if [ ! -s "$work/enabled.txt" ]; then
	echo "NOLINT FAIL: golangci-lint reports no enabled linters, so nothing can be checked" >&2
	exit 1
fi

# git grep, not a filesystem walk: tracked Go files are the same set the gate's
# gofmt step reads. The pattern only has to find candidate lines — the awk below
# applies golangci-lint's own grammar to them. Status 1 means no candidates at
# all, which is not an error; anything above it is.
set +e
git -C "$repo_root" grep -nE '//[/ ]*nolint' -- '*.go' >"$work/hits.txt"
grep_status=$?
set -e
if [ "$grep_status" -gt 1 ]; then
	echo "NOLINT FAIL: git grep exited $grep_status" >&2
	exit 1
fi

# The awk program mirrors extractInlineRangeFromComment in
# pkg/result/processors/nolint_filter.go: strip leading '/' and spaces, require
# what is left to start with "nolint" followed by a space, a colon or the end of
# the comment, cut a trailing "// reason", split the rest on commas, and trim and
# lower-case each name.
if ! awk -v enabled_file="$work/enabled.txt" -v max="$max_reported" '
BEGIN {
	while ((getline name < enabled_file) > 0) {
		enabled[name] = 1
		known++
	}
	close(enabled_file)
	if (known == 0) {
		print "NOLINT FAIL: the enabled set came through empty" > "/dev/stderr"
		fatal = 1
		exit 1
	}
}

function trim(s) {
	sub("^[ \t\r\n]+", "", s)
	sub("[ \t\r\n]+$", "", s)
	return s
}

function emit(file, lineno, why) {
	findings++
	if (findings <= max) printf "NOLINT FAIL: %s:%s: %s\n", file, lineno, why
}

# check reads one directive and reports every name in it that golangci-lint is
# not running.
function check(file, lineno, cand,   body, cut, n, parts, i, name) {
	directives++
	if (substr(cand, 1, 7) != "nolint:") {
		# A bare //nolint, or //nolint followed by prose, suppresses every enabled
		# linter. It names nothing to cross-check, which also makes it the one way
		# to write a suppression this check cannot see through, so it fails here
		# rather than passing silently.
		emit(file, lineno, "//nolint names no linter, so it suppresses every enabled one")
		return
	}
	body = substr(cand, 8)
	cut = index(body, "//")
	if (cut > 0) body = substr(body, 1, cut - 1)

	n = split(body, parts, ",")
	if (n == 0) {
		# split of the empty string yields no fields at all, so a bare "//nolint:"
		# would otherwise leave the loop below with nothing to look at.
		emit(file, lineno, "//nolint names no linter, so it suppresses every enabled one")
		return
	}

	for (i = 1; i <= n; i++) {
		name = tolower(trim(parts[i]))
		checked++
		# An empty name (a stray comma) resolves to no linter, and "all" is
		# golangci-lint spelling out the blanket form above.
		if (name == "" || name == "all") {
			emit(file, lineno, "//nolint names no linter, so it suppresses every enabled one")
		} else if (!(name in enabled)) {
			emit(file, lineno, sprintf("//nolint names \"%s\", which golangci-lint is not running", name))
		}
	}
}

# scan tries every "//" on the line rather than only the one opening the comment.
# Telling those apart needs a Go parser: "//" also occurs inside string literals
# and inside prose quoting a directive. Trying all of them over-reports (a
# directive spelled inside a string literal is reported although golangci-lint
# would never see it) and never under-reports, which is the direction a check
# written because another check missed something has to fail in.
function scan(file, lineno, text,   pos, cand) {
	# text loses at least pos + 1 characters per turn, so the loop is bounded by
	# the length of the line.
	while ((pos = index(text, "//")) > 0) {
		cand = substr(text, pos)
		text = substr(text, pos + 2)
		sub("^[/ ]+", "", cand)
		if (cand == "nolint" || cand ~ "^nolint[ :]") check(file, lineno, cand)
	}
}

$0 == "" { next }

# git grep -n emits "path:line:text". No tracked path here holds a colon, so the
# first two colons split it; a line without them is a broken invariant, not a
# finding.
{
	p = index($0, ":")
	rest = substr($0, p + 1)
	q = index(rest, ":")
	if (p == 0 || q == 0) {
		printf "NOLINT FAIL: unparsable git grep line: %s\n", $0 > "/dev/stderr"
		fatal = 1
		exit 1
	}
	scan(substr($0, 1, p - 1), substr(rest, 1, q - 1), substr(rest, q + 1))
}

END {
	if (fatal) exit 1
	if (findings > max) printf "  ... and %d more\n", findings - max
	if (findings > 0) {
		printf "nolint gate failed: %d problem(s) across %d directive(s).\n", findings, directives
		exit 1
	}
	printf "nolint gate passed: %d directive(s), %d linter name(s), all enabled.\n", directives, checked
}
' "$work/hits.txt"; then
	exit 1
fi
