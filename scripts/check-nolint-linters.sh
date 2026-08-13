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
#
# scripts/verify-nolint-grammar.sh drives this script over every grammar form,
# and mutates it to prove each case bites.
set -euo pipefail

repo_root="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"

# Bound the failure output: bad directives arrive in blocks — dropping a linter
# from the config invalidates every directive naming it at once — and the first
# screenful is what gets read.
max_reported=25

for tool in git jq golangci-lint; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "NOLINT FAIL: $tool is not on PATH; the enabled set cannot be derived"
		exit 1
	fi
done

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# Both lists are asked from the repo being scanned, not from wherever the caller
# stood: golangci-lint discovers .golangci.yml relative to its own working
# directory, so an unanchored call answers for a config that has nothing to do
# with the directives git grep is about to read below.
#
# Formatters are a separate list in golangci-lint v2, but `run` reports their
# findings under their own name ("File is not properly formatted (gci)"), so
# //nolint:gci is a real directive and both lists belong in the enabled set.
(cd "$repo_root" && golangci-lint linters --json) >"$work/linters.json"
(cd "$repo_root" && golangci-lint formatters --json) >"$work/formatters.json"

# Read each list on its own, and tolerate a null .Enabled. jq reports only the
# status of its LAST input, so one call over both files exits 0 when the first
# one is null — silently shrinking the enabled set to whatever the second held,
# which is how every real directive comes to be reported as not running. The
# guard between the two reads is what that would otherwise slip past.
jq -r '(.Enabled // [])[].name' "$work/linters.json" >"$work/enabled.txt"
if [ ! -s "$work/enabled.txt" ]; then
	echo "NOLINT FAIL: golangci-lint reports no enabled linters, so nothing can be checked"
	exit 1
fi
jq -r '(.Enabled // [])[].name' "$work/formatters.json" >>"$work/enabled.txt"
# -u drops what the locale's collation calls equal, which is a byte comparison
# only under C. No name golangci-lint ships is affected — glibc separates
# "gocritic", "go-critic" and "GoCritic" under en_US.UTF-8 — so this is insurance
# against a name that would collide rather than a fix for one that does.
LC_ALL=C sort -u -o "$work/enabled.txt" "$work/enabled.txt"

# git grep -n emits "path:line:text", which the awk below splits on the first two
# colons. That is only correct while no path carries one, so refuse a tree where
# one does rather than misparse it: the split would silently fold the line number
# into the scanned text and report the finding at a location without one.
colon_paths="$(git -C "$repo_root" ls-files -- '*.go' | grep ':' || true)"
if [ -n "$colon_paths" ]; then
	echo "NOLINT FAIL: a tracked path holds a colon, which git grep -n output cannot be split:"
	echo "$colon_paths" | sed 's/^/  /'
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
	echo "NOLINT FAIL: git grep exited $grep_status"
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
		print "NOLINT FAIL: the enabled set came through empty"
		fatal = 1
		exit 1
	}
}

function trim(s) {
	sub("^[ \t\r\n]+", "", s)
	sub("[ \t\r\n]+$", "", s)
	return s
}

# stripcr drops the trailing CR that go/scanner drops before the nolint filter
# reads the comment. Without it a CRLF file leaves this grammar looking at
# "nolint\r", which matches neither anchor below, while golangci-lint honours the
# bare //nolint it came from.
function stripcr(s) {
	sub("\r$", "", s)
	return s
}

function emit(file, lineno, why) {
	findings++
	if (findings <= max) printf "NOLINT FAIL: %s:%s: %s\n", file, lineno, why
}

# blanket reports a directive that suppresses every enabled linter at once. It
# names nothing to cross-check, which also makes it the one way to write a
# suppression this check cannot see through, so it fails here rather than
# passing silently. (nolintlint has require-specific, which covers the same
# ground when it is enabled; this check does not depend on that.)
function blanket(file, lineno) {
	emit(file, lineno, "//nolint suppresses every enabled linter, so nothing here names what it hides")
}

# check reads one directive and reports every name in it that golangci-lint is
# not running.
function check(file, lineno, cand,   body, cut, n, parts, i, name) {
	directives++

	# extractInlineRangeFromComment blankets on a directive that does not start
	# "nolint:", and — testing HasPrefix before it splits — on one that starts
	# "nolint:all", which takes "nolint:allfoo" with it.
	if (substr(cand, 1, 7) != "nolint:" || substr(cand, 8, 3) == "all") {
		blanket(file, lineno)
		return
	}

	body = substr(cand, 8)
	cut = index(body, "//")
	if (cut > 0) body = substr(body, 1, cut - 1)

	n = split(body, parts, ",")
	if (n == 0) {
		# awk splits the empty string into no fields where Go splits it into one
		# empty one, so "//nolint:" would otherwise skip the loop entirely.
		n = 1
		parts[1] = ""
	}

	for (i = 1; i <= n; i++) {
		name = tolower(trim(parts[i]))
		# "all" anywhere in the list is golangci-lint spelling out the blanket
		# form above, and it stops reading the rest of the list there too.
		if (name == "all") {
			blanket(file, lineno)
			return
		}
		checked++
		if (name == "") {
			# A stray comma, or nothing at all after the colon. golangci-lint reads
			# it as a linter named "", which none is, so this name suppresses
			# nothing; where it is the only name, the whole directive is inert and
			# the finding beneath it is still reported.
			emit(file, lineno, "//nolint has an empty linter name, which suppresses nothing")
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
function scan(file, lineno, text,   pos, cand, lead) {
	# text loses at least pos + 1 characters per turn, so the loop is bounded by
	# the length of the line.
	while ((pos = index(text, "//")) > 0) {
		cand = substr(text, pos)
		lead = cand
		sub("^[/ ]+", "", cand)
		# Resume past the whole run of slashes and spaces that opened this
		# comment. Resuming after the first two re-enters the same run, which
		# reads one "////nolint:x" as two directives and reports it twice.
		text = substr(text, pos + length(lead) - length(cand))
		if (cand == "nolint" || cand ~ "^nolint[ :]") check(file, lineno, cand)
	}
}

# git grep -n emits "path:line:text", and the shell above has already refused a
# tree in which a path could hold a colon, so the first two split it; a line
# without them is a broken invariant, not a finding.
{
	$0 = stripcr($0)

	p = index($0, ":")
	rest = substr($0, p + 1)
	q = index(rest, ":")
	if (p == 0 || q == 0) {
		printf "NOLINT FAIL: unparsable git grep line: %s\n", $0
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
