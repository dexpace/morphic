# Ground truth for #381: fix: count each coverage block once regardless of cache warmth

Initial commit reviewed: 5d7ac495f024649bcb06b3831f78015611e021ec

Each item below is a commit pushed after the initial one. Most fix a defect a local review found in the initial change; some are planned continuation work and are NOT defects -- judge each.

## b1c7a5c6
fix(scripts): test the coverage block merge and fix its failure output

The merge added by the parent commit had no test and could not be given one:
`go test ./...` ran unconditionally, so driving the counting over a profile
meant editing the script. Every shape the merge exists for — a block repeated
three times, a block one fragment covered and another did not, a span two
fragments size differently — is a shape no committed spec produces, so the
merge was the only untested part of the gate that guards every other test.

check-coverage.sh now takes an optional profile argument that skips the suite,
and scripts/verify-coverage-count.sh drives twelve cases through it. Each case
is paired with a mutation of the merge that must turn it red, so a case that
stops reaching the decision it names fails rather than staying quietly green.
The mutations replace exact text rather than matching a regex, since a pattern
that means something else under GNU sed would be indistinguishable from a case
that catches nothing. Wired into the gate ahead of the coverage step.

Also fixed, all found by running the script rather than reading it:

- A conflicting statement count was the one failure that printed nothing to
  stdout: the message went to stderr and `set -e` aborted the assignment before
  any summary. It now reports on stdout with a summary line, like every other
  failure here.
- The note claimed every block lands three times after `go clean -testcache`.
  With no cached fragment to find every block lands once, which is what a
  runner with a cold build cache — CI's usual state — actually sees. Replaced
  the count with the mechanism.
- Counts merged by max, and four lines defended max over sum, but nothing reads
  the result as anything but zero or non-zero: `count[$1] = 1` gives identical
  output on real warm and cold profiles. The merge now records whether some
  fragment ran the block, which is the only question the gate asks, and says so.
- "added 2 to the total and 1 to the hits" holds only for a one-statement
  block; an N-statement block added 2N and N.
- The note read as though repeated fragments come from one run. They need not:
  cmd/go merges a cached fragment before rejecting the cached result as
  expired, so a block only a replayed fragment covered now counts as covered.
  Stated where a reader will reach it.
- The merged profile was re-emitted through three further passes to filter,
  total, and list it; one sorted pass does all three.

## a921dba3
fix(scripts): refuse a malformed profile, stop listing empty blocks

Three defects, all found by feeding the counter profiles rather than reading it.

**A block with no statements was listed as uncovered beside a passing verdict.**
`go` emits `numstmt=0` blocks for an empty body — an unreached `case x:`, say —
and 16 of them are in this tree's own profile today. Collapsing the reporting
into one streaming pass made the listing print such a block while `missed`
stayed 0, so the run ended:

    COVERAGE FAIL: zeroblk/z.go:8.9,8.9 (0 statement(s) uncovered)
    Coverage gate passed: all 3 statements covered.
    exit 0

A COVERAGE FAIL beside a pass and a zero exit is the worst shape this script has.
It is a regression from the previous commit, which reached the listing only once
the totals already disagreed. A zero-statement block cannot be uncovered and
contributes nothing either way, so it is now skipped outright, which puts the
listing and the verdict back in lockstep.

**A profile with no `mode:` header passed while hiding an uncovered block.** The
body was read from line 2 unconditionally, so a body-only file lost its first
block: a two-block profile whose first block was uncovered reported `all 5
statements covered` instead of failing 5 of 10. That predates this branch, but
nothing could reach it while the script always wrote the profile it read; taking
a profile argument is what made it reachable. The first line must now be a
`mode:` header.

**A line with the wrong field count was read positionally anyway**, quietly
becoming a zero-statement block. Block lines must now have exactly three fields.

Both refusals report on stdout with a summary, like every other failure here.

The verifier grows six assertions and four mutations to match, and gains two
things it needed regardless: assertions that a substring is *absent*, without
which the abort flag that keeps merged blocks out of a failure report had no
test at all; and an anchor counter that counts occurrences rather than lines,
so an anchor appearing twice on one line can no longer read as unique.

## 24af67c6
fix(scripts): pin the failure listing's sort to LC_ALL=C

`sort`'s collation follows the locale. en_US.UTF-8 folds case and skips punctuation
where C compares bytes, so four uncovered blocks in one file come out in two
different orders on two machines:

    LC_ALL=C            LC_ALL=en_US.UTF-8
    x/a-c.go            x/a_b.go
    x/aB.go             x/a-c.go
    x/a_b.go            x/aa.go
    x/aa.go             x/aB.go

The comment above the sort claimed the opposite — that sorting makes the same
failure read the same way on every run. It does within a machine; across two it
did not, and because the listing is capped at 25 blocks that decided which blocks
a reader is shown at all, not merely their order. Two people looking at the same
failing profile could see disjoint subsets.

The verifier checks this by running one profile under two locales and comparing,
rather than by inspecting the pin. A locale that is not installed falls back to C
silently, which would make the comparison pass without comparing anything, so the
second locale is chosen by its observed effect on sort rather than by name — and
if none of the candidates collates differently, the check says it is skipping
instead of reporting a pass. It also mutates the pin away first and requires the
two runs to disagree, since a probe that cannot see an unpinned sort proves
nothing about a pinned one. Both of those refusals were confirmed by forcing them.

Locally this changes nothing: C.UTF-8 and C order these identically.

## 04a8b7b6
test(scripts): fail a case whose profile was never built

A case names the profile it runs against. If nothing builds that profile, the
counter is handed a path that does not exist — and its answer to that is
"COVERAGE FAIL: cannot read profile ..." with exit 1, which satisfies any case
expecting a failure and a COVERAGE FAIL substring. So a typo in the case table
passed, quietly retiring the check it was meant to be:

    ok: daed: COVERAGE FAIL          # 'dead' misspelled; nothing ran

The mutation sweep does not cover this. It proves a case notices a broken
counter, and only for the cases a mutation targets — a case that never ran at
all still reports ok. Cases carry the profile check themselves now, so it holds
wherever they are used.

## a867c475
docs: list the coverage-counting check with the rest of the gate

CLAUDE.md's command block says it is "the same checks CI's gate job runs
(.github/workflows/gate.yml), in that order". Adding a step to that job without
adding it here made the claim false, and a contributor following the block would
pass locally and then fail on a check they were never told to run. README's
pre-landing list has the same job and the same omission.

Realigned CLAUDE.md's trailing comments to the longest entry so the column holds.
