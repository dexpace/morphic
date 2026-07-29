# #110 design decisions, settled against the verified inventory

All four load-bearing claims from the investigation were independently re-verified before these
decisions were made. Evidence lines cited are from `main` at `996e40a`.

## A. `openapi:oneOf` / `anyOf` → `degraded_lowering`, and amend §4.8

The investigation could not classify these: they satisfy §4.8's "no faithful target in any SDK
language" (a struct ∧ union intersection) but fail its "normative documented lowering", because
nothing in the doc describes them.

**Decision: amend §4.8 to document the lowering, then classify as `degraded_lowering`.**

§4.8's stated purpose is that "degradation is a decision, not an accident". An undocumented
degradation is precisely what that section exists to eliminate, so documenting it is using the
section as designed rather than stretching it.

The alternative — adding an intersection node to the IR — is a capability addition that touches
invariant 9 ("the IR capability surface is complete from day one") and deserves its own design
cycle with the sibling formats considered. TypeSpec has intersection types (`A & B`); Smithy does
not. That is a real question and it is not this PR's question. **Raise it as a follow-up issue.**

## B. `openapi:itemEncoding` → fix the defect, remove from the taxonomy

`Content.ItemEncoding map[string]PartEncoding` exists at `ir/operation.go:122` and is documented in
`ir-design.md` §7.2. `compilers/openapi/content.go:76` nevertheless preserves the source construct
raw under `openapi:itemEncoding` with `codeDegradedConstruct`, and its message contradicts both.

**Decision: populate the existing field. Delete the raw preservation and its diagnostic.** This is a
dropped-data defect in its own right — the same class as #114 — not a member of any reason category.

## C. Split the type: `Preserved` is not what the binding fields hold

`Extensions` is currently reused for two unrelated purposes:

1. **Unmodeled source kept verbatim** — `TypeCommon.Extensions`, `Property.Extensions`, and the rest.
   These become `Preserved`, gaining `Reason` and `Provenance`.
2. **Modeled protocol configuration whose shape the IR deliberately does not constrain** —
   `Server.Bindings`, `Channel.Bindings`, `Message.Bindings`, `ProtocolDecl.Options`
   (`ir/service.go:51`) and the binding map in `ir/bindings.go`. These are *declared* config, not
   preserved leftovers. A `Reason` on them would be meaningless, and calling them "Preserved" would
   misdescribe them.

**Decision: introduce a distinct type for (2) — `RawConfig` — carrying the same untyped map shape
with no `Reason` and no `Provenance`.** Renaming everything to `Preserved` would be the same
category error the rename is meant to fix.

## D. `Reason` lives inside the map value, not in a parallel map

`reconcileProperty` merges extension maps with `maps.Copy`. A parallel `map[string]Reason` would be
silently dropped on every merge. The entry must be a struct.

## E. Do not derive `Reason` from the diagnostic code

Five of the six `no_ir_home` keys carry `codeDegradedConstruct`, which is what makes them look like
§4.8. But `diag.go:43-49` documents that code as deliberately spanning three situations: "preserved
raw for want of a structural home, lowered to a weaker shape, or dropped". The code marks *lossiness*,
not *reason*. Classify each write site from what it does, never from the diagnostic it emits.

## The enum

```go
// PreserveReason says why a construct was kept verbatim instead of modeled.
type PreserveReason string

const (
    // ReasonVendorExtension: the source format assigns the key no semantics
    // (`x-*`). Unbounded set; nothing can be inferred about the value.
    ReasonVendorExtension PreserveReason = "vendorExtension"

    // ReasonValidationOnly: validation logic, not data shape. The IR's
    // structural picture is complete without it, and no target type system or
    // sibling source format has an equivalent (ir-design.md §4.7).
    ReasonValidationOnly PreserveReason = "validationOnly"

    // ReasonDegradedLowering: no faithful target in any SDK language, so the
    // construct was lowered to a documented weaker shape and the original kept
    // alongside it (ir-design.md §4.8).
    ReasonDegradedLowering PreserveReason = "degradedLowering"

    // ReasonNoIRHome: expressible in target languages and modeled by a sibling
    // IR node at analogous scope, but no field exists here yet. A gap, not a
    // boundary — each of these is a candidate for promotion.
    ReasonNoIRHome PreserveReason = "noIRHome"
)
```

Counts after decisions A and B: `validationOnly` 5 keys, `noIRHome` 5 keys (itemEncoding removed),
`degradedLowering` 2 keys (oneOf/anyOf, newly documented), `vendorExtension` unbounded from 7 sites.

## What came out of auditing the taxonomy

Everything that audit raised has landed on this branch except one item, filed as **#128**:
`preserve()` guards `raw == nil` but not `len(raw) == 0`, and an empty non-nil `RawValue` makes the
whole document fail to marshal. Not reachable from the current compiler, so it is filed rather than
fixed here.

One entry in that audit is worth keeping for the mistake in it rather than the fix. It justified the
new `irverify` check with:

> `internal/harness` runs `Verify` over the whole corpus, so the check gets corpus-wide coverage
> with no new wiring.

That sentence was wrong, and it was never checked. The harness is not in the CI gate, and nothing
runs `irverify` over compiler output, so the check it justified could not fire on anything the
compiler produced — a defect planted in the compiler passed the entire suite. The claim then
propagated: it was repeated in `README.md` as "covers this corpus-wide", where it read as
established fact.

The general shape is the one this work kept meeting: **"no new wiring needed" is a claim about the
world, not about the code being written, and it is the kind nobody tests.** An audit that recommends
a check should say how it verified the check will run, not assume existing infrastructure reaches
it.
