### 3.2 TypePlan — per-type projections computed once

```go
type TypePlan struct {
    Flattened  []ir.PropID          // ir.FlattenedProperties over Base+Implements+Mixins+own: the
                                    // declaration each member takes, in layout order — COMPUTED
    Required   []ir.PropID          // the flattened properties an instance must carry: each one
                                    // a declaration marks Required or a Requires names
    Undeclared []string             // the names Requires lists that no flattened property declares,
                                    // in order: members only the catch-all governs
    Unflattened []ir.TypeID         // the composition entries naming no model, which Flattened
                                    // cannot hold: each lowered beside the properties, or degraded
    Union      UnionShape           // a union's wire shape, one of four (§4.4); "" = not a union
    Discrim    *DiscrimFacts        // a union's or hierarchy's discriminator, as variant IDs
    Open       bool                 // a union or polymorphic base keeps a value no variant
                                    // recognizes rather than refusing it (§3.5, §4.4)
    Scalar     *ScalarChain         // a scalar's Base chain, walked once; nil = not a scalar
    Shapes     map[ir.Lifecycle]ModelShape // the wire shapes the document exercises (§3.3)
    Alias      ir.TypeID            // the type every reference to this one is emitted as, this
                                    // one emitting nothing: a decorator's end (§4.15), or a dedup
                                    // alias's target once that pass (architecture.md §2.2) and its
                                    // record (ir-design §16, question 2) exist; "" = itself
    Reach      []ir.GroupID         // the top-level operation groups whose operations reach this
                                    // type, sorted; empty = none does (§4.17)
    Inferred   map[string]ir.Provenance
}

// UnionShape is the wire shape a union's serializer reads, from Exclusive, WireTagged and
// Discriminator together (ir-design §4.4's tag-mode conventions).
type UnionShape string              // "one_of" | "any_of" | "discriminated" | "key_tagged"

type ScalarChain struct {           // a Scalar's Base chain walked to its root (§4.9)
    Root        ir.TypeID           // the chain's first node that is not a Scalar: a primitive, or
                                    // what an alias stands over (ir-design §4.2); "" = opaque
    Constraints *ir.Constraints     // merged along the chain, decimal throughout (§4.11)
    Encoding    *ir.Encoding        // the encoding declared nearest this scalar; nil = the root's
}

type DiscrimFacts struct {          // Mapping/Default resolved to TypeIDs, never names (INV3)
    Locator  DiscrimLocator         // property | property_name | index
    WireKey  string                 // wire name of the tag property ("" when Locator == index)
    Index    *int                   // positional tag element (Erlang tagged tuples); nil otherwise
    Mapping  map[string]ir.TypeID   // wire tag value → variant ID
    Default  ir.TypeID              // variant for absent/unrecognized tag; "" = none
    Envelope string                 // "" = inline tag | "object" = {kind,value} wrapper
    EnvelopeValueName string        // wire name of the envelope value property (Envelope=="object")
}
type DiscrimLocator string          // "property" | "property_name" | "index"
```

Note what `TypePlan` does *not* do: it does not choose Go's sealed interface vs a native union, does
not case anything, does not resolve reserved words, and does not copy a fact the IR states outright
(§3.6). `Flattened` is the *computed* flattening; the IR keeps `Base`, `Implements` and `Mixins`
distinct beside it, so a class-based target can map `Base→extends` / `Implements→interfaces` /
`Mixins→inline` where the three first targets flatten (§8.8). The plan does not decide which — that
is per-language (§4).

`Flattened`, `Required`, `Undeclared` and `Unflattened` are one `ir.FlattenedProperties` walk, by
the rules ir-design §4.3 states: its order, which declaration of a redeclared member is taken, and
when a member is required. A flattened property is required where any of its declarations says so,
an overridden one included, or where this model or one it composes lists its wire name in
`Requires`, as `allOf: [{$ref: User}, {required: [id]}]` makes `User`'s `id` required on the
composed model. A name `Requires` lists that no flattened property declares is
`Undeclared`: a member only the catch-all governs, whose value the catch-all types —
`AdditionalProps.Value`, or the top type where the model has no catch-all or scopes values by
`Patterns`, since which pattern governs a name is ECMA-262's to say and no target's matcher is held
to it. A conjunct that is no model, as `Tags` is in `allOf: [{$ref: Pet}, {$ref: Tags}]` when
`Tags` is a list, is in `Unflattened` as the entry names it: the struct over `Flattened` does not
hold what it admits, so each refiner lowers it or degrades with a code of its own.

### 3.3 ModelShape — visibility projection

One logical model produces N wire shapes across the lifecycle. `ModelShape(model, lifecycle)` is a
**computed traversal in the plan layer**; the IR stores only the single logical model plus visibility
facts and never the projected variants (INV2; `ir-design.md §5.2`). The plan emits one `ModelShape`
per lifecycle the document actually exercises, in the type's `TypePlan.Shapes`, so a refiner knows
exactly which shapes to build: those keys are the projections the type needs, and nothing else
lists them.

```go
type ModelShape struct {
    Props   []ir.PropID  // properties visible in this lifecycle
    Patch   bool         // PATCH shape: properties are implicitly optional unless the binding disables it
    Inferred map[string]ir.Provenance
}
```

`Props` are drawn from `TypePlan.Flattened` (§3.2), so a composed model's shapes hold what it
inherits and mixes in, each member visible as the declaration it takes says (ir-design §4.3).
`readOnly → Only:[read, delete, query]`; `writeOnly → Only:[create, update]` (ir-design §5.2);
`Visibility.None` → excluded from every projection (distinct from the zero value); PATCH flips
properties optional unless `HTTPBinding.PatchImplicitOptionality` points at `false` (nil is the
protocol default, which flips them), and a patch shape never carries a property's default onto the
wire (§4.10); operation-level `ParameterVisibility` /
`ReturnTypeVisibility` override which filter applies. Unknown visibility classes are opaque
filters evaluated per class (`ir-design.md §5.2`).
