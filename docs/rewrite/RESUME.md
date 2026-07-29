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
deliberately did not fix are #120 and everything from #123 upward; anything below #120 that is
still open predates this effort.

## Branch layout

`feat/annotation-gaps-and-preserved` is the single trunk and is PR #119. Every other branch that
carried part of this work has been merged into it; the exception is `wip/b11-distributed-lowering`,
which was superseded rather than merged and so will not show as merged. Naming the rest here is what
went stale last time — derive them:

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

**2. Review the newest commits.** Four independent reviews have now run — the whole branch, the
first fix wave, `ir`+`pass`, and the shipped prose — and every one returned findings. Everything
they raised is landed or filed. What has *not* been reviewed is the fixes for the last two, which
are the newest commits on the trunk. By `CLAUDE.md`'s own standard that makes the branch unreviewed
at its final state.

What those reviews found is worth knowing before re-reviewing, because the same shapes recur:

- The **Critical** was the same pointer-collision hazard the branch had already fixed once for the
  union lowering, sitting untouched in the inline-position hoist. Fixed, and both carriers that
  inherited the same order-dependence with it. If you look for one class of defect here, look for a
  fix that was scoped to where it was noticed rather than to the mechanism.
- **`irverify` reached less compiler output than it looked.** An earlier draft of this file said it
  "never ran over compiler output anywhere", which is false — `internal/harness` has always verified
  compiler output. The true gap is narrower: `harness.Check` returns at the first error diagnostic,
  so every fixture under `testdata/dangling` went unverified, and no committed spec reaches four of
  the compiler's preserve calls. State the mutation precisely, because generalising from one run is
  how the wrong version got written. Planting `Reason: ""` at a preserve call no
  committed spec reaches — `openapi:servers` is one — passed the entire suite before this branch and
  fails only the out-of-corpus test after it. Planting it in the shared `preserve` helper, which
  moves every call at once, failed five packages even before this branch, because most preserve
  calls *are* corpus-reachable. Both facts are real; neither generalises to the other. When you
  quote a mutation as evidence, name the site.
- **Two `pass` tests pinned a sort order neither checked**; `return 0` from the comparator left the
  tree green. Now pinned literally.
- **A new check rejected the pipeline's own output.** `pass` had stamped `Provenance{Source: -1}`
  for its own findings since before this branch; the normative shape named no sentinel; the verifier
  added here rejected it. Every `engine.Compile` document with a pass diagnostic failed the oracle.
  Resolved by declaring `ir.NoSource` — `ir-design.md` §13 now says what `-1` means, and it is the
  only out-of-table value admitted.
- **A behaviour shipped unasserted.** `fillCarrierDocs` merges a referent's `title`/`externalDocs`
  onto the carrier. That is correct — §14 mandates it "applied uniformly" — but nothing tested it,
  so narrowing it left the whole repo green. Check the contract before calling a behaviour a
  regression, and check that a behaviour you keep is pinned.
- Everything adjacent was filed rather than fixed, per the directive above: #123 through #134.

**3. Take #119 out of draft** once that review is clean. The body should match what landed; as of
this writing it carries the Breaking section, the `IRVersion` bump, and every `Closes` link.

### One thing deliberately left

Four intermediate commit subjects exceed the 72-char cap. Two attempts to reword them across the
merge commits failed — the second completed with an identical tree but the rewords silently did not
apply. It is left alone on purpose: the branch is squash-merged, so the only subject that reaches
`main` is the PR title, which is within cap. Stated in the PR body too. If you want it fixed anyway,
rewrite before the merges rather than through them.

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

**One verified run is not a verified class.** This is the failure that survived every other rule
here. A mutation was planted, the suite went red, and the conclusion was written as though it held
for every site of that kind — it did not, and the same shape produced three separate wrong claims:
that `irverify` never met compiler output, that a golden count was 44 of 45, and that a preserve
call's coverage generalised to all of them. Each was *measured*; each was then over-generalised in
the sentence describing it. When you quote a measurement, name the site it was taken at.

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
