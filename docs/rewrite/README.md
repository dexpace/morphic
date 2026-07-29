# Compiler rewrite — working notes

**Temporary.** These files exist so work spanning several sessions can be resumed without
re-deriving decisions. Delete this directory when the rewrite lands; anything here that deserves to
outlive it belongs in `docs/ir-design.md`, `docs/architecture.md`, or a GitHub issue.

Nothing here is normative. Where a file disagrees with `ir-design.md`, the design doc wins.

## Where things stand

| | state |
|---|---|
| Annotation retention suite | merged (#111) |
| Reference resolver extraction | merged (#112) |
| Review standard in `CLAUDE.md` | merged (#113) |
| Seven dropped annotations (#114) | on `feat/annotation-gaps-and-preserved` |
| `Extensions` → `Preserved` (#110) | on `feat/annotation-gaps-and-preserved` |
| Intersection combinator (#115) | design question open |
| Inline positions drop data (#116) | open |
| `propertyNames` unhandled (#117) | open |
| CLI truncates `-o` on failure (#118) | on `fix/cli-output-truncation` |

## The finding that redirected this work

The rewrite began as a plan to decompose the OpenAPI compiler into isolated per-annotation units.
A conformance suite was built first, to measure whether the premise held. **It did not.** The
site-versus-referent rule the decomposition was meant to fix is already correct for eight of nine
reference-site annotations.

The real duplication was on a different seam: annotation handling was bolted to `fillModelDetail`,
reachable from only two of `lower()`'s six destinations, so a component whose body lowered elsewhere
lost its documentation, its extensions, and its constraints. That is #114, and it is a much smaller
change than the decomposition it replaced.

`design.md` records the original argument and carries its own correction at the top. Read it as a
worked example of a plausible design overturned by measurement, not as a plan to execute.

## The files

| file | what it is |
|---|---|
| `design.md` | The original decomposition design, with a "Measured outcome" section that overturns its central recommendation, and a follow-up list. |
| `plan-phase-1.md` | The implementation plan for the retention suite and resolver extraction. Both merged; kept because its task structure is a usable template. |
| `decisions-preserved.md` | The five design decisions behind #110's `PreserveReason` taxonomy, each traced to verified evidence, plus queued audit findings not yet applied. |
| `findings-inline-positions.md` | Compiled evidence for #116, including the control table proving the mechanism is pointer ownership rather than schema position. |

## Queued, not yet applied

`decisions-preserved.md` ends with seven items from consumer and acceptance audits that are
verified but unimplemented. The two that matter most:

- **`irverify` has no `Preserved` check.** A document with an empty `Reason` yields zero violations,
  round-trips clean, and passes `pass.Validate`. `checkRegistryKeys` already emits `ir/empty-*-id`
  for empty keys, so the precedent is direct.
- **A test that claims to guard the zero value does not.** `schema_test.go` records
  `seen[entry.Reason] = true` and then asserts only that the four known reasons appear — an entry
  with `Reason: ""` sets `seen[""]` and every assertion still passes.

## What was learned about verifying work here

Recorded because it changed the outcome repeatedly, and because `CLAUDE.md`'s review standard is
the short form of it.

**Reading a test agrees with it.** Nine review passes read assertions that looked correct. The
defects surfaced only when someone broke the thing an assertion described and watched whether it
failed. Three tests were found asserting nothing; one "safety net" test permitted the exact addition
it advertised catching.

**Claims in prose are where the errors live.** Six separate instances: a count referring to a
different branch, "every X cites Y" when five of seven did, "confirmed by probe" citing a deleted
artifact, "one map field" where there were two, a doc comment describing an intended end state in
the present tense, and a normative definition falsified by its only write site. None were caught by
tests, because none were testable.

**Corpora share the blind spots of the code they cover.** Regenerating goldens after fixing #114
produced a zero-byte diff: no corpus spec annotated a non-model declaration, so the defect was
invisible to the golden suite. The corpus needed the missing shape added before it could witness
the fix.
