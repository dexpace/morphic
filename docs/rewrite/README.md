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
| Intersection combinator (#115) | **answered and implemented** — no node added; §B11 distribution landed as `998c3a6`+`716d8eb` |
| Inline positions drop data (#116) | in progress |
| Name of the `Preserved` field (#120) | open — needs a decision, see the issue |
| `validate` misses ref-bearing fields (#121) | in progress |
| `propertyNames` unhandled (#117) | open |
| CLI truncates `-o` on failure (#118) | on `fix/cli-output-truncation` |
| `irverify` accepted a meaningless entry | on `fix/irverify-preserved` |

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
| `findings-intersection.md` | The #115 analysis: why an intersection combinator should not be added, the per-format capability table, the cost if it were, and the §B11 prescription the compiler currently inverts. |

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

## Resume here

Four branches are open and unmerged. All build and pass; none is on a remote.

| branch | contains |
|---|---|
| `feat/annotation-gaps-and-preserved` | #114 and #110, plus review fixes. The trunk of this work. |
| `fix/cli-output-truncation` | #118. Self-contained, `cmd/morphic` only. |
| `fix/irverify-preserved` | `irverify` checks for `Preserved`, a `PreserveReason.Valid()` guard, two doc corrections. |
| `docs/rewrite-working-notes` | this directory. |
| `wip/b11-distributed-lowering` | **incomplete** — see below. Not for merge. |

Merge order does not matter much; the four complete branches touch disjoint files.

### #115 is answered — do not add an intersection combinator

Only OpenAPI and AsyncAPI can express struct ∧ union, and they share a code path, so that is one
data point rather than two. TypeSpec's `&` is model-only — `ir-spec-matrix.md` line 20 already
records this as `⚠ & (model is)`. Smithy, GraphQL, Protobuf and Erlang cannot express it. Protobuf's
`message{kind; oneof}` is already served by Model + Union-typed property + `Property.Flatten`.

Cost if it were added: roughly 27 edit sites, **none caught by the compiler** — there is no
`go:generate`, `exhaustive` is not enabled in `.golangci.yml`, and all four sealed-interface
switches have silent `default:` arms. That is worth its own issue independent of #115: `CLAUDE.md`
mandates a switch-completeness test that is not actually enforced.

### But the current behaviour is a known bug, and the fix is already prescribed

`docs/reference-learnings.md` §B11 — *"Latent compiler bug #2 — allOf + oneOf co-occurrence dropping
allOf"* — studied four reference implementations and prescribed **distributing the composition
across the union variants**, using only nodes the IR already has. The compiler does the inverse:
keeps the structural body and discards the union to `Preserved`. Worse, this branch *documented*
that inversion in §4.8 as a deliberate degraded lowering.

### Where `wip/b11-distributed-lowering` stopped

Partial implementation of §B11 across `compose.go`, `diag.go` and `schema.go`. Tests pass, but the
lowering is not correct yet, and it stopped on a case §B11 does not address:

> Distribution is not universally valid. A branch that is itself a scalar — `oneOf` with a
> `{type: string}` member — cannot absorb a struct composition. The classifier has to decide **per
> branch** whether distribution applies before emitting a variant.

Resolve that before continuing. The branches that *can* absorb the composition and those that cannot
may need different treatment, and forcing one shape onto both would repeat the mistake this fix
exists to correct.

### Also still open

- **#116** — inline positions drop annotations *and constraints*. Fix shape recommended in
  `findings-inline-positions.md`: hoist rather than add per-position fallback homes.
- **#117** — `propertyNames` is neither lowered nor preserved.
- One handover from the `irverify` work: `compilers/openapi/schema_test.go`'s
  `TestPreserve_AllReasonsReachable` claims a per-site universal ("no site leaves the field at its
  zero value") while its body asserts a per-reason existential, so an entry with `Reason: ""` passes.
  `ir/empty-preserve-reason` now covers this corpus-wide, so the comment can simply be narrowed.
- A repository sweep for miscounted or unverifiable claims in comments was started and not finished.
  Three instances are known and two are fixed; `ir/property_test.go` still says "one map field" where
  `Property` has three.

## Update — #115 resolved

No intersection combinator was added; §B11's distribution landed instead (`998c3a6`, `716d8eb`).
Two findings from that work are worth carrying forward.

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

### `reference-learnings.md` §B11's second shape is not implementable

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

## Update — the comment sweep

Ten false claims corrected across `ir/`, `internal/`, `cmd/`, `engine/` and two design docs
(`dc15bfc`). The pattern held: counts that were wrong, "validated" for properties nothing validates,
a doc citing a section that makes no such point, and a struct sketch with a field the real type does
not have.

One instance was a comment wrong **because the code is wrong**, now filed as #121: `validate`'s
dangling-`TypeRef` check claims to cover every ref and misses six ref-bearing fields. Latent today
because only the OpenAPI compiler exists and it does not populate them; live the moment AsyncAPI or
Protobuf lands.

Worth noting one correction became wrong again as the code moved: a comment that had been fixed from
"every" to "most" is now "0 of 2", because #114 changed which `knownGap` reasons exist. Counts in
prose rot even after being corrected — which is the argument for deriving them or omitting them
rather than maintaining them.

## CRITICAL — open defect in the B11 lowering (`998c3a6`)

**Do not treat `998c3a6` as finished.** Review found a defect that produces silently wrong IR with
no diagnostic and a clean `pass/validate`.

`composedVariant` (`compose.go`) interns each composed variant Model at the branch's **own** JSON
pointer, `…/oneOf/N`. `intern` (`hoist.go`) returns any pre-existing node at a pointer without
checking. So any `$ref` elsewhere in the document that targets `…/oneOf/N` claims that pointer
first, and the variant silently becomes whatever that reference lowered to.

Two confirmed repros, neither needing a source-order trick:

- `Other.properties.x: {$ref: '#/components/schemas/Combo/oneOf/0'}` declared before `Combo`
- `A.properties.a: {$ref: '…/Combo/oneOf/1'}` where `A` is branch 0's own target

Result in both: a `Union` whose variant 0 lost the composed body while variant 1 kept it — exactly
the state §4.3 and the commit message promise cannot occur. It is also order-dependent: swapping the
two component declarations changes both the IR and what the outside reference resolves to.

**Fix options named by the review:** give the composed variant a distinct synthetic pointer, or
detect the collision and fall back to the preserved lowering.

The irony is instructive. The implementation's own reasoning was *"an inline branch's node would
have to be hoisted at the branch pointer, which is exactly where the composed variant lives."* That
is right — and it holds when the branch is a `$ref` too, which the reasoning did not follow through.

### Two further Important findings on the same commits

**The `$ref` test is not equivalent to the property it stands for, in either direction.**
`conjoinBranch` puts `Base`/`Mixins` on non-Model nodes — a `$ref` to a scalar component yields
`Base` = `Scalar`, to a union yields `Base` = `Union` — while §4.8 in the same diff says
`Base`/`Mixins` is model composition only. And `isRefBranch` is true but unusable for `{$ref: ""}`,
cross-document refs, and refs to undeclared components, each of which **half-distributes**,
contradicting the "never halfway" guarantee.

**`oneOf`+`anyOf` co-declared reports a false reason.** Three messages cover four documented shapes,
so that case emits *"a branch names no referent to conjoin the body with"* when every branch is in
fact a `$ref`. The test asserts only the `Reason`, never the message, so it cannot catch this —
the same test-does-not-check-what-it-claims pattern as everywhere else in this work.

### Confirmed good on the same review

§B11 shape #2's rejection was independently verified by compiling the idiom against clean exports of
both revisions: byte-identical IR, identical pre-existing `discriminator-missing-variant` errors.
Zero-golden churn was established empirically — all 80 spec files under `testdata/` compiled with
both compilers, byte-identical — with the cause identified: no corpus spec co-declares a union with
structural keywords, and the near-miss `oneof-discriminated.yaml` misses because `discriminator` is
not in `declaresShape`. Termination holds for self-reference, 3-cycles, and self-referencing
branches.
