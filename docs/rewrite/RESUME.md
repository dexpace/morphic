# Resume prompt

Paste this into a fresh session to pick the work up. It is written to be acted on, not just read.
`README.md` beside it carries the reasoning; this file carries the state and the next moves.

State that would rot is derived here rather than written down — commit counts, issue lists and
branch layout all come from commands you run, not from prose in this file. That is not fastidiousness:
every count this directory has ever hardcoded went stale, including in the commit that claimed to
refresh it.

---

You are continuing a multi-session compiler rewrite in `morphic`, a spec-to-SDK compiler.
PR **#119** is the trunk. Read these first, in order:

1. `docs/rewrite/README.md` — what was decided and why, including two decisions that were reversed
   by measurement. Read the "What was learned about verifying work here" section before writing any
   test.
2. `CLAUDE.md` — binding. The **zero nits, zero flaws** standard under "Commits & pull requests" is
   the bar; "Minor" is a severity, not permission to ship.
3. `gh pr view 119`, then `gh issue list --state open` for what is filed.

## Standing directive — file, do not fix

Prioritise landing the rewrite. **A bug found along the way gets a GitHub issue, not a fix.** This
work has a strong tendency to spawn adjacent defects — the corpus gaps, the dropped keywords, the
`allOf` branch loss were all found while doing something else, and each is genuinely worth fixing
later. Fixing them inline is how the PR stops converging.

The exception is a defect **in code this work introduced** — a guard that does not guard, a walk
that is not deterministic, a test that does not check what it claims. That is finishing the job,
not detouring.

`gh issue list --state open` is the authoritative list. Of those, the ones this work filed and
deliberately did not fix are #120 and #123 through #127; everything numbered below #120 that is
still open predates this effort.

## Branch layout

`feat/annotation-gaps-and-preserved` is the single trunk and is PR #119. Everything else that
carried part of this work — `fix/irverify-preserved`, `fix/cli-output-truncation`,
`docs/rewrite-working-notes` — is merged into it and can be deleted. So can
`wip/b11-distributed-lowering`, which was superseded rather than merged.

```sh
git rev-list --count main..HEAD      # size of the trunk
git branch --merged feat/annotation-gaps-and-preserved
```

## Do these next, in this order

**1. Re-verify the trunk rather than trusting any summary, including this one.**

```sh
git status --short                              # clean
go build ./... && go vet ./... && go test ./...
gofmt -l $(git ls-files '*.go')                 # prints nothing
golangci-lint run
./scripts/check-coverage.sh
go test ./compilers/openapi -run TestConformance # no -update; must pass unchanged
```

**2. Review the branch at its final state.** Every review in this work found something, and a review
that predates later commits is stale by `CLAUDE.md`'s own standard. Split what it finds by the
directive above.

**3. Take #119 out of draft**, with a body that matches what actually landed — including a Breaking
section, which the IR shape changes on this branch require.

## How to work here

These are not style preferences. Each was learned by shipping the opposite.

**Compile, do not read.** Every real defect on this project was found by compiling a probe and
inspecting the emitted IR and the full diagnostic list. Reading the source has repeatedly agreed
with claims that turned out false — including in review passes that declared the code clean.

**Prove a test by breaking what it describes.** Several tests here were found asserting nothing, and
one "safety net" permitted the exact addition it advertised catching. If a test is meant to fail
when something breaks, break that thing in a throwaway patch and watch it fail, then revert.

**A check that never runs is not coverage.** A verifier can be complete, well-tested against its own
fixtures, and still never meet the output it exists to check. Before trusting one, plant the defect
it names in real pipeline output and confirm the suite goes red.

**Distrust counts and universals in prose.** They are where the errors in this work concentrated — a
count referring to a different branch, "every X cites Y" when most did, "confirmed by probe" citing
a deleted artifact, a normative definition falsified by its only write site. None were caught by
tests, because none were testable. Derive a count or omit it; do not maintain one.

**A corpus shares the blind spots of the code it covers.** Fixing #114 produced a zero-byte golden
diff because no corpus spec exercised the defect. A regeneration that changes nothing looks exactly
like a broken `-update` — check that a deliberate deletion from the source makes the test fail.

**Read `reference-learnings.md` carefully but not credulously.** Its §B11 was right in direction and
wrong in one of its two prescriptions; only implementing it revealed which.

## Standing constraints

Four gates green (`gofmt -l $(git ls-files '*.go')`, `golangci-lint run`, `go vet ./...`,
`go test ./...`), coverage 100% total and per package, `ir` imports only the stdlib, functions
≤70 lines, Conventional Commits with subject ≤72 chars, and **never** a `Co-Authored-By` trailer or
any mention of an AI assistant in a commit, PR, or issue.

Delete `docs/rewrite/` when the rewrite lands. Anything here worth keeping belongs in
`docs/ir-design.md`, `docs/architecture.md`, or an issue.
