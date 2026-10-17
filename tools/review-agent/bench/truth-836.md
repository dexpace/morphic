# Ground truth for #836: build: refuse lint suppression comments rather than check them

Initial commit reviewed: df615a30988f3777031088aeda96334d0d5f3e44

Each item below is a commit pushed after the initial one. Most fix a defect a local review found in the initial change; some are planned continuation work and are NOT defects -- judge each.

## 57d238ba
build: refuse //nolint directives rather than check them

A //nolint comment naming a linter the config leaves off is read by nothing:
golangci-lint drops the unused-directive finding for a disabled linter, so
the comment stays in the source as a live constraint while suppressing
nothing, and the tree is green either way. Two scripts existed to catch that,
one re-implementing the directive grammar in awk and one testing that
re-implementation by mutating it.

With no directive left in the tree there is nothing to go stale. nolintlint
and both scripts go, with their Makefile targets and gate step, and make
nolint now fails on any //nolint in a Go file. A suppression is an entry in
the exclusions block of .golangci.yml with its reason beside it, which keeps
every one in a single place.

## 34311cd0
test(archtest): refuse lint suppression comments by parsing them

The gate refused //nolint by grepping for the text "//nolint". golangci-lint
reads more than that: it trims leading slashes and spaces, so "// nolint:x",
"//  nolint:x", a bare "// nolint" and "/// nolint:x" are directives too, and
each one suppresses its finding. nolintlint flagged those spellings on main,
so dropping it for the grep would have let them through. revive honours
//revive:disable and its -line and -next-line forms the same way, which
nolintlint never saw.

The new test reads the comments of every Go file the go tool builds and
refuses a nolint, revive or lint:ignore directive. Matching comments rather
than text means a sentence or a string literal that names a directive is not
one, a file git does not track yet is read, and a test can spell a directive in
a fixture. Each pattern is at least as wide as its tool's, so the case, tab and
block spellings golangci-lint v2.14.0 ignores are refused as well.

walkGoFiles is the walk it uses. The tests that still carry their own copy are
tracked in #846.

## aaeb9332
build: drop make nolint and keep suppressions in one block

internal/archtest now refuses the suppression comments, so the git grep target
and the gate step that ran it have nothing left to do. The grep stopped at the
exact text "//nolint", the test reads comments.

The comment above the exclusions block claimed golangci-lint does not report a
rule that matches nothing, and that every rule names a single path and a
message. Neither held: warn-unused makes golangci-lint log a warning for a rule
that skipped nothing, and the _test.go rule has no message. warn-unused is now
on, so a rule left behind by deleted code shows up in the lint log, and the
comment says what is true. The warning does not fail the run.

The project instructions state the rule where the other archtest-enforced caps
are.

## 6bbc620e
refactor(cmd/morphic): make findCommand return nothing on failure

On failure findCommand handed back what cobra's Find returns alongside its
error, the root command and every argument it was given, so a caller that
skipped the found check would have read a plausible command. It now returns
nil, nil and false, and a test pins that with the found case beside it.

## 8ed8074b
test(compilers/openapi): wrap and reword the resolution failure report

The report ran to 150 columns and ended in "reported N failure(s) and: err".
It now wraps and says what the library's resolution returned.

## 3e1bbbf2
test(archtest): state what lint:ignore does and plant the prose case

The comment on the lint:ignore pattern called the directive inert under
golangci-lint. It is inert for SA1012, but staticcheck's unused analyzer reads
//lint:ignore and //lint:file-ignore for U1000 itself, so under this config it
suppresses an unused-symbol finding. The comment now says both, and a row
plants the U1000 spelling.

A doc-comment line that merely begins with the word, "// nolint is a word...",
is read by golangci-lint as a bare directive and silences every finding on the
declaration below it. The patterns already refuse it by matching the start of a
comment; a row now pins that.

## 15f8e0d8
build: say that a narrowed lint run warns about rules it did not reach

warn-unused logs a warning for each exclusion rule that skipped nothing. A run
limited to some packages skips nothing for the rules whose files it never
read, so it warns about all of them; a full run, which is what make lint and
CI do, warns about none. The comment above the exclusions now says so, to keep
the warning from being read as a stale rule.

## f32b094a
test(archtest): tell a reader how to resolve a prose false match

golangci-lint reads a comment that opens with the word as a directive, so a
sentence like "nolint is a word..." is refused, and rightly: it silences the
declaration below it. The failure said only to add an exclusion, which is the
wrong advice for prose. It now says to reword the comment in that case, and the
test's doc stops saying a sentence that names a directive is never one.
