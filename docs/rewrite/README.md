# Compiler rewrite — working notes

**Temporary.** These files exist so work spanning several sessions can be resumed without
re-deriving decisions. Delete this directory when the rewrite lands; anything here that deserves to
outlive it belongs in `docs/ir-design.md`, `docs/architecture.md`, or a GitHub issue.

Nothing here is normative. Where a file disagrees with `ir-design.md`, the design doc wins.

Nothing here is authoritative about *state*, either. Branch layout, commit counts and issue status
are derived by the commands in `RESUME.md` rather than recorded here — every count this directory
hardcoded went stale, including in the commit that claimed to refresh them.

## Where things stand

`feat/annotation-gaps-and-preserved` is the trunk, and is PR **#119**. Read `RESUME.md` first; it
carries the next moves and how to verify them.

| | |
|---|---|
| Annotation retention suite (#111) | merged to `main` |
| Reference resolver extraction (#112) | merged to `main` |
| Review standard in `CLAUDE.md` (#113) | merged to `main` |
| Dropped annotations at declarations (#114) | on #119 |
| `Extensions` → `Preserved` (#110) | on #119 |
| Intersection combinator (#115) | answered — no node; §B11 distribution landed instead |
| Inline positions dropping data (#116) | on #119 |
| `propertyNames` (#117) | on #119 |
| CLI `-o` truncation (#118) | on #119 |
| `validate` reference coverage (#121, #122) | on #119 |

Filed and deliberately unstarted: run `gh issue list --state open`. Everything this work filed and
chose not to fix is numbered #120 and #123 upward; anything below #120 that is still open predates
this effort. `RESUME.md` says why that split exists.

## The finding that redirected this work

The rewrite began as a plan to decompose the OpenAPI compiler into isolated per-annotation units.
A conformance suite was built first, to measure whether the premise held. **It did not.** The
site-versus-referent rule the decomposition was meant to fix was already correct for eight of the
nine reference-site annotations measured against `main`; this branch closed the ninth, so every
reference-site cell now passes and all five surviving gaps are declaration-site.

The real duplication was on a different seam: annotation handling was bolted to `fillModelDetail`,
reachable from only two of `lower()`'s six destinations, so a component whose body lowered elsewhere
lost its documentation, its extensions, and its constraints. That is #114, and it is a much smaller
change than the decomposition it replaced.

`design.md` records the original argument and carries its own correction at the top. Read it as a
worked example of a plausible design overturned by measurement, not as a plan to execute.

## The files

| file | what it is |
|---|---|
| `RESUME.md` | **Start here after an interruption.** A prompt to paste into a fresh session: what to do next in order, and how to verify work in this repo. |
| `design.md` | The original decomposition design, with a "Measured outcome" section that overturns its central recommendation, and a follow-up list. |
| `decisions-preserved.md` | The design decisions behind #110's `PreserveReason` taxonomy, each traced to verified evidence. |
| `findings-inline-positions.md` | Compiled evidence for #116, including the control table proving the mechanism is pointer ownership rather than schema position. |
| `findings-intersection.md` | The #115 analysis: why an intersection combinator should not be added, the per-format capability table, and the cost if it were. |

## What was learned about verifying work here

Recorded because it changed the outcome repeatedly, and because `CLAUDE.md`'s review standard is
the short form of it.

**Reading a test agrees with it.** Review passes read assertions that looked correct and found
nothing. The defects surfaced only when someone broke the thing an assertion described and watched
whether it failed. Several tests were found asserting nothing; one "safety net" test permitted the
exact addition it advertised catching.

**A check that runs is not the same as a check that reaches.** `irverify` gained checks for
`Preserved`, provenance and the registry indices, and it *was* already run over compiler output by
`internal/harness`. But `harness.Check` returns at the first error diagnostic, so the deliberately
broken fixtures never reached it, and no committed spec exercises four of the compiler's preserve
calls — so a defect planted at one of those passed the whole suite. The first telling of this said
the checks "never met compiler output", which was simply wrong and took three rounds to catch.
Establish where a check actually reaches before claiming what it covers.

**Claims in prose are where the errors live.** A count referring to a different branch, "every X
cites Y" when most did, "confirmed by probe" citing a deleted artifact, a doc comment describing an
intended end state in the present tense, a normative definition falsified by its only write site.
None were caught by tests, because none were testable. Derive a count or omit it.

**Corpora share the blind spots of the code they cover.** Regenerating goldens after fixing #114
produced a zero-byte diff: no corpus spec annotated a non-model declaration, so the defect was
invisible to the golden suite. This recurred often enough to be filed as #126.

**A hazard fixed in one place is not fixed.** The pointer-collision defect described below was found
in the union lowering, fixed there, and written into `ir-design.md` §4.3 — and the same hazard was
sitting in the inline-position hoist the same branch introduced, because the fix was scoped to the
namespace where it was noticed rather than to the mechanism that caused it. Both are fixed now. The
generalisation is in §4.3: any lowering that mints a node at a pointer the source can also name
needs a namespace of its own.

**Order-dependence hides from every test that runs one order.** Both pointer collisions produced a
different IR depending on which of two declarations lowered first, with no diagnostic in either
order and a clean `pass/validate` on both. Nothing in the suite compared the two, so nothing failed.
The regression tests now compile both orders and diff the documents, which is the only shape of
assertion that can catch it.

## #115 is answered — do not add an intersection combinator

Only OpenAPI and AsyncAPI can express struct ∧ union, and they share a code path, so that is one
data point rather than two. TypeSpec's `&` is model-only — `ir-spec-matrix.md` line 20 already
records this as `⚠ & (model is)`. Smithy, GraphQL, Protobuf and Erlang cannot express it. Protobuf's
`message{kind; oneof}` is already served by Model + Union-typed property + `Property.Flatten`.

The cost if it were added is roughly two dozen edit sites, **none caught by the compiler** — there is
no `go:generate`, `exhaustive` is not enabled in `.golangci.yml`, and the surviving sealed-interface
switches have silent `default:` arms. That is worth its own issue independent of #115: `CLAUDE.md`
mandates a switch-completeness test that is not actually enforced.

`docs/reference-learnings.md` §B11 — *"Latent compiler bug #2 — allOf + oneOf co-occurrence dropping
allOf"* — prescribed **distributing the composition across the union variants**, using only nodes the
IR already has. That is what landed.

### The distribution gate is syntactic, and all-or-nothing

An earlier attempt stalled trying to decide **per branch** whether a composition could be
distributed, because a scalar branch cannot absorb one. That framing was wrong. The rule is:

> Distribute only when **every** branch is a `$ref`.

`Base` and `Mixins` conjoin *by reference* — they hold a `TypeRef`, so a conjunct needs a node with
an ID, which a `$ref` branch has. An inline branch's node would have to be hoisted at the branch
pointer, which is exactly where the composed variant lives, so it could only be merged keyword by
keyword with a conjunction rule per keyword — `additionalProperties` open-versus-closed, nested
`allOf`, nested discriminator, the branch's own annotations and constraints. One wrong rule is a
silently wrong shape.

The syntactic test subsumes the scalar-branch problem (a scalar branch is inline) and needs no
reference resolution, so cycles and unresolved targets cannot make the classification unreliable.

Half-distributing was rejected outright: it yields a `Union` whose variants disagree about whether
they carry the body, with nothing in the IR recording which.

### The composed variant needs a pointer of its own

Interning a composed variant at the branch's own JSON pointer (`…/oneOf/N`) is unsafe. `intern`
returns any pre-existing node at a pointer without checking, so a `$ref` elsewhere in the document
targeting `…/oneOf/N` claims it first and the variant silently becomes whatever that reference
lowered to — no diagnostic, clean `pass/validate`, and order-dependent, since swapping two component
declarations changes both the IR and what the outside reference resolves to.

Composed variants therefore get a synthetic `t/composed…` ID rather than the branch pointer. The
rule is recorded in `ir-design.md` §4.3, and it generalises: **any** lowering that mints a node at a
pointer the source can also name needs a namespace of its own.

### §B11's second shape is not implementable

§B11 offers two lowerings. The first — distribute across variants — is what shipped. **The second
is wrong.** It proposes folding branches into a Model's discriminator mapping for the
`allOf:[base] + oneOf:[subtypes]` idiom, but that requires each branch to declare the model as its
`Base`, which `discriminator` + `oneOf` never states. Compiled, `pass/validate` rejects the result
with `discriminator-missing-variant`, and that error is pre-existing rather than introduced.

So a declared `discriminator` now preserves verbatim with an accurate reason instead.

This matters beyond the immediate fix: §B11 was written from studying four reference
implementations, which is exactly the kind of document that gets trusted without re-derivation. It
was right in direction and wrong in one of its two prescriptions, and only implementing it revealed
which.

## The comment sweep

False claims were corrected across `ir/`, `internal/`, `cmd/`, `engine/` and two design docs
(`dc15bfc`). The pattern held: counts that were wrong, "validated" for properties nothing validates,
a doc citing a section that makes no such point, and a struct sketch with a field the real type does
not have.

One instance was a comment wrong **because the code was wrong**, filed as #121 and fixed on this
branch: `validate`'s dangling-`TypeRef` check claimed to cover every ref and missed several
ref-bearing fields, one of them reachable from ordinary multipart input.

Worth noting one correction became wrong again as the code moved — a comment fixed from "every" to
"most" was falsified when #114 changed which `knownGap` reasons exist. Counts in prose rot even
after being corrected, which is the argument for deriving them rather than maintaining them.
