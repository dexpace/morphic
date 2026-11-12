# Models

## Visibility projections → N wire shapes

`ProjectVisibilityShapes` consumes the precomputed `ModelShape` list and emits the concrete
create/read/update/patch declarations the target needs — Speakeasy's "public type vs wire
(de)serialize shape" split made explicit — for every type a shape reaches, so a union or a container
holding a projected model is projected with it, as a model holding one is
([TypePlan](../plan/types.md#a-shape-reaches-what-it-holds)). `None` properties are excluded from
every projection; PATCH shapes make properties optional unless the binding disables it. A model used
as both input and output (`Facts.Usage` = Input|Output, `InputOnly` distinct identity for GraphQL
inputs) may emit two shapes. How each shape's declaration is named, beyond `User`, `UserCreate` and
`UserPatch`, is not stated yet (#1405).

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
`Nullable && Inferred == ""` instead, which is the declared bit
([type-graph](../../ir/type-graph.md#type-references)).

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

A union in `TypePlan.Unflattened`, an entry along the model's composition, has no members to flatten
in: each instance of the model is an instance of one of the union's variants, with the rest of the
composition beside it ([models](../../ir/models.md#a-union-a-composition-names)). So each of the
three targets lowers the model as the union it narrows, distributing the composition over the
union's variants as [models](../../ir/models.md#composition-beside-a-union) distributes a union
written beside one: a variant for each of the union's, the model with that variant's type in the
union's place, flattened and lowered as any model is, and by this rule where that composes a union
in turn. A target with intersection types may write the composition as it stands instead, so the
distribution is a shared refine helper rather than a plan fact ([plan or
refine](../plan/plan-or-refine.md)), run by `LowerModels` before the steps projecting shapes and
lowering unions and discriminators reach the variants ([steps](steps.md)). The model takes the
union's wire shape, a discriminator on the union routing each tag, and its default, to the variant
composed over the one it names. In each shape it is inclusive and strict where the union is, since
two variants the union keeps apart stay apart once composed, and it is open as `TypePlan.Open` says
of the model ([policy](../plan/policy.md#a-union-a-client-reads-is-open)). Every path to a composed
variant passes through the model, which a target declares as a union, so it breaks a cycle through
the variant as a union does ([recursive references](#recursive-references)). A composed variant has
no `TypeID`, so `Ctx.Syms` keys it by the model's and its variant's, which name it from one
generation to the next ([naming](naming.md#collisions-and-the-names-a-generation-shipped)), and it
goes to the model's output unit ([output units](output-units.md)). A model adding nothing to its
union is that union, which each target's lowering table aliases ([Go](../targets/go/lowering.md),
[Kotlin](../targets/kotlin/lowering.md)). A model with two unions in `Unflattened`, one whose union
has a variant composing the model in turn, one over a key-tagged or enveloped union, whose variants
are not the values it admits, and one over a union tagged by a tuple index, whose values are no
objects, are each a diagnosed degrade. T-46 holds the rule.

## Unknown members

`Additional` decides for its own model alone, whatever decodes around it: a closed model nested in
an open one stays strict, and an open one nested in a closed one stays open, under a strict caller
too. Each target says how ([Go](../targets/go/models.md#user-in-go),
[Kotlin](../targets/kotlin/models.md#unknown-members)).

## Recursive references

`Facts.Recursive` says whether a value of a type can hold, at any depth, a value of its own shape,
and `Facts.Component` which types share that cycle ([derived-facts](../../ir/derived-facts.md)); the
engine derives both once per document ([who computes the
plan](../contract/emitter.md#who-computes-the-plan)). What a target must do about a cycle is its
own, since what breaks one is a property of its type system ([plan or
refine](../plan/plan-or-refine.md)). Each says what it does
([Go](../targets/go/models.md#recursive-references),
[Kotlin](../targets/kotlin/models.md#recursive-references)). A cycle a target must break lies within
one of `Facts.Component`'s components, and a shared refine helper finds the cycles a target's
by-value references close within each. T-20 holds the rule.
