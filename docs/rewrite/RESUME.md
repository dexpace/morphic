# Resume prompt

Paste this into a fresh session to pick the work up. It is written to be acted on, not just read.
`README.md` beside it carries the reasoning; this file carries the state and the next moves.

---

You are continuing a multi-session compiler rewrite in `morphic`, a spec-to-SDK compiler.
Draft PR **#119** is the trunk. Read these first, in order:

1. `docs/rewrite/README.md` — what was decided and why, including two decisions that were reversed
   by measurement. Read the "What was learned about verifying work here" section before writing any
   test.
2. `CLAUDE.md` — binding. The **zero nits, zero flaws** standard under "Commits & pull requests" is
   the bar; "Minor" is a severity, not permission to ship.
3. `gh pr view 119` and the open issues listed below.

## Standing directive — file, do not fix

Prioritise landing the rewrite. **A bug found along the way gets a GitHub issue, not a fix.** This
work has a strong tendency to spawn adjacent defects — the corpus gaps, the dropped keywords, the
`allOf` branch loss were all found while doing something else, and each is genuinely worth fixing
later. Fixing them inline is how the PR stops converging.

The exception is a defect **in code this work introduced** — a guard that does not guard, a walk
that is not deterministic. That is finishing the job, not detouring.

Currently filed and deliberately untouched:

| issue | |
|---|---|
| **#123** | a non-object inline `allOf` branch is dropped **whole** — type, constraints, annotations. Partly blocked on `ir.Model` having no `Constraints` field. |
| **#126** | the conformance corpus never exercises 33 IR fields the compiler assigns, including the entire `allOf`-inheritance-with-discriminator path |
| **#125** | `dependentRequired`, the `content*` family and `$dynamicRef` are silently dropped; §4.7's keyword list looks hand-assembled |
| **#127** | `pass` and `irverify` disagree about whether `Provenance.Source = -1` is valid — latent until anything verifies engine output |
| **#124** | `xml` on a parameter schema is discarded; may want a diagnostic rather than an IR field |
| **#120** | settle the name of the `Preserved` field — cheap now, breaking once #119 merges |

#121 and #122 were fixed rather than filed, before this directive; both ride the PR.

## Branch layout

**All four branches are merged.** `feat/annotation-gaps-and-preserved` is the single trunk, 63
commits from `main`, and is PR **#119** (draft). The others are merged into it and can be deleted:
`fix/irverify-preserved`, `fix/cli-output-truncation`, `docs/rewrite-working-notes`. Also delete
`wip/b11-distributed-lowering` — it was superseded, not merged.

At the merge point: all four gates green, coverage 100% total and per package, and the conformance
corpus passes **without** regeneration.

## Do these next, in this order

**1. Re-verify the merged trunk.** Do not take the paragraph above on trust — `git status` clean,
`go build ./...`, `go test ./...`, `./scripts/check-coverage.sh`, and
`go test ./compilers/openapi -run TestConformance` without `-update`.

**2. Run a whole-branch review at the final state.** Every review in this work found something, and
none of the 63 commits has been reviewed *as merged*. Expect findings; split them by the directive
above — defects in this branch's own code get fixed, anything pre-existing or adjacent becomes an
issue.

**3. Take #119 out of draft**, with a PR body that matches what actually landed. The current body
predates roughly half these commits.

| issue | |
|---|---|
| **#123** | a non-object inline `allOf` branch is dropped **whole** — type, constraints, annotations. Blocked in part on `ir.Model` having no `Constraints` field. |
| **#122** | `validate` checks no `ChannelID`/`MessageID` refs, and `Servers` integer indices are unbounded |
| **#117** | `propertyNames` is neither lowered nor preserved |
| **#124** | `xml` on a parameter schema is discarded; may want a diagnostic rather than a field |
| **#120** | decide the name of the `Preserved` field — cheap now, breaking after #119 merges |

### Nothing is queued behind those

Every review finding from this session has been landed, and every bug found along the way is filed
rather than half-fixed. The known-open list is exactly the issues named above.

## How to work here

These are not style preferences. Each was learned by shipping the opposite.

**Compile, do not read.** Every real defect on this project was found by compiling a probe and
inspecting the emitted IR and the full diagnostic list. Reading the source has repeatedly agreed
with claims that turned out false — including in review passes that declared the code clean.

**Prove a test by breaking what it describes.** Three tests were found asserting nothing; one
"safety net" permitted the exact addition it advertised catching. If a test is meant to fail when
something breaks, break that thing in a throwaway patch and watch it fail, then revert.

**Distrust counts and universals in prose.** Roughly ten were found wrong — a count referring to a
different branch, "every X cites Y" when five of seven did, "confirmed by probe" citing a deleted
artifact, "one map field" where there were two or three. None were caught by tests, because none
were testable. Prefer deriving a count or omitting it to maintaining one.

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
