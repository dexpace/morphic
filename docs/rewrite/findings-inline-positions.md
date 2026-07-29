# Inline schema positions silently drop declared annotations and constraints

Method: every claim below is from compiling a probe spec through
`openapi.New().Compile(...)` (throwaway module, `replace` → this worktree) and inspecting the
emitted IR JSON plus the full diagnostic list. Nothing here is inferred from reading the source.

## 1. The mechanism (one root cause)

`attachDeclaredAnnotations` no-ops unless the pointer owns a node:

```go
id, owned := l.byPointer[pointer]
if !owned { return }
```

`byPointer[pointer]` is written **only** by `intern()`, reached **only** via `internNode()`.
Two lowering destinations never call `internNode` — they return a *shared* node:

- `scalarTypeID` → `l.primID(prim)` for any `(type, format)` pair present in `formatTable`
  (`string`, `integer`, `boolean`, `number`, `string/date-time`, …).
- `lowerUntyped` → `l.primID(ir.PrimAny)` when there is no type and no `properties`.

`effectiveTypes` strips `null` first, so `type: [string, "null"]` also lands in the first case.

Therefore the boundary is **pointer ownership, not position**: a body that reduces to a shared
primitive/any leaves its pointer unowned, and every declaration-scoped annotation *and* every
value constraint written at that position is discarded with **no diagnostic**.

Four positions re-own the pointer or supply an alternate home:

| position | fallback |
| --- | --- |
| component schema | `lowerComponentSchema` → `internAlias` |
| `$ref`-target internal sub-schema | `hoistSubSchema` → `internAlias` |
| model property | `fillPropertyDetail` (alternate home on `ir.Property`) |
| `oneOf`/`anyOf` with structural siblings | `lowerWithUnionSiblings` → `internAlias` |

Every other inline position has neither.

## 2. Confirmed dropped — zero diagnostics in all cases

`✗` = absent from the emitted IR. Probe shape at each position:
`{type: string, description: D, x-vendor: V, xml: {name: X}, not: {const: N}, maxLength: M}`.

| position | description | `x-*` | `xml` | `not` | constraints |
| --- | --- | --- | --- | --- | --- |
| `items` | ✗ | ✗ | ✗ | ✗ | ✗ |
| `items` nested inside `items` | ✗ | ✗ | ✗ | ✗ | ✗ |
| `additionalProperties` | ✗ | ✗ | ✗ | ✗ | ✗ |
| `prefixItems[0]` | ✗ | ✗ | ✗ | ✗ | ✗ |
| `prefixItems[3]` | ✗ | ✗ | ✗ | ✗ | ✗ |
| `patternProperties` value | ✗ | ✗ | ✗ | ✗ | ✗ |
| `allOf` branch member | ✗ | ✗ | ✗ | ✗ | ✗ |
| `oneOf` branch member | ✗ | ✗ | ✗ | ✗ | ✗ |
| `anyOf` branch member | ✗ | ✗ | ✗ | ✗ | ✗ |
| parameter `schema` | ✗ | ✗ | ✗ | ✗ | **kept** (`ir.Parameter.Constraints`) |
| header `schema` | ✗ | ✗ | ✗ | ✗ | ✗ |
| requestBody media-type `schema` | ✗ | ✗ | ✗ | ✗ | ✗ |
| response media-type `schema` | ✗ | ✗ | ✗ | ✗ | ✗ |
| `propertyNames` | **entire subschema dropped** — never lowered, never preserved |

The three reported cases reproduce exactly. Minimal repro for `items`:

```yaml
components:
  schemas:
    A:
      type: array
      items: { type: string, description: DOC, x-vendor: V, xml: {name: X}, not: {const: N} }
```

Emitted IR — 0 diagnostics, no trace of any of the five keywords:

```json
"t/openapi/components/schemas/A": {
  "kind": "list", "docs": {},
  "elem": { "target": "t/prim/string", "nullable": false }
}
```

## 3. Not affected — and the control that proves the mechanism

**Preserved raw, so annotations ride along inside the blob:** `not`, `if`/`then`/`else`,
`contains`, `unevaluatedItems`, `unevaluatedProperties`. These subschemas are never lowered;
`fillValidationOnly` stores them verbatim.

**Owns a node, so annotations survive at the *same* positions:** any body that reaches
`internNode` — `type: object` (Model), `type: array` (List/Tuple), `enum`, `const`,
`format: byte`, an unknown `format`, multi-type `type: [string, integer]` (Union), boolean
`false`.

This is the decisive control. At one position, `items`:

| `items:` body | survives? |
| --- | --- |
| `{type: object, description: D, x-vendor: V, xml: …, not: …}` | **all kept** |
| `{type: string, format: weirdfmt, description: D, x-vendor: V}` | **all kept** |
| `{type: string, enum: [a,b], description: D, x-vendor: V}` | **all kept** |
| `{type: string, format: byte, description: D, x-vendor: V}` | **all kept** |
| `{type: [string, integer], description: D, x-vendor: V}` | **all kept** |
| `{type: string, description: D, x-vendor: V}` | all dropped |
| `{type: [string, "null"], description: D, x-vendor: V}` | all dropped |
| `{description: D, x-vendor: V}` (untyped → any) | all dropped |

Same position, same keywords, opposite outcome — the discriminator is solely whether the body
interned a node.

## 4. Findings beyond the reported annotation gap

1. **Constraint loss is structural, not cosmetic.** `items: {type: string, maxLength: 21}` emits
   `elem: {target: "t/prim/string"}` — `maxLength` is gone. Same at `additionalProperties`,
   `prefixItems`, `patternProperties`, header schemas, and both media-type schemas. An emitter
   generates a validator that does not validate. This is a correctness defect, not a docs defect,
   and it is the strongest argument for prioritising the issue.

2. **`$ref` siblings at an unowned inline position.** `items: {$ref: S, maxLength: 25,
   description: D}` lowers `elem` straight to `S`; every sibling is discarded. The sibling
   preservation added in #114 covers the *property* position only.

3. **Headers never receive property detail.** `lowerHeaders` (`content.go:175`) builds an
   `ir.Property` inline and never calls `fillPropertyDetail`. A header schema therefore loses
   docs, `x-*`, `xml`, `not` *and* constraints, even though the identical shape written at a model
   property keeps all five and `ir.Property` already has fields for them. Cheapest fix in the set.

4. **`propertyNames` is unimplemented.** It appears only in `cycles.go:73`'s traversal key set.
   Nothing lowers it and nothing preserves it, so `propertyNames: {type: string, pattern: "^[a-z]+$"}`
   vanishes entirely with no diagnostic. Distinct defect; deserves its own issue.

5. **Pre-existing double-storage.** When a property's schema *does* own a node,
   `description` and `x-*` are written **twice** — once on the type node, once on `ir.Property`.
   `fillPropertyExamples` and `fillPropertyValidationOnly` guard with `ownsNode`;
   `Docs.Description`, `XML`, and extensions do not. Any fix that makes more pointers owned
   multiplies this, so it must be settled as part of the same change.

## 5. Recommended fix shape

**Hoist. Do not add per-position fallback homes.**

The fallback option needs a home that does not exist on `ir.List` (element annotations),
`ir.AdditionalProps`, `ir.PatternProps`, `ir.Tuple` (per-element), `ir.Content`, and the header
`ir.Property` path — six-plus new IR fields, each a new place annotations can live, each needing
emitter support. It contradicts the "one structural home per declaration" rule that `TypeCommon`
exists to enforce, and it would change the IR surface, which invariant 9 forbids as compilers land.

Hoisting reuses a mechanism already implemented twice. Concrete shape: lift the alias fallback out
of `lowerComponentSchema` and `hoistSubSchema` into `schemaBody` — the universal entry point that
already calls `attachDeclaredAnnotations`. After `lowerSchemaBody`, if the pointer is still
unowned **and** the schema declares anything worth keeping (any annotation, any constraint, any
validation-only keyword, any `$ref` sibling), `internAlias` at the pointer, then attach. Ownership
then follows declaration content rather than lowering destination — which is the invariant the
#114 commit message already claimed.

Cost:

- **Golden churn, bounded.** Gating on "declares something" keeps bare `items: {type: string}`
  resolving to the shared `t/prim/string`, so most goldens do not move. Only annotated inline
  scalars gain a node; the conformance corpus needs regeneration for those cases
  (`constraints`, `docs-summary-desc`, `deprecation`, and a handful more).
- **Type-graph change is real.** One new anonymous `Scalar` per annotated inline scalar position.
  Emitters switching on `Primitive` at element positions now see `Scalar{Base: prim}` — already
  what a named scalar component produces today, so it is a supported shape, not a new one.
- **Fix the double-storage in the same change** (finding 4.5), or the duplication spreads.
- **Not covered by this fix:** headers (needs a `fillPropertyDetail` call) and `propertyNames`
  (needs lowering or preservation from scratch).

## 6. Severity ranking

Ranked by (real-world frequency × what is lost). Constraint loss weighs heavier than docs loss.

1. **Media-type schemas, request and response** — every operation has them; an inline
   `schema: {type: string, description: …}` on a body is ordinary; loses constraints too.
2. **`items`** — one of the most common shapes in real specs
   (`items: {type: string, description: "ISO-4217 code", maxLength: 3}`); loses both.
3. **Parameter schemas** — very common, but constraints survive, so the loss is docs/`x-*`/`xml`
   only.
4. **Header schemas** — less common than parameters, but loses constraints as well, and loses them
   despite building an `ir.Property` that already has the fields. Most obviously fixable.
5. **`additionalProperties`** — common in map-shaped payloads; a documented value schema is normal.
6. **`allOf` branch member** — inline branches are common, annotated ones less so (most branches
   are `$ref`s, which are unaffected).
7. **`oneOf`/`anyOf` branch member** — same argument, and inline union branches are usually objects,
   which own nodes and are therefore safe.
8. **`patternProperties` value** — rare keyword.
9. **`prefixItems[n]`** — rare in OpenAPI; total loss where used, but the base rate is small.
10. **`propertyNames`** — low frequency, but a *total* loss rather than an annotation loss.
    Track separately.
