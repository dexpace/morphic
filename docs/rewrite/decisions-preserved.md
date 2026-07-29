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

## Queued for the next fix wave (consumer audit findings)

1. IMPORTANT — irverify gains a Preserved check. Verified: Reason:"" yields 0 violations today,
   round-trips clean, passes pass.Validate. Precedent: checkRegistryKeys emits ir/empty-*-id for
   empty keys; checkDiagnostics validates a scalar field's well-formedness. internal/harness runs
   Verify over the whole corpus, so the check gets corpus-wide coverage with no new wiring.
2. IMPORTANT — schema_test.go:635-668 does not guard what its comment claims. `seen[entry.Reason]`
   means an entry with Reason:"" sets seen[""] and every assertion still passes. Also inspects only
   Common().Preserved on two component schemas — no operation/param/response/auth site.
3. IMPORTANT — preserve() (schema.go:877-879) guards raw == nil but not len(raw) == 0. An empty
   non-nil RawValue makes the WHOLE document unmarshalable. Unreachable today, and the harness
   would misfile it as OutcomeRoundtrip rather than OutcomeViolations.
4. MINOR — no enumerate-and-test guard for PreserveReason; nothing rejects unknown/empty on
   deserialize. Repo precedent: ir/typedef_test.go allKinds + TestNewTypeDef_UnknownKind, and the
   const-block tie test. TypeKind is also a bare string enum and does have the guard.
5. NIT — ir/preserved.go:41 "byte-faithful" overstates: json.Marshal compacts and HTML-escapes
   RawMessage, and the OpenAPI path re-encodes, coercing ill-formed UTF-8 to U+FFFD.
6. NIT (pre-existing, broader) — Provenance.Source is never bounds-checked against len(doc.Sources)
   by either verifier. The rename multiplied Provenance instances, so consider it now.
CLEAN: pass/, engine/, cmd/*, internal/harness, ir/irtest, architecture.md, README.md.
7. NIT (from the a80f711 review) — ir/operation_test.go:196 says the test "pins Class C for
   Content's ONE map field". False count: Content has two map-typed fields, Encoding and
   Preserved (map[string]PreservedEntry). Coverage is fine, the wording is not. Sibling comments
   (ir/channel_test.go:42) use countless phrasing — match that. Same shape as every other finding
   this session: a count asserted in prose that nothing verifies, and wrong.

a80f711 VERDICT: clean, approve. Invariant-9 question answered more strongly than I framed it —
   the map was the WRONG CONTAINER even hypothetically, since per-stream-item encoding already
   has homes (Variant.Event/EventInfo, Message.ContentType, Property.Encoding) and all key by
   variant or message, not PropID. Upstream is ItemEncoding *Encoding, mutually exclusive with
   Encoding. 9 probe shapes at both revisions: 4 byte-identical, 5 differ only by envelope
   removal. Verifier reach PROVEN by planting a dangling TypeID and a cased Naming inside
   ItemEncoding.Headers[0] and confirming both are reported.
