### 4.6 Visibility projections → N wire shapes

`ProjectVisibilityShapes` consumes the precomputed `ModelShape` map and emits the concrete
create/read/update/patch structs the target needs — Speakeasy's "public type vs wire (de)serialize
shape" split made explicit. `None` properties are excluded from every projection; PATCH shapes make
properties optional unless the binding disables it. A model used as both input and output
(`Facts.Usage` = Input|Output, `InputOnly` distinct identity for GraphQL inputs) may emit two
shapes.

### 4.7 Optionality × Nullability × Presence (INV8)

`LowerOptionality` renders the four states — `Property.Required` (wire presence) ⊥
`TypeRef.Nullable` (admits null) — plus protobuf `Property.Presence` (implicit/explicit/required) as
a *third* axis that is not nullability (protobuf has no null). A model's member is present where
`TypePlan.Required` lists it (§3.2), which counts every declaration of it and every `Requires`, not
only the `Property.Required` of the declaration the flattening takes. The reference Go lowering is
ogen's generic-box scheme (`gen/ir/generics.go`), which independently arrives at exactly these four
states: required-nonnull → bare `T`; optional → `Opt[T]` (a `{value T; set bool}` box, so presence
is distinct from the zero value); nullable → `Nil[T]`; both → `OptNil[T]`. A mature real-world
generator needing all four first-class boxes is the strongest external confirmation of INV8, and —
decisively — ogen makes this decision in its *emitter* (`boxType`), keeping `Required` and
`Nullable` orthogonal in the schema model right up to that point. That is Morphic's split precisely:
orthogonal bits in the IR, boxed in refine. Two Go-specific optimizations to inherit: reference
types (slice/map/pointer) can encode one of the four states in their own `nil` (ogen's
`NilSemantic: invalid | optional | null`), saving a wrapper; `explicit` protobuf presence →
pointer/hazzer, `implicit` → `omitzero,omitempty`, so a zero is not serialized. `ClientOptional`
relaxes the client-side type even when `Required=true`; `DefaultAdded` may be suppressed for
back-compat. Where Go's zero value would force a *deliberate* collapse of two states, a `Diagnostic`
is emitted — never a silent conflation (§13 test T-3).

A nullable reference whose `TypeRef.Inferred` names a heuristic renders as nullable like any
other: the compiler's policy already chose to read null in, and the marker is there so that choice
stays auditable (INV6). A configuration that wants only what the source declares reads
`Nullable && Inferred == ""` instead, which is the declared bit (ir-design §3.3).

Presence is stated in `encoding/json/v2` terms, and `omitempty` never keeps a presence state. v2
defines `omitempty` by the encoded JSON — a field is dropped when it would encode as `null`, `""`,
`{}` or `[]` — so it keeps a zero number or `false` and drops a pointer to an empty struct: a set
but empty value turns into an absent one, the silent conflation just forbidden. `omitzero` is
defined by the Go value and asks the type's `IsZero` method when it has one. The boxes implement
`IsZero` as "not set" beside their own `MarshalJSONTo`/`UnmarshalJSONFrom`, so a box field tagged
`omitzero` is omitted exactly when unset, and an `Opt[int]` set to `0` is written. The `implicit`
tag carries both options because each alone misses a zero: `omitempty` keeps `0` and `false`, and
`omitzero` keeps an empty non-nil slice or map.

### 4.16 Recursive references

`Facts.Recursive` says whether a value of a type can hold, at any depth, a value of its own shape,
and `Facts.Component` which types share that cycle (ir-design §4.9); the engine derives both once
per document (§2.2). What a target must do about a cycle is its own, since what breaks one is a
property of its type system (§3.6).

- **Go** holds a struct field by value, so a cycle of struct fields is an invalid recursive type. A
  slice, a map, a pointer and an interface each break a cycle, so a list, a map, a union and a
  polymorphic base — the last two a struct holding a sealed interface (§4.4) — already do; a model,
  a tuple's positional struct and the boxes of §4.7 hold their contents by value, and do not. So a
  reference is held through a pointer when it lies on a cycle of references Go holds by value, read
  after `Base` and `Mixins` are flattened into the declarations they land in. Such a cycle lies
  within one of `Facts.Component`'s components, and a shared refine helper finds the cycles a
  target's by-value references close within each. The pointer stands for a box's state where it can
  (§4.7): a required reference is `*T`, which `Validate` reports when nil; an optional or a nullable
  one is `*T`, nil meaning absent or null; and an optional nullable one is `Opt[*T]`.
- **Kotlin** holds every class by reference, so a cycle asks nothing of either Kotlin target.

Go breaks every reference on such a cycle rather than the fewest that would do. The fewest depend
on the whole graph — one type added elsewhere can change which reference is picked — and each pick
that moves changes a field's type for every caller. Nor does it break every by-value reference whose
two ends merely share a component: a cycle that passes through a list, a map or a union already
compiles, and a pointer on it would ask every caller to check for nil where nothing requires it.
A reference is a pointer exactly when a by-value cycle passes through it, which only a change to
that cycle moves. T-20 holds the rule.
