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

## Branch layout

Five branches, none merged. Work happens in git worktrees so several agents can run at once without
contending for one index.

| branch | scope | state |
|---|---|---|
| `feat/annotation-gaps-and-preserved` | `compilers/openapi/`, `ir/`, `docs/`, `testdata/` | trunk of the work; PR #119 |
| `fix/irverify-preserved` | `pass/`, `ir/`, `ir/irverify/` | complete-ish |
| `fix/cli-output-truncation` | `cmd/morphic/` | complete |
| `docs/rewrite-working-notes` | `docs/rewrite/` | this directory |
| `wip/b11-distributed-lowering` | — | **superseded, do not merge** |

`integration/rewrite-trial` is a disposable branch used to trial-merge the four; recreate it rather
than trusting an old one.

**The first two both touch `ir/` heavily.** Trial-merge before assuming they compose — a check added
on one branch has never seen the other's compiler output.

## Do these next, in this order

**1. Verify what is actually committed.** Several agents were interrupted mid-run at least once.
For every branch: `git status` clean, `go build ./...`, `go test ./...`, `./scripts/check-coverage.sh`.
Trust the tree over any summary, including `README.md`.

**2. Land the outstanding review findings.** Each was reproduced by compiling, not by reading:

- A **Critical** in the §B11 union lowering: `composedVariant` interns a composed variant at the
  branch's own JSON pointer (`…/oneOf/N`), and `intern` returns any pre-existing node there without
  checking — so an unrelated `$ref` to that pointer silently steals it, producing a `Union` whose
  variants disagree about whether they carry the composed body. No diagnostic; `pass/validate`
  clean; order-dependent. A fix was dispatched but may not have landed — check `compose.go`.
- Two Important findings on the same commits: `conjoinBranch` puts `Base`/`Mixins` on non-Model
  nodes while §4.8 says model-only; and `oneOf`+`anyOf` co-declared reports a false reason, with a
  test that asserts only the `Reason` and so cannot catch it.

**3. Then the remaining issues**, roughly by severity:

| issue | |
|---|---|
| **#123** | a non-object inline `allOf` branch is dropped **whole** — type, constraints, annotations. Blocked in part on `ir.Model` having no `Constraints` field. |
| **#122** | `validate` checks no `ChannelID`/`MessageID` refs, and `Servers` integer indices are unbounded |
| **#117** | `propertyNames` is neither lowered nor preserved |
| **#124** | `xml` on a parameter schema is discarded; may want a diagnostic rather than a field |
| **#120** | decide the name of the `Preserved` field — cheap now, breaking after #119 merges |

**4. One handover not yet done.** `compilers/openapi/schema_test.go`'s
`TestPreserve_AllReasonsReachable` claims a per-site universal ("no site leaves the field at its
zero value") while its body asserts a per-reason existential — an entry with `Reason: ""` passes.
`ir/empty-preserve-reason` now covers this corpus-wide, so narrowing the comment is enough.

**5. Then merge, review the whole branch, and take #119 out of draft.**

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
