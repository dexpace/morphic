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
`fix/verifier-review`, `docs/rewrite-working-notes`, `docs/rewrite-notes-truth` — is merged into it
and can be deleted. So can `wip/b11-distributed-lowering`, which was superseded rather than merged,
and `backup/pre-reword`, a safety ref from a history rewrite that was abandoned.

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

All of the above were green at the head this file was last updated against, and the coverage gate
now counts statements exactly rather than reading `%.1f`-rounded output, so 100% means 100%.

**2. Finish the whole-branch review.** One ran at the merged head and returned HOLD with 20
findings. Everything it raised is either landed or filed, but **the fixes themselves have not been
reviewed** — they are the newest commits on the trunk, and by `CLAUDE.md`'s own standard a review
that predates later commits is stale. Re-review the branch as it will be merged.

What that review found, and what became of it, is worth knowing before re-reviewing:

- The **Critical** was the same pointer-collision hazard the branch had already fixed once for the
  union lowering, sitting untouched in the inline-position hoist. Fixed, and both carriers that
  inherited the same order-dependence with it. If you look for one class of defect here, look for a
  fix that was scoped to where it was noticed rather than to the mechanism.
- **`irverify` never ran over compiler output anywhere.** Its checks only saw hand-built fixtures,
  so a `Reason: ""` planted in the compiler passed `go test ./...` entirely. Now wired, and the
  mutation fails. Note the test that catches it is the out-of-corpus one — no committed spec reaches
  that preserve call.
- **Two `pass` tests pinned a sort order neither checked**; `return 0` from the comparator left the
  tree green. Now pinned literally.
- Everything adjacent was filed rather than fixed, per the directive: #123 through #128.

**3. Take #119 out of draft** once that review is clean. The body is current: it has the Breaking
section, the `IRVersion` bump, and all eight `Closes` links.

### One thing deliberately left

Four intermediate commit subjects exceed the 72-char cap. Two attempts to reword them across the
five merge commits failed — the second completed with an identical tree but the rewords silently did
not apply. It is left alone on purpose: the branch is squash-merged, so the only subject that reaches
`main` is the PR title, which is within cap. Stated in the PR body too. If you want it fixed anyway,
rewrite before the merges rather than through them.

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
