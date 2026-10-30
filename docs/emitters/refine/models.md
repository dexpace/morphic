# Models

## Visibility projections → N wire shapes

`ProjectVisibilityShapes` consumes the precomputed `ModelShape` map and emits the concrete
create/read/update/patch declarations the target needs — Speakeasy's "public type vs wire
(de)serialize shape" split made explicit. `None` properties are excluded from every projection;
PATCH shapes make properties optional unless the binding disables it. A model used as both input
and output (`Facts.Usage` = Input|Output, `InputOnly` distinct identity for GraphQL inputs) may emit
two shapes.

## Optionality × Nullability × Presence (INV8)

`LowerOptionality` renders the four states — `Property.Required` (wire presence) ⊥
`TypeRef.Nullable` (admits null) — plus protobuf `Property.Presence` (implicit/explicit/required) as
a *third* axis that is not nullability (protobuf has no null). A model's member is present where
`TypePlan.Required` lists it ([TypePlan](../plan/types.md)), which counts every declaration of it
and every `Requires`, not only the `Property.Required` of the declaration the flattening takes. Each
target's forms are its own ([Go](../targets/go/models.md#optionality),
[Kotlin](../targets/kotlin/models.md#optionality)).

`ClientOptional` relaxes the client-side type even when `Required=true`; `DefaultAdded` may be
suppressed for back-compat. Where a target's representation would force a *deliberate* collapse of
two states, a `Diagnostic` is emitted — never a silent conflation
([T-3](../testing/mandatory-tests.md)).

A nullable reference whose `TypeRef.Inferred` names a heuristic renders as nullable like any
other: the compiler's policy already chose to read null in, and the marker is there so that choice
stays auditable (INV6). A configuration that wants only what the source declares reads
`Nullable && Inferred == ""` instead, which is the declared bit (ir-design §3.3).

A merge-patch body (`application/merge-patch+json`) is where all three of an optional field's states
change the request: absent leaves the member as it is and `null` removes it. Every target sends both
through the presence box (`Optional<T?>` on Kotlin, `OptNil[T]` on Go), never through a nullable
alone.

## Inheritance

Go has only embedding. Kotlin's single class inheritance cannot hold several `Mixins`, and extending
a base would make it an open class the SDK must keep compatible for every subclass. So on all three
targets a model's `Base` and `Mixins` are flattened into it (`TypePlan.Flattened`), which holds the
members its `Implements` declare as well, since a type conforming to an interface carries them.
Subtyping survives as interfaces: `Implements` becomes an interface, and a discriminated or abstract
base a sealed interface of its subtypes, which Go holds in a struct ([Go
unions](../targets/go/unions.md)).

## Recursive references

`Facts.Recursive` says whether a value of a type can hold, at any depth, a value of its own shape,
and `Facts.Component` which types share that cycle (ir-design §4.9); the engine derives both once
per document ([who computes the plan](../contract/emitter.md#who-computes-the-plan)). What a target
must do about a cycle is its own, since what breaks one is a property of its type system ([plan or
refine](../plan/plan-or-refine.md)). Each says what it does
([Go](../targets/go/models.md#recursive-references),
[Kotlin](../targets/kotlin/models.md#recursive-references)). A cycle a target must break lies within
one of `Facts.Component`'s components, and a shared refine helper finds the cycles a target's
by-value references close within each. T-20 holds the rule.
